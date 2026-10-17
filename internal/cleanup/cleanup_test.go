package cleanup

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/hyperlab-sh/homebot/internal/linker"
	"github.com/hyperlab-sh/homebot/internal/state"
)

const stamp = ".homebot-backup-20261012-100000"

// --- findBackups ---

func TestFindBackupsAtTheTopOfHomeAndDeeperNextToLinks(t *testing.T) {
	b := newBox(t)
	moved := b.backedUp(
		conflict{".gitconfig", "repo", "mine"},
		conflict{".config/app/settings", "r", "m"},
	)
	if len(moved) != 2 {
		t.Fatalf("moved = %q", moved)
	}
	found := b.find()
	var paths []string
	for _, f := range found {
		paths = append(paths, f.Path)
	}
	slices.Sort(paths)
	slices.Sort(moved)
	if !slices.Equal(paths, moved) {
		t.Errorf("found %q, want %q", paths, moved)
	}
	want := Backup{Path: b.h(".gitconfig" + stamp), Target: b.h(".gitconfig"), IsDir: false}
	if !slices.Contains(found, want) {
		t.Errorf("found %+v, want it to hold %+v", found, want)
	}
}

func TestFindBackupsAFolderMovedAsideIsAFolderBackup(t *testing.T) {
	b := newBox(t)
	// ~/.config/tool links somewhere outside the repo, so --backup moves the
	// link aside whole.
	b.Files(map[string]string{
		"home/.home/autolink/common/config/tool/a": "repo",
		"elsewhere/tool/b":                         "only mine",
	})
	elsewhere := filepath.Join(b.Dir, "elsewhere/tool")
	b.symlink(elsewhere, b.h(".config/tool"))
	if moved := b.backupAndLink(); !slices.Equal(moved, []string{b.h(".config/tool" + stamp)}) {
		t.Fatalf("moved = %q", moved)
	}
	got := b.only()
	if got.Target != b.h(".config/tool") || !got.IsDir {
		t.Errorf("backup = %+v, want a folder backup of ~/.config/tool", got)
	}
	diff := backupDiff(b.Ctx, got, false)
	if want := "The backup is a link to " + elsewhere + "."; !strings.HasPrefix(diff, want) {
		t.Errorf("diff = %q, want it to start with %q", diff, want)
	}
	contains(t, diff, "-only mine")
	contains(t, diff, "+repo")
	if _, err := applyAnswer(b.Ctx, got, Delete); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(elsewhere, "b")) {
		t.Error("deleting the backup removed what its link pointed at")
	}
}

func TestFindBackupsNextToAFolderAboveALink(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{
		"home/.home/autolink/common/(config)/(deep)/tool/a": "a",
		"home/.config/deep" + stamp:                         "was a file",
	})
	b.link()
	found := b.find()
	if len(found) != 1 || found[0].Target != b.h(".config/deep") {
		t.Errorf("found %+v, want one backup of ~/.config/deep", found)
	}
}

func TestFindBackupsIgnoresLookAlikesAndFoldersTheLinksNeverTouch(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{
		"home/.home/autolink/common/zshrc":                     "z",
		"home/.zshrc.homebot-backup-old":                       "x",
		"home/.zshrc.bak":                                      "x",
		"home/Documents/report.homebot-backup-20261012-100000": "elsewhere",
	})
	if found := b.find(); len(found) != 0 {
		t.Errorf("found %+v", found)
	}
}

func TestFindBackupsNothingInAnEmptyHome(t *testing.T) {
	b := newBox(t)
	if found := b.find(); len(found) != 0 {
		t.Errorf("found %+v", found)
	}
}

func TestFindBackupsFindsNamesDothaulWrote(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{
		"home/.home/autolink/common/zshrc":           "z",
		"home/.zshrc.dothaul-backup-20250101-090000": "old",
	})
	found := b.find()
	if len(found) != 1 || found[0].Target != b.h(".zshrc") {
		t.Errorf("found %+v, want the dothaul backup of ~/.zshrc", found)
	}
}

