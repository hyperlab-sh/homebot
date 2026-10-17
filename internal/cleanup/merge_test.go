package cleanup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/hyperlab-sh/homebot/internal/msg"
)

// twoHunks is a 20-line repo file and a backup that changes lines 2 and 18:
// two separate hunks.
func twoHunks(b *box) Backup {
	b.backedUp(conflict{".cfg", lines(nil), lines(map[int]string{2: "backup 2", 18: "backup 18"})})
	return b.only()
}

// sh is a Picker that runs cmd with bash in the scratch repo, stdin as its
// input, with git pointed at that repo as TerminalPicker points it; a
// failing cmd fails the pick.
func (b *box) sh(cmd, stdin string) Picker {
	return func(dir, _ string) error {
		c := exec.Command("bash", "-c", cmd)
		c.Dir = dir
		c.Env = scratchEnv(b.Ctx.Env, dir).Environ()
		c.Stdin = strings.NewReader(stdin)
		var stderr bytes.Buffer
		c.Stderr = &stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("picker: %w: %s", err, stderr.String())
		}
		return nil
	}
}

// addP stages the first hunk and not the second.
func (b *box) addP() Picker {
	return func(dir, file string) error { return b.sh("git add -p -- "+file, "y\nn\n")(dir, file) }
}

func (b *box) merge(got Backup, pick Picker) (string, bool) {
	b.t.Helper()
	done, took, err := mergeBackup(b.Ctx, got, pick)
	if err != nil {
		b.t.Fatalf("mergeBackup: %v", err)
	}
	if took != (done.ID() != "") {
		b.t.Errorf("took = %v but said %v", took, done)
	}
	if !took {
		return "", false
	}
	return b.say(done), true
}

func TestMergeWhatIsStagedBecomesTheRepoFileAndTheBackupStays(t *testing.T) {
	b := newBox(t)
	got := twoHunks(b)
	said, _ := b.merge(got, b.addP())
	if said != "merged into autolink/common/cfg; homebot save commits it" {
		t.Errorf("said %q", said)
	}
	want := lines(map[int]string{2: "backup 2"})
	if repo := b.read(b.r("autolink/common/cfg")); repo != want {
		t.Errorf("repo file = %q", repo)
	}
	if home := b.read(b.h(".cfg")); home != want {
		t.Errorf("~/.cfg = %q", home)
	}
	if !exists(got.Path) {
		t.Error("the backup is gone")
	}
	diff := backupDiff(b.Ctx, got, false)
	contains(t, diff, "-backup 18")
	lacks(t, diff, "backup 2\n")
}

func TestMergeThePickerSeesTheReposVersionCommittedAndTheBackupInTheWorkingTree(t *testing.T) {
	b := newBox(t)
	got := twoHunks(b)
	var seen, name string
	b.merge(got, func(dir, file string) error {
		name = file
		c := exec.Command("git", "diff", "--no-color", "-U0")
		c.Dir = dir
		c.Env = b.Ctx.Env.Environ()
		out, err := c.Output()
		seen = string(out)
		return err
	})
	if name != ".cfg" {
		t.Errorf("file = %q", name)
	}
	contains(t, seen, "-line 2\n+backup 2")
	contains(t, seen, "-line 18\n+backup 18")
}

func TestMergeNothingStagedLeavesTheRepoFileAndSaysNothing(t *testing.T) {
	b := newBox(t)
	got := twoHunks(b)
	if said, took := b.merge(got, noPick); took {
		t.Errorf("took something: %q", said)
	}
	if repo := b.read(b.r("autolink/common/cfg")); repo != lines(nil) {
		t.Errorf("repo file = %q", repo)
	}
	if !exists(got.Path) {
		t.Error("the backup is gone")
	}
}

func TestMergeCommittingInThePickerCountsLikeStaging(t *testing.T) {
	b := newBox(t)
	got := twoHunks(b)
	b.merge(got, b.sh("git add -A && git commit -q -m mine", ""))
	if repo := b.read(b.r("autolink/common/cfg")); repo != lines(map[int]string{2: "backup 2", 18: "backup 18"}) {
		t.Errorf("repo file = %q", repo)
	}
}

