package linker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestApplyRecordsLinksSorted(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/zshrc": "z", "autolink/common/bashrc": "b", "autolink/common/differ": "repo"})
	b.Files(map[string]string{"home/.differ": "mine"})
	b.linkAndCheck()
	if d := cmp.Diff([]string{b.h(".bashrc"), b.h(".zshrc")}, b.recorded()); d != "" {
		t.Errorf("recorded (-want +got):\n%s", d)
	}
	data, err := os.ReadFile(filepath.Join(b.Ctx.StateDir, "links.json"))
	must(t, err)
	if len(data) == 0 || data[0] != '[' {
		t.Errorf("links.json = %q, want a JSON array", data)
	}
}

func TestApplyKeepsRecordedLinksAndDropsGoneOnes(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/a": "a"})
	b.linkAndCheck()
	// A recorded link a person removed is forgotten; one still there is kept.
	b.repo(map[string]string{"autolink/common/c": "c"})
	must(t, os.Symlink(b.r("autolink/common/c"), b.h(".c")))
	b.linkAndCheck()
	if d := cmp.Diff([]string{b.h(".a"), b.h(".c")}, b.recorded()); d != "" {
		t.Errorf("recorded (-want +got):\n%s", d)
	}
	must(t, os.Remove(b.h(".a")))
	b.apply(b.plan(Private), ApplyOptions{})
	if d := cmp.Diff([]string{b.h(".c")}, b.recorded()); d != "" {
		t.Errorf("recorded (-want +got):\n%s", d)
	}
}

func TestUnlinkAllWithNothingRecordedWritesAnEmptyList(t *testing.T) {
	b := newBox(t)
	removed, err := UnlinkAll(b.Ctx)
	must(t, err)
	if len(removed) != 0 {
		t.Errorf("removed = %v", removed)
	}
	data, err := os.ReadFile(filepath.Join(b.Ctx.StateDir, "links.json"))
	must(t, err)
	if got := string(data); got != "[]\n" && got != "[]" {
		t.Errorf("links.json = %q, want []", got)
	}
}

func TestUnlinkAllLeavesLinksAPersonRepointed(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{"home/.home/autolink/common/a": "a", "home/other": "o"})
	b.linkAndCheck()
	must(t, os.Remove(b.h(".a")))
	must(t, os.Symlink(b.h("other"), b.h(".a")))
	removed, err := UnlinkAll(b.Ctx)
	must(t, err)
	if len(removed) != 0 {
		t.Errorf("removed = %v", removed)
	}
	if !isLink(b.h(".a")) {
		t.Error("the repointed link was removed")
	}
}

func TestOwnsLink(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{"home/.home/autolink/common/a": "a", "home/file": "f", "home/outside": "o"})
	must(t, os.Symlink(b.r("autolink/common/a"), b.h("abs")))
	must(t, os.Symlink(".home/autolink/common/a", b.h("rel")))
	must(t, os.Symlink(b.h("outside"), b.h("out")))
	cases := []struct {
		name  string
		path  string
		was   string
		owned bool
	}{
		{"absolute link into the repo", b.h("abs"), b.r("autolink/common/a"), true},
		{"relative link into the repo", b.h("rel"), b.r("autolink/common/a"), true},
		{"link outside the repo", b.h("out"), "", false},
		{"real file", b.h("file"), "", false},
		{"nothing there", b.h("none"), "", false},
		{"a file on the way", b.h("file/below"), "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			was, owned, err := OwnsLink(b.Ctx, c.path)
			must(t, err)
			if was != c.was || owned != c.owned {
				t.Errorf("OwnsLink = %q, %v; want %q, %v", was, owned, c.was, c.owned)
			}
		})
	}
}

func TestPlanChangesAndSummary(t *testing.T) {
	p := Plan{
		{Kind: OK, Target: "/h/ok"},
		{Kind: Create, Target: "/h/create"},
		{Kind: Adopt, Target: "/h/adopt"},
		{Kind: Conflict, Target: "/h/conflict"},
		{Kind: Unfold, Target: "/h/unfold"},
		{Kind: Locked, Target: "/h/locked"},
		{Kind: Replace, Target: "/h/replace"},
		{Kind: Invalid, Target: "/h/invalid"},
		{Kind: Prune, Target: "/h/prune"},
	}
	targets := func(p Plan) []string {
		var out []string
		for _, op := range p {
			out = append(out, op.Target)
		}
		return out
	}
	if d := cmp.Diff([]string{"/h/create", "/h/adopt", "/h/unfold", "/h/replace", "/h/prune"}, targets(p.Pending())); d != "" {
		t.Errorf("Pending (-want +got):\n%s", d)
	}
	if d := cmp.Diff([]string{"/h/create", "/h/unfold", "/h/replace", "/h/prune"}, targets(p.Changes(ApplyOptions{Safe: true}))); d != "" {
		t.Errorf("safe Changes (-want +got):\n%s", d)
	}
	want := Summary{OK: 1, Create: 1, Replace: 1, Adopt: 1, Unfold: 1, Prune: 1, Locked: 1, Conflict: 1, Invalid: 1}
	if got := p.Summary(); got != want {
		t.Errorf("Summary = %+v", got)
	}
}

