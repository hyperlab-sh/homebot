package linker

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func (b *box) wantKinds(p Plan, want map[string]Kind) {
	b.t.Helper()
	if d := cmp.Diff(want, b.kinds(p)); d != "" {
		b.t.Errorf("kinds (-want +got):\n%s", d)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// planLinks on an empty home

func TestLinksFilesAndFoldsSingleLayerFolders(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{
		"autolink/common/zshrc":                "z",
		"autolink/common/config/nvim/init.lua": "n",
		"autolink/common/bin/tool":             "t",
	})
	b.wantKinds(b.plan(), map[string]Kind{".zshrc": Create, ".config/nvim": Create, ".bin": Create})
	b.linkAndCheck()
	if got := b.readlink(b.h(".zshrc")); got != b.r("autolink/common/zshrc") {
		t.Errorf(".zshrc points to %s", got)
	}
	if got := b.readlink(b.h(".config/nvim")); got != b.r("autolink/common/config/nvim") {
		t.Errorf(".config/nvim points to %s", got)
	}
	// .config itself is never a symlink: apps must be able to add their own folders
	if !isRealDir(b.h(".config")) {
		t.Error(".config is not a real folder")
	}
	if got := b.read(b.h(".config/nvim/init.lua")); got != "n" {
		t.Errorf("init.lua = %q", got)
	}
}

func TestMergesAFolderThatTwoLayersProvideFileByFile(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{
		"autolink/common/config/git/config":   "c",
		"autolink/linux/config/git/linux.inc": "l",
	})
	b.linkAndCheck()
	if !isRealDir(b.h(".config/git")) {
		t.Error(".config/git is not a real folder")
	}
	if got := b.readlink(b.h(".config/git/config")); got != b.r("autolink/common/config/git/config") {
		t.Errorf("config points to %s", got)
	}
	if got := b.readlink(b.h(".config/git/linux.inc")); got != b.r("autolink/linux/config/git/linux.inc") {
		t.Errorf("linux.inc points to %s", got)
	}
}

func TestIgnoresTheOtherOSLayer(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/macos/hammerspoon/init.lua": "x"})
	if p := b.plan(); len(p) != 0 {
		t.Errorf("plan = %+v, want none", p)
	}
}

func TestSkipsGitkeepAndDSStore(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/.gitkeep": "", "autolink/linux/.DS_Store": ""})
	if p := b.plan(); len(p) != 0 {
		t.Errorf("plan = %+v, want none", p)
	}
}

func TestIgnoresDotNamesAtTheTopLinksThemDeeperDown(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{
		"autolink/common/.notes.md":    "n",
		"autolink/common/.hidden/x":    "x",
		"autolink/common/(zsh)/.zshrc": "z",
	})
	b.wantKinds(b.plan(), map[string]Kind{".zsh/.zshrc": Create})
}

func TestLibraryAndItsBuiltInFoldersStayRealTheAppFolderFolds(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/Library/Application Support/Code/User/settings.json": "{}"})
	b.wantKinds(b.plan(), map[string]Kind{"Library/Application Support/Code": Create})
	b.linkAndCheck()
	if !isRealDir(b.h("Library")) || !isRealDir(b.h("Library/Application Support")) {
		t.Error("Library or Library/Application Support is not a real folder")
	}
	if got := b.read(b.h("Library/Application Support/Code/User/settings.json")); got != "{}" {
		t.Errorf("settings.json = %q", got)
	}
}

func TestBuiltInFoldersNeverFold(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/local/bin/tool": "t", "autolink/common/ssh/config": "c"})
	b.wantKinds(b.plan(), map[string]Kind{".local/bin/tool": Create, ".ssh/config": Create})
	b.linkAndCheck()
	if !isRealDir(b.h(".local/bin")) || !isRealDir(b.h(".ssh")) {
		t.Error(".local/bin or .ssh is not a real folder")
	}
}

// (name) folders

func TestAMarkedFolderIsRealItsFilesAreLinks(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/(claude)/settings.json": "{}"})
	b.wantKinds(b.plan(), map[string]Kind{".claude/settings.json": Create})
	b.linkAndCheck()
	if !isRealDir(b.h(".claude")) {
		t.Error(".claude is not a real folder")
	}
	if got := b.readlink(b.h(".claude/settings.json")); got != b.r("autolink/common/(claude)/settings.json") {
		t.Errorf("settings.json points to %s", got)
	}
}