func TestMergeBytesThatAreNotTextComeThroughUnchanged(t *testing.T) {
	b := newBox(t)
	raw := []byte{0x00, 0xff, 0xfe, 0x0a, 0xc3, 0x28, 0x0a}
	b.backedUp(conflict{".bin", "text\n", "x"})
	got := b.only()
	b.write(got.Path, string(raw))
	b.merge(got, b.sh("git add -A", ""))
	if repo := b.read(b.r("autolink/common/bin")); repo != string(raw) {
		t.Errorf("repo file = %x, want %x", repo, raw)
	}
}

func TestMergeTheUsersGitConfigCannotBreakTheScratchRepo(t *testing.T) {
	b := newBox(t)
	hooks := filepath.Join(b.Dir, "hooks")
	ran := filepath.Join(b.Dir, "fsmonitor-ran")
	// Signing with a key that is not there, a hook that always fails, a
	// global gitignore that ignores the very file merged, and an fsmonitor
	// hook that must not start in the scratch repo.
	b.Files(map[string]string{
		"hooks/pre-commit": "#!/bin/sh\nexit 1\n",
		"fsmonitor":        "#!/bin/sh\ntouch " + ran + "\nexit 1\n",
		"ignore":           ".cfg\n",
		"global.gitconfig": "[commit]\n\tgpgsign = true\n[gpg]\n\tformat = ssh\n[user]\n\tsigningkey = " +
			filepath.Join(b.Dir, "no-such-key") + "\n[core]\n\thooksPath = " + hooks +
			"\n\texcludesFile = " + filepath.Join(b.Dir, "ignore") +
			"\n\tfsmonitor = " + filepath.Join(b.Dir, "fsmonitor") + "\n",
	})
	for _, f := range []string{"hooks/pre-commit", "fsmonitor"} {
		if err := os.Chmod(filepath.Join(b.Dir, f), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"GIT_CONFIG_GLOBAL": filepath.Join(b.Dir, "global.gitconfig")})
	got := twoHunks(b)
	b.merge(got, b.sh("git status --short >/dev/null && git add -A && git commit -q -m mine", ""))
	if repo := b.read(b.r("autolink/common/cfg")); repo != lines(map[int]string{2: "backup 2", 18: "backup 18"}) {
		t.Errorf("repo file = %q", repo)
	}
	if exists(ran) {
		t.Error("the global fsmonitor hook ran in the scratch repo")
	}
}

func TestMergeLineEndingsComeThroughUnchangedWhateverTheUsersGitConverts(t *testing.T) {
	// Each way the user's git can turn CRLF into LF on the way in: settings
	// in the global config, or a template whose info/attributes git init
	// would copy into the scratch repo, where it beats every setting.
	for name, convert := range map[string]struct {
		global string
		env    func(work string) map[string]string
	}{
		"global autocrlf and attributesFile": {
			global: "[core]\n\tautocrlf = input\n\tattributesFile = $WORK/attributes\n",
		},
		"global init.templateDir": {
			global: "[init]\n\ttemplateDir = $WORK/template\n",
		},
		"GIT_TEMPLATE_DIR": {
			env: func(work string) map[string]string {
				return map[string]string{"GIT_TEMPLATE_DIR": filepath.Join(work, "template")}
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			b := newBox(t)
			crlf, edited := "a\r\nb\r\n", "a\r\nc\r\n"
			b.backedUp(conflict{".cfg", crlf, edited})
			got := b.only()
			b.Files(map[string]string{
				"attributes":               "* text eol=lf\n",
				"template/info/attributes": "* text eol=lf\n",
				"global.gitconfig":         strings.ReplaceAll(convert.global, "$WORK", b.Dir),
			})
			b.Ctx.Env = b.Ctx.Env.With(map[string]string{"GIT_CONFIG_GLOBAL": filepath.Join(b.Dir, "global.gitconfig")})
			if convert.env != nil {
				b.Ctx.Env = b.Ctx.Env.With(convert.env(b.Dir))
			}

			// Converted on the way in, the repo's own version would read as
			// a change and replace the repo file with LF endings.
			if said, took := b.merge(got, noPick); took {
				t.Errorf("took something: %q", said)
			}
			if repo := b.read(b.r("autolink/common/cfg")); repo != crlf {
				t.Errorf("repo file after taking nothing = %q", repo)
			}
			b.merge(got, b.sh("git add -A", ""))
			if repo := b.read(b.r("autolink/common/cfg")); repo != edited {
				t.Errorf("repo file after taking it all = %q, want %q", repo, edited)
			}
		})
	}
}

