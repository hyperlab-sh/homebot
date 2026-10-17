package secrets

import (
	"bufio"
	"bytes"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
	"filippo.io/age/agessh"
	"golang.org/x/crypto/cryptobyte"
	"golang.org/x/crypto/ssh"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// SealResult is what a seal did.
type SealResult struct {
	// Sealed are the secrets encrypted, as paths relative to private/.
	Sealed []string
}

// Seal encrypts new and changed plaintext in private/ to the public key in
// private/recipients.txt. It needs no passphrase. Secrets that did not change
// are left alone, so their .age files don't churn.
func Seal(ctx *app.Context) (SealResult, error) {
	return seal(ctx, false)
}

// seal is Seal; with ours it also seals conflicts and unverified plaintext,
// keeping this machine's version. When a secret fails, the ones sealed
// before it stay sealed and recorded, and the result says which.
func seal(ctx *app.Context, ours bool) (SealResult, error) {
	list, err := List(ctx)
	if err != nil {
		return SealResult{}, err
	}
	var todo []string
	for _, s := range list {
		if s.State == New || s.State == Modified || (ours && (s.State == Conflict || s.State == Unverified)) {
			todo = append(todo, s.Rel)
		}
	}
	if len(todo) == 0 {
		return SealResult{}, nil
	}
	l := ctx.Layout()
	text, err := os.ReadFile(l.Recipients)
	if errors.Is(err, fs.ErrNotExist) {
		return SealResult{}, fail.Say(msg.SecretsErrorNoRecipients())
	}
	if err != nil {
		return SealResult{}, err
	}
	recipients, err := parseRecipients(text)
	if err != nil {
		return SealResult{}, sealFailed(todo[0], err)
	}
	records, err := state.LoadSecrets(ctx)
	if err != nil {
		return SealResult{}, err
	}
	var res SealResult
	for _, rel := range todo {
		rec, err := sealOne(ctx, rel, recipients)
		if err != nil {
			err = sealFailed(rel, err)
			if len(res.Sealed) > 0 {
				err = errors.Join(err, records.Save(ctx))
			}
			return res, err
		}
		records.Records.Set(rel, rec)
		res.Sealed = append(res.Sealed, rel)
	}
	return res, records.Save(ctx)
}

// sealOne encrypts the plaintext of the secret rel to its .age file, through
// a temp file beside it and a rename, and returns the hashes of what it read
// and wrote. It never writes over the keys or another file that is not a
// secret.
func sealOne(ctx *app.Context, rel string, recipients []age.Recipient) (state.SecretRecord, error) {
	l := ctx.Layout()
	plain := filepath.Join(l.Private, filepath.FromSlash(rel))
	if reservedTarget(l, plain+".age") {
		return state.SecretRecord{}, refuseReserved(ctx, plain+".age")
	}
	data, err := os.ReadFile(plain)
	if err != nil {
		return state.SecretRecord{}, err
	}
	defer clear(data)
	var sealed bytes.Buffer
	if err := encrypt(&sealed, data, recipients...); err != nil {
		return state.SecretRecord{}, ageError{err}
	}
	if err := fsx.WriteFileAtomic(plain+".age", sealed.Bytes(), 0o666); err != nil {
		return state.SecretRecord{}, err
	}
	return state.SecretRecord{Plain: fsx.Sha256(data), Age: fsx.Sha256(sealed.Bytes())}, nil
}

// parseRecipients reads private/recipients.txt as age -R did: one public key
// a line, blank lines and # comments skipped. It takes the keys the age
// library knows (age1..., age1pq1...) and ssh-ed25519 and ssh-rsa keys;
// plugin and tag recipients are refused. A file without a key is the user's
// error, and so is an ssh key age cannot encrypt to: the age CLI skipped it
// with a warning, but homebot never seals to fewer keys than the file lists.
func parseRecipients(text []byte) ([]age.Recipient, error) {
	var sshKeys []age.Recipient
	// rest is the file with the ssh keys commented out, so age's errors keep
	// their line numbers.
	var rest bytes.Buffer
	native := false
	sc := bufio.NewScanner(bytes.NewReader(text))
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if keyType, ok := unsupportedSSHKey(line); ok {
			return nil, fail.Say(msg.SecretsErrorUnsupportedSSHKey(n, keyType))
		}
		switch {
		case strings.HasPrefix(line, "ssh-"):
			r, err := agessh.ParseRecipient(line)
			if err != nil {
				return nil, ageError{fmt.Errorf("error at line %d: %w", n, err)}
			}
			sshKeys = append(sshKeys, r)
			line = "#"
		case line != "" && !strings.HasPrefix(line, "#"):
			native = true
		}
		rest.WriteString(line)
		rest.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		return nil, ageError{err}
	}
	if !native {
		if len(sshKeys) == 0 {
			return nil, fail.Say(msg.SecretsErrorEmptyRecipients())
		}
		return sshKeys, nil
	}
	recipients, err := age.ParseRecipients(&rest)
	if err != nil {
		return nil, ageError{err}
	}
	return append(recipients, sshKeys...), nil
}