func TestInsideAMarkedFolderMarkedChildrenStayRealPlainOnesFold(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{
		"autolink/common/(claude)/(sessions)/keep.md": "k",
		"autolink/common/(claude)/projects/p.md":      "p",
	})
	b.wantKinds(b.plan(), map[string]Kind{".claude/projects": Create, ".claude/sessions/keep.md": Create})
	b.linkAndCheck()
	if !isRealDir(b.h(".claude/sessions")) {
		t.Error(".claude/sessions is not a real folder")
	}
	if !isLink(b.h(".claude/projects")) {
		t.Error(".claude/projects is not a link")
	}
}

func TestAChainOfMarkedFoldersThenPlainOnes(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/(a)/(b)/c/d": "d"})
	b.wantKinds(b.plan(), map[string]Kind{".a/b/c": Create})
	b.linkAndCheck()
	if !isRealDir(b.h(".a/b")) {
		t.Error(".a/b is not a real folder")
	}
	if got := b.read(b.h(".a/b/c/d")); got != "d" {
		t.Errorf("d = %q", got)
	}
}

func TestBuiltInsCountAsMarked(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/config/(app)/(state)/x": "x"})
	b.wantKinds(b.plan(), map[string]Kind{".config/app/state/x": Create})
}

func TestAPlainFolderCannotHoldAMarkedOne(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/a/(b)/c": "c", "autolink/common/zshrc": "z"})
	p := b.plan()
	b.wantKinds(p, map[string]Kind{".a": Invalid, ".zshrc": Create})
	reason := b.firstReason(p)
	b.contains(reason, `rename "a" to "(a)"`)
	b.contains(reason, "autolink/common/a/(b)")
	b.linkAndCheck()
	if exists(b.h(".a")) {
		t.Error(".a was linked")
	}
}

func TestTheChainMustBeUnbroken(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/(a)/b/(c)/d": "d"})
	p := b.plan()
	b.wantKinds(p, map[string]Kind{".a/b": Invalid})
	b.contains(b.firstReason(p), `rename "b" to "(b)"`)
}

func TestTheHintAsksToRenameInEveryLayer(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/a/(b)/x": "x", "autolink/linux/a/y": "y"})
	b.contains(b.firstReason(b.plan()), "in every layer")
}

func TestOneFolderSpelledTwoWaysIsAnError(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
	}{
		{"across layers", map[string]string{"autolink/common/(claude)/x": "x", "autolink/linux/claude/y": "y"}},
		{"across trees", map[string]string{"autolink/common/(claude)/x": "x", "private/common/claude/token": "t"}},
		{"in one layer", map[string]string{"autolink/common/(claude)/x": "x", "autolink/common/claude/y": "y"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newBox(t)
			b.repo(c.files)
			p := b.plan()
			b.wantKinds(p, map[string]Kind{".claude": Invalid})
			b.contains(b.firstReason(p), "use one spelling everywhere")
		})
	}
}

func TestBuiltInsMayBeSpelledEitherWay(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/config/a/x": "x", "autolink/linux/(config)/b/y": "y"})
	b.wantKinds(b.plan(), map[string]Kind{".config/a": Create, ".config/b": Create})
}

func TestParensOnAFileIsAnErrorItsNeighboursStillLink(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/(claude)/(settings.json)": "{}", "autolink/common/(claude)/ok.md": "ok"})
	p := b.plan()
	b.wantKinds(p, map[string]Kind{".claude/(settings.json)": Invalid, ".claude/ok.md": Create})
	b.contains(b.firstReason(p), `() marks folders only: rename "(settings.json)" to "settings.json"`)
}

func TestABadNameInsideAFolderThatWouldFoldStopsTheFold(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/app/deep/(x.json)": "{}"})
	p := b.plan()
	b.wantKinds(p, map[string]Kind{".app": Invalid})
	b.contains(b.firstReason(p), "marks folders only")
}

func TestBadNamesAtTheTopAreErrors(t *testing.T) {
	for _, bad := range []string{"()", "((a))", "dot-config"} {
		t.Run(bad, func(t *testing.T) {
			b := newBox(t)
			b.repo(map[string]string{"autolink/common/" + bad + "/x": "x"})
			var got []Kind
			for _, op := range b.plan() {
				got = append(got, op.Kind)
			}
			if d := cmp.Diff([]Kind{Invalid}, got); d != "" {
				t.Errorf("kinds (-want +got):\n%s", d)
			}
		})
	}
}

