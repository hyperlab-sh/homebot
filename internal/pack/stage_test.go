package pack

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// staged lists the paths git has staged in the sandbox's home repo.
func (b *box) staged() []string {
	b.t.Helper()
	return strings.Fields(b.GitIn(b.Root, "diff", "--cached", "--name-only"))
}

// untracked lists the untracked, not ignored paths of the home repo.
func (b *box) untracked() []string {
	b.t.Helper()
	return strings.Fields(b.GitIn(b.Root, "ls-files", "--others", "--exclude-standard"))
}

func TestPackStagesWhatItMovedWhenTheHomeIsARepo(t *testing.T) {
	b := newBox(t, true)
	b.GitIn(b.Root, "add", "-A")
	b.GitIn(b.Root, "commit", "-qm", "init")
	b.home(map[string]string{".config/zed/settings.json": "{}", ".config/zed/themes/x.json": "x", ".vimrc": "set nu"})

	b.pack("~/.config/zed", Options{})
	b.pack("~/.vimrc", Options{Layer: "linux"})

	want := []string{"autolink/common/config/zed/settings.json", "autolink/common/config/zed/themes/x.json", "autolink/linux/vimrc"}
	if got := b.staged(); !slices.Equal(got, want) {
		t.Errorf("staged = %q, want %q", got, want)
	}
	if got := b.untracked(); len(got) != 0 {
		t.Errorf("untracked after pack = %q, want none", got)
	}
}

func TestPackStagesOnlyWhatItMoved(t *testing.T) {
	b := newBox(t, true)
	b.GitIn(b.Root, "add", "-A")
	b.GitIn(b.Root, "commit", "-qm", "init")
	b.repo(map[string]string{"notes.txt": "mine"})
	b.home(map[string]string{".tool": "t"})

	b.pack("~/.tool", Options{})

	if got := b.staged(); !slices.Equal(got, []string{"autolink/common/tool"}) {
		t.Errorf("staged = %q, want only autolink/common/tool", got)
	}
	if got := b.untracked(); !slices.Equal(got, []string{"notes.txt"}) {
		t.Errorf("untracked = %q, want notes.txt left alone", got)
	}
}

func TestPackWorksInARepoWithNoCommitsYet(t *testing.T) {
	b := newBox(t, true)
	b.home(map[string]string{".tool": "t"})
	b.pack("~/.tool", Options{})
	if got := b.staged(); !slices.Contains(got, "autolink/common/tool") {
		t.Errorf("staged = %q, want autolink/common/tool among them", got)
	}
}

func TestPackStagesNothingOffARepo(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{".tool": "t"})
	b.pack("~/.tool", Options{})
	if _, err := os.Stat(b.r(".git")); err == nil {
		t.Error("pack made the home a git repo")
	}
}

func TestPackLeavesAPathTheRepoIgnoresUnstaged(t *testing.T) {
	b := newBox(t, true)
	b.repo(map[string]string{".gitignore": "/autolink/common/tool\n"})
	b.GitIn(b.Root, "add", "-A")
	b.GitIn(b.Root, "commit", "-qm", "init")
	b.home(map[string]string{".tool": "t"})

	r := b.pack("~/.tool", Options{})

	if got := b.staged(); len(got) != 0 {
		t.Errorf("staged = %q, want nothing", got)
	}
	if got := b.readlink(b.h(".tool")); got != r.To {
		t.Errorf("link = %q, want %q", got, r.To)
	}
}

func TestPackPrivateStagesNothing(t *testing.T) {
	b := newBox(t, true)
	testutil.TestKeys(t, b.Root)
	b.GitIn(b.Root, "add", "-A")
	b.GitIn(b.Root, "commit", "-qm", "init")
	b.home(map[string]string{".netrc": "password"})

	b.pack("~/.netrc", Options{Private: true})

	// save stages private/ itself; the plaintext is ignored there anyway.
	if got := b.staged(); len(got) != 0 {
		t.Errorf("staged = %q, want nothing", got)
	}
	if got := b.untracked(); !slices.Equal(got, []string{"private/common/netrc.age"}) {
		t.Errorf("untracked = %q, want only the sealed file", got)
	}
}

func TestPackReportsAFailedStageAfterTheMove(t *testing.T) {
	notRoot(t)
	b := newBox(t, true)
	b.GitIn(b.Root, "add", "-A")
	b.GitIn(b.Root, "commit", "-qm", "init")
	b.home(map[string]string{".tool": "t"})
	// A read-only .git keeps git add from writing the index lock.
	gitDir := b.r(".git")
	if err := os.Chmod(gitDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(gitDir, 0o755) })

	r, err := Pack(b.Ctx, "~/.tool", Options{})

	wantUserError(t, err, `^git add autolink/common/tool failed\.`)
	if r.From != b.h(".tool") || r.To != b.r(filepath.Join("autolink", "common", "tool")) {
		t.Errorf("Result = %+v, want From and To set", r)
	}
	if got := b.readlink(b.h(".tool")); got != r.To {
		t.Errorf("link = %q, want %q: the move and link stand", got, r.To)
	}
}