func TestFindBackupsInUsesTheGivenPlanAndTheRecordedLinks(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{
		"home/.home/autolink/common/config/app/settings": "s",
		"home/.config/app/settings" + stamp:              "planned",
		"home/.config/gone/thing" + stamp:                "recorded",
	})
	if err := (&state.Links{Targets: []string{b.h(".config/gone/thing")}}).Save(b.Ctx); err != nil {
		t.Fatal(err)
	}
	targets := func(found []Backup) []string {
		var out []string
		for _, f := range found {
			out = append(out, f.Target)
		}
		return out
	}
	// No plan: only the recorded link's folder is looked in.
	found, err := FindBackupsIn(b.Ctx, linker.Plan{})
	if err != nil {
		t.Fatal(err)
	}
	if got := targets(found); !slices.Equal(got, []string{b.h(".config/gone/thing")}) {
		t.Errorf("without a plan: %q", got)
	}
	found, err = FindBackupsIn(b.Ctx, b.plan())
	if err != nil {
		t.Fatal(err)
	}
	if got := targets(found); !slices.Equal(got, []string{b.h(".config/app/settings"), b.h(".config/gone/thing")}) {
		t.Errorf("with the plan: %q", got)
	}
}

// --- backupDiff and repoFileFor ---

func TestBackupDiffShowsWhatOnlyTheBackupHasAndWhatOnlyTheLinkHas(t *testing.T) {
	b := newBox(t)
	moved := b.backedUp(conflict{".gitconfig", "shared\nfrom repo\n", "shared\nfrom forge\n"})
	got := b.only()
	if got.Path != moved[0] {
		t.Errorf("path = %q, want %q", got.Path, moved[0])
	}
	diff := backupDiff(b.Ctx, got, false)
	contains(t, diff, "-from forge")
	contains(t, diff, "+from repo")
	lacks(t, diff, "\x1b[")
	colored := backupDiff(b.Ctx, got, true)
	contains(t, colored, "\x1b[")
	contains(t, ansi.Strip(colored), "-from forge")
	// The header names both by ~/ paths.
	contains(t, diff, "--- ~/.gitconfig"+stamp)
}

// $HOME behind a link (another volume; every sandbox on macOS): the diff
// still names both sides by ~/ paths, and a backup that is a link reads as
// ~/... whichever way the link names home. The checks against the resolved
// folder catch a path leaking out in its resolved form, which is not the
// form the Context names.
func TestBackupDiffWithHomeBehindALink(t *testing.T) {
	t.Run("the header names both sides by ~/ paths", func(t *testing.T) {
		b := newLinkedBox(t)
		b.backedUp(conflict{".gitconfig", "shared\nfrom repo\n", "shared\nfrom forge\n"})
		diff := backupDiff(b.Ctx, b.only(), false)
		contains(t, diff, "--- ~/.gitconfig"+stamp+"\n")
		contains(t, diff, "+++ ~/.home/autolink/common/gitconfig\n")
		contains(t, diff, "-from forge")
		lacks(t, diff, b.Dir)
		lacks(t, diff, b.resolved(b.Dir))
	})
	links := []struct {
		name string
		dest func(b *box) string
		want string // what the message names; ~/stuff/tool when empty
	}{
		{"as homebot names home", func(b *box) string { return b.h("stuff/tool") }, ""},
		{"through home's real folder", func(b *box) string { return filepath.Join(b.resolved(b.Home), "stuff/tool") }, ""},
		{"relative", func(*box) string { return "../stuff/tool" }, ""},
		// An absolute destination is cleaned, as the linker reads it.
		{"with a trailing slash", func(b *box) string { return b.h("stuff/tool") + "/" }, ""},
		{"with doubled slashes", func(b *box) string { return b.Home + "//stuff//tool" }, ""},
		// The link's own destination, the first hop, not where the chain ends.
		{"to a link", func(b *box) string {
			b.symlink(b.h("stuff/tool"), b.h("hop"))
			return b.h("hop")
		}, "~/hop"},
	}
	for _, tc := range links {
		t.Run("a backup that is a link, "+tc.name, func(t *testing.T) {
			b := newLinkedBox(t)
			b.Files(map[string]string{
				"home/.home/autolink/common/config/tool/a": "repo",
				"home/stuff/tool/b":                        "only mine",
			})
			b.symlink(tc.dest(b), b.h(".config/tool"))
			b.backupAndLink()
			diff := backupDiff(b.Ctx, b.only(), false)
			want := tc.want
			if want == "" {
				want = "~/stuff/tool"
			}
			if want := "The backup is a link to " + want + ".\n"; !strings.HasPrefix(diff, want) {
				t.Errorf("diff = %q, want it to start with %q", diff, want)
			}
			lacks(t, diff, b.Dir)
			lacks(t, diff, b.resolved(b.Dir))
			contains(t, diff, "--- ~/stuff/tool/b\n")
			contains(t, diff, "+++ ~/.home/autolink/common/config/tool/a\n")
		})
	}
}

