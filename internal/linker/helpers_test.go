package linker

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// box is a sandbox with the helpers linker.test.ts had.
type box struct {
	*testutil.Box
	t *testing.T
}

func newBox(t *testing.T) *box {
	t.Helper()
	return &box{Box: testutil.Sandbox(t, testutil.Options{}), t: t}
}

// h is a path in $HOME.
func (b *box) h(p string) string { return filepath.Join(b.Home, p) }

// r is a path in the home repo.
func (b *box) r(p string) string { return filepath.Join(b.Root, p) }

// repo writes files given by paths relative to the home repo.
func (b *box) repo(files map[string]string) {
	b.t.Helper()
	in := map[string]string{}
	for k, v := range files {
		in["home/.home/"+k] = v
	}
	b.Files(in)
}

func (b *box) plan(trees ...Tree) Plan {
	b.t.Helper()
	p, err := PlanLinks(b.Ctx, trees...)
	if err != nil {
		b.t.Fatalf("PlanLinks: %v", err)
	}
	return p
}

func (b *box) apply(p Plan, opts ApplyOptions) {
	b.t.Helper()
	if err := ApplyLinks(b.Ctx, p, opts); err != nil {
		b.t.Fatalf("ApplyLinks: %v", err)
	}
}

// linkAndCheck links everything, then checks a second plan finds nothing to
// do.
func (b *box) linkAndCheck() {
	b.t.Helper()
	b.apply(b.plan(), ApplyOptions{})
	if left := b.plan().Pending(); len(left) > 0 {
		b.t.Fatalf("a second run has work left: %+v", left)
	}
}

// kinds maps each op's target, relative to $HOME, to its kind. A later op for
// the same target wins, as Object.fromEntries did.
func (b *box) kinds(p Plan) map[string]Kind {
	m := map[string]Kind{}
	for _, op := range p {
		m[strings.TrimPrefix(op.Target, b.Home+"/")] = op.Kind
	}
	return m
}

func (b *box) readlink(p string) string {
	b.t.Helper()
	dest, err := os.Readlink(p)
	if err != nil {
		b.t.Fatalf("readlink: %v", err)
	}
	return dest
}

func (b *box) read(p string) string {
	b.t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		b.t.Fatalf("read: %v", err)
	}
	return string(data)
}

func (b *box) recorded() []string {
	b.t.Helper()
	got, err := RecordedLinks(b.Ctx)
	if err != nil {
		b.t.Fatalf("RecordedLinks: %v", err)
	}
	return got
}

func lstat(p string) fs.FileInfo {
	st, err := os.Lstat(p)
	if err != nil {
		return nil
	}
	return st
}

func isLink(p string) bool {
	st := lstat(p)
	return st != nil && st.Mode()&fs.ModeSymlink != 0
}

func isRealDir(p string) bool {
	st := lstat(p)
	return st != nil && st.IsDir()
}

func exists(p string) bool { return lstat(p) != nil }

// reasons returns the reasons of p's invalid ops.
func reasons(p Plan) []string {
	var out []string
	for _, op := range p.Only(Invalid) {
		out = append(out, op.Reason)
	}
	return out
}

// firstReason is the reason of p's first invalid op.
func (b *box) firstReason(p Plan) string {
	b.t.Helper()
	r := reasons(p)
	if len(r) == 0 {
		b.t.Fatalf("no invalid op in %+v", p)
	}
	return r[0]
}

func (b *box) contains(s, sub string) {
	b.t.Helper()
	if !strings.Contains(s, sub) {
		b.t.Errorf("%q does not contain %q", s, sub)
	}
}