func TestMarkingAFoldedFolderUnfoldsIt(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/config/app/a": "a"})
	b.linkAndCheck()
	if !isLink(b.h(".config/app")) {
		t.Fatal(".config/app is not a link")
	}
	must(t, os.Rename(b.r("autolink/common/config/app"), b.r("autolink/common/config/(app)")))
	b.wantKinds(b.plan(), map[string]Kind{".config/app": Unfold, ".config/app/a": Create})
	b.linkAndCheck()
	if !isRealDir(b.h(".config/app")) {
		t.Error(".config/app is not a real folder")
	}
	if got := b.readlink(b.h(".config/app/a")); got != b.r("autolink/common/config/(app)/a") {
		t.Errorf("a points to %s", got)
	}
}

func TestLinksUnderABrokenPartOfTheRepoAreKept(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/(claude)/x": "x"})
	b.linkAndCheck()
	b.repo(map[string]string{"autolink/linux/claude/y": "y"})
	p := b.plan()
	b.wantKinds(p, map[string]Kind{".claude": Invalid})
	b.apply(p, ApplyOptions{})
	if !isLink(b.h(".claude/x")) {
		t.Error(".claude/x was removed")
	}
}

// planLinks with things already in $HOME

func TestAdoptsIdenticalFilesBlocksDifferentOnes(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{
		"home/.home/autolink/common/same":   "same",
		"home/.home/autolink/common/differ": "repo",
		"home/.same":                        "same",
		"home/.differ":                      "mine",
	})
	p := b.plan()
	b.wantKinds(p, map[string]Kind{".same": Adopt, ".differ": Conflict})
	b.apply(p, ApplyOptions{})
	if !isLink(b.h(".same")) {
		t.Error(".same was not adopted")
	}
	if got := b.read(b.h(".differ")); got != "mine" {
		t.Errorf(".differ = %q", got)
	}
}

func TestSafeModeNeverReplacesRealFiles(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{"home/.home/autolink/common/same": "same", "home/.same": "same"})
	p := b.plan()
	if done := p.Changes(ApplyOptions{Safe: true}); len(done) != 0 {
		t.Errorf("safe changes = %+v, want none", done)
	}
	b.apply(p, ApplyOptions{Safe: true})
	if isLink(b.h(".same")) {
		t.Error(".same was replaced in safe mode")
	}
}

func TestBlocksSymlinksThatPointOutsideTheRepo(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{"home/.home/autolink/common/vimrc": "v", "home/elsewhere": "e"})
	must(t, os.Symlink(b.h("elsewhere"), b.h(".vimrc")))
	p := b.plan()
	if len(p) == 0 || p[0].Kind != Conflict {
		t.Fatalf("plan = %+v, want a conflict first", p)
	}
	b.contains(p[0].Reason, "outside this repo")
}

func TestRepointsStaleLinksIntoTheRepo(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/vimrc": "v"})
	must(t, os.Symlink(b.r("autolink/common/old-name"), b.h(".vimrc")))
	b.wantKinds(b.plan(), map[string]Kind{".vimrc": Replace})
	b.linkAndCheck()
	if got := b.readlink(b.h(".vimrc")); got != b.r("autolink/common/vimrc") {
		t.Errorf(".vimrc points to %s", got)
	}
}

func TestMergesIntoAnExistingRealFolder(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{"home/.home/autolink/common/config/app/a": "a", "home/.config/app/local-state": "s"})
	b.linkAndCheck()
	if !isLink(b.h(".config/app/a")) {
		t.Error(".config/app/a is not a link")
	}
	if got := b.read(b.h(".config/app/local-state")); got != "s" {
		t.Errorf("local-state = %q", got)
	}
}

func TestBlocksAFileWhereTheRepoHasAFolderAndViceVersa(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{
		"home/.home/autolink/common/thing/inner": "x",
		"home/.home/autolink/common/other":       "y",
		"home/.thing":                            "a file",
		"home/.other/inside":                     "z",
	})
	b.wantKinds(b.plan(), map[string]Kind{".thing": Conflict, ".other": Conflict})
}

