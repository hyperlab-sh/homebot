package linker

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/hyperlab-sh/homebot/internal/app"
)

// The repo can be reached by more than one path: ~/.home may be a symlink to
// the folder the repo really is in, and homebot finds the repo through the
// working directory (symlinks resolved) or through ~/.home (as written). A
// link made through one path must count as homebot's when homebot runs
// through the other, so these checks accept a path that is textually right
// or that reaches the same file. New links are still written with the path
// homebot was given.

// pointsInside reports whether path is dir or below it: as written, or
// because path, or one of the folders it is in, is dir reached another way
// (a symlink to it, a bind mount). path need not exist: a link to a file the
// repo no longer has still points inside the repo.
func pointsInside(path, dir string) bool {
	if app.IsInside(path, dir) {
		return true
	}
	want, err := os.Stat(dir)
	if err != nil {
		return false
	}
	for p := path; ; {
		if st, err := os.Stat(p); err == nil && os.SameFile(st, want) {
			return true
		}
		parent := filepath.Dir(p)
		if parent == p {
			return false
		}
		p = parent
	}
}

// sameTarget reports whether a link to dest is a link to want: the same
// path, or the same name in the same folder reached another way. Only the
// folders are compared as files, not dest and want themselves: a repo file
// that is itself a symlink must not make a foreign link to where it points
// count as homebot's.
func sameTarget(dest, want string) bool {
	if dest == want {
		return true
	}
	if filepath.Base(dest) != filepath.Base(want) {
		return false
	}
	a, err := os.Stat(filepath.Dir(dest))
	if err != nil {
		return false
	}
	b, err := os.Stat(filepath.Dir(want))
	if err != nil {
		return false
	}
	return os.SameFile(a, b)
}

// stillLinkTo reports whether target is still a symlink to was, or to the
// same file, as when the plan was made. Anything else there (a real file, a
// folder, a link somewhere else, nothing) means it changed since.
func stillLinkTo(target, was string) bool {
	st, err := os.Lstat(target)
	if err != nil || st.Mode()&fs.ModeSymlink == 0 {
		return false
	}
	dest, err := ReadlinkAbs(target)
	return err == nil && sameTarget(dest, was)
}
