package linker

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// ── (..) markers (final review home M1) ──

func TestDotDotBelowTheTopLinksNothingOutsideItsFolder(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{
		"autolink/common/config/(..)/zshrc":         "z",
		"autolink/common/config/(x)/(..)/(..)/evil": "e",
		"autolink/common/config/(x)/ok":             "ok",
	})
	p := b.plan()
	r := reasons(p)
	if len(r) != 2 {
		t.Fatalf("invalid reasons = %q, want two", r)
	}
	for _, reason := range r {
		b.contains(reason, `"(..)" names no folder`)
	}
	b.apply(p, ApplyOptions{})
	for _, p := range []string{b.h("zshrc"), b.h(".zshrc"), b.h("evil"), filepath.Join(b.Dir, "evil"), b.h(".config/evil")} {
		if exists(p) {
			t.Errorf("%s was linked", p)
		}
	}
	if !isLink(b.h(".config/x/ok")) {
		t.Error("the good name next to them was not linked")
	}
}

func TestLandsBelow(t *testing.T) {
	for _, c := range []struct {
		target, dir string
		want        bool
	}{
		{"/h/.config/a", "/h/.config", true},
		{"/h/.config/a/b", "/h/.config", true},
		{"/h/.config", "/h/.config", false},
		{"/h", "/h/.config", false},
		{"/", "/h", false},
		{"/h/.configx", "/h/.config", false},
	} {
		if got := landsBelow(c.target, c.dir); got != c.want {
			t.Errorf("landsBelow(%q, %q) = %v, want %v", c.target, c.dir, got, c.want)
		}
	}
}

// ── one repo, two paths to it (final review home I1) ──

// aliasBox is a sandbox whose home repo really is in $WORK/code/home, with
// ~/.home a symlink to it, as on a machine where the dotfiles live elsewhere.
// b.Ctx reaches the repo through ~/.home (the helper's HOMEBOT_ROOT, or
// homebot run from ~); real reaches it as the working directory resolves it
// (homebot run from inside the repo).
func aliasBox(t *testing.T) (b *box, real *app.Context) {
	t.Helper()
	b = newBox(t)
	dir := filepath.Join(b.Dir, "code", "home")
	must(t, os.MkdirAll(filepath.Dir(dir), 0o755))
	must(t, os.Rename(b.Root, dir))
	must(t, os.Symlink(dir, b.Root))
	real, err := app.New(app.Options{Env: b.Ctx.Env, Cwd: dir, Root: dir, OS: app.Linux, Host: testutil.TestHost})
	must(t, err)
	b.repo(map[string]string{
		"autolink/common/zshrc":                  "z",
		"autolink/common/vim/vimrc":              "set nu",
		"autolink/common/config/(git)/config":    "[user]",
		"private/common/netrc":                   "machine x",
		"private/common/config/(gh)/hosts.yml":   "token",
		"autolink/common/config/(gh)/config.yml": "editor: vim",
	})
	return b, real
}

func planWith(t *testing.T, c *app.Context) Plan {
	t.Helper()
	p, err := PlanLinks(c)
	must(t, err)
	return p
}

var aliasTargets = []string{".config/gh/config.yml", ".config/gh/hosts.yml", ".config/git/config", ".netrc", ".vim", ".zshrc"}

func (b *box) wantAllOK(p Plan) {
	b.t.Helper()
	want := map[string]Kind{}
	for _, t := range aliasTargets {
		want[t] = OK
	}
	b.wantKinds(p, want)
}

func (b *box) wantRecorded() {
	b.t.Helper()
	var want []string
	for _, t := range aliasTargets {
		want = append(want, b.h(t))
	}
	if d := cmp.Diff(want, b.recorded()); d != "" {
		b.t.Errorf("recorded (-want +got):\n%s", d)
	}
}