func TestTheSameFileInTwoLayersIsALayoutError(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/x": "1", "autolink/linux/x": "2"})
	p := b.plan()
	if len(p) == 0 || p[0].Kind != Invalid {
		t.Fatalf("plan = %+v, want invalid first", p)
	}
	b.contains(p[0].Reason, "autolink/common/x and autolink/linux/x")
	moved, err := BackupConflicts(b.Ctx, p[:1])
	must(t, err)
	if len(moved) != 0 {
		t.Errorf("moved = %v, want none", moved)
	}
}

func TestBackupMovesBlockersAsideAndLinksInTheirPlace(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{"home/.home/autolink/common/differ": "repo", "home/.differ": "mine"})
	b.Ctx.Now = func() time.Time { return time.Date(2026, 10, 11, 12, 34, 56, 0, time.UTC) }
	moved, err := BackupConflicts(b.Ctx, b.plan())
	must(t, err)
	if d := cmp.Diff([]string{b.h(".differ.homebot-backup-20261011-123456")}, moved); d != "" {
		t.Fatalf("moved (-want +got):\n%s", d)
	}
	b.linkAndCheck()
	if got := b.read(b.h(".differ")); got != "repo" {
		t.Errorf(".differ = %q", got)
	}
	if got := b.read(moved[0]); got != "mine" {
		t.Errorf("backup = %q", got)
	}
}

// folding over time

func TestUnfoldsAFoldedFolderWhenASecondLayerJoins(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/config/git/config": "c"})
	b.linkAndCheck()
	if !isLink(b.h(".config/git")) {
		t.Fatal(".config/git is not a link")
	}
	b.repo(map[string]string{"autolink/linux/config/git/linux.inc": "l"})
	b.wantKinds(b.plan(), map[string]Kind{".config/git": Unfold, ".config/git/config": Create, ".config/git/linux.inc": Create})
	b.linkAndCheck()
	if !isRealDir(b.h(".config/git")) {
		t.Error(".config/git is not a real folder")
	}
	if d := cmp.Diff([]string{"config", "linux.inc"}, names(t, b.h(".config/git"))); d != "" {
		t.Errorf(".config/git (-want +got):\n%s", d)
	}
	// the repo was not touched by unfolding
	if d := cmp.Diff([]string{"config"}, names(t, b.r("autolink/common/config/git"))); d != "" {
		t.Errorf("repo folder (-want +got):\n%s", d)
	}
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	must(t, err)
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	slices.Sort(out)
	return out
}

func TestPrunesLinksWhoseSourceIsGone(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/a": "a", "autolink/common/b": "b"})
	b.linkAndCheck()
	must(t, os.Remove(b.r("autolink/common/b")))
	b.wantKinds(b.plan(), map[string]Kind{".a": OK, ".b": Prune})
	b.linkAndCheck()
	if exists(b.h(".b")) {
		t.Error(".b is still there")
	}
	if !isLink(b.h(".a")) {
		t.Error(".a is not a link")
	}
	if d := cmp.Diff([]string{b.h(".a")}, b.recorded()); d != "" {
		t.Errorf("recorded (-want +got):\n%s", d)
	}
}

func TestDoesNotPruneLinksAPersonRepointedElsewhere(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{"home/.home/autolink/common/a": "a", "home/other": "o"})
	b.linkAndCheck()
	must(t, os.Remove(b.h(".a")))
	must(t, os.Symlink(b.h("other"), b.h(".a")))
	must(t, os.Remove(b.r("autolink/common/a")))
	if p := b.plan(); len(p) != 0 {
		t.Errorf("plan = %+v, want none", p)
	}
}

func TestPlanningOneTreeNeverPrunesTheOther(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"private/common/netrc": "secret", "autolink/common/a": "a"})
	b.linkAndCheck()
	must(t, os.Remove(b.r("autolink/common/a")))
	b.wantKinds(b.plan(Private), map[string]Kind{".netrc": OK})
}

func TestUnlinkAllRemovesOnlyHomebotsLinks(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{"home/.home/autolink/common/a": "a", "home/.real": "r"})
	b.linkAndCheck()
	removed, err := UnlinkAll(b.Ctx)
	must(t, err)
	if d := cmp.Diff([]string{b.h(".a")}, removed); d != "" {
		t.Errorf("removed (-want +got):\n%s", d)
	}
	if exists(b.h(".a")) {
		t.Error(".a is still there")
	}
	if !exists(b.h(".real")) {
		t.Error(".real was removed")
	}
	if !exists(b.r("autolink/common/a")) {
		t.Error("the repo file was removed")
	}
}