// ~/.home alone behind a link (another volume): the header still names the
// repo side ~/.home/..., the way the link names it, not by the folder it
// resolves to outside home.
func TestBackupDiffWithTheRepoBehindALink(t *testing.T) {
	t.Run("a file", func(t *testing.T) {
		b := newRepoLinkedBox(t)
		b.backedUp(conflict{".gitconfig", "shared\nfrom repo\n", "shared\nfrom forge\n"})
		diff := backupDiff(b.Ctx, b.only(), false)
		contains(t, diff, "--- ~/.gitconfig"+stamp+"\n")
		contains(t, diff, "+++ ~/.home/autolink/common/gitconfig\n")
		contains(t, diff, "-from forge")
		contains(t, diff, "+from repo")
		lacks(t, diff, b.Dir)
		lacks(t, diff, b.resolved(b.Root))
	})
	t.Run("a backup that is a link to a folder", func(t *testing.T) {
		b := newRepoLinkedBox(t)
		b.Files(map[string]string{
			"home/.home/autolink/common/config/tool/a": "repo",
			"home/stuff/tool/b":                        "only mine",
		})
		b.symlink(b.h("stuff/tool"), b.h(".config/tool"))
		b.backupAndLink()
		diff := backupDiff(b.Ctx, b.only(), false)
		contains(t, diff, "The backup is a link to ~/stuff/tool.\n")
		contains(t, diff, "--- ~/stuff/tool/b\n")
		contains(t, diff, "+++ ~/.home/autolink/common/config/tool/a\n")
		lacks(t, diff, b.Dir)
		lacks(t, diff, b.resolved(b.Root))
	})
	t.Run("a repo file that is itself a link reads as the real file", func(t *testing.T) {
		// Git would compare the link's text, not the file's lines.
		b := newRepoLinkedBox(t)
		b.Files(map[string]string{
			"home/.home/autolink/common/real": "shared\nfrom repo\n",
			"home/.gitconfig":                 "shared\nfrom forge\n",
		})
		b.symlink("real", b.r("autolink/common/gitconfig"))
		b.backupAndLink()
		diff := backupDiff(b.Ctx, b.only(), false)
		contains(t, diff, "-from forge")
		contains(t, diff, "+from repo")
		lacks(t, diff, "Subproject")
		lacks(t, diff, "new file mode 120000")
	})
}

// Each side of the header is named the way its link names it, else by its
// links resolved, whichever of the two is under $HOME: a repo or a backup's
// link destination on another volume reads by the link, and one that is
// outside $HOME by name but inside it for real reads by the real path.
func TestBackupDiffNamesEachSideByItsLinkThenByItsRealPath(t *testing.T) {
	t.Run("a repo outside home by name, inside it for real", func(t *testing.T) {
		// HOMEBOT_ROOT=/srv/dotfiles, and /srv/dotfiles links to ~/dotfiles.
		b := newOutsideRepoBox(t)
		b.backedUp(conflict{".gitconfig", "shared\nfrom repo\n", "shared\nfrom forge\n"})
		diff := backupDiff(b.Ctx, b.only(), false)
		contains(t, diff, "--- ~/.gitconfig"+stamp+"\n")
		contains(t, diff, "+++ ~/dotfiles/autolink/common/gitconfig\n")
		contains(t, diff, "-from forge")
		contains(t, diff, "+from repo")
		lacks(t, diff, b.Dir)
	})
	for name, mk := range map[string]func(*testing.T) *box{
		"a backup that links into a folder on another volume": newBox,
		"the same, with the repo on another volume too":       newRepoLinkedBox,
		"the same, with the home and the repo behind links":   newLinkedBox,
	} {
		t.Run(name, func(t *testing.T) {
			b := mk(t)
			b.Files(map[string]string{
				"home/.home/autolink/common/config/tool/a": "repo",
				"elsewhere/sync/tool/b":                    "only mine",
			})
			b.symlink(filepath.Join(b.Dir, "elsewhere/sync"), b.h("Sync"))
			b.symlink(b.h("Sync/tool"), b.h(".config/tool"))
			b.backupAndLink()
			diff := backupDiff(b.Ctx, b.only(), false)
			contains(t, diff, "The backup is a link to ~/Sync/tool.\n")
			contains(t, diff, "--- ~/Sync/tool/b\n")
			contains(t, diff, "+++ ~/.home/autolink/common/config/tool/a\n")
			lacks(t, diff, b.Dir)
			lacks(t, diff, b.resolved(b.Dir))
		})
	}
}