// unsupportedSSHKey reports whether line is a well-formed ssh public key
// that age cannot encrypt to, and its type: anything but ssh-ed25519 and
// ssh-rsa (ecdsa-sha2-*, sk-*, ssh-dss), or an ssh-rsa key under 2048
// bits. It is the age CLI's test (cmd/age/parse.go): the type named first
// on the line is the one inside the base64 key that follows it.
func unsupportedSSHKey(line string) (string, bool) {
	fields := strings.Split(line, " ")
	if len(fields) < 2 {
		return "", false
	}
	blob, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil {
		return "", false
	}
	k := cryptobyte.String(blob)
	var typeLen uint32
	var keyType []byte
	if !k.ReadUint32(&typeLen) || !k.ReadBytes(&keyType, int(typeLen)) || string(keyType) != fields[0] {
		return "", false
	}
	switch fields[0] {
	case "ssh-ed25519":
		return "", false
	case "ssh-rsa":
		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			return "", false
		}
		crypto, ok := key.(ssh.CryptoPublicKey)
		if !ok {
			return "", false
		}
		if rsaKey, ok := crypto.CryptoPublicKey().(*rsa.PublicKey); !ok || rsaKey.N.BitLen() >= 2048 {
			return "", false
		}
	}
	return fields[0], true
}

// ageError is an error age itself gave. A failure shows it as age's, as the
// TypeScript showed what the age CLI printed.
type ageError struct{ error }

func (e ageError) Unwrap() error { return e.error }

// failure is the error for a secret that could not be sealed or unlocked:
// summary, then what went wrong, labelled age's only when age said it. An
// A user message retains its context and can be voiced when printed.
func failure(summary msg.Text, err error) error {
	var userErr *fail.UserError
	if errors.As(err, &userErr) {
		return voice.Attach(err, summary, "\n  ")
	}
	tool := ""
	if errors.As(err, new(ageError)) {
		tool = "age"
	}
	return fmt.Errorf("%w%s", fail.Say(summary), fail.Failure("", err.Error(), tool))
}

func sealFailed(rel string, err error) error {
	var userErr *fail.UserError
	if errors.As(err, &userErr) {
		return voice.Attach(err, msg.SecretsErrorEncrypt(rel), "\n  ")
	}
	tool := ""
	if errors.As(err, new(ageError)) {
		tool = "age"
	}
	return fmt.Errorf("%w%s", fail.Say(msg.SecretsErrorEncrypt(rel)), fail.Failure("", err.Error(), tool))
}

// LockOptions say what Lock does besides sealing.
type LockOptions struct {
	// Ours also seals conflicts and unverified plaintext, keeping this
	// machine's version, which is what unlock's conflict hint offers. Wipe
	// ignores it.
	Ours bool
	// Wipe deletes the plaintext of every secret that is safely sealed.
	Wipe bool
}

// LockResult is what a lock did. Paths are relative to private/.
type LockResult struct {
	Sealed []string
	// Wiped is the plaintext deleted, with Wipe.
	Wiped []string
	// Kept is the plaintext Wipe left because it is not safely sealed.
	Kept []string
}

// Lock seals new and changed plaintext. With Wipe it then deletes the
// plaintext of every secret that is safely sealed, and keeps the rest.
func Lock(ctx *app.Context, opts LockOptions) (LockResult, error) {
	if !opts.Wipe {
		s, err := seal(ctx, opts.Ours)
		return LockResult{Sealed: s.Sealed}, err
	}
	s, err := seal(ctx, false)
	if err != nil {
		return LockResult{Sealed: s.Sealed}, err
	}
	res, err := wipe(ctx)
	res.Sealed = s.Sealed
	return res, err
}

// wipe deletes the plaintext of sealed secrets and lists what it kept.
func wipe(ctx *app.Context) (LockResult, error) {
	list, err := List(ctx)
	if err != nil {
		return LockResult{}, err
	}
	dir := ctx.Layout().Private
	var res LockResult
	for _, s := range list {
		switch s.State {
		case Locked:
		case Sealed:
			if err := os.Remove(filepath.Join(dir, filepath.FromSlash(s.Rel))); err != nil {
				return res, err
			}
			res.Wiped = append(res.Wiped, s.Rel)
		default:
			res.Kept = append(res.Kept, s.Rel)
		}
	}
	return res, nil
}