// clone makes dir a git repository with one commit, as git clone leaves it.
func (b *box) clone(dir string) {
	b.t.Helper()
	b.GitIn(dir, "init", "-q")
	b.GitIn(dir, "add", "-A")
	b.GitIn(dir, "commit", "-qm", "upstream")
}

// gitlinksIn lists the index entries of the home repo staged as gitlinks.
func (b *box) gitlinksIn() []string {
	b.t.Helper()
	var links []string
	for _, l := range strings.Split(b.GitIn(b.Root, "ls-files", "-s"), "\n") {
		if strings.HasPrefix(l, "160000 ") {
			links = append(links, l)
		}
	}
	return links
}

// A cloned config folder would be staged as a gitlink: a pointer whose
// files never reach the remote. pack refuses it before moving (D12).
func TestPackRefusesAFolderHoldingARepo(t *testing.T) {
	cases := map[string]struct {
		files map[string]string
		clone string // a folder to make a repo, relative to the home
		where string // the folder named in the error
	}{
		"a clone": {
			files: map[string]string{".config/nvim/init.lua": "x"},
			clone: ".config/nvim", where: "~/.config/nvim",
		},
		"a repo deeper down": {
			files: map[string]string{".config/nvim/init.lua": "x", ".config/nvim/pack/plug/x.lua": "x"},
			clone: ".config/nvim/pack/plug", where: "~/.config/nvim/pack/plug",
		},
		"a .git file (a worktree or submodule)": {
			files: map[string]string{".config/nvim/init.lua": "x", ".config/nvim/sub/.git": "gitdir: /elsewhere\n"},
			where: "~/.config/nvim/sub",
		},
		"a .GIT folder in another case": {
			files: map[string]string{".config/nvim/init.lua": "x", ".config/nvim/sub/.GIT/HEAD": "ref: refs/heads/main\n"},
			where: "~/.config/nvim/sub",
		},
	}
	for name, tc := range cases {
		for _, repo := range []bool{true, false} {
			t.Run(name, func(t *testing.T) {
				b := newBox(t, repo)
				b.home(tc.files)
				if tc.clone != "" {
					b.clone(b.h(tc.clone))
				}
				_, err := Pack(b.Ctx, "~/.config/nvim", Options{})
				wantUserError(t, err, `^`+regexp.QuoteMeta(tc.where)+` is a git repository of its own: .*its files would never reach the remote\. Remove its \.git, or leave it out of the home\.$`)
				if !b.isDir(b.h(".config/nvim")) || b.exists(b.r("autolink/common/config/nvim")) {
					t.Error("pack moved the folder before refusing")
				}
			})
		}
	}
}

func TestPackTakesAFolderNamedGitAsItself(t *testing.T) {
	b := newBox(t, true)
	b.home(map[string]string{".config/tool/git/config": "x"})
	b.pack("~/.config/tool", Options{})
	if got := b.staged(); !slices.Contains(got, "autolink/common/config/tool/git/config") {
		t.Errorf("staged = %q", got)
	}
}

// Triage stages a repo candidate through Stage; a nested repo there is
// unstaged again, never left as a gitlink (D12).
func TestStageNeverLeavesAGitlink(t *testing.T) {
	b := newBox(t, true)
	b.GitIn(b.Root, "add", "-A")
	b.GitIn(b.Root, "commit", "-qm", "init")
	b.repo(map[string]string{"vendor/lib/x.txt": "x", "vendor/notes.txt": "n"})
	b.clone(b.r("vendor/lib"))

	err := Stage(b.Ctx, "vendor", func(string) error { t.Fatal("git add failed"); return nil })

	wantUserError(t, err, `^~/\.home/vendor/lib is a git repository of its own: `)
	if got := b.gitlinksIn(); len(got) != 0 {
		t.Errorf("gitlinks left in the index: %q", got)
	}
	if got := b.staged(); !slices.Equal(got, []string{"vendor/notes.txt"}) {
		t.Errorf("staged = %q, want vendor/notes.txt only", got)
	}
}