func TestLinksMadeThroughOnePathToTheRepoAreOursThroughTheOther(t *testing.T) {
	for _, tc := range []struct {
		name string
		// made is true when the links are made through ~/.home and checked
		// through the real folder, false for the other way round.
		viaLink bool
	}{{"made through ~/.home", true}, {"made through the real folder", false}} {
		t.Run(tc.name, func(t *testing.T) {
			b, real := aliasBox(t)
			maker, checker := b.Ctx, real
			if !tc.viaLink {
				maker, checker = real, b.Ctx
			}
			must(t, ApplyLinks(maker, planWith(t, maker), ApplyOptions{}))
			made := map[string]string{}
			for _, target := range aliasTargets {
				made[target] = b.readlink(b.h(target))
			}
			if !strings.HasPrefix(made[".zshrc"], maker.Root+"/") {
				t.Fatalf(".zshrc -> %s, want a link through %s", made[".zshrc"], maker.Root)
			}

			// The other path sees every link as right: no conflict, nothing
			// to replace.
			p := planWith(t, checker)
			b.wantAllOK(p)

			// link --backup through it moves nothing aside.
			moved, err := BackupConflicts(checker, p)
			must(t, err)
			if len(moved) != 0 {
				t.Errorf("moved aside %v", moved)
			}
			// Applying keeps every link, as it was written, and links.json.
			must(t, ApplyLinks(checker, p, ApplyOptions{}))
			for target, dest := range made {
				if got := b.readlink(b.h(target)); got != dest {
					t.Errorf("%s -> %s, was %s", target, got, dest)
				}
			}
			b.wantRecorded()

			// It owns them, so it prunes what the repo no longer has.
			must(t, os.Remove(filepath.Join(checker.Root, "autolink/common/zshrc")))
			p = planWith(t, checker)
			if k := b.kinds(p)[".zshrc"]; k != Prune {
				t.Fatalf(".zshrc is %q, want prune", k)
			}
			must(t, ApplyLinks(checker, p, ApplyOptions{}))
			if exists(b.h(".zshrc")) {
				t.Error(".zshrc was not pruned")
			}

			// And unlink through it removes them all.
			removed, err := UnlinkAll(checker)
			must(t, err)
			if len(removed) != len(aliasTargets)-1 {
				t.Errorf("removed %v", removed)
			}
			for _, target := range aliasTargets {
				if isLink(b.h(target)) {
					t.Errorf("%s is still linked", target)
				}
			}
		})
	}
}

// A link through the other path to the wrong repo file is repointed, not a
// conflict; one out of the repo stays a conflict.
func TestALinkThroughTheOtherPathIsReplacedNotBlocked(t *testing.T) {
	b, real := aliasBox(t)
	must(t, os.Symlink(b.r("autolink/common/old-zshrc"), b.h(".zshrc")))
	b.Files(map[string]string{"elsewhere/vimrc": "x"})
	must(t, os.Symlink(filepath.Join(b.Dir, "elsewhere"), b.h(".vim")))
	p := planWith(t, real)
	k := b.kinds(p)
	if k[".zshrc"] != Replace || k[".vim"] != Conflict {
		t.Fatalf("kinds = %v; want .zshrc replace, .vim conflict", k)
	}
	must(t, ApplyLinks(real, p, ApplyOptions{}))
	if got := b.readlink(b.h(".zshrc")); got != filepath.Join(real.Root, "autolink/common/zshrc") {
		t.Errorf(".zshrc -> %s", got)
	}
}

// A repo file that is itself a symlink does not make a foreign link to where
// it points homebot's: only the folders are compared as files, so that link
// stays a conflict, as in the TypeScript.
func TestAForeignLinkToWhereARepoSymlinkPointsIsNotOurs(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{"elsewhere/zshrc": "z"})
	far := filepath.Join(b.Dir, "elsewhere", "zshrc")
	must(t, os.MkdirAll(b.r("autolink/common"), 0o755))
	must(t, os.Symlink(far, b.r("autolink/common/zshrc")))
	must(t, os.Symlink(far, b.h(".zshrc")))
	if k := b.kinds(planWith(t, b.Ctx)); k[".zshrc"] != Conflict {
		t.Errorf(".zshrc = %v, want conflict", k[".zshrc"])
	}
}

