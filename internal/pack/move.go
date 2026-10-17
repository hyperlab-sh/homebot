package pack

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/hyperlab-sh/homebot/internal/fsx"
)

// renameFunc is os.Rename. Pack takes it as an argument so tests can simulate
// another device without any state shared between tests.
type renameFunc func(from, to string) error

// sourceKeptError says a move crossed devices, copied the tree whole, and
// could not remove the original. The copy is in the repo.
type sourceKeptError struct{ err error }

func (e *sourceKeptError) Error() string { return "could not remove the original: " + e.err.Error() }
func (e *sourceKeptError) Unwrap() error { return e.err }

// move puts from at to, creating the folders above to. Across devices it
// copies the tree and removes the original once the copy is whole; a failed
// copy leaves the original alone and removes the partial copy. When only the
// removal fails it returns a *sourceKeptError.
func move(from, to string, rename renameFunc) error {
	if _, err := os.Lstat(to); err == nil {
		return &fs.PathError{Op: "move", Path: to, Err: fs.ErrExist}
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o777); err != nil {
		return err
	}
	err := rename(from, to)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	if err := copyTree(from, to); err != nil {
		return errors.Join(err, fsx.RemoveAll(to))
	}
	if err := fsx.RemoveAll(from); err != nil {
		return &sourceKeptError{err}
	}
	return nil
}

// dirFix is the permissions and time a copied folder gets once the whole
// tree is copied.
type dirFix struct {
	path  string
	mode  fs.FileMode
	mtime time.Time
}

// copyTree copies the file, folder or symlink at from to to. Symlinks are
// copied as links, with the target text they have; files and folders keep
// their permissions and modification times. Folders are owner-only until the
// whole tree is copied, so a failed copy can always be removed.
func copyTree(from, to string) error {
	var fixes []dirFix
	if err := copyInto(from, to, &fixes); err != nil {
		return err
	}
	// Deepest first, so a folder's time is set after what is inside it and
	// a read-only folder is closed after its children.
	for _, f := range fixes {
		if err := os.Chmod(f.path, f.mode); err != nil {
			return err
		}
		if err := os.Chtimes(f.path, f.mtime, f.mtime); err != nil {
			return err
		}
	}
	return nil
}

func copyInto(from, to string, fixes *[]dirFix) error {
	st, err := os.Lstat(from)
	if err != nil {
		return err
	}
	switch mode := st.Mode(); {
	case mode&fs.ModeSymlink != 0:
		dest, err := os.Readlink(from)
		if err != nil {
			return err
		}
		return os.Symlink(dest, to)
	case mode.IsDir():
		if err := os.Mkdir(to, 0o700); err != nil {
			return err
		}
		entries, err := os.ReadDir(from)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyInto(filepath.Join(from, e.Name()), filepath.Join(to, e.Name()), fixes); err != nil {
				return err
			}
		}
		*fixes = append(*fixes, dirFix{path: to, mode: mode.Perm(), mtime: st.ModTime()})
		return nil
	case mode.IsRegular():
		return copyFile(from, to, st)
	}
	return fmt.Errorf("cannot copy %s: not a file, folder or symlink", from)
}

func copyFile(from, to string, st fs.FileInfo) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }() // read only: nothing to lose
	// Created owner-only so a secret is never readable by others while it
	// is being copied; the real permissions are set at the end.
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	// After the close: a flush on close may touch the time.
	if err := os.Chmod(to, st.Mode().Perm()); err != nil {
		return err
	}
	return os.Chtimes(to, st.ModTime(), st.ModTime())
}
