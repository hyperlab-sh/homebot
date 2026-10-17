// Package secrets keeps the home repo's private/ folder. Plaintext secrets
// are sealed into *.age files with the public key in private/recipients.txt,
// and only those are committed. The private key is itself sealed with a
// passphrase in private/identity.age; unlocking asks for the passphrase on
// the terminal and decrypts every secret next to its .age file.
//
// Everything runs in-process on filippo.io/age. The private key lives in
// memory only, and decrypted secrets are written only below private/, which
// is gitignored but for the .age files. A per-machine record of hashes
// (state.Secrets) tells "changed here" apart from "changed in the repo".
package secrets

import (
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/state"
)

// State is where a secret stands on this machine.
type State string

const (
	// Sealed: the plaintext and the .age file are what this machine last saw.
	Sealed State = "sealed"
	// New: plaintext without a .age file; it needs sealing.
	New State = "new"
	// Modified: the plaintext changed here; it needs sealing.
	Modified State = "modified"
	// Locked: a .age file without plaintext; it needs unlocking.
	Locked State = "locked"
	// Upstream: the .age file changed, pulled from elsewhere; it needs unlocking.
	Upstream State = "upstream"
	// Conflict: both changed.
	Conflict State = "conflict"
	// Unverified: both exist, but this machine has no record of them.
	Unverified State = "unverified"
)

// Secret is one secret in private/.
type Secret struct {
	// Rel is the path relative to private/, slash-separated, without .age.
	Rel   string
	State State
}

// meta are the files at the top of private/ that are not secrets and are
// committed as they are.
var meta = []string{"README.md", ".gitignore", ".gitkeep", "recipients.txt", "identity.age"}

// notSecret are the names that are never secrets, at any depth. A file
// named only ".age" would seal a secret with an empty name.
var notSecret = []string{".gitkeep", "README.md", ".gitignore", ".age"}

// reserved reports whether rel, a slash-separated path relative to
// private/, is one of the files there that are not secrets. Case is
// ignored: on a case-insensitive filesystem, macOS's by default,
// private/Identity.age is private/identity.age and readme.md is README.md.
func reserved(rel string) bool {
	base := path.Base(rel)
	return (!strings.Contains(rel, "/") && foldedIn(meta, base)) || foldedIn(notSecret, base)
}

// foldedIn reports whether name is one of names, ignoring case.
func foldedIn(names []string, name string) bool {
	return slices.ContainsFunc(names, func(n string) bool { return strings.EqualFold(n, name) })
}

// reservedTarget reports whether writing target would write over the keys
// or another file of private/ that is not a secret, or outside private/.
// Sealing and unlocking check it before they write. Like reserved, it
// ignores case.
func reservedTarget(l app.Layout, target string) bool {
	if strings.EqualFold(target, l.Identity) || strings.EqualFold(target, l.Recipients) {
		return true
	}
	rel, err := filepath.Rel(l.Private, target)
	if err != nil || !filepath.IsLocal(rel) {
		return true
	}
	return reserved(filepath.ToSlash(rel))
}

// refuseReserved is the error for writing over a file reservedTarget names.
func refuseReserved(ctx *app.Context, target string) error {
	return fail.Say(msg.SecretsErrorReserved(shownPath(ctx, target)))
}

// shownPath is p as messages show it: relative to the repo, slash-separated.
func shownPath(ctx *app.Context, p string) string {
	if rel, err := filepath.Rel(ctx.Root, p); err == nil {
		return filepath.ToSlash(rel)
	}
	return p
}

// List returns every secret in private/, sorted by path, with its state.
func List(ctx *app.Context) ([]Secret, error) {
	dir := ctx.Layout().Private
	files, err := fsx.WalkFiles(dir)
	if err != nil {
		return nil, err
	}
	records, err := state.LoadSecrets(ctx)
	if err != nil {
		return nil, err
	}
	plains := map[string]bool{}
	sealed := map[string]bool{}
	for _, rel := range files {
		name, isSealed := strings.CutSuffix(rel, ".age")
		// A crash can leave a temp file of fsx's behind; it is no secret.
		// Its plaintext and its .age file are skipped alike: a .age file
		// listed without its plaintext would unlock over it.
		if fsx.IsTempName(path.Base(name)) {
			continue
		}
		// A secret is never one of the repo's own files, and neither its
		// plaintext nor its .age file may be one: sealing private/identity
		// would write over private/identity.age.
		if reserved(name) || reserved(name+".age") {
			continue
		}
		if isSealed {
			sealed[name] = true
		} else {
			plains[rel] = true
		}
	}
	all := maps.Clone(plains)
	maps.Copy(all, sealed)
	var out []Secret
	for _, rel := range slices.Sorted(maps.Keys(all)) {
		s := Secret{Rel: rel}
		switch {
		case !sealed[rel]:
			s.State = New
		case !plains[rel]:
			s.State = Locked
		default:
			s.State, err = compare(dir, rel, records)
			if err != nil {
				return nil, err
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// compare returns the state of a secret that has both plaintext and a .age
// file, from the hashes this machine recorded.
func compare(dir, rel string, records *state.Secrets) (State, error) {
	rec, ok := records.Records.Get(rel)
	if !ok {
		return Unverified, nil
	}
	plain := filepath.Join(dir, filepath.FromSlash(rel))
	plainSha, err := fsx.FileSha256(plain)
	if err != nil {
		return "", err
	}
	ageSha, err := fsx.FileSha256(plain + ".age")
	if err != nil {
		return "", err
	}
	plainChanged, ageChanged := rec.Plain != plainSha, rec.Age != ageSha
	switch {
	case plainChanged && ageChanged:
		return Conflict, nil
	case plainChanged:
		return Modified, nil
	case ageChanged:
		return Upstream, nil
	}
	return Sealed, nil
}

// Count is how many secrets are in one state.
type Count struct {
	State State
	N     int
}

// Counts are the states the secrets are in, each once, in the order List
// first meets them; states no secret is in are left out.
type Counts []Count

// Of returns how many secrets are in state s.
func (c Counts) Of(s State) int {
	for _, n := range c {
		if n.State == s {
			return n.N
		}
	}
	return 0
}

// Status counts the secrets in private/ by state.
func Status(ctx *app.Context) (Counts, error) {
	list, err := List(ctx)
	if err != nil {
		return nil, err
	}
	var out Counts
	for _, s := range list {
		i := slices.IndexFunc(out, func(c Count) bool { return c.State == s.State })
		if i < 0 {
			out = append(out, Count{State: s.State})
			i = len(out) - 1
		}
		out[i].N++
	}
	return out, nil
}

// KeysExist reports whether the repo has both private/recipients.txt and
// private/identity.age.
func KeysExist(ctx *app.Context) bool {
	l := ctx.Layout()
	return exists(l.Recipients) && exists(l.Identity)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
