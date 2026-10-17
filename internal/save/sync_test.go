package save_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/save"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func sync(t *testing.T, e *env, push bool) save.SyncResult {
	t.Helper()
	r, err := save.Sync(e.Ctx, save.SyncOptions{Push: push})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func commitReadme(t *testing.T, e *env, text string) {
	t.Helper()
	e.write("README.md", text)
	if _, err := save.Save(e.Ctx, save.Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestSyncPushesLocalCommits(t *testing.T) {
	e := setup(t)
	commitReadme(t, e, "edited\n")
	r := sync(t, e, true)
	eq(t, r, save.SyncResult{Reachable: true, Pushed: 1}, "result")
	eq(t, strings.TrimSpace(e.GitIn(e.remote, "log", "-1", "--format=%s")), "chore(home): save changes from testhost", "remote head")
}

func TestSyncDoesNotPushWhenToldNotTo(t *testing.T) {
	e := setup(t)
	commitReadme(t, e, "edited\n")
	eq(t, sync(t, e, false).Pushed, 0, "pushed")
	eq(t, strings.TrimSpace(e.GitIn(e.remote, "log", "-1", "--format=%s")), "init", "remote head")
}

func TestSyncFastForwardsWhenOnlyTheRemoteMoved(t *testing.T) {
	e := setup(t)
	e.otherPushes("from-other")
	eq(t, sync(t, e, true).Pulled, 1, "pulled")
	if !e.exists("from-other") {
		t.Fatal("from-other did not arrive")
	}
}

func TestSyncCountsEveryCommitItPulls(t *testing.T) {
	e := setup(t)
	other := e.otherClone()
	for _, f := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(other, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		e.GitIn(other, "add", "-A")
		e.GitIn(other, "commit", "-qm", f)
	}
	e.GitIn(other, "push", "-q")
	eq(t, sync(t, e, true).Pulled, 3, "pulled")
}

func TestSyncRebasesWhenBothMovedWithoutTouchingTheSameLines(t *testing.T) {
	e := setup(t)
	e.otherPushes("from-other")
	commitReadme(t, e, "mine\n")
	r := sync(t, e, true)
	eq(t, r.Pulled, 1, "pulled")
	eq(t, r.Pushed, 1, "pushed")
	eq(t, e.log()[:2], []string{"chore(home): save changes from testhost", "other"}, "log")
}

func TestSyncAbortsAConflictingRebaseAndLeavesTheRepoClean(t *testing.T) {
	e := setup(t)
	other := e.otherClone()
	if err := os.WriteFile(filepath.Join(other, "README.md"), []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.GitIn(other, "commit", "-qam", "other")
	e.GitIn(other, "push", "-q")
	commitReadme(t, e, "mine\n")

	_, err := save.Sync(e.Ctx, save.SyncOptions{Push: true})
	want := "This machine and the remote changed the same files. Resolve it by hand:\n  git -C " + e.Root + " pull --rebase"
	if err == nil || err.Error() != want {
		t.Fatalf("error: %v", err)
	}
	if e.exists(".git/rebase-merge") || e.exists(".git/rebase-apply") {
		t.Fatal("the rebase was not aborted")
	}
	eq(t, e.read("README.md"), "mine\n", "README")
	eq(t, strings.TrimSpace(e.GitIn(e.Root, "status", "--porcelain")), "", "status")
}

func TestSyncWaitsInsteadOfPullingOverUncommittedEdits(t *testing.T) {
	e := setup(t)
	e.otherPushes("from-other")
	e.write("README.md", "uncommitted\n")
	r := sync(t, e, true)
	if !r.Waiting || r.Pulled != 0 || r.Pushed != 0 {
		t.Fatalf("%+v", r)
	}
	eq(t, e.read("README.md"), "uncommitted\n", "README")
	if e.exists("from-other") {
		t.Fatal("pulled over uncommitted edits")
	}
}

func TestAnUnreachableRemoteIsNotAnError(t *testing.T) {
	e := setup(t)
	e.GitIn(e.Root, "remote", "set-url", "origin", filepath.Join(e.Dir, "missing.git"))
	r := sync(t, e, true)
	eq(t, r, save.SyncResult{}, "result")
	eq(t, e.state().LastSync, int64(0), "lastSync")
}

func TestARepoWithoutARemoteIsNotAnError(t *testing.T) {
	e := setup(t)
	e.GitIn(e.Root, "remote", "remove", "origin")
	commitReadme(t, e, "edited\n")
	eq(t, sync(t, e, true), save.SyncResult{}, "result")
	eq(t, e.state().LastSync, int64(0), "lastSync")
}

func TestLastSyncIsWrittenAfterAGoodFetch(t *testing.T) {
	e := setup(t)
	r := sync(t, e, true)
	eq(t, r, save.SyncResult{Reachable: true}, "result")
	eq(t, e.state().LastSync, testutil.TestNow, "lastSync")
}

func TestLastSyncIsWrittenEvenWhenNothingIsPushed(t *testing.T) {
	e := setup(t)
	sync(t, e, false)
	eq(t, e.state().LastSync, testutil.TestNow, "lastSync")
}

func TestFirstPushSetsTheUpstream(t *testing.T) {
	e := setup(t)
	e.GitIn(e.Root, "checkout", "-q", "-b", "fresh")
	commitReadme(t, e, "fresh\n")
	eq(t, sync(t, e, true).Pushed, 1, "pushed")
	eq(t, strings.TrimSpace(e.GitIn(e.Root, "rev-parse", "--abbrev-ref", "@{u}")), "origin/fresh", "upstream")
}

func TestTheFirstPushOfABranchAlwaysReportsOneCommit(t *testing.T) {
	e := setup(t)
	e.GitIn(e.Root, "checkout", "-q", "-b", "fresh")
	commitReadme(t, e, "one\n")
	commitReadme(t, e, "two\n")
	eq(t, sync(t, e, true).Pushed, 1, "pushed")
}

func TestABranchWithoutAnOriginRemoteIsNotPushed(t *testing.T) {
	e := setup(t)
	e.GitIn(e.Root, "remote", "rename", "origin", "upstream")
	e.GitIn(e.Root, "checkout", "-q", "-b", "fresh")
	commitReadme(t, e, "fresh\n")
	r := sync(t, e, true)
	eq(t, r, save.SyncResult{Reachable: true}, "result")
}

func TestAFailedPushIsAnErrorButDoesNotLookLikeARefusedCommit(t *testing.T) {
	e := setup(t)
	hook := filepath.Join(e.remote, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho nope >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	commitReadme(t, e, "changed\n")
	_, err := save.Sync(e.Ctx, save.SyncOptions{Push: true})
	if err == nil || !strings.HasPrefix(err.Error(), "Push failed.\n") {
		t.Fatalf("error: %v", err)
	}
	if e.state().Blocked != nil {
		t.Fatal("a failed push must not block")
	}
}

func TestAFailedFirstPushIsAnError(t *testing.T) {
	e := setup(t)
	hook := filepath.Join(e.remote, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	e.GitIn(e.Root, "checkout", "-q", "-b", "fresh")
	commitReadme(t, e, "fresh\n")
	if _, err := save.Sync(e.Ctx, save.SyncOptions{Push: true}); err == nil || !strings.HasPrefix(err.Error(), "Push failed.") {
		t.Fatalf("error: %v", err)
	}
}

func TestSyncWithNothingToPushPushesNothing(t *testing.T) {
	e := setup(t)
	eq(t, sync(t, e, true).Pushed, 0, "pushed")
}

// F7: a git remote that fails is an error, not "no remote".
func TestAFailedGitRemoteIsAnError(t *testing.T) {
	e := setup(t)
	commitReadme(t, e, "edited\n")
	gitFailing(t, e, "remote")
	r, err := save.Sync(e.Ctx, save.SyncOptions{Push: true})
	if err == nil || !strings.HasPrefix(err.Error(), "git remote failed.\n") {
		t.Fatalf("error: %v", err)
	}
	eq(t, r, save.SyncResult{}, "result")
	eq(t, strings.TrimSpace(e.GitIn(e.remote, "log", "-1", "--format=%s")), "init", "remote head")
}

// F7: when the conflicting rebase cannot be aborted, the error says the repo
// is still mid-rebase and how to finish, not only that both sides changed.
func TestSyncSaysWhenItCannotAbortTheRebase(t *testing.T) {
	e := setup(t)
	other := e.otherClone()
	if err := os.WriteFile(filepath.Join(other, "README.md"), []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.GitIn(other, "commit", "-qam", "other")
	e.GitIn(other, "push", "-q")
	commitReadme(t, e, "mine\n")
	gitFailing(t, e, "--abort")

	_, err := save.Sync(e.Ctx, save.SyncOptions{Push: true})
	want := "This machine and the remote changed the same files, and git could not undo its rebase: " + e.Root +
		" is still in the middle of one. Resolve the conflicts and run git rebase --continue, or give up on it with:\n  git -C " + e.Root + " rebase --abort\n"
	if err == nil || !strings.HasPrefix(err.Error(), want) || !strings.Contains(err.Error(), "stubbed failure") {
		t.Fatalf("error: %v\nwant it to start with %q", err, want)
	}
	if !e.exists(".git/rebase-merge") && !e.exists(".git/rebase-apply") {
		t.Fatal("the stub let the abort through")
	}
}