// private tree

func TestLinksPlaintextReportsLockedSecretsNeverFolds(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{
		"private/common/ssh/id_ed25519":     "key",
		"private/common/ssh/id_ed25519.age": "enc",
		"private/common/ssh/work_key.age":   "enc",
		"private/common/secretsdir/token":   "t",
	})
	b.wantKinds(b.plan(), map[string]Kind{
		".ssh/id_ed25519":   Create,
		".ssh/work_key":     Locked,
		".secretsdir/token": Create,
	})
	b.linkAndCheck()
	if !isRealDir(b.h(".ssh")) || !isRealDir(b.h(".secretsdir")) {
		t.Error(".ssh or .secretsdir is not a real folder")
	}
	if exists(b.h(".ssh/id_ed25519.age")) {
		t.Error("the sealed file was linked")
	}
}

func TestAutolinkAndPrivateShareAFolder(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{
		"autolink/common/ssh/config":    "Host *",
		"private/common/ssh/id_ed25519": "key",
	})
	b.linkAndCheck()
	if got := b.readlink(b.h(".ssh/config")); got != b.r("autolink/common/ssh/config") {
		t.Errorf("config points to %s", got)
	}
	if got := b.readlink(b.h(".ssh/id_ed25519")); got != b.r("private/common/ssh/id_ed25519") {
		t.Errorf("id_ed25519 points to %s", got)
	}
}

func TestFilesWrittenIntoAFoldedFolderLandInTheRepo(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/config/app/a": "a"})
	b.linkAndCheck()
	must(t, os.WriteFile(b.h(".config/app/new-state.json"), []byte("{}"), 0o644))
	if !exists(b.r("autolink/common/config/app/new-state.json")) {
		t.Error("the new file is not in the repo")
	}
	must(t, os.Mkdir(b.h(".config/other"), 0o777))
	for _, op := range b.plan() {
		if op.Kind != OK {
			t.Errorf("op %+v is not ok", op)
		}
	}
}

// The exact wording and order of what the plan reports.

func TestLockedSecretsAreListedAfterWhatIsProvided(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{
		"private/common/zz.age":      "enc",
		"private/common/aa.age":      "enc",
		"private/linux/aa.age":       "enc",
		"private/common/mm":          "plain",
		"private/linux/provided.age": "enc",
		"autolink/common/provided":   "p",
	})
	p := b.plan()
	var got []string
	for _, op := range p {
		got = append(got, string(op.Kind)+" "+op.Target[len(b.Home)+1:])
	}
	// Plaintext is planned in name order; locked secrets follow in the order
	// first seen, the linux layer's copy winning for the source. A sealed name
	// another provider links is not locked.
	want := []string{"create .mm", "create .provided", "locked .aa", "locked .zz"}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("plan (-want +got):\n%s", d)
	}
	if src := p.Only(Locked)[0].Source; src != b.r("private/linux/aa") {
		t.Errorf("locked source = %s", src)
	}
}

func TestInvalidReasonsAreTheTypeScriptWords(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"both layers", map[string]string{"autolink/common/x": "1", "autolink/linux/x/y": "2"},
			[]string{"provided by both autolink/common/x and autolink/linux/x; keep it in one place"}},
		{"spelling", map[string]string{"autolink/common/(claude)/x": "x", "autolink/linux/claude/y": "y", "private/common/(claude)/z": "z"},
			[]string{`spelled "(claude)" in private/common/(claude), autolink/common/(claude) but "claude" in autolink/linux/claude; use one spelling everywhere`}},
		{"must mark", map[string]string{"autolink/common/a/(b)/c": "c"},
			[]string{`rename "a" to "(a)": it holds autolink/common/a/(b), which must not be a link, so it can't be one either`}},
		{"inner bad name", map[string]string{"autolink/common/a/(())/c": "c"},
			[]string{`autolink/common/a/(()): "(())": wrap the name in one pair of (), like "()"`}},
		{"inner file", map[string]string{"autolink/common/a/(f)": "f"},
			[]string{`autolink/common/a/(f): () marks folders only: rename "(f)" to "f"`}},
		{"file in parens", map[string]string{"autolink/common/(f)": "f"},
			[]string{`() marks folders only: rename "(f)" to ".f"`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newBox(t)
			b.repo(c.files)
			if d := cmp.Diff(c.want, reasons(b.plan())); d != "" {
				t.Errorf("reasons (-want +got):\n%s", d)
			}
		})
	}
}