// A file whose own name means something to git must not mean it in the
// scratch repo: a merged .gitattributes holding `* text eol=lf` would apply
// to itself and be stored as LF, and the repo file would be rewritten.
// Pins every name git gives meaning to, in any case: only a case-folding
// volume tells the case variants apart in a merge, and the .gitignore,
// .gitmodules and .mailmap merges come through unchanged either way.
func TestScratchName(t *testing.T) {
	for name, want := range map[string]string{
		".git": "git", ".gitattributes": "gitattributes", ".gitignore": "gitignore",
		".gitmodules": "gitmodules", ".mailmap": "mailmap",
		".GIT": "GIT", ".GitAttributes": "GitAttributes", ".GITIGNORE": "GITIGNORE",
		".GitModules": "GitModules", ".MailMap": "MailMap",
		".gitmodule\u017f": "gitmodule\u017f", // ſ folds to s
		// Everything else keeps its name.
		".gitconfig": ".gitconfig", ".zshrc": ".zshrc", "cfg": "cfg", "gitattributes": "gitattributes",
		"mailmap": "mailmap", ".git-blame-ignore-revs": ".git-blame-ignore-revs", ".gitattributes.bak": ".gitattributes.bak",
		"": "",
	} {
		if got := scratchName(name); got != want {
			t.Errorf("scratchName(%q) = %q, want %q", name, got, want)
		}
	}
	// A renamed copy is never special itself.
	for _, special := range scratchSpecial {
		if got := scratchName(scratchName(special)); got != scratchName(special) {
			t.Errorf("scratchName of %q's copy = %q", special, got)
		}
		for _, other := range scratchSpecial {
			if strings.EqualFold(scratchName(special), other) {
				t.Errorf("%q becomes %q, which is special", special, scratchName(special))
			}
		}
	}
}

func TestMergeAFileGitGivesMeaningToByNameComesThroughUnchanged(t *testing.T) {
	for _, name := range []string{".gitattributes", ".GitAttributes", ".gitignore", ".gitmodules", ".mailmap", ".git"} {
		t.Run(name, func(t *testing.T) {
			b := newBox(t)
			crlf, edited := "* text eol=lf\r\n*.log\r\nb\r\n", "* text eol=lf\r\n*.log\r\nc\r\n"
			b.backedUp(conflict{name, crlf, edited})
			got := b.only()
			repo := b.r("autolink/common/" + strings.TrimPrefix(name, "."))
			if said, took := b.merge(got, noPick); took {
				t.Errorf("took something: %q", said)
			}
			if after := b.read(repo); after != crlf {
				t.Errorf("repo file after taking nothing = %q, want %q", after, crlf)
			}
			b.merge(got, b.sh("git add -A", ""))
			if after := b.read(repo); after != edited {
				t.Errorf("repo file after taking it all = %q, want %q", after, edited)
			}
		})
	}
}