func TestOwnsLinkThroughEitherPath(t *testing.T) {
	b, real := aliasBox(t)
	must(t, os.Symlink(b.r("autolink/common/zshrc"), b.h("via-link")))
	must(t, os.Symlink(filepath.Join(real.Root, "autolink/common/zshrc"), b.h("via-real")))
	must(t, os.Symlink(b.r("autolink/common/gone"), b.h("gone-via-link")))
	must(t, os.Symlink(filepath.Join(b.Dir, "code"), b.h("parent")))
	for _, c := range []*app.Context{b.Ctx, real} {
		for _, name := range []string{"via-link", "via-real", "gone-via-link"} {
			if _, owned, err := OwnsLink(c, b.h(name)); err != nil || !owned {
				t.Errorf("OwnsLink(%s) with root %s = %v, %v", name, c.Root, owned, err)
			}
		}
		if _, owned, _ := OwnsLink(c, b.h("parent")); owned {
			t.Errorf("a link to the folder above the repo is owned with root %s", c.Root)
		}
	}
}

// ── re-checks just before removing (final review home M2) ──

// Between the plan and apply an app can save over a link homebot planned to
// remove (rename into place), or point it elsewhere. apply leaves it, says
// so, and still does the rest.
func TestApplyLeavesALinkThatChangedSinceThePlan(t *testing.T) {
	setups := []struct {
		name  string
		setup func(b *box)
		kind  Kind
	}{
		{"replace", func(b *box) {
			b.repo(map[string]string{"autolink/common/a": "a"})
			must(t, os.Symlink(b.r("autolink/common/old-name"), b.h(".a")))
		}, Replace},
		{"prune", func(b *box) {
			b.repo(map[string]string{"autolink/common/a": "a"})
			b.linkAndCheck()
			must(t, os.Remove(b.r("autolink/common/a")))
		}, Prune},
		{"unfold", func(b *box) {
			b.repo(map[string]string{"autolink/common/a/x": "x"})
			b.linkAndCheck()
			must(t, os.Rename(b.r("autolink/common/a"), b.r("autolink/common/(a)")))
		}, Unfold},
	}
	changes := []struct {
		name   string
		change func(b *box)
		check  func(b *box)
	}{
		{"a real file saved in its place", func(b *box) {
			tmp := b.h(".a.tmp")
			must(t, os.WriteFile(tmp, []byte("the app's own"), 0o644))
			must(t, os.Rename(tmp, b.h(".a")))
		}, func(b *box) {
			if isLink(b.h(".a")) || b.read(b.h(".a")) != "the app's own" {
				t.Error("the real file at .a was removed")
			}
		}},
		{"a link to somewhere else", func(b *box) {
			b.Files(map[string]string{"elsewhere/keep": "k"})
			must(t, os.Remove(b.h(".a")))
			must(t, os.Symlink(filepath.Join(b.Dir, "elsewhere"), b.h(".a")))
		}, func(b *box) {
			if got := b.readlink(b.h(".a")); got != filepath.Join(b.Dir, "elsewhere") {
				t.Errorf(".a -> %s", got)
			}
			// Nothing planned below .a lands in the folder it now points at.
			entries, err := os.ReadDir(filepath.Join(b.Dir, "elsewhere"))
			must(t, err)
			if len(entries) != 1 {
				t.Errorf("elsewhere holds %d entries, want only keep", len(entries))
			}
		}},
	}
	for _, s := range setups {
		for _, c := range changes {
			t.Run(s.name+"/"+c.name, func(t *testing.T) {
				b := newBox(t)
				s.setup(b)
				b.repo(map[string]string{"autolink/common/z": "z"})
				p := b.plan()
				if k := b.kinds(p)[".a"]; k != s.kind {
					t.Fatalf(".a is %q, want %q", k, s.kind)
				}
				c.change(b)
				err := ApplyLinks(b.Ctx, p, ApplyOptions{})
				var ue *fail.UserError
				if !errors.As(err, &ue) {
					t.Fatalf("err = %v, want a user error naming .a", err)
				}
				b.contains(err.Error(), "~/.a changed while homebot was linking it, so it was left alone. Run homebot link again.")
				c.check(b)
				if !isLink(b.h(".z")) {
					t.Error("the rest of the plan was not done")
				}
			})
		}
	}
}

// A link that is still the planned one, reached through the other path to
// the repo, is not "changed".
func TestApplyRemovesALinkStillPlannedThroughTheOtherPath(t *testing.T) {
	b, real := aliasBox(t)
	must(t, ApplyLinks(b.Ctx, planWith(t, b.Ctx), ApplyOptions{}))
	must(t, os.Remove(filepath.Join(real.Root, "autolink/common/zshrc")))
	must(t, ApplyLinks(real, planWith(t, real), ApplyOptions{}))
	if exists(b.h(".zshrc")) {
		t.Error(".zshrc was not pruned")
	}
}