func TestConflictReasonsAreTheTypeScriptWords(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{
		"home/.home/autolink/common/thing/inner": "x",
		"home/.home/autolink/common/other":       "y",
		"home/.home/autolink/common/differ":      "repo",
		"home/.home/autolink/common/link":        "l",
		"home/.home/autolink/common/dirlink/a":   "a",
		"home/.thing":                            "a file",
		"home/.other/inside":                     "z",
		"home/.differ":                           "mine",
		"home/elsewhere":                         "e",
	})
	must(t, os.Symlink(b.h("elsewhere"), b.h(".link")))
	must(t, os.Symlink("elsewhere", b.h(".dirlink")))
	got := map[string]string{}
	for _, op := range b.plan().Only(Conflict) {
		got[op.Target[len(b.Home)+1:]] = op.Reason
	}
	want := map[string]string{
		".thing":   "is a file, but the repo has a folder here",
		".other":   "is a folder, but the repo has a file here",
		".differ":  "already exists with different content",
		".link":    "is a symlink to " + b.h("elsewhere") + ", outside this repo",
		".dirlink": "is a symlink to " + b.h("elsewhere") + ", outside this repo",
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("reasons (-want +got):\n%s", d)
	}
}

func TestInvalidOpsUseTheRepoSpelling(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/(.dotted)/a": "a", "autolink/linux/()/a": "a"})
	p := b.plan()
	var got [][2]string
	for _, op := range p {
		got = append(got, [2]string{op.Target, op.Source})
	}
	want := [][2]string{
		{b.h("(.dotted)"), b.r("autolink/common/(.dotted)")},
		{b.h("()"), b.r("autolink/linux/()")},
	}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("targets and sources (-want +got):\n%s", d)
	}
}

// A name that maps onto the home repo, or into it, would link the repo into
// itself. The TypeScript did; homebot reports it instead.

func TestANameThatMapsOntoTheRepoIsInvalid(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
	}{
		{"a folder", map[string]string{"autolink/common/home/x": "x"}},
		{"a marked folder", map[string]string{"autolink/common/(home)/x": "x"}},
		{"a file", map[string]string{"autolink/common/home": "x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newBox(t)
			b.repo(c.files)
			b.repo(map[string]string{"autolink/common/zshrc": "z"})
			p := b.plan()
			if got := reasons(p); len(got) != 1 || got[0] != "would be linked at ~/.home, inside the home repo; rename it" {
				t.Errorf("reasons = %q", got)
			}
			b.apply(p, ApplyOptions{})
			if !isLink(b.h(".zshrc")) {
				t.Error("the rest of the repo was not linked")
			}
			for _, name := range []string{"x", "home"} {
				if exists(b.r(name)) {
					t.Errorf("%s was written into the repo", name)
				}
			}
		})
	}
}

func TestAnInvalidNameOntoTheRepoUsesTheRepoSpelling(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/home/x": "x"})
	p := b.plan()
	b.wantKinds(p, map[string]Kind{"home": Invalid})
	if p[0].Source != b.r("autolink/common/home") {
		t.Errorf("source = %s", p[0].Source)
	}
}

func TestANameOntoTheRepoIsInvalidThroughASymlinkedHome(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/home/x": "x"})
	link := filepath.Join(b.Dir, "homelink")
	must(t, os.Symlink(b.Home, link))
	b.Ctx.Home = link
	p := b.plan()
	if got := reasons(p); len(got) != 1 || got[0] != "would be linked at ~/.home, inside the home repo; rename it" {
		t.Errorf("reasons = %q", got)
	}
	b.apply(p, ApplyOptions{})
	if exists(b.r("x")) {
		t.Error("x was written into the repo")
	}
}

func TestANameNextToTheRepoIsFine(t *testing.T) {
	b := newBox(t)
	b.repo(map[string]string{"autolink/common/home-notes/x": "x", "autolink/common/homebot/y": "y"})
	b.wantKinds(b.plan(), map[string]Kind{".home-notes": Create, ".homebot": Create})
}
