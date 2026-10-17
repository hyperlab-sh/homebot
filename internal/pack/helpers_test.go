package pack

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// box is a sandbox with the helpers pack-triage.test.ts had.
type box struct {
	*testutil.Box
	t *testing.T
}

func newBox(t *testing.T, git bool) *box {
	t.Helper()
	return &box{Box: testutil.Sandbox(t, testutil.Options{Git: git}), t: t}
}

// h is a path in $HOME.
func (b *box) h(p string) string { return filepath.Join(b.Home, p) }

// r is a path in the home repo.
func (b *box) r(p string) string { return filepath.Join(b.Root, p) }

// home writes files given by paths relative to $HOME.
func (b *box) home(files map[string]string) {
	b.t.Helper()
	in := map[string]string{}
	for k, v := range files {
		in["home/"+k] = v
	}
	b.Files(in)
}

// repo writes files given by paths relative to the home repo.
func (b *box) repo(files map[string]string) {
	b.t.Helper()
	in := map[string]string{}
	for k, v := range files {
		in["home/.home/"+k] = v
	}
	b.Files(in)
}

func (b *box) read(p string) string {
	b.t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		b.t.Fatal(err)
	}
	return string(data)
}

func (b *box) readlink(p string) string {
	b.t.Helper()
	dest, err := os.Readlink(p)
	if err != nil {
		b.t.Fatalf("readlink: %v", err)
	}
	return dest
}

func (b *box) exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func (b *box) isRegular(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.Mode().IsRegular()
}

func (b *box) isDir(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.IsDir()
}

// pack packs path and fails the test on an error.
func (b *box) pack(path string, o Options) Result {
	b.t.Helper()
	r, err := Pack(b.Ctx, path, o)
	if err != nil {
		b.t.Fatalf("Pack(%q): %v", path, err)
	}
	return r
}

// packWith packs path with rename as the move's first try, failing the test
// on an error.
func (b *box) packWith(rename renameFunc, path string, o Options) Result {
	b.t.Helper()
	r, err := packWith(b.Ctx, path, o, rename)
	if err != nil {
		b.t.Fatalf("Pack(%q): %v", path, err)
	}
	return r
}

// wantUserError checks err is a user error matching re.
func wantUserError(t *testing.T, err error, re string) {
	t.Helper()
	var ue *fail.UserError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %v (%T), want a *fail.UserError matching %q", err, err, re)
	}
	if !regexp.MustCompile(re).MatchString(ue.Msg) {
		t.Fatalf("error %q does not match %q", ue.Msg, re)
	}
}

// exdev is a rename that always says the destination is on another device.
func exdev(from, to string) error {
	return &os.LinkError{Op: "rename", Old: from, New: to, Err: syscall.EXDEV}
}

// notRoot skips a test that needs permissions to deny.
func notRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
}

// filesHolding returns the files under dir (links not followed, .git skipped)
// whose content contains needle, relative to dir.
func filesHolding(t *testing.T, dir, needle string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), needle) {
			rel, _ := filepath.Rel(dir, p)
			found = append(found, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}
