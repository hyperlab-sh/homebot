package triage

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/hyperlab-sh/homebot/internal/helper"
	"github.com/hyperlab-sh/homebot/internal/linker"
	"github.com/hyperlab-sh/homebot/internal/pack"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func TestTriageListsUnmanagedThingsInWatchedFolders(t *testing.T) {
	b := newBox(t, true)
	b.home(map[string]string{".config/app/x": "", ".config/other.conf": "", ".bashrc": "", "visible": ""})
	// ~/.config is a watched container and ~/.home is the repo: neither is
	// offered; ~/visible is not a dot file, and ~/.* lists only dot files.
	want := []string{"~/.config/app", "~/.config/other.conf", "~/.bashrc"}
	if got := b.keys(Home); !slices.Equal(got, want) {
		t.Errorf("home candidates = %q, want %q", got, want)
	}
}

func TestTriageCandidatesAreAbsoluteAndInNameOrderPerPattern(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{".zz": "", ".aa": "", ".config/b": "", ".config/a": ""})
	got := b.candidates()
	want := []Candidate{
		{Key: "~/.config/a", Abs: b.h(".config/a"), Kind: Home},
		{Key: "~/.config/b", Abs: b.h(".config/b"), Kind: Home},
		{Key: "~/.aa", Abs: b.h(".aa"), Kind: Home},
		{Key: "~/.zz", Abs: b.h(".zz"), Kind: Home},
	}
	if !slices.Equal(got, want) {
		t.Errorf("candidates = %+v, want %+v", got, want)
	}
}

func TestTriageHidesManagedIgnoredAndSnoozedThings(t *testing.T) {
	b := newBox(t, true)
	b.home(map[string]string{".config/managed/a": "", ".config/ignored/a": "", ".config/snoozed/a": "", ".config/new/a": ""})
	if _, err := pack.Pack(b.Ctx, b.h(".config/managed"), pack.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := AddIgnore(b.Ctx, "~/.config/ignored"); err != nil {
		t.Fatal(err)
	}
	if err := state.Snooze(b.Ctx, "~/.config/snoozed", 7); err != nil {
		t.Fatal(err)
	}
	if got := b.keys(Home); !slices.Equal(got, []string{"~/.config/new"}) {
		t.Errorf("home candidates = %q, want only ~/.config/new", got)
	}
}

func TestTriageSnoozesExpire(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{".config/later/a": ""})
	if err := state.Snooze(b.Ctx, "~/.config/later", 7); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(b.keys(), "~/.config/later") {
		t.Fatal("a snoozed item is offered")
	}
	b.at(b.Ctx.Now().Add(7*24*time.Hour - time.Millisecond))
	if slices.Contains(b.keys(), "~/.config/later") {
		t.Error("offered a millisecond before the snooze ends")
	}
	b.at(b.Ctx.Now().Add(8 * 24 * time.Hour))
	if !slices.Contains(b.keys(), "~/.config/later") {
		t.Error("not offered 8 days later")
	}
}

func TestTriageSnoozesRepoItemsToo(t *testing.T) {
	b := newBox(t, true)
	b.repo(map[string]string{"notes.txt": "x", "other.txt": "x"})
	if err := state.Snooze(b.Ctx, "notes.txt", 7); err != nil {
		t.Fatal(err)
	}
	if got := b.keys(Repo); !slices.Equal(got, []string{"other.txt"}) {
		t.Errorf("repo candidates = %q, want only other.txt", got)
	}
}

func TestTriageCountsARealFolderHoldingLinksAsManaged(t *testing.T) {
	b := newBox(t, true)
	b.repo(map[string]string{"autolink/common/config/git/config": "", "autolink/linux/config/git/l": ""})
	plan, err := linker.PlanLinks(b.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := linker.ApplyLinks(b.Ctx, plan, linker.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if !b.isDir(b.h(".config/git")) {
		t.Fatal("~/.config/git should be a real folder holding two links")
	}
	if slices.Contains(b.keys(), "~/.config/git") {
		t.Error("~/.config/git is offered though it holds homebot's links")
	}
}

func TestTriageSkipsBackupsAndLocalFilesButNotDotLocal(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{
		".gitconfig.local":                      "[commit]\n",
		".npmrc.local":                          "x",
		".zshrc.homebot-backup-20261012-100000": "old",
		".zshrc.dothaul-backup-20261012-100000": "old",
		".local/share/x":                        "x",
		".toolrc":                               "x",
	})
	keys := b.keys()
	for _, want := range []string{"~/.local", "~/.toolrc"} {
		if !slices.Contains(keys, want) {
			t.Errorf("%s is not offered: %q", want, keys)
		}
	}
	for _, not := range []string{"~/.gitconfig.local", "~/.npmrc.local", "~/.zshrc.homebot-backup-20261012-100000", "~/.zshrc.dothaul-backup-20261012-100000"} {
		if slices.Contains(keys, not) {
			t.Errorf("%s is offered", not)
		}
	}
}

func TestTriageListsUntrackedRepoFilesButNotPrivate(t *testing.T) {
	b := newBox(t, true)
	b.repo(map[string]string{
		"autolink/common/config/app/state.json": "{}",
		"furniture/thing":                       "x",
		// save stages private/ itself: a sealed file is not news.
		"private/common/token.age": "sealed",
		"private/README.md":        "x",
	})
	if got := b.keys(Repo); !slices.Equal(got, []string{"autolink", "furniture"}) {
		t.Errorf("repo candidates = %q, want autolink and furniture", got)
	}
}

func TestTriageSkipsAWhollyUntrackedPrivateFolder(t *testing.T) {
	b := newBox(t, true)
	if err := os.RemoveAll(b.r("private")); err != nil {
		t.Fatal(err)
	}
	b.commit()
	b.repo(map[string]string{"private/common/token.age": "sealed", "private/.gitignore": testutil.PrivateGitignore, "privateer.txt": "x"})
	if got := b.keys(Repo); !slices.Equal(got, []string{"privateer.txt"}) {
		t.Errorf("repo candidates = %q, want only privateer.txt", got)
	}
}

func TestTriageOffARepoHasNoRepoCandidates(t *testing.T) {
	b := newBox(t, false)
	b.repo(map[string]string{"notes.txt": "x"})
	b.home(map[string]string{".bashrc": ""})
	if got := b.keys(); !slices.Equal(got, []string{"~/.bashrc"}) {
		t.Errorf("candidates = %q, want only ~/.bashrc", got)
	}
}

func TestTriageWatchPatternsListTheirFolder(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{
		"projects/p1/readme": "x",
		"projects/.hidden":   "x",
		"projects/notes.md":  "x",
		".config/.dotted":    "x",
		".config/plain":      "x",
	})
	b.watch("~/projects/*", "~/.config/.*")
	want := []string{"~/projects/notes.md", "~/projects/p1", "~/.config/.dotted"}
	if got := b.keys(); !slices.Equal(got, want) {
		t.Errorf("candidates = %q, want %q", got, want)
	}
}