func TestPendingChanges(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/a": "a", "autolink/common/differ": "repo"})
	b.Files(map[string]string{"home/.differ": "mine"})
	p, err := PendingChanges(b.Ctx)
	must(t, err)
	b.wantKinds(p, map[string]Kind{".a": Create})
	b.linkAndCheck()
	p, err = PendingChanges(b.Ctx)
	must(t, err)
	if len(p) != 0 {
		t.Errorf("pending after linking = %+v", p)
	}
}

// readOnly makes dir read-only for the rest of the test, so nothing can be
// made or removed in it. root can write anyway, so the test is skipped then.
func readOnly(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root can write to a read-only folder")
	}
	must(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

func TestApplyRecordsTheLinksMadeBeforeAFailure(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/a": "a", "autolink/common/(dir)/b": "b"})
	must(t, os.Mkdir(b.h(".dir"), 0o755))
	readOnly(t, b.h(".dir"))
	p := b.plan()
	b.wantKinds(p, map[string]Kind{".a": Create, ".dir/b": Create})
	if err := ApplyLinks(b.Ctx, p, ApplyOptions{}); err == nil {
		t.Fatal("ApplyLinks succeeded in a read-only folder")
	}
	if !isLink(b.h(".a")) {
		t.Fatal(".a was not linked before the failure")
	}
	if d := cmp.Diff([]string{b.h(".a")}, b.recorded()); d != "" {
		t.Errorf("recorded (-want +got):\n%s", d)
	}
}

// What apply removes was checked when the plan was made. Something else may
// be there by the time it runs; a folder is never removed, an emptied one
// included.
func TestApplyNeverRemovesAFolderThatAppearedSinceThePlan(t *testing.T) {
	cases := []struct {
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
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newBox(t)
			c.setup(b)
			p := b.plan()
			if k := b.kinds(p)[".a"]; k != c.kind {
				t.Fatalf(".a is %q, want %q", k, c.kind)
			}
			must(t, os.Remove(b.h(".a")))
			must(t, os.Mkdir(b.h(".a"), 0o755))
			if err := ApplyLinks(b.Ctx, p, ApplyOptions{}); err == nil {
				t.Error("ApplyLinks succeeded over a folder")
			}
			if !isRealDir(b.h(".a")) {
				t.Error("the folder at .a was removed")
			}
		})
	}
}

func TestAdoptChecksTheFileAgainBeforeReplacingIt(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{"home/.home/autolink/common/same": "same", "home/.same": "same"})
	p := b.plan()
	b.wantKinds(p, map[string]Kind{".same": Adopt})
	must(t, os.WriteFile(b.h(".same"), []byte("edited"), 0o644))
	err := ApplyLinks(b.Ctx, p, ApplyOptions{})
	if err == nil {
		t.Fatal("ApplyLinks adopted a file that changed")
	}
	b.contains(err.Error(), "~/.same changed while homebot was linking it, so it was left alone. Run homebot link again.")
	if isLink(b.h(".same")) {
		t.Fatal(".same was replaced")
	}
	if got := b.read(b.h(".same")); got != "edited" {
		t.Errorf(".same = %q", got)
	}
}

// enotdirBox has a link recorded at ~/.a/b, where a file is now at ~/.a: the
// path cannot be looked at (ENOTDIR), which is no link.
func enotdirBox(t *testing.T) *box {
	t.Helper()
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/(a)/b": "b"})
	b.linkAndCheck()
	must(t, os.RemoveAll(b.r("autolink/common/(a)")))
	must(t, os.Remove(b.h(".a/b")))
	must(t, os.Remove(b.h(".a")))
	must(t, os.WriteFile(b.h(".a"), []byte("a file now"), 0o644))
	if d := cmp.Diff([]string{b.h(".a/b")}, b.recorded()); d != "" {
		t.Fatalf("recorded (-want +got):\n%s", d)
	}
	return b
}

func TestAFileWhereARecordedLinksFolderWasIsNoLink(t *testing.T) {
	t.Run("PlanLinks prunes nothing", func(t *testing.T) {
		b := enotdirBox(t)
		if p := b.plan(); len(p) != 0 {
			t.Errorf("plan = %+v, want nothing", p)
		}
	})
	t.Run("ApplyLinks forgets it", func(t *testing.T) {
		b := enotdirBox(t)
		b.apply(b.plan(), ApplyOptions{})
		if got := b.recorded(); len(got) != 0 {
			t.Errorf("recorded = %v, want none", got)
		}
	})
	t.Run("UnlinkAll skips it", func(t *testing.T) {
		b := enotdirBox(t)
		removed, err := UnlinkAll(b.Ctx)
		must(t, err)
		if len(removed) != 0 {
			t.Errorf("removed = %v", removed)
		}
		if got := b.read(b.h(".a")); got != "a file now" {
			t.Errorf(".a = %q", got)
		}
	})
}
