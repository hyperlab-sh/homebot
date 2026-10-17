package triage

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// decide runs Decide and returns what it said, failing the test on an error.
func (b *box) decide(cand Candidate, a Answer) string {
	b.t.Helper()
	said, done, err := Decide(b.Ctx, cand, a)
	if err != nil {
		b.t.Fatalf("Decide(%s, %s): %v", cand.Key, a, err)
	}
	if !done {
		return ""
	}
	return voice.Plain(b.Ctx, said)
}

func home(b *box, rel string) Candidate {
	return Candidate{Key: "~/" + rel, Abs: b.h(rel), Kind: Home}
}

func repoItem(b *box, rel string) Candidate {
	return Candidate{Key: rel, Abs: b.r(rel), Kind: Repo}
}

func TestDecideTracksAndIgnoresRepoFiles(t *testing.T) {
	b := newBox(t, true)
	b.repo(map[string]string{"autolink/common/config/app/state.json": "{}", "furniture/thing": "x"})
	items := slices.DeleteFunc(b.candidates(), func(c Candidate) bool { return c.Kind != Repo })
	if len(items) != 2 {
		t.Fatalf("repo candidates = %+v, want autolink and furniture", items)
	}
	if got := b.decide(items[0], Ignore); got != "added /autolink to .gitignore" {
		t.Errorf("ignore said %q", got)
	}
	if got := b.decide(items[1], Track); got != "staged furniture" {
		t.Errorf("track said %q", got)
	}
	if got := b.read(b.r(".gitignore")); got != "/autolink\n" {
		t.Errorf(".gitignore = %q, want /autolink", got)
	}
	if got := b.staged(); !slices.Equal(got, []string{"furniture/thing"}) {
		t.Errorf("staged = %q, want furniture/thing", got)
	}
}

func TestDecideStartsTheGitignoreLineOnItsOwnLine(t *testing.T) {
	cases := map[string]string{
		"no newline at the end": "/build",
		"a newline at the end":  "/build\n",
		"CRLF":                  "/build\r\n",
	}
	for name, before := range cases {
		t.Run(name, func(t *testing.T) {
			b := newBox(t, true)
			b.repo(map[string]string{".gitignore": before, "scratch/x": "x"})
			b.decide(repoItem(b, "scratch"), Ignore)
			want := strings.TrimSuffix(before, "\n") + "\n/scratch\n"
			if got := b.read(b.r(".gitignore")); got != want {
				t.Errorf(".gitignore = %q, want %q", got, want)
			}
			if slices.Contains(b.keys(Repo), "scratch") {
				t.Error("scratch is still offered")
			}
		})
	}
}

func TestDecideCreatesAnEmptyGitignore(t *testing.T) {
	b := newBox(t, true)
	b.repo(map[string]string{".gitignore": "", "scratch/x": "x"})
	b.decide(repoItem(b, "scratch"), Ignore)
	if got := b.read(b.r(".gitignore")); got != "/scratch\n" {
		t.Errorf(".gitignore = %q, want /scratch alone", got)
	}
}

func TestDecideReportsAFailedGitAdd(t *testing.T) {
	b := newBox(t, true)
	_, done, err := Decide(b.Ctx, repoItem(b, "missing"), Track)
	wantUserError(t, err, `^git add missing failed\.\n  pathspec 'missing' did not match any files$`)
	if done {
		t.Error("done = true for a failed git add")
	}
}

func TestDecideTracksHomeThingsIntoALayer(t *testing.T) {
	b := newBox(t, true)
	b.home(map[string]string{".config/a/x": "x", ".toolrc": "t"})
	if got := b.decide(home(b, ".config/a"), Track); got != "packed ~/.config/a into autolink/common/config/a" {
		t.Errorf("track said %q", got)
	}
	if got := b.decide(home(b, ".toolrc"), TrackOS); got != "packed ~/.toolrc into autolink/linux/toolrc" {
		t.Errorf("track-os said %q", got)
	}
	for _, p := range []string{".config/a", ".toolrc"} {
		if !b.isLink(b.h(p)) {
			t.Errorf("~/%s is not a link after tracking", p)
		}
	}
	// pack stages what it moved (D5), so save commits it.
	want := []string{"autolink/common/config/a/x", "autolink/linux/toolrc"}
	if got := b.staged(); !slices.Equal(got, want) {
		t.Errorf("staged = %q, want %q", got, want)
	}
}