func TestStageTakesNamesLiterally(t *testing.T) {
	b := newBox(t, true)
	b.GitIn(b.Root, "add", "-A")
	b.GitIn(b.Root, "commit", "-qm", "init")
	b.repo(map[string]string{"a*": "1", "ab": "2", "a[b]": "3", "a?": "4"})
	for _, name := range []string{"a*", "a[b]", "a?"} {
		if err := Stage(b.Ctx, name, func(s string) error { t.Fatalf("git add %s: %s", name, s); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if got := b.staged(); !slices.Equal(got, []string{"a*", "a?", "a[b]"}) {
		t.Errorf("staged = %q, want the three names only, not ab", got)
	}
}

func TestPackStagesANameWithGlobCharactersOnly(t *testing.T) {
	b := newBox(t, true)
	b.GitIn(b.Root, "add", "-A")
	b.GitIn(b.Root, "commit", "-qm", "init")
	b.repo(map[string]string{"autolink/common/toolx": "mine"})
	b.home(map[string]string{".tool?": "t"})
	b.pack("~/.tool?", Options{})
	if got := b.staged(); !slices.Equal(got, []string{"autolink/common/tool?"}) {
		t.Errorf("staged = %q, want autolink/common/tool? only", got)
	}
}

// A gitlink HEAD already holds was committed on purpose (a submodule):
// Stage leaves it staged as it is and stages the rest.
func TestStageLeavesACommittedGitlinkAlone(t *testing.T) {
	b := newBox(t, true)
	b.repo(map[string]string{"vendor/sub/x.txt": "x"})
	b.clone(b.r("vendor/sub"))
	b.GitIn(b.Root, "add", "-A")
	b.GitIn(b.Root, "commit", "-qm", "init with a gitlink")
	if len(b.gitlinksIn()) != 1 {
		t.Fatalf("the setup has no committed gitlink: %q", b.gitlinksIn())
	}
	b.repo(map[string]string{"vendor/notes.txt": "n"})

	if err := Stage(b.Ctx, "vendor", func(s string) error { t.Fatalf("git add: %s", s); return nil }); err != nil {
		t.Fatal(err)
	}
	if got := b.gitlinksIn(); len(got) != 1 {
		t.Errorf("gitlinks = %q, want the committed one kept", got)
	}
	if got := b.GitIn(b.Root, "status", "--porcelain"); got != "A  vendor/notes.txt\n" {
		t.Errorf("status =\n%s\nwant only vendor/notes.txt added", got)
	}
}

// A folder pack cannot read could hold a .git, so plain pack refuses before
// moving anything (D12), naming the folder.
func TestPackRefusesAFolderItCannotLookInto(t *testing.T) {
	notRoot(t)
	b := newBox(t, true)
	b.home(map[string]string{".config/app/x": "x", ".config/app/locked/y": "y"})
	locked := b.h(".config/app/locked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	_, err := Pack(b.Ctx, "~/.config/app", Options{})

	wantUserError(t, err, `^Can't read ~/\.config/app/locked, so homebot can't check it for a git repository of its own\. Nothing was moved; `)
	if !b.isDir(b.h(".config/app")) || b.exists(b.r("autolink/common/config/app")) {
		t.Error("pack moved the folder")
	}
}

// A nested repo where HEAD has a file is a type change, not an addition:
// Stage still never leaves the gitlink, and gives the index HEAD's file
// back instead of staging its deletion.
func TestStageNeverLeavesAGitlinkReplacingACommittedFile(t *testing.T) {
	b := newBox(t, true)
	b.repo(map[string]string{"vendor/lib": "a file"})
	b.GitIn(b.Root, "add", "-A")
	b.GitIn(b.Root, "commit", "-qm", "init")
	if err := os.Remove(b.r("vendor/lib")); err != nil {
		t.Fatal(err)
	}
	b.repo(map[string]string{"vendor/lib/x.txt": "x"})
	b.clone(b.r("vendor/lib"))

	err := Stage(b.Ctx, "vendor", func(s string) error { t.Fatalf("git add: %s", s); return nil })

	wantUserError(t, err, `^~/\.home/vendor/lib is a git repository of its own: `)
	if got := b.gitlinksIn(); len(got) != 0 {
		t.Errorf("gitlinks left in the index: %q", got)
	}
	if got := b.staged(); len(got) != 0 {
		t.Errorf("staged = %q, want nothing", got)
	}
	if got := b.GitIn(b.Root, "ls-files", "-s", "--", "vendor/lib"); !strings.HasPrefix(got, "100644 ") {
		t.Errorf("index entry = %q, want HEAD's file back", got)
	}
}

func TestPackPrivateNamesAFolderItCannotRead(t *testing.T) {
	notRoot(t)
	b := newBox(t, true)
	testutil.TestKeys(t, b.Root)
	b.home(map[string]string{".config/app/x": "x", ".config/app/locked/y": "y"})
	locked := b.h(".config/app/locked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	_, err := Pack(b.Ctx, "~/.config/app", Options{Private: true})

	wantUserError(t, err, `^Can't read ~/\.config/app/locked, so homebot can't check it `)
	if !b.isDir(b.h(".config/app")) {
		t.Error("pack moved the folder")
	}
}