// Whatever repository or settings the user's environment points git at,
// the merge works in the scratch repo alone: a decoy repo named by the
// environment is left exactly as it was.
func TestMergeAnExportedGitEnvironmentCannotSendTheMergeToAnotherRepo(t *testing.T) {
	for name, exported := range map[string]func(decoy, work string) map[string]string{
		"GIT_DIR, GIT_WORK_TREE and GIT_INDEX_FILE": func(decoy, _ string) map[string]string {
			return map[string]string{
				"GIT_DIR":        filepath.Join(decoy, ".git"),
				"GIT_WORK_TREE":  decoy,
				"GIT_INDEX_FILE": filepath.Join(decoy, ".git", "index"),
			}
		},
		// GIT_COMMON_DIR alone sends the scratch repo's config writes
		// (signing off, hooksPath), objects and refs into the decoy.
		"GIT_COMMON_DIR": func(decoy, _ string) map[string]string {
			return map[string]string{"GIT_COMMON_DIR": filepath.Join(decoy, ".git")}
		},
		// The -c settings would sign with a key that is not there and run a
		// hook that always fails, over the scratch repo's own settings.
		"GIT_COMMON_DIR and git -c settings": func(decoy, work string) map[string]string {
			return map[string]string{
				"GIT_COMMON_DIR":        filepath.Join(decoy, ".git"),
				"GIT_OBJECT_DIRECTORY":  filepath.Join(decoy, ".git", "objects"),
				"GIT_CONFIG_PARAMETERS": "'commit.gpgsign'='true' 'gpg.format'='ssh' 'user.signingkey'='" + filepath.Join(work, "no-such-key") + "'",
				"GIT_CONFIG_COUNT":      "1",
				"GIT_CONFIG_KEY_0":      "core.hooksPath",
				"GIT_CONFIG_VALUE_0":    filepath.Join(work, "hooks"),
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			b := newBox(t)
			got := twoHunks(b)
			decoy := filepath.Join(b.Dir, "decoy")
			b.Files(map[string]string{"decoy/notes": "decoy\n", "hooks/pre-commit": "#!/bin/sh\nexit 1\n"})
			if err := os.Chmod(filepath.Join(b.Dir, "hooks", "pre-commit"), 0o755); err != nil {
				t.Fatal(err)
			}
			env := b.Ctx.Env
			git := func(args ...string) string {
				t.Helper()
				c := exec.Command("git", append([]string{"-C", decoy}, args...)...)
				c.Env = env.Environ()
				out, err := c.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v: %s", args, err, out)
				}
				return string(out)
			}
			git("init", "-q")
			git("add", "notes")
			git("-c", "user.name=Decoy", "-c", "user.email=decoy@example.com", "-c", "commit.gpgsign=false",
				"commit", "-q", "-m", "decoy")
			state := func() string {
				index, err := os.ReadFile(filepath.Join(decoy, ".git", "index"))
				if err != nil {
					t.Fatal(err)
				}
				return b.read(filepath.Join(decoy, ".git", "config")) + git("for-each-ref") +
					git("log", "--format=%H %s") + git("status", "--porcelain") +
					git("count-objects", "-v") + string(index)
			}
			before := state()

			b.Ctx.Env = b.Ctx.Env.With(exported(decoy, b.Dir))
			b.Ctx.Stdin = strings.NewReader("y\nn\n")
			if _, took := b.merge(got, TerminalPicker(b.Ctx)); !took {
				t.Fatal("git add -p took nothing")
			}
			if repo := b.read(b.r("autolink/common/cfg")); repo != lines(map[int]string{2: "backup 2"}) {
				t.Errorf("repo file = %q", repo)
			}
			if after := state(); after != before {
				t.Errorf("the decoy repo changed:\n%s\nwas\n%s", after, before)
			}
		})
	}
}

