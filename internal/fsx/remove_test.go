package fsx

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// strictRemoveAll is os.RemoveAll that first refuses, as the kernel does for
// anyone but root, when a folder in the tree has entries but no write
// permission for its owner. Root ignores the permission, so this keeps the
// tests meaningful when they run as root.
func strictRemoveAll(path string) error {
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		st, err := os.Lstat(p)
		if err != nil {
			return err
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			return err
		}
		if len(entries) > 0 && st.Mode().Perm()&0o200 == 0 {
			return &fs.PathError{Op: "unlinkat", Path: p, Err: fs.ErrPermission}
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return os.RemoveAll(path)
}

// readOnlyTree makes root/ro/inner/f with ro and inner read-only (0o555).
func readOnlyTree(t *testing.T, root string) string {
	t.Helper()
	ro := filepath.Join(root, "ro")
	if err := os.MkdirAll(filepath.Join(ro, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ro, "inner", "f"), []byte("f"), 0o444); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{filepath.Join(ro, "inner"), ro} {
		if err := os.Chmod(d, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { MakeWritable(root) })
	return ro
}

func TestRemoveAllRemovesReadOnlyFolders(t *testing.T) {
	top := filepath.Join(t.TempDir(), "top")
	readOnlyTree(t, top)
	if err := strictRemoveAll(top); err == nil {
		t.Fatal("strictRemoveAll removed a read-only tree: the test cannot tell")
	}
	// strictRemoveAll checks the whole tree before it removes anything.
	if err := removeAllWith(top, strictRemoveAll); err != nil {
		t.Fatalf("removeAllWith: %v", err)
	}
	if _, err := os.Lstat(top); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s is still there (%v)", top, err)
	}
}

// The real call, on the real file system: as anyone but root this is the
// case the retry is for.
func TestRemoveAllRemovesReadOnlyFoldersForReal(t *testing.T) {
	top := filepath.Join(t.TempDir(), "top")
	readOnlyTree(t, top)
	if err := RemoveAll(top); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if _, err := os.Lstat(top); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s is still there (%v)", top, err)
	}
}

func TestRemoveAllOfNothingIsNoError(t *testing.T) {
	if err := RemoveAll(filepath.Join(t.TempDir(), "none")); err != nil {
		t.Fatal(err)
	}
}

// MakeWritable never follows a link: a read-only folder a link in the tree
// points at keeps its permissions, and is not removed.
func TestRemoveAllNeverFollowsALinkToMakeItWritable(t *testing.T) {
	outside := t.TempDir()
	ro := readOnlyTree(t, outside)
	top := filepath.Join(t.TempDir(), "top")
	readOnlyTree(t, top)
	if err := os.Chmod(filepath.Join(top, "ro"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(ro, filepath.Join(top, "ro", "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(top, "ro"), 0o555); err != nil {
		t.Fatal(err)
	}
	if err := removeAllWith(top, strictRemoveAll); err != nil {
		t.Fatalf("removeAllWith: %v", err)
	}
	for _, p := range []string{ro, filepath.Join(ro, "inner")} {
		st, err := os.Lstat(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if st.Mode().Perm() != 0o555 {
			t.Errorf("%s mode = %v, want 0555: the link was followed", p, st.Mode().Perm())
		}
	}
	if _, err := os.Lstat(filepath.Join(ro, "inner", "f")); err != nil {
		t.Errorf("the file behind the link is gone: %v", err)
	}
}