func TestTriageWatchedFolderThatIsMissingOrAFileHasNothing(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{"projects": "a file, not a folder", ".bashrc": ""})
	b.watch("~/missing/*", "~/projects/*", "~/.*")
	if got := b.keys(); !slices.Equal(got, []string{"~/.bashrc"}) {
		t.Errorf("candidates = %q, want only ~/.bashrc", got)
	}
}

func TestTriageOffersAnItemOnceThoughTwoPatternsList(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{".config/a": ""})
	b.watch("~/.config/*", "~/.config/*")
	if got := b.keys(); !slices.Equal(got, []string{"~/.config/a"}) {
		t.Errorf("candidates = %q, want ~/.config/a once", got)
	}
}

func TestTriageNeverOffersTheFolderHoldingTheRepo(t *testing.T) {
	b := newBox(t, false)
	// The repo is ~/.home; a watch on ~/.* would list it, and ~/.home/* would
	// list inside it.
	b.watch("~/.*", "~/.home/*")
	b.home(map[string]string{".bashrc": ""})
	if got := b.keys(); !slices.Equal(got, []string{"~/.bashrc"}) {
		t.Errorf("candidates = %q, want only ~/.bashrc", got)
	}
}

func TestTriageLeavesAFolderHoldingOnlyTheHelperAlone(t *testing.T) {
	b := newBox(t, false)
	installHelper(t, b)
	if slices.Contains(b.keys(Home), "~/.config/systemd") {
		t.Error("~/.config/systemd holding only the helper is offered")
	}
}

func TestTriageOffersTheHelperFolderWithAnythingElseInIt(t *testing.T) {
	b := newBox(t, false)
	installHelper(t, b)
	b.home(map[string]string{".config/systemd/user/syncthing.service": "[Unit]\n"})
	if !slices.Contains(b.keys(Home), "~/.config/systemd") {
		t.Error("~/.config/systemd with another unit in it is not offered")
	}
}

func TestTriageOffersAnEmptyHelperFolder(t *testing.T) {
	b := newBox(t, false)
	if err := os.MkdirAll(b.h(".config/systemd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(b.keys(Home), "~/.config/systemd") {
		t.Error("an empty ~/.config/systemd is not offered")
	}
}

// installHelper writes the helper's service and timer, and the wants link,
// as homebot setup does on Linux.
func installHelper(t *testing.T, b *box) {
	t.Helper()
	files := helper.Files(b.Ctx)
	if len(files) != 3 {
		t.Fatalf("helper.Files = %q, want service, timer and wants link", files)
	}
	service, timer, wanted := files[0], files[1], files[2]
	for path, content := range map[string]string{service: "[Unit]\n", timer: "[Timer]\n"} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(wanted), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(timer, wanted); err != nil {
		t.Fatal(err)
	}
}

func TestTriageCandidatesFailOnABadDecisionsFile(t *testing.T) {
	b := newBox(t, false)
	b.repo(map[string]string{"decisions.yml": "ignore: ~/.bashrc\n"})
	_, err := Candidates(b.Ctx)
	wantUserError(t, err, `decisions\.yml: ignore must be a list of paths$`)
}

func TestHoldsOnly(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{"d/a": "", "d/sub/b": "", "e/sub/x": ""})
	if err := os.MkdirAll(b.h("empty/sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		dir     string
		allowed []string
		want    bool
	}{
		{"d", []string{b.h("d/a"), b.h("d/sub/b")}, true},
		{"d", []string{b.h("d/a")}, false},
		{"empty", nil, false},
		{"empty", []string{b.h("empty/sub/x")}, false},
		{"e", []string{b.h("e/sub/x")}, true},
	}
	for _, tc := range cases {
		if got := holdsOnly(b.h(tc.dir), tc.allowed); got != tc.want {
			t.Errorf("holdsOnly(%s, %q) = %v, want %v", tc.dir, tc.allowed, got, tc.want)
		}
	}
}

func TestHoldsOnlyCountsAnUnreadableFolderAsAStranger(t *testing.T) {
	notRoot(t)
	b := newBox(t, false)
	b.home(map[string]string{"d/a": "", "d/locked/b": ""})
	if err := os.Chmod(b.h("d/locked"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(b.h("d/locked"), 0o755) })
	if holdsOnly(b.h("d"), []string{b.h("d/a"), b.h("d/locked/b")}) {
		t.Error("holdsOnly = true for a folder it cannot read")
	}
}