// linkShown gives the destination as the link names it only when that
// reaches the same file; a relative link that climbs out of a folder which
// is itself a link reads as where it really goes. The folder is behind a
// link itself, so the resolved form differs from the one the links name.
func TestLinkShown(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "link")
	if err := os.Mkdir(filepath.Join(filepath.Dir(dir), "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(filepath.Dir(dir), "real"), dir); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"far/cfg", "far/stuff", "home/stuff"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := func(dest, path string) string {
		t.Helper()
		p := filepath.Join(dir, path)
		if err := os.Symlink(dest, p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	resolved := func(p string) string {
		t.Helper()
		real, err := filepath.EvalSymlinks(p)
		if err != nil {
			t.Fatal(err)
		}
		return real
	}
	stuff := filepath.Join(dir, "home/stuff")
	if resolved(stuff) == stuff {
		t.Fatal("the test folder is not behind a link")
	}
	abs := link(stuff, "home/abs")
	if got := linkShown(abs, resolved(abs)); got != stuff {
		t.Errorf("absolute: %q, want %q", got, stuff)
	}
	rel := link("stuff", "home/rel")
	if got := linkShown(rel, resolved(rel)); got != stuff {
		t.Errorf("relative: %q, want %q", got, stuff)
	}
	// A destination written with a trailing or doubled slash reads clean.
	slash := link(stuff+"/", "home/slash")
	if got := linkShown(slash, resolved(slash)); got != stuff {
		t.Errorf("trailing slash: %q, want %q", got, stuff)
	}
	doubled := link(filepath.Join(dir, "home")+"//stuff", "home/doubled")
	if got := linkShown(doubled, resolved(doubled)); got != stuff {
		t.Errorf("doubled slash: %q, want %q", got, stuff)
	}
	// A link to a link shows the first hop, as written.
	chain := link(abs, "home/chain")
	if got := linkShown(chain, resolved(chain)); got != abs {
		t.Errorf("link to a link: %q, want its first hop %q", got, abs)
	}
	// home/.config is far/cfg; ../stuff from inside it is far/stuff, while
	// the path read as written, home/stuff, is another folder.
	link(filepath.Join(dir, "far/cfg"), "home/.config")
	climb := link("../stuff", "home/.config/tool")
	if got, want := linkShown(climb, resolved(climb)), resolved(filepath.Join(dir, "far/stuff")); got != want {
		t.Errorf("climbing out of a linked folder: %q, want %q", got, want)
	}
	if got := linkShown(filepath.Join(dir, "home/stuff"), "/x"); got != "/x" {
		t.Errorf("not a link: %q", got)
	}
}

func TestBackupDiffIdenticalFilesHaveAnEmptyDiff(t *testing.T) {
	b := newBox(t)
	b.backedUp(conflict{".x", "same", "different"})
	got := b.only()
	b.write(got.Path, "same")
	if diff := backupDiff(b.Ctx, got, false); diff != "" {
		t.Errorf("diff = %q", diff)
	}
}

func TestBackupDiffSaysSoWhenNothingIsAtTheTargetAnyMore(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{
		"home/.gone" + stamp:              "only copy",
		"home/.home/autolink/common/keep": "k",
	})
	found, err := FindBackupsIn(b.Ctx, b.plan())
	if err != nil || len(found) != 1 {
		t.Fatalf("found %+v, %v", found, err)
	}
	contains(t, backupDiff(b.Ctx, found[0], false), "this backup is the only copy")
	if dest := repoFileFor(b.Ctx, found[0]); dest != "" {
		t.Errorf("repoFileFor = %q", dest)
	}
}

func TestBackupDiffSaysSoWhenTheBackupIsAFileAndTheTargetAFolder(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{
		"home/.home/autolink/common/config/thing/a": "a",
		"home/.config/thing" + stamp:                "was a file",
	})
	b.link()
	contains(t, backupDiff(b.Ctx, b.only(), false), "is a folder now")
}