func TestScratchEnvDropsWhatPointsGitElsewhereAndKeepsTheRest(t *testing.T) {
	b := newBox(t)
	env := b.Ctx.Env.With(map[string]string{
		"GIT_DIR": "/elsewhere/.git", "GIT_COMMON_DIR": "/elsewhere/.git", "GIT_NAMESPACE": "ns",
		"GIT_CONFIG": "/elsewhere/config", "GIT_CONFIG_PARAMETERS": "'a.b'='c'", "GIT_CONFIG_COUNT": "2",
		"GIT_CONFIG_KEY_0": "a.b", "GIT_CONFIG_VALUE_0": "c", "GIT_CONFIG_KEY_1": "d.e", "GIT_CONFIG_VALUE_1": "f",
		"GIT_EDITOR": "vi", "GIT_ATTR_NOSYSTEM": "0",
	})
	got := scratchEnv(env, "/scratch")
	want := map[string]string{
		"GIT_DIR": "/scratch/.git", "GIT_WORK_TREE": "/scratch", "GIT_INDEX_FILE": "/scratch/.git/index",
		"GIT_EDITOR": "vi", "GIT_ATTR_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": b.Ctx.Env.Get("GIT_CONFIG_GLOBAL"), "HOME": b.Ctx.Env.Get("HOME"),
	}
	for k, v := range want {
		if g, ok := got.Lookup(k); !ok || g != v {
			t.Errorf("%s = %q (set %v), want %q", k, g, ok, v)
		}
	}
	for _, k := range []string{"GIT_COMMON_DIR", "GIT_NAMESPACE", "GIT_CONFIG", "GIT_CONFIG_PARAMETERS",
		"GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "GIT_CONFIG_KEY_1", "GIT_CONFIG_VALUE_1"} {
		if v, ok := got.Lookup(k); ok {
			t.Errorf("%s is still set: %q", k, v)
		}
	}
}

func TestMergeRemovesScratchReposLeftBehindForOverADay(t *testing.T) {
	b := newBox(t)
	got := twoHunks(b)
	cache := b.Ctx.CacheDir
	for _, f := range []string{"cleanup-merge-old/.git/HEAD", "cleanup-merge-fresh/.git/HEAD", "other-old/keep"} {
		b.write(filepath.Join(cache, f), "x")
	}
	for name, age := range map[string]time.Duration{
		"cleanup-merge-old":   48 * time.Hour,
		"cleanup-merge-fresh": time.Hour,
		"other-old":           48 * time.Hour,
	} {
		when := b.Ctx.Now().Add(-age)
		if err := os.Chtimes(filepath.Join(cache, name), when, when); err != nil {
			t.Fatal(err)
		}
	}
	b.merge(got, noPick)
	if exists(filepath.Join(cache, "cleanup-merge-old")) {
		t.Error("the scratch repo from two days ago is still there")
	}
	for _, name := range []string{"cleanup-merge-fresh", "other-old"} {
		if !exists(filepath.Join(cache, name)) {
			t.Errorf("%s is gone", name)
		}
	}
}

