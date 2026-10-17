package cleanup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/hyperlab-sh/homebot/internal/linker"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// box is a sandbox whose home repo is a git repository, with the helpers
// cleanup-signing.test.ts had.
type box struct {
	*testutil.Box
	t *testing.T
}

func newBox(t *testing.T) *box {
	t.Helper()
	return &box{Box: testutil.Sandbox(t, testutil.Options{Git: true}), t: t}
}

// newLinkedBox is newBox with $HOME behind a symlink, as a home on another
// volume is, and every sandbox on macOS.
func newLinkedBox(t *testing.T) *box {
	t.Helper()
	return &box{Box: testutil.Sandbox(t, testutil.Options{Git: true, BehindLink: true}), t: t}
}

// newRepoLinkedBox is newBox with only the home repo behind a symlink, as
// ~/.home is when it points at another volume while $HOME does not.
func newRepoLinkedBox(t *testing.T) *box {
	t.Helper()
	return &box{Box: testutil.Sandbox(t, testutil.Options{Git: true, RepoBehindLink: true}), t: t}
}

// newOutsideRepoBox is newBox with the home repo at ~/dotfiles and Root a
// link to it from outside $HOME, as HOMEBOT_ROOT=/srv/dotfiles is.
func newOutsideRepoBox(t *testing.T) *box {
	t.Helper()
	return &box{Box: testutil.Sandbox(t, testutil.Options{Git: true, RepoLinkedFromOutside: true}), t: t}
}

// resolved is path with every link resolved.
func (b *box) resolved(path string) string {
	b.t.Helper()
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		b.t.Fatal(err)
	}
	return real
}

// backupTime is when backedUp moves things aside: 2026-10-12T10:00:00Z.
var backupTime = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

func (b *box) h(p string) string { return filepath.Join(b.Home, p) }
func (b *box) r(p string) string { return filepath.Join(b.Root, p) }

// conflict is a file in $HOME with another version in the repo.
type conflict struct{ home, repo, mine string }

// backedUp writes each repo file and the conflicting one in $HOME, then
// moves what is in the way aside and links, like `homebot link --backup` at
// backupTime. It returns the backups made.
func (b *box) backedUp(files ...conflict) []string {
	b.t.Helper()
	for _, f := range files {
		b.write(b.r("autolink/common/"+strings.TrimPrefix(f.home, ".")), f.repo)
		b.write(b.h(f.home), f.mine)
	}
	return b.backupAndLink()
}

// backupAndLink moves conflicts aside at backupTime and links.
func (b *box) backupAndLink() []string {
	b.t.Helper()
	now := b.Ctx.Now
	b.Ctx.Now = func() time.Time { return backupTime }
	moved, err := linker.BackupConflicts(b.Ctx, b.plan())
	b.Ctx.Now = now
	if err != nil {
		b.t.Fatalf("BackupConflicts: %v", err)
	}
	b.link()
	return moved
}

func (b *box) plan() linker.Plan {
	b.t.Helper()
	p, err := linker.PlanLinks(b.Ctx)
	if err != nil {
		b.t.Fatalf("PlanLinks: %v", err)
	}
	return p
}

func (b *box) link() {
	b.t.Helper()
	if err := linker.ApplyLinks(b.Ctx, b.plan(), linker.ApplyOptions{}); err != nil {
		b.t.Fatalf("ApplyLinks: %v", err)
	}
}

func (b *box) find() []Backup {
	b.t.Helper()
	found, err := FindBackups(b.Ctx)
	if err != nil {
		b.t.Fatalf("FindBackups: %v", err)
	}
	return found
}

// only is the one backup there is.
func (b *box) only() Backup {
	b.t.Helper()
	found := b.find()
	if len(found) != 1 {
		b.t.Fatalf("found %d backups, want 1: %+v", len(found), found)
	}
	return found[0]
}

// pinPath leaves only $WORK/bin and links to git and the few tools the stubs
// use on the PATH, so an fzf or lazygit the host has in /usr/bin cannot
// count.
func (b *box) pinPath() {
	b.t.Helper()
	tools := filepath.Join(b.Dir, "tools")
	if err := os.MkdirAll(tools, 0o755); err != nil {
		b.t.Fatal(err)
	}
	for _, name := range []string{"git", "cat", "dirname"} {
		bin, err := proc.LookPath(b.Ctx, name)
		if err != nil {
			b.t.Fatalf("no %s: %v", name, err)
		}
		if err := os.Symlink(bin, filepath.Join(tools, name)); err != nil && !os.IsExist(err) {
			b.t.Fatal(err)
		}
	}
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": b.Bin + string(filepath.ListSeparator) + tools})
}

func (b *box) write(path, content string) {
	b.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		b.t.Fatal(err)
	}
}

func (b *box) read(path string) string {
	b.t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		b.t.Fatalf("read: %v", err)
	}
	return string(data)
}

func (b *box) symlink(dest, path string) {
	b.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.t.Fatal(err)
	}
	if err := os.Symlink(dest, path); err != nil {
		b.t.Fatal(err)
	}
}

// say is t in the neutral voice.
func (b *box) say(t msg.Text) string { return voice.Plain(b.Ctx, t) }

// out is what was printed since the last call, without styles, one entry
// per line.
func (b *box) out() []string {
	s := ansi.Strip(b.Stdout.String())
	b.Stdout.Reset()
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func contains(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Errorf("%q does not contain %q", s, sub)
	}
}

func lacks(t *testing.T, s, sub string) {
	t.Helper()
	if strings.Contains(s, sub) {
		t.Errorf("%q contains %q", s, sub)
	}
}

// lines is 20 lines "line 1".."line 20", with the numbered ones in change
// replaced.
func lines(change map[int]string) string {
	var s strings.Builder
	for i := 1; i <= 20; i++ {
		if l, ok := change[i]; ok {
			s.WriteString(l + "\n")
		} else {
			s.WriteString("line " + strconv(i) + "\n")
		}
	}
	return s.String()
}

func strconv(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return strconv(i/10) + string(rune('0'+i%10))
}