// ── Ctrl-C (final review home M3) ──

func TestApplyStopsBetweenOpsWhenTheCommandIsStopped(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/a": "a", "autolink/common/b": "b", "autolink/common/c": "c"})
	ctx, cancel := context.WithCancelCause(context.Background())
	c := b.Ctx.WithContext(ctx)
	var done []string
	err := ApplyLinks(c, b.plan(), ApplyOptions{Done: func(op Op) {
		done = append(done, op.Target)
		cancel(fail.Silently(130))
	}})
	var exit *fail.Exit
	if !errors.As(err, &exit) || exit.Code != 130 {
		t.Fatalf("err = %v, want the silent 130", err)
	}
	if d := cmp.Diff([]string{b.h(".a")}, done); d != "" {
		t.Errorf("done (-want +got):\n%s", d)
	}
	if exists(b.h(".b")) || exists(b.h(".c")) {
		t.Error("linked after the stop")
	}
	if d := cmp.Diff([]string{b.h(".a")}, b.recorded()); d != "" {
		t.Errorf("recorded (-want +got):\n%s", d)
	}
}

func TestBackupConflictsStopsBetweenItemsWhenTheCommandIsStopped(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/a": "a", "autolink/common/b": "b"})
	b.Files(map[string]string{"home/.a": "mine", "home/.b": "mine"})
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(fail.Silently(130))
	moved, err := BackupConflicts(b.Ctx.WithContext(ctx), b.plan())
	var exit *fail.Exit
	if !errors.As(err, &exit) || exit.Code != 130 {
		t.Fatalf("err = %v, want the silent 130", err)
	}
	if len(moved) != 0 || b.read(b.h(".a")) != "mine" || b.read(b.h(".b")) != "mine" {
		t.Errorf("moved %v after the stop", moved)
	}
}

// ── folders for private files (final review security F8) ──

func perm(t *testing.T, p string) fs.FileMode {
	t.Helper()
	st, err := os.Lstat(p)
	must(t, err)
	return st.Mode().Perm()
}

func TestFoldersMadeForPrivateFilesAre0700(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{
		"private/common/(gnupg)/private-keys-v1.d/key": "k",
		"private/common/(vault)/a/b/f":                 "f",
		"private/common/config/(gh)/hosts.yml":         "token",
		"autolink/common/config/(gh)/config.yml":       "editor: vim",
		"autolink/common/(public)/deep/f":              "f",
	})
	must(t, os.Mkdir(b.h(".config"), 0o755))
	must(t, os.Chmod(b.h(".config"), 0o755))
	b.linkAndCheck()
	for _, d := range []string{".gnupg", ".gnupg/private-keys-v1.d", ".vault", ".vault/a", ".vault/a/b", ".config/gh"} {
		if got := perm(t, b.h(d)); got != 0o700 {
			t.Errorf("~/%s is %o, want 700", d, got)
		}
	}
	if got := perm(t, b.h(".config")); got != 0o755 {
		t.Errorf("~/.config, which was there, is %o now", got)
	}
	// Folders for the autolink tree alone keep 777 less the umask, as a
	// folder made the same way does.
	ref := filepath.Join(b.Dir, "ref")
	must(t, os.Mkdir(ref, 0o777))
	if got, want := perm(t, b.h(".public")), perm(t, ref); got != want {
		t.Errorf("~/.public is %o, want %o", got, want)
	}
}

func TestUnfoldingAFolderForPrivateFilesMakesIt0700(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/a/x": "x"})
	b.linkAndCheck()
	b.repo(map[string]string{"private/common/a/secret": "s"})
	p := b.plan()
	if k := b.kinds(p)[".a"]; k != Unfold {
		t.Fatalf(".a is %q, want unfold", k)
	}
	b.apply(p, ApplyOptions{})
	if got := perm(t, b.h(".a")); got != 0o700 {
		t.Errorf("~/.a is %o, want 700", got)
	}
	if !isLink(b.h(".a/secret")) || !isLink(b.h(".a/x")) {
		t.Error("the unfolded folder was not filled")
	}
}
