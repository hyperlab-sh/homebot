package pack

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

var long = time.Date(2020, 5, 17, 3, 4, 5, 123_000_000, time.UTC)

// tree builds a folder with every kind of entry copyTree handles and returns
// its path.
func tree(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "src")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(dir, "sub", "deep"), 0o755))
	must(os.Mkdir(filepath.Join(dir, "empty"), 0o750))
	must(os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644))
	must(os.WriteFile(filepath.Join(dir, "sub", "run.sh"), []byte("#!/bin/sh\n"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "sub", "deep", "key"), []byte("k"), 0o600))
	must(os.WriteFile(filepath.Join(dir, "ro"), []byte("r"), 0o400))
	must(os.Symlink("a.txt", filepath.Join(dir, "rel-link")))
	must(os.Symlink("/nowhere/at/all", filepath.Join(dir, "dangling")))
	must(os.Symlink("sub", filepath.Join(dir, "dir-link")))
	// Deepest first, so a folder's time is set after what is inside it.
	for _, p := range []string{"sub/deep/key", "sub/run.sh", "a.txt", "ro", "sub/deep", "sub", "empty"} {
		must(os.Chtimes(filepath.Join(dir, p), long, long))
	}
	return dir
}

func TestCopyTreeKeepsSymlinksVerbatimAndTimesAndModes(t *testing.T) {
	src := tree(t)
	dst := filepath.Join(t.TempDir(), "dst")
	if err := copyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	for link, want := range map[string]string{"rel-link": "a.txt", "dangling": "/nowhere/at/all", "dir-link": "sub"} {
		got, err := os.Readlink(filepath.Join(dst, link))
		if err != nil || got != want {
			t.Errorf("%s -> %q, %v; want %q", link, got, err, want)
		}
	}
	for _, p := range []string{".", "a.txt", "ro", "empty", "sub", "sub/run.sh", "sub/deep", "sub/deep/key"} {
		from, err := os.Lstat(filepath.Join(src, p))
		if err != nil {
			t.Fatal(err)
		}
		to, err := os.Lstat(filepath.Join(dst, p))
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		if to.Mode() != from.Mode() {
			t.Errorf("%s: mode %v, want %v", p, to.Mode(), from.Mode())
		}
		if p != "." && !to.ModTime().Equal(from.ModTime()) {
			t.Errorf("%s: mtime %v, want %v", p, to.ModTime(), from.ModTime())
		}
	}
	if data, _ := os.ReadFile(filepath.Join(dst, "sub", "deep", "key")); string(data) != "k" {
		t.Errorf("key = %q", data)
	}
}

func TestCopyTreeCopiesASingleFileAndASymlink(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	// The umask may have narrowed it; the copy must match whatever it is.
	src, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, long, long); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(file, filepath.Join(dir, "g")); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, "g"))
	if err != nil || st.Mode().Perm() != src.Mode().Perm() || !st.ModTime().Equal(long) {
		t.Errorf("copy = %v, %v", st, err)
	}
	if err := os.Symlink("f", filepath.Join(dir, "l")); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(filepath.Join(dir, "l"), filepath.Join(dir, "m")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.Readlink(filepath.Join(dir, "m")); got != "f" {
		t.Errorf("m -> %q", got)
	}
}

func TestCopyTreeRefusesWhatIsNotAFileFolderOrSymlink(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip("no fifos here:", err)
	}
	if err := copyTree(fifo, filepath.Join(dir, "copy")); err == nil {
		t.Error("a fifo was copied")
	}
}

func TestMoveRenames(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	to := filepath.Join(dir, "a", "b", "f")
	if err := move(filepath.Join(dir, "f"), to, os.Rename); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "f")); err == nil {
		t.Error("the source is still there")
	}
	if data, _ := os.ReadFile(to); string(data) != "x" {
		t.Errorf("moved = %q", data)
	}
}

func TestMoveAcrossDevicesCopiesTheTreeThenRemovesTheSource(t *testing.T) {
	src := tree(t)
	want := filepath.Join(t.TempDir(), "dst", "nested", "tree")
	if err := move(src, want, exdev); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(src); err == nil {
		t.Error("the source is still there")
	}
	if got, _ := os.Readlink(filepath.Join(want, "dangling")); got != "/nowhere/at/all" {
		t.Errorf("dangling -> %q", got)
	}
	st, err := os.Stat(filepath.Join(want, "sub", "deep", "key"))
	if err != nil || !st.ModTime().Equal(long) {
		t.Errorf("key = %v, %v", st, err)
	}
}

func TestMoveRefusesToReplaceAnythingAtTheDestination(t *testing.T) {
	dir := t.TempDir()
	src, to := filepath.Join(dir, "src"), filepath.Join(dir, "to")
	for p, content := range map[string]string{src: "new", to: "old"} {
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := move(src, to, os.Rename); !errors.Is(err, os.ErrExist) {
		t.Errorf("err = %v, want ErrExist", err)
	}
	// A dangling symlink is something too.
	if err := os.Remove(to); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nowhere", to); err != nil {
		t.Fatal(err)
	}
	if err := move(src, to, os.Rename); !errors.Is(err, os.ErrExist) {
		t.Errorf("dangling: err = %v, want ErrExist", err)
	}
	if data, _ := os.ReadFile(src); string(data) != "new" {
		t.Errorf("src = %q", data)
	}
}