func TestRepoFileForIsTheLinksDestinationAndNoneForFoldersAndOutsideTargets(t *testing.T) {
	b := newBox(t)
	b.backedUp(conflict{".gitconfig", "r", "m"})
	got := b.only()
	// With its links resolved, as realRoot is: repoRel names it relative to
	// that (macOS's temp dir, under /var, resolves to /private/var).
	want, err := filepath.EvalSymlinks(b.r("autolink/common/gitconfig"))
	if err != nil {
		t.Fatal(err)
	}
	if dest := repoFileFor(b.Ctx, got); dest != want {
		t.Errorf("repoFileFor = %q, want %q", dest, want)
	}
	folder := got
	folder.IsDir = true
	if dest := repoFileFor(b.Ctx, folder); dest != "" {
		t.Errorf("folder: repoFileFor = %q", dest)
	}
	b.Files(map[string]string{"outside/file": "o"})
	b.symlink(filepath.Join(b.Dir, "outside/file"), b.h(".elsewhere"))
	outside := Backup{Path: b.h(".elsewhere" + stamp), Target: b.h(".elsewhere")}
	if dest := repoFileFor(b.Ctx, outside); dest != "" {
		t.Errorf("outside: repoFileFor = %q", dest)
	}
}

// --- applyAnswer ---

func TestApplyAnswerDeleteRemovesAFileBackupAndLeavesTheLinkAlone(t *testing.T) {
	b := newBox(t)
	moved := b.backedUp(conflict{".gitconfig", "r", "m"})
	done, err := applyAnswer(b.Ctx, b.only(), Delete)
	if err != nil {
		t.Fatal(err)
	}
	if got := b.say(done); got != "deleted ~/.gitconfig"+stamp {
		t.Errorf("said %q", got)
	}
	if exists(moved[0]) {
		t.Error("the backup is still there")
	}
	if got := b.read(b.h(".gitconfig")); got != "r" {
		t.Errorf("~/.gitconfig = %q", got)
	}
}

func TestApplyAnswerDeleteRemovesAFolderBackupWithEverythingInside(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{
		"home/.home/autolink/common/config/tool/a": "a",
		"home/.config/tool" + stamp + "/deep/b":    "b",
	})
	b.link()
	got := b.only()
	if _, err := applyAnswer(b.Ctx, got, Delete); err != nil {
		t.Fatal(err)
	}
	if exists(got.Path) {
		t.Error("the folder backup is still there")
	}
}

func TestApplyAnswerUseCopiesTheBackupIntoTheRepoFileAndRemovesIt(t *testing.T) {
	b := newBox(t)
	b.backedUp(conflict{".gitconfig", "repo version", "machine version"})
	done, err := applyAnswer(b.Ctx, b.only(), Use)
	if err != nil {
		t.Fatal(err)
	}
	contains(t, b.say(done), "copied into autolink/common/gitconfig")
	if got := b.read(b.r("autolink/common/gitconfig")); got != "machine version" {
		t.Errorf("repo file = %q", got)
	}
	if got := b.read(b.h(".gitconfig")); got != "machine version" {
		t.Errorf("the link shows %q", got)
	}
	if found := b.find(); len(found) != 0 {
		t.Errorf("left %+v", found)
	}
}

func TestApplyAnswerUseRefusesWithoutARepoFileAndKeepsTheBackup(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{
		"home/.home/autolink/common/keep": "k",
		"home/.gone" + stamp:              "x",
	})
	got := b.only()
	_, err := applyAnswer(b.Ctx, got, Use)
	if err == nil {
		t.Fatal("use went through")
	}
	contains(t, err.Error(), "isn't a file in the repo")
	if !exists(got.Path) {
		t.Error("the backup is gone")
	}
}

func TestApplyAnswerKeepAndQuitChangeNothing(t *testing.T) {
	b := newBox(t)
	moved := b.backedUp(conflict{".x", "r", "m"})
	got := b.only()
	for _, a := range []Answer{Keep, Quit} {
		done, err := applyAnswer(b.Ctx, got, a)
		if err != nil || done.ID() != "" {
			t.Errorf("%s: %v, %v", a, done, err)
		}
	}
	if !exists(moved[0]) {
		t.Error("the backup is gone")
	}
}