func TestDecidePassesOnWhatPackRefuses(t *testing.T) {
	b := newBox(t, false)
	if err := os.Symlink("/elsewhere", b.h(".foreign")); err != nil {
		t.Fatal(err)
	}
	_, _, err := Decide(b.Ctx, home(b, ".foreign"), Track)
	wantUserError(t, err, `symlink`)
}

func TestDecideIgnoresHomeThingsInDecisions(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{".config/b/x": ""})
	if got := b.decide(home(b, ".config/b"), Ignore); got != "will never ask about ~/.config/b again" {
		t.Errorf("ignore said %q", got)
	}
	if got := b.ignores(); !slices.Equal(got, []string{"~/.config/b"}) {
		t.Errorf("LoadIgnores = %q", got)
	}
	if !b.isDir(b.h(".config/b")) {
		t.Error("ignoring moved the folder")
	}
}

func TestDecideSnoozesForSevenDays(t *testing.T) {
	for _, cand := range []func(*box) Candidate{
		func(b *box) Candidate { return home(b, ".config/c") },
		func(b *box) Candidate { return repoItem(b, "notes.txt") },
	} {
		b := newBox(t, true)
		c := cand(b)
		if got := b.decide(c, Snooze); got != "snoozed "+c.Key+" for 7 days" {
			t.Errorf("snooze said %q", got)
		}
		s, err := state.LoadSnoozes(b.Ctx)
		if err != nil {
			t.Fatal(err)
		}
		until, ok := s.Until.Get(c.Key)
		if want := b.Ctx.Now().Add(7 * 24 * time.Hour).UnixMilli(); !ok || until != want {
			t.Errorf("snooze.json[%s] = %d, %v; want %d", c.Key, until, ok, want)
		}
	}
}

func TestDecideSkipAndQuitChangeNothing(t *testing.T) {
	b := newBox(t, true)
	b.home(map[string]string{".config/d/x": ""})
	for _, a := range []Answer{Skip, Quit} {
		said, done, err := Decide(b.Ctx, home(b, ".config/d"), a)
		if err != nil || done || said.ID() != "" {
			t.Errorf("Decide(%s) = %q, %v, %v; want nothing done", a, said.ID(), done, err)
		}
	}
	if !b.isDir(b.h(".config/d")) || len(b.ignores()) != 0 {
		t.Error("skip or quit changed something")
	}
	if _, err := os.Stat(state.SnoozePath(b.Ctx)); err == nil {
		t.Error("skip or quit wrote snooze.json")
	}
}

