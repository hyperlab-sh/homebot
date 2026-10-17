package secrets

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"filippo.io/age"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// UnlockOptions say how Unlock treats conflicts.
type UnlockOptions struct {
	// Theirs overwrites conflicts with the repo's version.
	Theirs bool
}

// UnlockResult is what an unlock did. Paths are relative to private/.
type UnlockResult struct {
	// Unlocked are the secrets decrypted and written.
	Unlocked []string
	// Verified are plaintext this machine had no record of that matched its
	// .age file.
	Verified []string
	// Conflicts are left as they are: changed here and in the repo.
	Conflicts []string
}

// Unlock asks for the passphrase on the terminal, decrypts the private key
// in memory and with it every locked secret and every secret changed in the
// repo. The plaintext is written next to its .age file, readable by its
// owner only. A local edit is never overwritten unless Theirs says so. It
// asks even when there is nothing to decrypt, as the TypeScript did.
func Unlock(ctx *app.Context, opts UnlockOptions) (UnlockResult, error) {
	ids, err := readIdentity(ctx)
	if err != nil {
		return UnlockResult{}, err
	}
	return unlockWith(ctx, ids, opts)
}

// readIdentity decrypts private/identity.age with the passphrase and parses
// the private key in it. The key never leaves memory.
func readIdentity(ctx *app.Context) ([]age.Identity, error) {
	sealed, err := os.ReadFile(ctx.Layout().Identity)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fail.Say(msg.SecretsErrorNoIdentity())
	}
	if err != nil {
		return nil, err
	}
	p := &passphrase{ctx: ctx}
	defer p.close()
	r, err := age.Decrypt(bytes.NewReader(sealed), p)
	if err != nil {
		var userErr *fail.UserError
		var noMatch *age.NoIdentityMatchError
		switch {
		case errors.As(err, &userErr):
			return nil, err
		case p.asked && errors.As(err, &noMatch):
			return nil, fail.Say(msg.SecretsErrorPassphrase())
		}
		// Not sealed with a passphrase, or damaged: a bad header MAC or a
		// missing nonce means the passphrase was right.
		return nil, fail.Say(msg.SecretsErrorIdentityUnreadable())
	}
	// The passphrase opened the header, so a payload that does not decrypt
	// is a damaged file, not a wrong passphrase.
	key, err := io.ReadAll(r)
	defer clear(key)
	if err != nil {
		return nil, fail.Say(msg.SecretsErrorIdentityUnreadable())
	}
	ids, err := age.ParseIdentities(bytes.NewReader(key))
	if err != nil {
		return nil, fail.Say(msg.SecretsErrorIdentityUnreadable())
	}
	return ids, nil
}

// passphrase is the identity that opens private/identity.age. It asks for
// the passphrase only when the file is sealed with one, so a damaged or
// foreign file is reported without a pointless prompt.
type passphrase struct {
	ctx   *app.Context
	term  terminal
	asked bool
}

func (p *passphrase) Unwrap(stanzas []*age.Stanza) ([]byte, error) {
	if len(stanzas) != 1 || stanzas[0].Type != "scrypt" {
		return nil, fmt.Errorf("%w: not sealed with a passphrase", age.ErrIncorrectIdentity)
	}
	t, err := openTerminal(p.ctx)
	if err != nil {
		return nil, err
	}
	p.term = t
	pass, err := t.secret(voice.Plain(p.ctx, msg.UnlockPromptPassphrase()))
	if err != nil {
		return nil, err
	}
	p.asked = true
	id, err := age.NewScryptIdentity(string(pass))
	clear(pass)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", age.ErrIncorrectIdentity, err)
	}
	return id.Unwrap(stanzas)
}

func (p *passphrase) close() {
	if p.term != nil {
		p.term.close()
	}
}

// unlockWith decrypts with ids what Unlock decrypts. When a secret fails,
// what was done before it is still recorded.
func unlockWith(ctx *app.Context, ids []age.Identity, opts UnlockOptions) (UnlockResult, error) {
	list, err := List(ctx)
	if err != nil {
		return UnlockResult{}, err
	}
	records, err := state.LoadSecrets(ctx)
	if err != nil {
		return UnlockResult{}, err
	}
	var res UnlockResult
	var failed error
	for _, s := range list {
		if s.State == Sealed || s.State == New || s.State == Modified {
			continue
		}
		if s.State == Conflict && !opts.Theirs {
			res.Conflicts = append(res.Conflicts, s.Rel)
			continue
		}
		outcome, rec, err := unlockOne(ctx, s.Rel, s.State, ids, opts.Theirs)
		if err != nil {
			failed = failure(msg.SecretsErrorDecrypt(s.Rel), err)
			break
		}
		switch outcome {
		case unlocked:
			res.Unlocked = append(res.Unlocked, s.Rel)
		case verified:
			res.Verified = append(res.Verified, s.Rel)
		case conflicted:
			res.Conflicts = append(res.Conflicts, s.Rel)
			continue
		}
		records.Records.Set(s.Rel, rec)
	}
	return res, errors.Join(failed, records.Save(ctx))
}

type outcome int

const (
	unlocked outcome = iota
	verified
	conflicted
)

// unlockOne decrypts the .age file of the secret rel and, unless the secret
// is unverified and its plaintext differs, writes the plaintext with mode
// 0600 through a temp file beside it. It never writes over the keys or
// another file that is not a secret. It clears the decrypted bytes before
// it returns, as a best effort only: copies made on the way, by age and by
// io.ReadAll growing its buffer, are left to the garbage collector.
func unlockOne(ctx *app.Context, rel string, st State, ids []age.Identity, theirs bool) (outcome, state.SecretRecord, error) {
	l := ctx.Layout()
	plain := filepath.Join(l.Private, filepath.FromSlash(rel))
	if reservedTarget(l, plain) {
		return 0, state.SecretRecord{}, refuseReserved(ctx, plain)
	}
	sealed, err := os.ReadFile(plain + ".age")
	if err != nil {
		return 0, state.SecretRecord{}, err
	}
	r, err := age.Decrypt(bytes.NewReader(sealed), ids...)
	if err != nil {
		return 0, state.SecretRecord{}, ageError{err}
	}
	data, err := io.ReadAll(r)
	defer clear(data)
	if err != nil {
		return 0, state.SecretRecord{}, ageError{err}
	}
	rec := state.SecretRecord{Plain: fsx.Sha256(data), Age: fsx.Sha256(sealed)}
	if st == Unverified {
		current, err := fsx.FileSha256(plain)
		if err != nil {
			return 0, state.SecretRecord{}, err
		}
		if current == rec.Plain {
			return verified, rec, nil
		}
		if !theirs {
			return conflicted, state.SecretRecord{}, nil
		}
	}
	if err := fsx.WriteFileAtomic(plain, data, 0o600); err != nil {
		return 0, state.SecretRecord{}, err
	}
	if err := os.Chmod(plain, 0o600); err != nil {
		return 0, state.SecretRecord{}, err
	}
	return unlocked, rec, nil
}