func TestReviewStoppedDuringAMergeEndsQuietly(t *testing.T) {
	b := newBox(t)
	b.backedUp(conflict{".cfg", lines(nil), lines(map[int]string{2: "backup 2"})},
		conflict{".other", "a\n", "b\n"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.Ctx = b.Ctx.WithContext(ctx)
	r, err := Review(b.Ctx, b.find(), scripted(nil, Merge), func(string, string) error {
		// Ctrl-C reaches git add -p too, which exits as it is stopped.
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if r != (Result{Kept: 2}) {
		t.Errorf("result = %+v", r)
	}
	if out := b.out(); len(out) != 0 {
		t.Errorf("printed %q", out)
	}
	if repo := b.read(b.r("autolink/common/cfg")); repo != lines(nil) {
		t.Errorf("repo file = %q", repo)
	}
}

func TestMergeTheScratchRepoIsUnderTheCacheAndRemovedAfterwardsAlsoWhenThePickerFails(t *testing.T) {
	b := newBox(t)
	got := twoHunks(b)
	var scratch string
	_, _, err := mergeBackup(b.Ctx, got, func(dir, _ string) error {
		scratch = dir
		return errors.New("picker crashed")
	})
	if err == nil || !strings.Contains(err.Error(), "picker crashed") {
		t.Errorf("err = %v", err)
	}
	if scratch == "" {
		t.Fatal("the picker never ran")
	}
	if !strings.HasPrefix(scratch, b.Ctx.CacheDir+string(filepath.Separator)+"cleanup-merge-") {
		t.Errorf("scratch = %q, want it under %q", scratch, b.Ctx.CacheDir)
	}
	if exists(scratch) {
		t.Error("the scratch repo is still there")
	}
	if repo := b.read(b.r("autolink/common/cfg")); repo != lines(nil) {
		t.Errorf("repo file = %q", repo)
	}
	b.merge(got, func(dir, _ string) error { scratch = dir; return nil })
	if exists(scratch) {
		t.Error("the scratch repo is still there after a merge that took nothing")
	}
}

func TestMergeRefusesWhenThereIsNoRepoFileToMergeInto(t *testing.T) {
	b := newBox(t)
	b.Files(map[string]string{
		"home/.home/autolink/common/keep": "k",
		"home/.gone" + stamp:              "x",
	})
	_, _, err := mergeBackup(b.Ctx, b.only(), noPick)
	if err == nil || !strings.Contains(err.Error(), "nothing to merge into") {
		t.Errorf("err = %v", err)
	}
}

func TestReviewAsksAboutTheSameBackupAfterAMergeWithWhatIsLeft(t *testing.T) {
	b := newBox(t)
	twoHunks(b)
	var seen []Question
	picks := []Picker{b.addP(), noPick}
	round := 0
	r, err := Review(b.Ctx, b.find(), scripted(&seen, Merge, Merge, Delete), func(dir, file string) error {
		round++
		return picks[round-1](dir, file)
	})
	if err != nil {
		t.Fatal(err)
	}
	if r != (Result{Deleted: 1, Merged: 1}) {
		t.Errorf("result = %+v", r)
	}
	var counts []string
	for _, q := range seen {
		counts = append(counts, strconvQ(q))
	}
	if !slices.Equal(counts, []string{"1/1", "1/1", "1/1"}) {
		t.Errorf("asked %q", counts)
	}
	contains(t, ansi.Strip(seen[0].Diff), "-backup 2")
	lacks(t, ansi.Strip(seen[1].Diff), "-backup 2")
	contains(t, ansi.Strip(seen[1].Diff), "-backup 18")
	want := []string{
		"merged into autolink/common/cfg; homebot save commits it",
		"Nothing taken from the backup.",
		"deleted ~/.cfg" + stamp,
	}
	if out := b.out(); !slices.Equal(out, want) {
		t.Errorf("printed %q, want %q", out, want)
	}
	if repo := b.read(b.r("autolink/common/cfg")); repo != lines(map[int]string{2: "backup 2"}) {
		t.Errorf("repo file = %q", repo)
	}
}

func TestReviewAFailedMergeIsReportedAndTheQuestionAskedAgain(t *testing.T) {
	b := newBox(t)
	twoHunks(b)
	r, err := Review(b.Ctx, b.find(), scripted(nil, Merge, Keep), func(string, string) error {
		return errors.New("lazygit exploded")
	})
	if err != nil {
		t.Fatal(err)
	}
	if r != (Result{Kept: 1}) {
		t.Errorf("result = %+v", r)
	}
	if out := b.out(); !slices.Equal(out, []string{"lazygit exploded"}) {
		t.Errorf("printed %q", out)
	}
}

func TestPickerIsLazygitWhenItIsInstalledGitAddPOtherwise(t *testing.T) {
	b := newBox(t)
	b.WithStubs(map[string]string{"lazygit": "true"})
	if got := pickerCommand(b.Ctx, "cfg"); !slices.Equal(got, []string{"lazygit"}) {
		t.Errorf("with lazygit: %q", got)
	}
	// Only $WORK/bin on the PATH, so a lazygit the host has cannot count.
	if err := os.Remove(filepath.Join(b.Bin, "lazygit")); err != nil {
		t.Fatal(err)
	}
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": b.Bin})
	if got := pickerCommand(b.Ctx, "cfg"); !slices.Equal(got, []string{"git", "add", "-p", "--", "cfg"}) {
		t.Errorf("without lazygit: %q", got)
	}
}

// When homebot is stopped (Ctrl-C), the tool gets SIGTERM and a moment to
// clean up, so lazygit can leave the terminal as it found it; SIGKILL at once
// would not give it that.
func TestPickerToolGetsSIGTERMBeforeSIGKILLWhenHomebotIsStopped(t *testing.T) {
	b := newBox(t)
	started, trapped := filepath.Join(b.Dir, "started"), filepath.Join(b.Dir, "trapped")
	b.WithStubs(map[string]string{"lazygit": `trap 'echo term > "` + trapped + `"; exit 3' TERM
echo up > "` + started + `"
while :; do sleep 0.05; done`})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer cancel()
		for range 250 {
			if exists(started) {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	c := b.Ctx.WithContext(ctx)
	done := make(chan error, 1)
	go func() { done <- TerminalPicker(c)(t.TempDir(), "cfg") }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the picker did not stop when homebot was stopped")
	}
	if !exists(started) {
		t.Fatal("the lazygit stub never ran")
	}
	if !exists(trapped) {
		t.Error("lazygit was killed without a SIGTERM first")
	}
}

func TestMergeTheFzfLineNamesTheToolAndTheSelectOffersMerge(t *testing.T) {
	b := newBox(t)
	b.pinPath()
	b.backedUp(conflict{".gitconfig", "a\n", "b\n"})
	got := b.only()
	q := Question{Index: 1, Total: 1, Diff: backupDiff(b.Ctx, got, false), CanUse: true}

	b.WithStubs(map[string]string{"fzf": fzfRecorder("merge   pick"), "lazygit": "true"})
	if answer, err := fzfChooser(b.Ctx)(got, q); err != nil || answer != Merge {
		t.Errorf("answer = %q, %v", answer, err)
	}
	contains(t, b.stubFile("stdin"), "(lazygit)")
	contains(t, b.stubFile("args"), "merge opens lazygit.")

	if err := os.Remove(filepath.Join(b.Bin, "lazygit")); err != nil {
		t.Fatal(err)
	}
	b.WithStubs(map[string]string{"fzf": `/bin/cat > "${0%/*}/stdin"; echo keep`})
	if _, err := fzfChooser(b.Ctx)(got, q); err != nil {
		t.Fatal(err)
	}
	contains(t, b.stubFile("stdin"), "(git add -p)")

	if answer := selected(t, b, got, q, "4\n"); answer != Merge {
		t.Errorf("select: answer = %q, want merge", answer)
	}
	cannot := q
	cannot.CanUse = false
	if answer := selected(t, b, got, cannot, "4\n3\n"); answer != Quit {
		t.Errorf("select without merge: answer = %q, want quit", answer)
	}
}

func TestTerminalPickerSaysWhichKeysThenRunsTheTool(t *testing.T) {
	b := newBox(t)
	got := twoHunks(b)
	b.Ctx.Stdin = strings.NewReader("y\nn\n")
	said, took := b.merge(got, TerminalPicker(b.Ctx))
	if !took {
		t.Fatal("git add -p took nothing")
	}
	contains(t, said, "merged into autolink/common/cfg")
	if repo := b.read(b.r("autolink/common/cfg")); repo != lines(map[int]string{2: "backup 2"}) {
		t.Errorf("repo file = %q", repo)
	}
	out := ansi.Strip(b.Stdout.String())
	contains(t, out, b.say(msg.CleanupPickerGit()))
}

// The tool sees the scratch repo as homebot names it, in $PWD and pwd, also
// when $HOME sits behind a link (the scratch repo is in the cache folder
// under it): without PWD set for its folder, a shell reads the folder with
// every link resolved.
func TestTerminalPickerRunsTheToolInTheScratchRepoAsNamed(t *testing.T) {
	b := newLinkedBox(t)
	got := twoHunks(b)
	b.Ctx.Stdin = strings.NewReader("")
	log := filepath.Join(b.Dir, "lazygit.log")
	b.WithStubs(map[string]string{"lazygit": `{ echo "$PWD"; pwd; } > '` + log + `'; git add -A`})
	var dir string
	picker := TerminalPicker(b.Ctx)
	if _, took := b.merge(got, func(d, file string) error { dir = d; return picker(d, file) }); !took {
		t.Fatal("the tool staged nothing")
	}
	real, err := filepath.EvalSymlinks(filepath.Dir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if real == filepath.Dir(dir) {
		t.Fatalf("the scratch repo %s is not behind a link", dir)
	}
	if seen := b.read(log); seen != dir+"\n"+dir+"\n" {
		t.Errorf("the tool saw $PWD and pwd as\n%s\nwant %s twice", seen, dir)
	}
}