func TestDescribe(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{"file": "mine\n", "dir/a": "mine\n", "dir/sub/b": "12345"})
	if err := os.MkdirAll(b.h("big"), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(b.h("big/blob"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(11 * 1024 * 1024); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	cases := map[string]string{
		"file":    "5 B",
		"dir":     "folder, 10 B",
		"big":     "folder, 11 MB: big, probably app data",
		"missing": "gone",
	}
	for rel, want := range cases {
		if got := Describe(b.Ctx, home(b, rel)); got != want {
			t.Errorf("Describe(%s) = %q, want %q", rel, got, want)
		}
	}
}

// A nested repo found as a repo candidate is never staged as a gitlink
// (D12): tracking it is refused and the index left as it was.
func TestDecideRefusesToTrackANestedRepo(t *testing.T) {
	b := newBox(t, true)
	b.commit()
	b.repo(map[string]string{"vendor/lib/x.txt": "x"})
	lib := b.r("vendor/lib")
	b.GitIn(lib, "init", "-q")
	b.GitIn(lib, "add", "-A")
	b.GitIn(lib, "commit", "-qm", "upstream")
	var cand Candidate
	for _, c := range b.candidates() {
		if c.Kind == Repo {
			cand = c
		}
	}
	if !strings.HasPrefix(cand.Key, "vendor") {
		t.Fatalf("the nested repo is not a repo candidate: %+v", b.candidates())
	}
	_, done, err := Decide(b.Ctx, cand, Track)
	wantUserError(t, err, `^~/\.home/vendor/lib is a git repository of its own: `)
	if done {
		t.Error("done = true for a refused track")
	}
	if got := b.GitIn(b.Root, "ls-files", "-s"); strings.Contains(got, "160000 ") {
		t.Errorf("a gitlink is staged:\n%s", got)
	}
	if got := b.staged(); len(got) != 0 {
		t.Errorf("staged = %q, want nothing", got)
	}
}

func TestDecideStagesANameWithGlobCharactersOnly(t *testing.T) {
	b := newBox(t, true)
	b.commit()
	b.repo(map[string]string{"notes?": "x", "notes1": "y"})
	if got := b.decide(repoItem(b, "notes?"), Track); got != "staged notes?" {
		t.Errorf("track said %q", got)
	}
	if got := b.staged(); !slices.Equal(got, []string{"notes?"}) {
		t.Errorf("staged = %q, want notes? only", got)
	}
}

// When pack moved the thing and then failed (here staging it), the error
// says where it went, as homebot pack does.
func TestDecideSaysWhereAFailedPackMovedIt(t *testing.T) {
	notRoot(t)
	b := newBox(t, true)
	b.commit()
	b.home(map[string]string{".toolrc": "t"})
	gitDir := b.r(".git")
	if err := os.Chmod(gitDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(gitDir, 0o755) })
	_, _, err := Decide(b.Ctx, home(b, ".toolrc"), Track)
	wantUserError(t, err, `(?s)^git add autolink/common/toolrc failed\..*\nmoved ~/\.toolrc → autolink/common/toolrc$`)
	if !b.isLink(b.h(".toolrc")) {
		t.Error("~/.toolrc is not linked back")
	}
}

func TestDecideIgnoresARepoNameExactly(t *testing.T) {
	b := newBox(t, true)
	b.repo(map[string]string{"a*b/x": "", "a[1] /x": ""})
	b.decide(repoItem(b, "a*b"), Ignore)
	b.decide(repoItem(b, "a[1] "), Ignore)
	if got := b.read(b.r(".gitignore")); got != "/a\\*b\n/a\\[1]\\ \n" {
		t.Errorf(".gitignore = %q", got)
	}
	if got := b.keys(Repo); !slices.Equal(got, []string{".gitignore"}) {
		t.Errorf("offered %q, want only the new .gitignore", got)
	}
}

func TestGitignoreEscape(t *testing.T) {
	for in, want := range map[string]string{
		"plain/name": "plain/name",
		"a*b?c[d]":   `a\*b\?c\[d]`,
		`back\slash`: `back\\slash`,
		"two  ":      `two\ \ `,
		"in side":    "in side",
	} {
		if got := gitignoreEscape(in); got != want {
			t.Errorf("gitignoreEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

// A line break would split the .gitignore line into two patterns, the
// second unanchored: ignoring such a name is refused and nothing written.
func TestDecideRefusesToIgnoreANameWithALineBreak(t *testing.T) {
	for _, name := range []string{"a\nb", "a\rb", "end\n"} {
		b := newBox(t, true)
		b.repo(map[string]string{name: "x"})
		_, done, err := Decide(b.Ctx, repoItem(b, name), Ignore)
		wantUserError(t, err, `^".*" has a line break in its name, which a \.gitignore line can't hold\. Nothing was written; `)
		if done {
			t.Errorf("%q: done = true", name)
		}
		if _, err := os.Stat(b.r(".gitignore")); err == nil {
			t.Errorf("%q: .gitignore was written", name)
		}
	}
}

// The home may sit below the top of the git repository it is in. Paths
// git gives from the top must still be matched to the home's, or the
// gitlink would stay staged (D12).
func TestDecideRefusesANestedRepoWhenTheHomeIsInASubfolderOfTheRepo(t *testing.T) {
	b := newBox(t, false)
	top := filepath.Dir(b.Root)
	b.GitIn(top, "init", "-q")
	b.GitIn(top, "add", ".home")
	b.GitIn(top, "commit", "-q", "--allow-empty", "-m", "init")
	b.repo(map[string]string{"vendor/lib/x.txt": "x"})
	lib := b.r("vendor/lib")
	b.GitIn(lib, "init", "-q")
	b.GitIn(lib, "add", "-A")
	b.GitIn(lib, "commit", "-qm", "upstream")
	var cand Candidate
	for _, c := range b.candidates() {
		if c.Kind == Repo && strings.HasPrefix(c.Key, "vendor") {
			cand = c
		}
	}
	if cand.Key == "" {
		t.Fatalf("the nested repo is not a repo candidate: %+v", b.candidates())
	}

	_, _, err := Decide(b.Ctx, cand, Track)

	wantUserError(t, err, `^~/\.home/vendor/lib is a git repository of its own: `)
	if got := b.GitIn(top, "ls-files", "-s"); strings.Contains(got, "160000 ") {
		t.Errorf("a gitlink is staged:\n%s", got)
	}
	if got := b.GitIn(top, "diff", "--cached", "--name-only"); got != "" {
		t.Errorf("staged:\n%s", got)
	}
}
