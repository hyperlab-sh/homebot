package selfname

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/proc"
)

func localDir(c *app.Context) string { return filepath.Join(c.Home, ".local") }

func binDir(c *app.Context) string { return filepath.Join(c.Home, ".local", "bin") }

func linkPath(c *app.Context, name string) string { return filepath.Join(binDir(c), name) }

// tempPath is where a re-point makes the new link before it renames it over
// the old one (D4).
func tempPath(c *app.Context, name string) string {
	return filepath.Join(binDir(c), "."+name+".homebot-tmp")
}

// syncedBin is ~/.local or ~/.local/bin when one of them is a symlink: then
// another tool manages the folder, and a link made in it would be synced
// (D3).
func syncedBin(c *app.Context) (string, bool) {
	for _, p := range []string{localDir(c), binDir(c)} {
		if fi, err := os.Lstat(p); err == nil && fi.Mode()&fs.ModeSymlink != 0 {
			return p, true
		}
	}
	return "", false
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// sameFile reports whether a and b resolve to the same file. A symlinked
// HOME, macOS's /var and /private/var, and two spellings of one folder all
// compare equal; a string compare would not.
func sameFile(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	fa, err := os.Stat(a)
	if err != nil {
		return false
	}
	fb, err := os.Stat(b)
	return err == nil && os.SameFile(fa, fb)
}

// onDisk is how dir spells the entry that name finds, and whether name
// finds one. On a case-insensitive volume, zork finds Zork; removing or
// re-pointing zork must then leave Zork alone (D4). An entry the folder
// can't be read for is spelled "" and treated as someone else's.
func onDisk(dir, name string) (string, bool) {
	if !exists(filepath.Join(dir, name)) {
		return "", false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", true
	}
	folded := ""
	for _, e := range entries {
		if e.Name() == name {
			return name, true
		}
		if folded == "" && strings.EqualFold(e.Name(), name) {
			folded = e.Name()
		}
	}
	return folded, true
}

// readTarget is the target of the symlink at path, and its absolute form.
// ok is false when path is no symlink, or its target can't be written to
// the record (a newline).
func readTarget(path string) (raw, abs string, ok bool) {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&fs.ModeSymlink == 0 {
		return "", "", false
	}
	raw, err = os.Readlink(path)
	if err != nil || raw == "" || strings.Contains(raw, "\n") {
		return "", "", false
	}
	return raw, resolve(filepath.Dir(path), raw), true
}

// linkSnapshot lets a transaction detect a replacement made after its first
// ownership check. This is a second check, not an atomic unlink primitive.
func linkSnapshot(path string) (os.FileInfo, string, bool) {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&fs.ModeSymlink == 0 {
		return nil, "", false
	}
	raw, err := os.Readlink(path)
	return fi, raw, err == nil
}

func sameLink(path string, before os.FileInfo, raw string) bool {
	after, nowRaw, ok := linkSnapshot(path)
	return ok && os.SameFile(before, after) && nowRaw == raw
}

func resolve(dir, target string) string {
	if !filepath.IsAbs(target) {
		target = filepath.Join(dir, target)
	}
	return filepath.Clean(target)
}

// pointsAtHomebot reports whether path is a symlink to a homebot binary
// (D4): its target, or the target's own target (one more hop at most), is
// named homebot, or it resolves to the same file as proc.SelfPath. The
// target need not exist: a link of ours whose binary is gone (a dangling
// link) is still ours, so it can be re-pointed or removed.
func pointsAtHomebot(c *app.Context, path string) (raw string, ok bool) {
	raw, abs, ok := readTarget(path)
	if !ok {
		return "", false
	}
	if filepath.Base(abs) == self {
		return raw, true
	}
	if next, err := os.Readlink(abs); err == nil && filepath.Base(resolve(filepath.Dir(abs), next)) == self {
		return raw, true
	}
	return raw, sameFile(path, proc.SelfPath(c))
}

// ours reports whether the link for name is ours (R12): the record says
// homebot made it, it is spelled exactly name on disk, and it points at a
// homebot binary.
func ours(c *app.Context, rec Rec, has bool, name string) bool {
	currentOwned := rec.Name == name && rec.Made && !rec.Pending
	oldOwned := rec.Old == name && rec.OldMade
	if !has || (!currentOwned && !oldOwned) {
		return false
	}
	if spelled, ok := onDisk(binDir(c), name); !ok || spelled != name {
		return false
	}
	_, ok := pointsAtHomebot(c, linkPath(c, name))
	return ok
}

// target is what a new or re-pointed link points at: proc.SelfPath, the
// stable homebot path, written as the relative "homebot" when it is in
// ~/.local/bin itself (the front door's build). A SelfPath that isn't
// named homebot, isn't absolute, doesn't exist, or can't be recorded is
// NoHomebot: a link to it would break on the next rename or upgrade.
func target(c *app.Context) (string, error) {
	p := proc.SelfPath(c)
	if !filepath.IsAbs(p) || filepath.Base(p) != self || strings.Contains(p, "\n") {
		return "", &Error{Reason: NoHomebot, Path: p}
	}
	if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		return "", &Error{Reason: NoHomebot, Path: p}
	}
	if filepath.Dir(p) == binDir(c) || sameFile(filepath.Dir(p), binDir(c)) {
		return self, nil
	}
	return p, nil
}

// removeIfMissing is os.Remove that ignores a file already gone.
func removeIfMissing(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
