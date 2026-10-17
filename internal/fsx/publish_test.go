package fsx

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
)

func TestPublishFileWritesANewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "key")
	if err := PublishFile(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "new" {
		t.Errorf("content = %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %o, want 600", mode)
	}
	if got := entries(t, filepath.Dir(path)); !slices.Equal(got, []string{"key"}) {
		t.Errorf("entries = %v, want only key", got)
	}
}

// inTheWay are the things already at a path that PublishFile must not
// replace.
var inTheWay = map[string]func(t *testing.T, path string){
	"a file": func(t *testing.T, path string) { write(t, path, "theirs") },
	"a dangling symlink": func(t *testing.T, path string) {
		if err := os.Symlink(filepath.Join(filepath.Dir(path), "nowhere"), path); err != nil {
			t.Fatal(err)
		}
	},
	"a folder": func(t *testing.T, path string) {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	},
}

func TestPublishFileNeverReplaces(t *testing.T) {
	for name, setup := range inTheWay {
		t.Run(name, func(t *testing.T) { wantNotReplaced(t, setup) })
	}
}

// wantNotReplaced puts something at a path with setup and checks that
// PublishFile leaves it and no temp file there.
func wantNotReplaced(t *testing.T, setup func(t *testing.T, path string)) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	setup(t, path)
	before, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	err = PublishFile(path, []byte("ours"), 0o600)
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("err = %v, want fs.ErrExist", err)
	}
	after, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Error("path was replaced")
	}
	if got := entries(t, dir); !slices.Equal(got, []string{"key"}) {
		t.Errorf("entries = %v: a temp file was left", got)
	}
}

// linkFails plays a filesystem whose link(2) fails with errno, and counts
// the calls.
func linkFails(t *testing.T, errno syscall.Errno) *int {
	t.Helper()
	saved := link
	t.Cleanup(func() { link = saved })
	calls := new(int)
	link = func(oldname, newname string) error {
		*calls++
		return &os.LinkError{Op: "link", Old: oldname, New: newname, Err: errno}
	}
	return calls
}

// noLinkErrnos are what link(2) says on a filesystem without hard links.
var noLinkErrnos = []syscall.Errno{syscall.EPERM, syscall.ENOTSUP, syscall.EOPNOTSUPP, syscall.EXDEV, syscall.EMLINK}

func TestPublishFileWithoutHardLinksRenames(t *testing.T) {
	for _, errno := range noLinkErrnos {
		t.Run(errno.Error(), func(t *testing.T) {
			calls := linkFails(t, errno)
			dir := t.TempDir()
			path := filepath.Join(dir, "key")
			if err := PublishFile(path, []byte("new"), 0o600); err != nil {
				t.Fatal(err)
			}
			if *calls != 1 {
				t.Errorf("link called %d times, want 1", *calls)
			}
			if got := read(t, path); got != "new" {
				t.Errorf("content = %q", got)
			}
			info, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if mode := info.Mode(); mode != 0o600 {
				t.Errorf("mode = %v, want a regular file with 600", mode)
			}
			if got := entries(t, dir); !slices.Equal(got, []string{"key"}) {
				t.Errorf("entries = %v, want only key", got)
			}
		})
	}
}

func TestPublishFileWithoutHardLinksNeverReplaces(t *testing.T) {
	for _, errno := range noLinkErrnos {
		for name, setup := range inTheWay {
			t.Run(errno.Error()+"/"+name, func(t *testing.T) {
				linkFails(t, errno)
				wantNotReplaced(t, setup)
			})
		}
	}
}

func TestPublishFileReportsOtherLinkFailures(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.EIO, syscall.EACCES, syscall.ENOSPC, syscall.EROFS} {
		t.Run(errno.Error(), func(t *testing.T) {
			linkFails(t, errno)
			dir := t.TempDir()
			path := filepath.Join(dir, "key")
			err := PublishFile(path, []byte("new"), 0o600)
			if !errors.Is(err, errno) {
				t.Fatalf("err = %v, want %v", err, errno)
			}
			if got := entries(t, dir); len(got) != 0 {
				t.Errorf("entries = %v, want none: no fallback, no temp file", got)
			}
		})
	}
}

func TestIsTempName(t *testing.T) {
	for name, want := range map[string]bool{
		"netrc.tmp-1x2y3z":   true,
		"a.age.tmp-0":        true,
		"a.tmp-zzzzzzz":      true,
		"netrc":              false,
		"netrc.tmp":          false,
		"netrc.tmp-":         false,
		"netrc.tmp-ABC":      false,
		"netrc.tmp-12345678": false,
		"netrc.tmp-1.age":    false,
	} {
		if got := IsTempName(name); got != want {
			t.Errorf("IsTempName(%q) = %v, want %v", name, got, want)
		}
	}
	// The names createTemp makes match.
	dir := t.TempDir()
	for range 50 {
		f, err := createTemp(filepath.Join(dir, "x"), 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		if !IsTempName(filepath.Base(f.Name())) {
			t.Fatalf("IsTempName(%q) = false", f.Name())
		}
	}
}