// --- Review ---

// scripted answers in order, then keep, and records each Question.
func scripted(seen *[]Question, answers ...Answer) Chooser {
	return func(_ Backup, q Question) (Answer, error) {
		if seen != nil {
			*seen = append(*seen, q)
		}
		if len(answers) == 0 {
			return Keep, nil
		}
		a := answers[0]
		answers = answers[1:]
		return a, nil
	}
}

func noPick(string, string) error { return nil }

func threeBackups(b *box) {
	b.backedUp(conflict{".a", "ra", "ma"}, conflict{".b", "rb", "mb"}, conflict{".c", "rc", "mc"})
}

func TestReviewAsksAboutEachBackupInTurnAndAppliesEachAnswer(t *testing.T) {
	b := newBox(t)
	threeBackups(b)
	var seen []Question
	r, err := Review(b.Ctx, b.find(), scripted(&seen, Delete, Keep, Use), noPick)
	if err != nil {
		t.Fatal(err)
	}
	if r != (Result{Deleted: 1, Used: 1, Kept: 1}) {
		t.Errorf("result = %+v", r)
	}
	var counts []string
	for _, q := range seen {
		counts = append(counts, strconvQ(q))
		if !q.CanUse {
			t.Errorf("%s: use is not offered", strconvQ(q))
		}
	}
	if !slices.Equal(counts, []string{"1/3", "2/3", "3/3"}) {
		t.Errorf("asked %q", counts)
	}
	contains(t, ansi.Strip(seen[0].Diff), "-ma")
	if got := b.read(b.r("autolink/common/c")); got != "mc" {
		t.Errorf("c = %q", got)
	}
	if found := b.find(); len(found) != 1 || found[0].Target != b.h(".b") {
		t.Errorf("left %+v", found)
	}
	if out := b.out(); len(out) != 2 {
		t.Errorf("printed %q, want two lines", out)
	}
}

func strconvQ(q Question) string { return strconv(q.Index) + "/" + strconv(q.Total) }

func TestReviewQuitStopsAndTheRestStayAndCountAsKept(t *testing.T) {
	b := newBox(t)
	threeBackups(b)
	r, err := Review(b.Ctx, b.find(), scripted(nil, Delete, Quit), noPick)
	if err != nil {
		t.Fatal(err)
	}
	if r != (Result{Deleted: 1, Kept: 2}) {
		t.Errorf("result = %+v", r)
	}
	if found := b.find(); len(found) != 2 {
		t.Errorf("left %+v", found)
	}
}

func TestReviewAFailedAnswerIsReportedAndTheBackupKept(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{
		"home/.home/autolink/common/keep": "k",
		"home/.gone" + stamp:              "x",
	})
	r, err := Review(b.Ctx, b.find(), scripted(nil, Use), noPick)
	if err != nil {
		t.Fatal(err)
	}
	if r != (Result{Kept: 1}) {
		t.Errorf("result = %+v", r)
	}
	out := b.out()
	if len(out) == 0 {
		t.Fatal("nothing printed")
	}
	contains(t, out[0], "isn't a file in the repo")
}

func TestReviewUseIsOfferedOnlyWhereItCanWork(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{
		"home/.home/autolink/common/keep": "k",
		"home/.gone" + stamp:              "x",
	})
	var seen []Question
	if _, err := Review(b.Ctx, b.find(), scripted(&seen, Keep), noPick); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0].CanUse {
		t.Errorf("asked %+v", seen)
	}
}

func TestReviewAChooserErrorStopsAndTheRestCountAsKept(t *testing.T) {
	b := newBox(t)
	threeBackups(b)
	calls := 0
	boom := func(Backup, Question) (Answer, error) {
		calls++
		if calls == 2 {
			return Quit, os.ErrClosed
		}
		return Delete, nil
	}
	r, err := Review(b.Ctx, b.find(), boom, noPick)
	if !errors.Is(err, os.ErrClosed) {
		t.Errorf("err = %v", err)
	}
	if r != (Result{Deleted: 1, Kept: 2}) {
		t.Errorf("result = %+v", r)
	}
}
