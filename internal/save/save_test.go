package save_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/save"
	"github.com/hyperlab-sh/homebot/internal/secrets"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// env is a home repo with one commit, pushed to a bare remote.
type env struct {
	*testutil.Box
	t      *testing.T
	remote string
}

func setup(t *testing.T) *env {
	t.Helper()
	sb := testutil.Sandbox(t, testutil.Options{Git: true})
	// save runs the guard's checks itself (D14), gitleaks among them.
	sb.WithStubs(map[string]string{"gitleaks": "exit 0"})
	e := &env{Box: sb, t: t, remote: filepath.Join(sb.Dir, "remote.git")}
	sb.GitIn(sb.Dir, "init", "-q", "--bare", "-b", "main", e.remote)
	e.write("README.md", "home\n")
	sb.GitIn(sb.Root, "add", "-A")
	sb.GitIn(sb.Root, "commit", "-qm", "init")
	sb.GitIn(sb.Root, "remote", "add", "origin", e.remote)
	sb.GitIn(sb.Root, "push", "-q", "-u", "origin", "main")
	return e
}

func (e *env) write(rel, content string) {
	e.t.Helper()
	p := filepath.Join(e.Root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) read(rel string) string {
	e.t.Helper()
	b, err := os.ReadFile(filepath.Join(e.Root, rel))
	if err != nil {
		e.t.Fatal(err)
	}
	return string(b)
}

func (e *env) exists(rel string) bool {
	_, err := os.Lstat(filepath.Join(e.Root, rel))
	return err == nil
}

func (e *env) log() []string {
	return strings.Split(strings.TrimSpace(e.GitIn(e.Root, "log", "--format=%s")), "\n")
}

func (e *env) state() *state.State {
	e.t.Helper()
	s, err := state.Load(e.Ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

// otherClone is a second clone of the same remote, acting as another machine.
func (e *env) otherClone() string {
	e.t.Helper()
	dir := filepath.Join(e.Dir, "other")
	e.GitIn(e.Dir, "clone", "-q", e.remote, dir)
	e.GitIn(dir, "config", "user.email", "o@example.com")
	e.GitIn(dir, "config", "user.name", "Other")
	return dir
}

// otherPushes makes the other machine push a commit adding file.
func (e *env) otherPushes(file string) {
	e.t.Helper()
	other := e.otherClone()
	if err := os.WriteFile(filepath.Join(other, file), []byte("x"), 0o644); err != nil {
		e.t.Fatal(err)
	}
	e.GitIn(other, "add", "-A")
	e.GitIn(other, "commit", "-qm", "other")
	e.GitIn(other, "push", "-q")
}

// hookRefusing installs a pre-commit hook that prints stderr and exits 1: a
// stand-in for the guard, whose output is pinned by testdata/guard.
func hookRefusing(t *testing.T, sb *testutil.Box, stderr string) {
	t.Helper()
	dir := filepath.Join(sb.Dir, "hooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncat >&2 <<'HOOK_EOF'\n" + stderr + "\nHOOK_EOF\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "pre-commit"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	sb.GitIn(sb.Root, "config", "core.hooksPath", dir)
}

func eq[T any](t *testing.T, got, want T, what string) {
	t.Helper()
	if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", want) {
		t.Fatalf("%s: got %#v, want %#v", what, got, want)
	}
}

func TestSaveCommitsTrackedChangesButLeavesNewFilesForTriage(t *testing.T) {
	e := setup(t)
	e.write("README.md", "edited\n")
	e.write("new-file", "x")
	r, err := save.Save(e.Ctx, save.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Committed {
		t.Fatal("not committed")
	}
	eq(t, r.Files, []string{"README.md"}, "files")
	eq(t, e.log()[0], "chore(home): save changes from testhost", "subject")
	if st := e.GitIn(e.Root, "status", "--porcelain"); !strings.Contains(st, "?? new-file") {
		t.Fatalf("new-file is not left untracked: %q", st)
	}
	body := e.GitIn(e.Root, "log", "-1", "--format=%b")
	eq(t, body, "1 file(s) changed on testhost (linux):\n\n- README.md\n\n", "body")
}

// Commit messages live in git history, so no persona may reach them. Today
// none of the save.commit.* ids is flagged `persona: true` and none has a
// {bot} slot, so voice.Plain would not voice them either; this test cannot
// catch a persona leaking through while that holds. It pins the stored text
// exactly under a persona that has a hostile line for every save.commit.* id,
// so flagging one of them and moving commitMessage off voice.Record, which is
// the regression it exists for, fails here instead of in a pushed commit.
func TestSaveCommitMessageStaysNeutralWithPersonaActive(t *testing.T) {
	hostile := func(e *env) {
		yes := true
		e.Ctx.StdoutTerminal = &yes
		persona.Override(e.Ctx, &persona.Voice{ID: "hostile", Name: "Hostile", Lines: map[string][]persona.Line{
			"save.commit.autosave":  {{Text: "PERSONA AUTOSAVE {host}", Lang: language.English}},
			"save.commit.manual":    {{Text: "PERSONA COMMIT {host}", Lang: language.English}},
			"save.commit.body":      {{Text: "PERSONA BODY {n} {files}", Lang: language.English}},
			"save.commit.body_item": {{Text: "PERSONA ITEM {file_path}", Lang: language.English}},
			"save.commit.body_more": {{Text: "PERSONA MORE {n}", Lang: language.English}},
		}})
	}

	t.Run("manual", func(t *testing.T) {
		e := setup(t)
		hostile(e)
		e.write("README.md", "edited\n")
		if _, err := save.Save(e.Ctx, save.Options{}); err != nil {
			t.Fatal(err)
		}
		eq(t, e.log()[0], "chore(home): save changes from testhost", "subject")
		eq(t, e.GitIn(e.Root, "log", "-1", "--format=%b"), "1 file(s) changed on testhost (linux):\n\n- README.md\n\n", "body")
	})

	t.Run("autosave", func(t *testing.T) {
		e := setup(t)
		hostile(e)
		e.write("README.md", "edited\n")
		if _, err := save.Save(e.Ctx, save.Options{Auto: true}); err != nil {
			t.Fatal(err)
		}
		eq(t, e.log()[0], "chore(autosave): sync testhost", "subject")
		eq(t, e.GitIn(e.Root, "log", "-1", "--format=%b"), "1 file(s) changed on testhost (linux):\n\n- README.md\n\n", "body")
	})

	t.Run("more than the listed files", func(t *testing.T) {
		e := setup(t)
		hostile(e)
		for i := range 52 {
			e.write(fmt.Sprintf("README.md.%02d", i), "x\n")
			e.GitIn(e.Root, "add", "-N", fmt.Sprintf("README.md.%02d", i))
		}
		if _, err := save.Save(e.Ctx, save.Options{}); err != nil {
			t.Fatal(err)
		}
		body := e.GitIn(e.Root, "log", "-1", "--format=%b")
		if !strings.HasPrefix(body, "52 file(s) changed on testhost (linux):\n\n- README.md.00\n") || !strings.Contains(body, "\n- ...and 2 more") {
			t.Errorf("body = %q", body)
		}
		if strings.Contains(body, "PERSONA") || strings.Contains(e.log()[0], "PERSONA") {
			t.Errorf("stored commit is voiced: %q / %q", e.log()[0], body)
		}
	})
}

func TestSaveRecordsDeletions(t *testing.T) {
	e := setup(t)
	if err := os.Remove(filepath.Join(e.Root, "README.md")); err != nil {
		t.Fatal(err)
	}
	r, err := save.Save(e.Ctx, save.Options{})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, r.Files, []string{"README.md"}, "files")
}

func TestAutosaveUsesItsOwnMessageAndRecordsTheTime(t *testing.T) {
	e := setup(t)
	e.write("README.md", "edited\n")
	if _, err := save.Save(e.Ctx, save.Options{Auto: true}); err != nil {
		t.Fatal(err)
	}
	eq(t, e.log()[0], "chore(autosave): sync testhost", "subject")
	if body := e.GitIn(e.Root, "log", "-1", "--format=%b"); !strings.Contains(body, "- README.md") {
		t.Fatalf("body: %q", body)
	}
	eq(t, e.state().LastAutosave, testutil.TestNow, "lastAutosave")
}

func TestManualSaveDoesNotRecordAnAutosave(t *testing.T) {
	e := setup(t)
	e.write("README.md", "edited\n")
	if _, err := save.Save(e.Ctx, save.Options{}); err != nil {
		t.Fatal(err)
	}
	eq(t, e.state().LastAutosave, int64(0), "lastAutosave")
}

func TestMessageReplacesTheSubjectAndTheBody(t *testing.T) {
	e := setup(t)
	e.write("README.md", "edited\n")
	if _, err := save.Save(e.Ctx, save.Options{Message: "feat: mine"}); err != nil {
		t.Fatal(err)
	}
	eq(t, e.log()[0], "feat: mine", "subject")
	eq(t, e.GitIn(e.Root, "log", "-1", "--format=%b"), "\n", "body")
}

func TestTheBodyListsFiftyFilesAndCountsTheRest(t *testing.T) {
	e := setup(t)
	for i := range 53 {
		e.write(fmt.Sprintf("f/%02d", i), "a")
	}
	e.GitIn(e.Root, "add", "-A")
	e.GitIn(e.Root, "commit", "-qm", "files")
	for i := range 53 {
		e.write(fmt.Sprintf("f/%02d", i), "b")
	}
	r, err := save.Save(e.Ctx, save.Options{})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, len(r.Files), 53, "files")
	body := e.GitIn(e.Root, "log", "-1", "--format=%b")
	if !strings.Contains(body, "53 file(s) changed on testhost (linux):") {
		t.Fatalf("body: %q", body)
	}
	if got := strings.Count(body, "\n- f/"); got != 50 {
		t.Fatalf("listed %d files:\n%s", got, body)
	}
	if !strings.Contains(body, "- ...and 3 more") {
		t.Fatalf("body: %q", body)
	}
}

func TestSaveCommitsSealedSecretsAndExportSnapshotsNeverPlaintext(t *testing.T) {
	e := setup(t)
	testutil.TestKeys(t, e.Root)
	e.GitIn(e.Root, "add", "private")
	e.GitIn(e.Root, "commit", "-qm", "keys")
	e.write("homebot.yml", "exports:\n  - name: list\n    out: manifests/{os}/list.txt\n    run: echo pkg\n")
	e.write("private/linux/secret", "s")
	e.write("manifests/linux/list.txt", "pkg\n")
	cfg, err := config.Load(e.Root)
	if err != nil {
		t.Fatal(err)
	}
	e.Ctx.Config = cfg
	e.GitIn(e.Root, "add", "homebot.yml")

	r, err := save.Save(e.Ctx, save.Options{Message: "feat: add things"})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, r.Sealed, 1, "sealed")
	eq(t, r.SealedPaths, []string{"linux/secret"}, "sealed paths")
	files := slices.Clone(r.Files)
	slices.Sort(files)
	eq(t, files, []string{"homebot.yml", "manifests/linux/list.txt", "private/linux/secret.age"}, "files")
	eq(t, e.log()[0], "feat: add things", "subject")
	if tracked := e.GitIn(e.Root, "ls-files"); strings.Contains(tracked, "private/linux/secret\n") {
		t.Fatalf("plaintext is tracked:\n%s", tracked)
	}
}

func TestSaveSkipsAnExportWhoseOutputDoesNotExistYet(t *testing.T) {
	e := setup(t)
	e.write("homebot.yml", "exports:\n  - name: list\n    out: manifests/{os}/list.txt\n    run: echo pkg\n")
	cfg, err := config.Load(e.Root)
	if err != nil {
		t.Fatal(err)
	}
	e.Ctx.Config = cfg
	e.GitIn(e.Root, "commit", "-qam", "exports")
	r, err := save.Save(e.Ctx, save.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Committed {
		t.Fatalf("committed %v", r.Files)
	}
}

func TestNothingToCommitIsNotAnError(t *testing.T) {
	e := setup(t)
	r, err := save.Save(e.Ctx, save.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Committed || len(r.Files) != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestASuccessfulSaveClearsAnEarlierRefusal(t *testing.T) {
	e := setup(t)
	if err := state.Update(e.Ctx, func(s *state.State) { s.Blocked = &state.Blocked{Reason: "old", At: 1} }); err != nil {
		t.Fatal(err)
	}
	e.write("README.md", "edited\n")
	if _, err := save.Save(e.Ctx, save.Options{}); err != nil {
		t.Fatal(err)
	}
	if b := e.state().Blocked; b != nil {
		t.Fatalf("still blocked: %+v", b)
	}
}

func TestARefusedCommitIsRecordedForTheInboxAndReported(t *testing.T) {
	e := setup(t)
	e.write("private/common/token", "abc")
	e.GitIn(e.Root, "add", "-f", "private/common/token")
	hookRefusing(t, e.Box, "homebot refused this commit:\n"+
		"  - private/common/token is plaintext. Only *.age files may be committed from private/. Unstage it:\n"+
		"    git rm --cached private/common/token\n")
	why := "Commit refused: private/common/token is plaintext. Only *.age files may be committed from private/. Unstage it:\n  git rm --cached private/common/token"
	_, err := save.Save(e.Ctx, save.Options{})
	if err == nil || err.Error() != why {
		t.Fatalf("error: %v", err)
	}
	// what the inbox shows: the problem and its fix, not the guard's header
	b := e.state().Blocked
	if b == nil || b.Reason != why {
		t.Fatalf("blocked: %+v", b)
	}
	eq(t, b.At, testutil.TestNow, "blocked.at")
}

func TestARefusedCommitReportsTheOtherProblemsAsDetails(t *testing.T) {
	e := setup(t)
	e.write("README.md", "edited\n")
	guard, err := os.ReadFile("../../testdata/guard/refusal-two.txt")
	if err != nil {
		t.Fatal(err)
	}
	hookRefusing(t, e.Box, strings.TrimSuffix(string(guard), "\n"))
	_, err = save.Save(e.Ctx, save.Options{})
	want := "Commit refused: private/a is plaintext. Only *.age files may be committed from private/. Unstage it:\n" +
		"  git rm --cached private/a\n" +
		"  - big.bin is 60 MB (limit 50 MB). Unstage it and add it to .gitignore."
	if err == nil || err.Error() != want {
		t.Fatalf("error: %v", err)
	}
	// only the first problem is stored
	if b := e.state().Blocked; b == nil || strings.Contains(b.Reason, "big.bin") {
		t.Fatalf("blocked: %+v", b)
	}
}

func TestARefusedCommitStillBlocks(t *testing.T) {
	e := setup(t)
	e.write("README.md", "changed\n")
	hook := filepath.Join(e.Root, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho refused >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := save.Save(e.Ctx, save.Options{})
	if err == nil || err.Error() != "Commit refused: refused" {
		t.Fatalf("error: %v", err)
	}
	if b := e.state().Blocked; b == nil || b.Reason != "Commit refused: refused" {
		t.Fatalf("blocked: %+v", b)
	}
}

func TestAnErrorFromGitAddIsAUserError(t *testing.T) {
	e := setup(t)
	e.write("README.md", "edited\n")
	// A stale lock makes git add fail.
	if err := os.WriteFile(filepath.Join(e.Root, ".git", "index.lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := save.Save(e.Ctx, save.Options{})
	// The tool's lines follow the user error in the wrapping error, so the
	// stored text is err.Error(), not the user error's own Msg.
	var uerr *fail.UserError
	if !errors.As(err, &uerr) || !strings.HasPrefix(err.Error(), "git add failed.\n  ") {
		t.Fatalf("error: %#v", err)
	}
	if e.state().Blocked != nil {
		t.Fatal("a failed git add must not look like a refused commit")
	}
}

func TestFirstSaveInARepoWithoutCommits(t *testing.T) {
	setupFresh := func(t *testing.T) (*env, string) {
		sb := testutil.Sandbox(t, testutil.Options{Git: true})
		sb.WithStubs(map[string]string{"gitleaks": "exit 0"})
		e := &env{Box: sb, t: t}
		bare := filepath.Join(sb.Dir, "bare.git")
		sb.GitIn(sb.Dir, "init", "-q", "--bare", "-b", "main", bare)
		sb.GitIn(sb.Root, "remote", "add", "origin", bare)
		e.write("autolink/common/zshrc", "z\n")
		e.write("private/common/secret", "plaintext")
		return e, bare
	}

	t.Run("save stages everything not ignored, and never plaintext private/", func(t *testing.T) {
		e, _ := setupFresh(t)
		r, err := save.Save(e.Ctx, save.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if !r.Committed {
			t.Fatal("not committed")
		}
		tracked := e.GitIn(e.Root, "ls-files")
		for _, want := range []string{"homebot.yml", "autolink/common/zshrc"} {
			if !strings.Contains(tracked, want) {
				t.Errorf("%s is not tracked:\n%s", want, tracked)
			}
		}
		if strings.Contains(tracked, "private/common/secret") {
			t.Errorf("plaintext is tracked:\n%s", tracked)
		}
	})

	// A work folder an export left behind (exports.WorkPattern) is never
	// committed, at the top of the repo or deeper.
	t.Run("work folders an export left behind are not staged", func(t *testing.T) {
		e, _ := setupFresh(t)
		e.write(".homebot-export-x/new/list.txt", "half\n")
		e.write("manifests/.homebot-export-123/old/repos/f", "old\n")
		e.write("manifests/brew.txt", "brew\n")
		e.write("manifests/keep.homebot-export-1", "not a work folder\n")
		r, err := save.Save(e.Ctx, save.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if !r.Committed {
			t.Fatal("not committed")
		}
		tracked := e.GitIn(e.Root, "ls-files")
		for _, want := range []string{"homebot.yml", "autolink/common/zshrc", "manifests/brew.txt", "manifests/keep.homebot-export-1"} {
			if !strings.Contains(tracked, want) {
				t.Errorf("%s is not tracked:\n%s", want, tracked)
			}
		}
		if strings.Contains(tracked, ".homebot-export-x") || strings.Contains(tracked, ".homebot-export-123") {
			t.Errorf("a work folder is tracked:\n%s", tracked)
		}
		for _, still := range []string{".homebot-export-x/new/list.txt", "manifests/.homebot-export-123/old/repos/f"} {
			if _, err := os.Stat(filepath.Join(e.Root, still)); err != nil {
				t.Errorf("%s is gone: %v", still, err)
			}
		}
	})

	t.Run("the pre-commit hook still runs for that first commit", func(t *testing.T) {
		e, _ := setupFresh(t)
		guard, err := os.ReadFile("../../testdata/guard/refusal-gitleaks.txt")
		if err != nil {
			t.Fatal(err)
		}
		hookRefusing(t, e.Box, strings.TrimSuffix(string(guard), "\n"))
		if _, err := save.Save(e.Ctx, save.Options{}); err == nil {
			t.Fatal("want a refusal")
		}
		b := e.state().Blocked
		if b == nil {
			t.Fatal("not recorded")
		}
		reason, _ := refusal(t, string(guard))
		eq(t, b.Reason, reason, "blocked.reason")
	})

	t.Run("sync with nothing committed does nothing and is not an error", func(t *testing.T) {
		e, _ := setupFresh(t)
		r, err := save.Sync(e.Ctx, save.SyncOptions{Push: true})
		if err != nil {
			t.Fatal(err)
		}
		if r.Pushed != 0 || r.Pulled != 0 {
			t.Fatalf("%+v", r)
		}
		if e.state().Blocked != nil {
			t.Fatal("blocked")
		}
	})

	t.Run("save then sync makes the first push", func(t *testing.T) {
		e, bare := setupFresh(t)
		if _, err := save.Save(e.Ctx, save.Options{}); err != nil {
			t.Fatal(err)
		}
		r, err := save.Sync(e.Ctx, save.SyncOptions{Push: true})
		if err != nil {
			t.Fatal(err)
		}
		eq(t, r.Pushed, 1, "pushed")
		if e.GitIn(bare, "log", "--format=%s", "main") == "" {
			t.Fatal("the remote has no commits")
		}
	})
}

func TestAFailedSealStopsBeforeStagingAndDoesNotBlock(t *testing.T) {
	e := setup(t)
	head := e.GitIn(e.Root, "rev-parse", "HEAD")
	e.write("private/recipients.txt", "not a recipient\n")
	e.write("private/common/x", "plaintext")
	e.write("README.md", "edited\n")
	_, err := save.Save(e.Ctx, save.Options{})
	if err == nil {
		t.Fatal("want a seal error")
	}
	eq(t, e.GitIn(e.Root, "rev-parse", "HEAD"), head, "HEAD")
	eq(t, e.GitIn(e.Root, "diff", "--cached", "--name-only"), "", "staged")
	if b := e.state().Blocked; b != nil {
		t.Fatalf("a failed seal must not block: %+v", b)
	}
}

// gitFailing puts a git first on PATH that fails, as git does, when its
// arguments contain match, and runs the real git otherwise.
func gitFailing(t *testing.T, e *env, match string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	e.WithStubs(map[string]string{"git": `case " $* " in *" ` + match + ` "*) echo "fatal: stubbed failure" >&2; exit 128;; esac
exec ` + real + ` "$@"`})
}

// D14: save runs the guard's checks itself before committing, so a home
// without the pre-commit hook (setup on a home that was not a repo yet, a
// tidy whose guard step failed) never commits what the guard refuses. The
// refusal reads and records as the hook's would.
func TestSaveRunsTheGuardWithoutTheHook(t *testing.T) {
	t.Run("plaintext under private/", func(t *testing.T) {
		e := setup(t)
		if out, err := exec.Command("git", "-C", e.Root, "config", "--get-all", "core.hooksPath").Output(); err == nil {
			t.Fatalf("the sandbox has a hooks path: %q", out)
		}
		head := e.GitIn(e.Root, "rev-parse", "HEAD")
		e.write("README.md", "edited\n")
		e.write("private/common/token", "abc")
		e.GitIn(e.Root, "add", "-f", "private/common/token")
		why := "Commit refused: private/common/token is plaintext. Only *.age files may be committed from private/. Unstage it:\n  git rm --cached private/common/token"
		r, err := save.Save(e.Ctx, save.Options{})
		if err == nil || err.Error() != why {
			t.Fatalf("error: %v", err)
		}
		if r.Committed {
			t.Error("Committed = true")
		}
		eq(t, e.GitIn(e.Root, "rev-parse", "HEAD"), head, "HEAD")
		if b := e.state().Blocked; b == nil || b.Reason != why {
			t.Fatalf("blocked: %+v", b)
		}
	})
	t.Run("gitleaks finds a secret", func(t *testing.T) {
		e := setup(t)
		e.WithStubs(map[string]string{"gitleaks": "echo 'Finding: token = REDACTED'; exit 99"})
		e.write("README.md", "token = x\n")
		_, err := save.Save(e.Ctx, save.Options{})
		if err == nil || !strings.HasPrefix(err.Error(), "Commit refused: gitleaks found something that looks like a secret.") {
			t.Fatalf("error: %v", err)
		}
		eq(t, e.log()[0], "init", "last commit")
	})
	t.Run("gitleaks missing", func(t *testing.T) {
		e := setup(t)
		if err := os.Remove(filepath.Join(e.Bin, "gitleaks")); err != nil {
			t.Fatal(err)
		}
		e.write("README.md", "edited\n")
		if _, err := save.Save(e.Ctx, save.Options{}); err == nil || !strings.HasPrefix(err.Error(), "Commit refused: gitleaks") {
			t.Fatalf("error: %v", err)
		}
		eq(t, e.log()[0], "init", "last commit")
	})
	t.Run("a clean commit goes through and clears blocked", func(t *testing.T) {
		e := setup(t)
		e.write("README.md", "edited\n")
		if err := state.Update(e.Ctx, func(s *state.State) { s.Blocked = &state.Blocked{Reason: "old", At: 1} }); err != nil {
			t.Fatal(err)
		}
		if r, err := save.Save(e.Ctx, save.Options{}); err != nil || !r.Committed {
			t.Fatalf("Save = %+v, %v", r, err)
		}
		if b := e.state().Blocked; b != nil {
			t.Fatalf("blocked: %+v", b)
		}
	})
}

// R1: when private/ is a folder without a .gitignore, save writes the one
// keys init and pack --private write before it stages anything, so
// plaintext stays out of git and the commit goes through. One that exists
// is never touched.
func TestSaveWritesPrivateGitignoreWhenMissing(t *testing.T) {
	t.Run("on the first save", func(t *testing.T) {
		sb := testutil.Sandbox(t, testutil.Options{Git: true})
		sb.WithStubs(map[string]string{"gitleaks": "exit 0"})
		e := &env{Box: sb, t: t}
		if err := os.Remove(filepath.Join(e.Root, "private", ".gitignore")); err != nil {
			t.Fatal(err)
		}
		e.write("private/common/netrc", "machine x password hunter2")
		r, err := save.Save(e.Ctx, save.Options{})
		if err != nil || !r.Committed {
			t.Fatalf("Save = %+v, %v", r, err)
		}
		eq(t, e.read("private/.gitignore"), secrets.PrivateGitignore, "private/.gitignore")
		tracked := e.GitIn(e.Root, "ls-files", "--", "private")
		eq(t, strings.TrimSpace(tracked), "private/.gitignore", "tracked under private/")
	})
	t.Run("on a later save", func(t *testing.T) {
		e := setup(t)
		e.GitIn(e.Root, "rm", "-q", "--cached", "private/.gitignore")
		e.GitIn(e.Root, "commit", "-qm", "drop private/.gitignore")
		if err := os.Remove(filepath.Join(e.Root, "private", ".gitignore")); err != nil {
			t.Fatal(err)
		}
		e.write("private/common/netrc", "machine x password hunter2")
		e.write("README.md", "edited\n")
		r, err := save.Save(e.Ctx, save.Options{})
		if err != nil || !r.Committed {
			t.Fatalf("Save = %+v, %v", r, err)
		}
		eq(t, e.read("private/.gitignore"), secrets.PrivateGitignore, "private/.gitignore")
		eq(t, strings.TrimSpace(e.GitIn(e.Root, "ls-files", "--", "private")), "private/.gitignore", "tracked under private/")
	})
	t.Run("an existing one is left alone", func(t *testing.T) {
		e := setup(t)
		e.write("private/.gitignore", "*\n")
		e.write("README.md", "edited\n")
		if _, err := save.Save(e.Ctx, save.Options{}); err != nil {
			t.Fatal(err)
		}
		eq(t, e.read("private/.gitignore"), "*\n", "private/.gitignore")
	})
	t.Run("no private/ at all", func(t *testing.T) {
		e := setup(t)
		if err := os.RemoveAll(filepath.Join(e.Root, "private")); err != nil {
			t.Fatal(err)
		}
		e.write("README.md", "edited\n")
		if _, err := save.Save(e.Ctx, save.Options{}); err != nil {
			t.Fatal(err)
		}
		if e.exists("private") {
			t.Error("save made private/")
		}
	})
	t.Run("private is a symlink", func(t *testing.T) {
		e := setup(t)
		vault := filepath.Join(e.Dir, "vault")
		if err := os.Rename(filepath.Join(e.Root, "private"), vault); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(vault, ".gitignore")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(vault, filepath.Join(e.Root, "private")); err != nil {
			t.Fatal(err)
		}
		_, _ = save.Save(e.Ctx, save.Options{})
		if _, err := os.Lstat(filepath.Join(vault, ".gitignore")); err == nil {
			t.Error("save wrote a .gitignore through the link")
		}
	})
}

// F7: a git diff --cached that fails is an error, never "Nothing new to
// commit.", and leaves an earlier refusal recorded.
func TestAFailedDiffIsAnErrorNotNothingToCommit(t *testing.T) {
	e := setup(t)
	e.write("README.md", "edited\n")
	if err := state.Update(e.Ctx, func(s *state.State) { s.Blocked = &state.Blocked{Reason: "earlier", At: 1} }); err != nil {
		t.Fatal(err)
	}
	gitFailing(t, e, "--name-only")
	r, err := save.Save(e.Ctx, save.Options{})
	var uerr *fail.UserError
	if !errors.As(err, &uerr) || !strings.HasPrefix(err.Error(), "git diff --cached failed.\n") || !strings.Contains(err.Error(), "stubbed failure") {
		t.Fatalf("error: %#v", err)
	}
	if r.Committed || len(r.Files) != 0 {
		t.Errorf("result: %+v", r)
	}
	eq(t, e.log()[0], "init", "last commit")
	if b := e.state().Blocked; b == nil || b.Reason != "earlier" {
		t.Fatalf("blocked: %+v, want the earlier refusal kept", b)
	}
}