func TestMoveReportsRenameErrorsOtherThanADeviceChangeWithoutCopying(t *testing.T) {
	boom := errors.New("boom")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := move(filepath.Join(dir, "f"), filepath.Join(dir, "g"), func(string, string) error { return boom }); !errors.Is(err, boom) {
		t.Errorf("err = %v, want boom", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "g")); err == nil {
		t.Error("a copy was made")
	}
}

func TestMoveAcrossDevicesLeavesTheSourceAndNoPartialCopyWhenTheCopyFails(t *testing.T) {
	notRoot(t)
	src := tree(t)
	bad := filepath.Join(src, "sub", "unreadable")
	if err := os.WriteFile(bad, []byte("secret"), 0o000); err != nil {
		t.Fatal(err)
	}
	to := filepath.Join(t.TempDir(), "dst")
	if err := move(src, to, exdev); err == nil {
		t.Fatal("move succeeded")
	}
	if _, err := os.Lstat(to); err == nil {
		t.Error("a partial copy is left behind")
	}
	if data, err := os.ReadFile(filepath.Join(src, "a.txt")); err != nil || string(data) != "a" {
		t.Errorf("the source lost a.txt: %q, %v", data, err)
	}
	if _, err := os.Lstat(bad); err != nil {
		t.Errorf("the source lost the unreadable file: %v", err)
	}
}

func TestPackAcrossDevicesLinksTheCopyBack(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{".config/tool/conf": "c", ".config/tool/sub/more": "m"})
	if err := os.Symlink("conf", b.h(".config/tool/alias")); err != nil {
		t.Fatal(err)
	}
	r := b.packWith(exdev, b.h(".config/tool"), Options{})
	if got := b.readlink(b.h(".config/tool")); got != r.To {
		t.Errorf("link = %q, want %q", got, r.To)
	}
	if got := b.read(b.h(".config/tool/sub/more")); got != "m" {
		t.Errorf("more = %q", got)
	}
	if got := b.readlink(r.To + "/alias"); got != "conf" {
		t.Errorf("alias -> %q", got)
	}
}

func TestPackPrivateAcrossDevicesLeavesNoPlaintextInHome(t *testing.T) {
	b := newBox(t, true)
	testutil.TestKeys(t, b.Root)
	b.home(map[string]string{".netrc": "canary-93aa10be"})
	r := b.packWith(exdev, b.h(".netrc"), Options{Private: true})
	if st, err := os.Lstat(b.h(".netrc")); err != nil || st.Mode().IsRegular() {
		t.Errorf("~/.netrc = %v, %v; want a link", st, err)
	}
	if got := filesHolding(t, b.Home, "canary-93aa10be"); len(got) != 1 || got[0] != ".home/private/common/netrc" {
		t.Errorf("files holding the secret = %q", got)
	}
	if !r.Sealed {
		t.Errorf("Result = %+v", r)
	}
}

func TestMoveAcrossDevicesRemovesAPartialCopyEvenWithReadOnlyFolders(t *testing.T) {
	notRoot(t)
	src := tree(t)
	// A read-only folder with a file, copied before the failure.
	ro := filepath.Join(src, "aaa-ro")
	if err := os.MkdirAll(filepath.Join(ro, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ro, "inner", "f"), []byte("f"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(ro, "inner"), 0o555); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fsx.MakeWritable(src) })
	if err := os.WriteFile(filepath.Join(src, "zzz-unreadable"), []byte("x"), 0o000); err != nil {
		t.Fatal(err)
	}
	to := filepath.Join(t.TempDir(), "dst")
	if err := move(src, to, exdev); err == nil {
		t.Fatal("move succeeded")
	}
	if _, err := os.Lstat(to); err == nil {
		t.Error("a partial copy is left behind")
	}
	if _, err := os.Lstat(filepath.Join(ro, "inner", "f")); err != nil {
		t.Errorf("the source lost a file: %v", err)
	}
}

func TestMoveAcrossDevicesCopiesAndRemovesAReadOnlyFolder(t *testing.T) {
	notRoot(t)
	src := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(filepath.Join(src, "ro", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "ro", "inner", "f"), []byte("f"), 0o444); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"ro/inner", "ro"} {
		if err := os.Chmod(filepath.Join(src, d), 0o555); err != nil {
			t.Fatal(err)
		}
	}
	to := filepath.Join(t.TempDir(), "dst")
	t.Cleanup(func() { fsx.MakeWritable(to) })
	if err := move(src, to, exdev); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(src); err == nil {
		t.Error("the source is still there")
	}
	st, err := os.Stat(filepath.Join(to, "ro"))
	if err != nil || st.Mode().Perm() != 0o555 {
		t.Errorf("ro = %v, %v; want 0555", st, err)
	}
	if data, _ := os.ReadFile(filepath.Join(to, "ro", "inner", "f")); string(data) != "f" {
		t.Errorf("f = %q", data)
	}
}

func TestMoveAcrossDevicesTellsWhenTheOriginalCannotBeRemoved(t *testing.T) {
	notRoot(t)
	parent := t.TempDir()
	src := filepath.Join(parent, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("f"), 0o644); err != nil {
		t.Fatal(err)
	}
	// src itself cannot be unlinked from a read-only parent.
	if err := os.Chmod(parent, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })
	to := filepath.Join(t.TempDir(), "dst")
	err := move(src, to, exdev)
	var kept *sourceKeptError
	if !errors.As(err, &kept) {
		t.Fatalf("err = %v, want a *sourceKeptError", err)
	}
	if data, _ := os.ReadFile(filepath.Join(to, "f")); string(data) != "f" {
		t.Errorf("the copy is not whole: %q", data)
	}
}
