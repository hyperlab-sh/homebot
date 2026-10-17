package triage

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

type box struct {
	*testutil.Box
	t *testing.T
}

// newBox is a sandbox; git makes the home a repo with one commit holding
// what the sandbox starts with.
func newBox(t *testing.T, git bool) *box {
	t.Helper()
	b := &box{Box: testutil.Sandbox(t, testutil.Options{Git: git}), t: t}
	if git {
		b.commit()
	}
	return b
}

func (b *box) commit() {
	b.t.Helper()
	b.GitIn(b.Root, "add", "-A")
	b.GitIn(b.Root, "commit", "-qm", "init", "--allow-empty")
}

func (b *box) h(rel string) string { return filepath.Join(b.Home, rel) }
func (b *box) r(rel string) string { return filepath.Join(b.Root, rel) }

// home writes files under $HOME.
func (b *box) home(files map[string]string) {
	b.t.Helper()
	m := map[string]string{}
	for rel, content := range files {
		m["home/"+rel] = content
	}
	b.Files(m)
}

// repo writes files under the repo.
func (b *box) repo(files map[string]string) {
	b.t.Helper()
	m := map[string]string{}
	for rel, content := range files {
		m["home/.home/"+rel] = content
	}
	b.Files(m)
}

// watch sets triage.watch.
func (b *box) watch(patterns ...string) {
	cfg := config.Default()
	cfg.Triage.Watch = patterns
	b.Ctx.Config = cfg
}

// at moves the clock.
func (b *box) at(t time.Time) { b.Ctx.Now = func() time.Time { return t } }

func (b *box) candidates() []Candidate {
	b.t.Helper()
	items, err := Candidates(b.Ctx)
	if err != nil {
		b.t.Fatalf("Candidates: %v", err)
	}
	return items
}

// keys are the candidates' keys, of kind when one is given.
func (b *box) keys(kind ...Kind) []string {
	b.t.Helper()
	var out []string
	for _, c := range b.candidates() {
		if len(kind) == 0 || c.Kind == kind[0] {
			out = append(out, c.Key)
		}
	}
	return out
}

func (b *box) read(path string) string {
	b.t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		b.t.Fatal(err)
	}
	return string(data)
}

func (b *box) ignores() []string {
	b.t.Helper()
	list, err := LoadIgnores(b.Ctx)
	if err != nil {
		b.t.Fatalf("LoadIgnores: %v", err)
	}
	return list
}

func (b *box) staged() []string {
	b.t.Helper()
	return strings.Fields(b.GitIn(b.Root, "diff", "--cached", "--name-only"))
}

func (b *box) isLink(path string) bool {
	st, err := os.Lstat(path)
	return err == nil && st.Mode()&os.ModeSymlink != 0
}

func (b *box) isDir(path string) bool {
	st, err := os.Lstat(path)
	return err == nil && st.IsDir()
}

// wantUserError fails unless err holds a *fail.UserError and err's whole
// stored text, Error(), matches pattern. A tool's lines and a "moved" line
// follow the user error in the error that wraps it, so the pattern is matched
// against the wrapper, not the user error's own Msg.
func wantUserError(t *testing.T, err error, pattern string) {
	t.Helper()
	var ue *fail.UserError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %v (%T), want a *fail.UserError", err, err)
	}
	if !regexp.MustCompile(pattern).MatchString(err.Error()) {
		t.Fatalf("err = %q, want it to match %q", err.Error(), pattern)
	}
}

// notRoot skips a test that relies on permissions root ignores.
func notRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
}
