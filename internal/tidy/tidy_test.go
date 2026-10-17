package tidy_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/exports"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/tidy"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// repo is a sandbox whose home repo has one commit and a homebot on PATH, so
// the commit guard's shim lets commits through.
func repo(t *testing.T, cfg string) *testutil.Box {
	t.Helper()
	b := testutil.Sandbox(t, testutil.Options{Git: true, Config: cfg})
	// save runs the guard's checks itself (D14), gitleaks among them.
	b.WithStubs(map[string]string{"homebot": "exit 0", "gitleaks": "exit 0"})
	b.GitIn(b.Root, "add", "-A")
	b.GitIn(b.Root, "commit", "-qm", "init")
	return b
}

func run(t *testing.T, b *testutil.Box) tidy.Result {
	t.Helper()
	r, err := tidy.Run(b.Ctx, tidy.Options{})
	if err != nil {
		t.Fatalf("tidy.Run: %v", err)
	}
	return r
}

func journal(t *testing.T, b *testutil.Box) []state.Entry {
	t.Helper()
	es, err := state.ReadJournal(b.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	return es
}

func journalMsgs(t *testing.T, b *testutil.Box) []string {
	t.Helper()
	var out []string
	for _, e := range journal(t, b) {
		out = append(out, e.Msg)
	}
	return out
}

func loadState(t *testing.T, b *testutil.Box) *state.State {
	t.Helper()
	s, err := state.Load(b.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func eqStrings(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s = %q, want %q", what, got, want)
	}
}

func hooksPath(t *testing.T, b *testutil.Box) string {
	t.Helper()
	return strings.TrimSpace(b.GitIn(b.Root, "config", "--local", "--get", "core.hooksPath"))
}

// TS: tidy "seals, links, installs the guard, records what it did, and
// refreshes the cache" (the cache half is TestTidyRefreshesTheInbox).
func TestTidySealsLinksInstallsTheGuardAndRecordsIt(t *testing.T) {
	b := repo(t, "")
	testutil.TestKeys(t, b.Root)
	b.Files(map[string]string{
		"home/.home/private/common/token":  "t",
		"home/.home/autolink/common/zshrc": "z",
	})
	r := run(t, b)
	if !r.Ran {
		t.Fatal("Ran = false")
	}
	eqStrings(t, "Did", r.Did, []string{"installed the commit guard", "sealed 1 secret", "fixed 2 links"})
	if len(r.Errors) != 0 {
		t.Errorf("Errors = %+v", r.Errors)
	}
	if want := filepath.Join(b.Dir, "xdg", "data", "homebot", "hooks"); hooksPath(t, b) != want {
		t.Errorf("core.hooksPath = %q, want %q", hooksPath(t, b), want)
	}
	if !slices.Contains(journalMsgs(t, b), "sealed 1 secret") {
		t.Errorf("journal = %q", journalMsgs(t, b))
	}
	for _, e := range journal(t, b) {
		if e.Error {
			t.Errorf("journal entry %+v is an error", e)
		}
	}
	if s := loadState(t, b); s.LastTidy != testutil.TestNow || len(s.TidyErrors) != 0 {
		t.Errorf("state lastTidy = %d, tidyErrors = %+v", s.LastTidy, s.TidyErrors)
	}
	// A second run finds nothing left to do.
	if again := run(t, b); len(again.Did) != 0 {
		t.Errorf("second run Did = %q, want none", again.Did)
	}
}

// TS: tidy "never replaces real files".
func TestTidyNeverReplacesRealFiles(t *testing.T) {
	b := repo(t, "")
	b.Files(map[string]string{
		"home/.home/autolink/common/zshrc": "repo",
		"home/.zshrc":                      "mine",
	})
	run(t, b)
	got, err := os.ReadFile(filepath.Join(b.Home, ".zshrc"))
	if err != nil || string(got) != "mine" {
		t.Errorf(".zshrc = %q, %v; want the real file untouched", got, err)
	}
}

// TS: tidy "autosaves on its cadence".
func TestTidyAutosavesOnItsCadence(t *testing.T) {
	b := repo(t, "autosave:\n  commit: daily\n")
	b.Files(map[string]string{"home/.home/homebot.yml": "autosave:\n  commit: daily\n# edited\n"})
	r := run(t, b)
	if !slices.Contains(r.Did, "committed 1 file") {
		t.Fatalf("Did = %q", r.Did)
	}
	if got := strings.TrimSpace(b.GitIn(b.Root, "log", "-1", "--format=%s")); got != "chore(autosave): sync testhost" {
		t.Errorf("subject = %q", got)
	}
	if s := loadState(t, b); s.LastAutosave != testutil.TestNow {
		t.Errorf("lastAutosave = %d", s.LastAutosave)
	}
	// Just autosaved: nothing is due on the next run, even with a new edit.
	b.Files(map[string]string{"home/.home/homebot.yml": "autosave:\n  commit: daily\n# edited again\n"})
	if again := run(t, b); slices.Contains(again.Did, "committed 1 file") {
		t.Errorf("committed again right after an autosave: %q", again.Did)
	}
}

func TestTidyDoesNotAutosaveWhenOff(t *testing.T) {
	b := repo(t, "")
	b.Files(map[string]string{"home/.home/homebot.yml": "profiles:\n  default: workstation\n# edited\n"})
	r := run(t, b)
	eqStrings(t, "Did", r.Did, []string{"installed the commit guard"})
	if got := strings.TrimSpace(b.GitIn(b.Root, "log", "-1", "--format=%s")); got != "init" {
		t.Errorf("subject = %q, want init", got)
	}
}

// TS: tidy "a failing chore is recorded, and the rest still run".
func TestTidyRecordsAFailingChoreAndKeepsGoing(t *testing.T) {
	b := repo(t, "exports:\n  - name: bad\n    out: x.txt\n    run: exit 1\n")
	b.Files(map[string]string{
		"home/.home/private/recipients.txt": "not-a-key\n",
		"home/.home/private/common/a":       "plaintext",
		"home/.home/autolink/common/zshrc":  "z",
	})
	r := run(t, b)
	if len(r.Errors) != 1 || r.Errors[0].Step != "seal" {
		t.Fatalf("Errors = %+v, want one, for seal", r.Errors)
	}
	if !slices.Contains(r.Did, "fixed 2 links") {
		t.Errorf("Did = %q: the link step should still have run", r.Did)
	}
	s := loadState(t, b)
	if len(s.TidyErrors) != 1 || !strings.Contains(s.TidyErrors[0].Message, "Could not encrypt private/common/a") {
		t.Errorf("state tidyErrors = %+v", s.TidyErrors)
	}
	if s.TidyErrors[0].At != testutil.TestNow {
		t.Errorf("error at = %d", s.TidyErrors[0].At)
	}
	if e, ok := s.Exports.Get("bad"); !ok || e.OK {
		t.Errorf("exports.bad = %+v, %v; want a recorded failure", e, ok)
	}
	var failed []state.Entry
	for _, e := range journal(t, b) {
		if e.Error {
			failed = append(failed, e)
		}
	}
	if len(failed) != 1 || !strings.HasPrefix(failed[0].Msg, "seal failed: ") {
		t.Errorf("error journal entries = %+v", failed)
	}
	// The failure is cleared by the next run that succeeds.
	if err := os.Remove(filepath.Join(b.Root, "private", "recipients.txt")); err != nil {
		t.Fatal(err)
	}
	run(t, b)
	if s := loadState(t, b); len(s.TidyErrors) != 0 {
		t.Errorf("tidyErrors after a clean run = %+v", s.TidyErrors)
	}
}

func TestTidyOutsideARepoSkipsTheGitChores(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{Config: "autosave:\n  commit: hourly\n"})
	b.Files(map[string]string{"home/.home/autolink/common/zshrc": "z"})
	r := run(t, b)
	eqStrings(t, "Did", r.Did, []string{"fixed 1 link"})
	if len(r.Errors) != 0 {
		t.Errorf("Errors = %+v", r.Errors)
	}
}

func TestTidyRefreshesExportsThatAreDue(t *testing.T) {
	b := repo(t, "exports:\n  - name: list\n    out: manifests/list.txt\n    run: echo item\n")
	r := run(t, b)
	if !slices.Contains(r.Did, "refreshed manifests/list.txt") {
		t.Fatalf("Did = %q", r.Did)
	}
	// Fresh for 20 hours: the next run leaves it alone.
	if again := run(t, b); slices.ContainsFunc(again.Did, func(d string) bool { return strings.HasPrefix(d, "refreshed") }) {
		t.Errorf("second run Did = %q", again.Did)
	}
}

// fakeExports makes the export chore return results and err, until the test
// ends.
func fakeExports(t *testing.T, results []exports.Result, err error) {
	t.Helper()
	t.Cleanup(tidy.SetRunExports(func(*app.Context, exports.Options) ([]exports.Result, error) {
		return results, err
	}))
}

// userEntries are the journal entries the inbox shows: the ones that are
// not errors.
func userEntries(t *testing.T, b *testutil.Box) []string {
	t.Helper()
	var out []string
	for _, e := range journal(t, b) {
		if !e.Error {
			out = append(out, e.Msg)
		}
	}
	return out
}

// An export's warning (a work folder it could not remove) is journaled as
// the inbox shows what tidy did, after a good export and a failed one alike;
// it is not a chore failure.
func TestTidyJournalsTheExportsWarnings(t *testing.T) {
	b := repo(t, "")
	fakeExports(t, []exports.Result{
		{Name: "a", Out: "manifests/a.txt", OK: true, Changed: true, Warning: "left /x/.homebot-export-1"},
		{Name: "b", Out: "manifests/b.txt", Error: "exited with 1", Warning: "left /x/.homebot-export-2"},
		{Name: "c", Out: "manifests/c.txt", OK: true},
	}, nil)
	r := run(t, b)
	eqStrings(t, "Warnings", r.Warnings, []string{"left /x/.homebot-export-1", "left /x/.homebot-export-2"})
	if len(r.Errors) != 0 {
		t.Errorf("Errors = %+v, want none: a warning is not a failure", r.Errors)
	}
	if !slices.Contains(r.Did, "refreshed manifests/a.txt") {
		t.Errorf("Did = %q, want the export that changed", r.Did)
	}
	got := userEntries(t, b)
	for _, w := range r.Warnings {
		if !slices.Contains(got, w) {
			t.Errorf("journal entries the inbox shows = %q, want %q among them", got, w)
		}
	}
}

// The warnings of the exports that ran before one that stopped the chore are
// still journaled, next to the chore's failure.
func TestTidyJournalsTheWarningsBeforeAFailingExport(t *testing.T) {
	b := repo(t, "")
	fakeExports(t, []exports.Result{{Name: "a", Out: "manifests/a.txt", OK: true, Changed: true, Warning: "left /x/.homebot-export-1"}}, errors.New("boom"))
	r := run(t, b)
	eqStrings(t, "Warnings", r.Warnings, []string{"left /x/.homebot-export-1"})
	if len(r.Errors) != 1 || r.Errors[0].Step != "export" || r.Errors[0].Message != "boom" {
		t.Errorf("Errors = %+v, want the export chore's failure", r.Errors)
	}
	if !slices.Contains(userEntries(t, b), "left /x/.homebot-export-1") {
		t.Errorf("journal = %+v, want the warning as an entry the inbox shows", journal(t, b))
	}
}

func TestTidyWithoutExportWarningsHasNone(t *testing.T) {
	b := repo(t, "exports:\n  - name: list\n    out: manifests/list.txt\n    run: echo item\n")
	if r := run(t, b); len(r.Warnings) != 0 {
		t.Errorf("Warnings = %q, want none", r.Warnings)
	}
}

func TestTidyPushesWhatItCommits(t *testing.T) {
	b := repo(t, "autosave:\n  commit: hourly\n  push: true\n")
	remote := filepath.Join(b.Dir, "remote.git")
	b.GitIn(b.Dir, "init", "-q", "--bare", "-b", "main", remote)
	b.GitIn(b.Root, "remote", "add", "origin", remote)
	b.GitIn(b.Root, "push", "-q", "-u", "origin", "main")
	b.Files(map[string]string{"home/.home/homebot.yml": "autosave:\n  commit: hourly\n  push: true\n# edited\n"})
	r := run(t, b)
	if !slices.Contains(r.Did, "committed 1 file") || !slices.Contains(r.Did, "pushed 1 commit") {
		t.Errorf("Did = %q", r.Did)
	}
	if got := strings.TrimSpace(b.GitIn(remote, "log", "-1", "--format=%s")); got != "chore(autosave): sync testhost" {
		t.Errorf("remote head = %q", got)
	}
}

func TestAutosaveDue(t *testing.T) {
	min := int64(60_000)
	cases := []struct {
		name    string
		cadence string
		last    int64 // minutes before now; -1 means never
		want    bool
	}{
		{"off", "off", -1, false},
		{"hourly, never saved", "hourly", -1, true},
		{"hourly, 49 minutes ago", "hourly", 49, false},
		{"hourly, 50 minutes ago", "hourly", 50, true},
		{"daily, 19 hours ago", "daily", 19 * 60, false},
		{"daily, 20 hours ago", "daily", 20 * 60, true},
		{"daily, an hour ago", "daily", 60, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := testutil.Sandbox(t, testutil.Options{Config: "autosave:\n  commit: " + tc.cadence + "\n"})
			if err := b.Ctx.LocateRoot(); err != nil {
				t.Fatal(err)
			}
			if b.Ctx.Config.Autosave.Commit != config.Cadence(tc.cadence) {
				t.Fatalf("config cadence = %q", b.Ctx.Config.Autosave.Commit)
			}
			if tc.last >= 0 {
				if err := state.Update(b.Ctx, func(s *state.State) { s.LastAutosave = testutil.TestNow - tc.last*min }); err != nil {
					t.Fatal(err)
				}
			}
			if got := tidy.AutosaveDue(b.Ctx); got != tc.want {
				t.Errorf("AutosaveDue = %v, want %v", got, tc.want)
			}
		})
	}
}

func lockPath(b *testutil.Box) string { return filepath.Join(b.Ctx.StateDir, "tidy.lock") }

// holdLock takes the tidy lock the way another process would: its own open
// file description, so flock treats it as a different holder.
func holdLock(t *testing.T, b *testutil.Box) (release func()) {
	t.Helper()
	if err := os.MkdirAll(b.Ctx.StateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(lockPath(b), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	return func() { _ = f.Close() }
}

// TS: tidy "two tidies never run at once".
func TestTidyDoesNothingWhileAnotherTidyHoldsTheLock(t *testing.T) {
	b := repo(t, "")
	b.Files(map[string]string{"home/.home/autolink/common/zshrc": "z"})
	release := holdLock(t, b)
	r, err := tidy.Run(b.Ctx, tidy.Options{})
	if err != nil {
		t.Fatalf("a second tidy is not an error, got %v", err)
	}
	if r.Ran || len(r.Did) != 0 || len(r.Errors) != 0 || len(r.Items) != 0 {
		t.Errorf("Result = %+v, want the zero Result", r)
	}
	if _, err := os.Lstat(filepath.Join(b.Home, ".zshrc")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the locked-out tidy linked .zshrc (%v)", err)
	}
	for _, f := range []string{"state.json", "journal.jsonl"} {
		if _, err := os.Stat(filepath.Join(b.Ctx.StateDir, f)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the locked-out tidy wrote %s (%v)", f, err)
		}
	}
	// Once the holder lets go, tidy runs.
	release()
	if r := run(t, b); !r.Ran {
		t.Error("tidy did not run after the lock was released")
	}
}

// The plan's done-when: two concurrent tidies, one runs, one exits 0. The
// first is held inside an export until the second has been refused.
func TestTwoConcurrentTidiesOneRuns(t *testing.T) {
	b := repo(t, "")
	marks := t.TempDir()
	started, release := filepath.Join(marks, "started"), filepath.Join(marks, "release")
	yml := "exports:\n  - name: slow\n    out: slow.txt\n    run: |\n      touch '" + started + "'\n      while [ ! -e '" + release + "' ]; do sleep 0.02; done\n      echo done\n"
	b.Files(map[string]string{"home/.home/homebot.yml": yml})
	cfg, err := config.Load(b.Root)
	if err != nil {
		t.Fatal(err)
	}
	b.Ctx.Config = cfg

	type outcome struct {
		r   tidy.Result
		err error
	}
	first := make(chan outcome, 1)
	go func() {
		r, err := tidy.Run(b.Ctx, tidy.Options{})
		first <- outcome{r, err}
	}()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the first tidy never reached its export")
		}
		time.Sleep(10 * time.Millisecond)
	}

	second, err := tidy.Run(b.Ctx, tidy.Options{})
	if err != nil {
		t.Fatalf("the second tidy failed instead of exiting quietly: %v", err)
	}
	if second.Ran {
		t.Error("the second tidy ran while the first held the lock")
	}

	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case o := <-first:
		if o.err != nil || !o.r.Ran {
			t.Fatalf("the first tidy = %+v, %v; want it to have run", o.r, o.err)
		}
		if !slices.Contains(o.r.Did, "refreshed slow.txt") {
			t.Errorf("first Did = %q", o.r.Did)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the first tidy never finished")
	}
}

// A tidy.lock file left behind (by the TypeScript build, or a crash) holds
// nothing: only a live flock does, so no 30-minute stale rule is needed.
func TestTidyIgnoresALockFileNobodyHolds(t *testing.T) {
	b := repo(t, "")
	if err := os.MkdirAll(b.Ctx.StateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath(b), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lockPath(b), old, old); err != nil {
		t.Fatal(err)
	}
	if r := run(t, b); !r.Ran {
		t.Error("tidy was blocked by a lock file nobody holds")
	}
	// And it leaves the lock free for the next run.
	if r := run(t, b); !r.Ran {
		t.Error("the second sequential tidy was blocked")
	}
}

func TestTidyStopsBetweenChoresWhenCancelled(t *testing.T) {
	b := repo(t, "")
	b.Files(map[string]string{"home/.home/autolink/common/zshrc": "z"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := b.Ctx.WithContext(ctx)
	if _, err := tidy.Run(c, tidy.Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(filepath.Join(b.Ctx.StateDir, "state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a cancelled tidy wrote state.json (%v)", err)
	}
	// The lock went with it.
	if r := run(t, b); !r.Ran {
		t.Error("the lock outlived the cancelled tidy")
	}
}

// TS: tidy "refreshes the cache" (the nudge file exists after a run) and "a
// failing chore is recorded": the inbox shows both a failed seal and a failed
// export.
func TestTidyRefreshesTheInbox(t *testing.T) {
	b := repo(t, "exports:\n  - name: bad\n    out: x.txt\n    run: exit 1\n")
	b.Files(map[string]string{
		"home/.home/private/recipients.txt": "not-a-key\n",
		"home/.home/private/common/a":       "plaintext",
		"home/.home/autolink/common/zshrc":  "z",
	})
	r := run(t, b)
	if _, err := os.Stat(filepath.Join(b.Ctx.CacheDir, "nudge")); err != nil {
		t.Errorf("no nudge cache after tidy: %v", err)
	}
	var ids []string
	for _, it := range r.Items {
		ids = append(ids, it.ID)
	}
	for _, want := range []string{"tidy:seal", "export:bad"} {
		if !slices.Contains(ids, want) {
			t.Errorf("Items = %q, want %s", ids, want)
		}
	}
	if !slices.ContainsFunc(journal(t, b), func(e state.Entry) bool { return e.Error && strings.HasPrefix(e.Msg, "seal failed") }) {
		t.Errorf("journal = %q, want a seal failed entry", journalMsgs(t, b))
	}
}

// A filesystem that cannot lock gives a message that names the lock file, not
// a bare system error, and runs no chore.
func TestTidyExplainsAFilesystemThatCannotLock(t *testing.T) {
	b := repo(t, "")
	b.Files(map[string]string{"home/.home/autolink/common/zshrc": "z"})
	defer tidy.SetFlock(func(int, int) error { return syscall.ENOSYS })()
	_, err := tidy.Run(b.Ctx, tidy.Options{})
	if err == nil {
		t.Fatal("tidy ran without a lock")
	}
	lockFile := filepath.Join(b.Ctx.StateDir, "tidy.lock")
	for _, want := range []string{lockFile, "function not implemented", "XDG_STATE_HOME"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if _, err := os.Lstat(filepath.Join(b.Home, ".zshrc")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("tidy linked .zshrc without the lock (%v)", err)
	}
}

// As in the TypeScript, a journal that cannot be written stops the run: a
// chore that succeeded counts as failed, and writing that failure fails too.
// Nothing is stamped in state.json, and the lock is let go.
func TestTidyStopsWhenTheJournalCannotBeWritten(t *testing.T) {
	b := repo(t, "")
	b.Files(map[string]string{
		"home/.home/autolink/common/zshrc":  "z",
		"xdg/state/homebot/journal.jsonl/x": "a directory where the journal goes",
	})
	if _, err := tidy.Run(b.Ctx, tidy.Options{}); err == nil {
		t.Fatal("tidy ignored a journal it cannot write")
	}
	if _, err := os.Stat(filepath.Join(b.Ctx.StateDir, "state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("state.json was written (%v)", err)
	}
	if err := os.RemoveAll(filepath.Join(b.Ctx.StateDir, "journal.jsonl")); err != nil {
		t.Fatal(err)
	}
	if r := run(t, b); !r.Ran {
		t.Error("the lock was still held after the failed run")
	}
}

// D14: when the guard step fails, tidy does not autosave, and records the
// autosave as failed with the reason, as it records any chore's failure.
func TestTidySkipsTheAutosaveWhenTheGuardFails(t *testing.T) {
	b := repo(t, "autosave:\n  commit: daily\n")
	// A worktree config wins over the hooks path homebot sets.
	b.GitIn(b.Root, "config", "extensions.worktreeConfig", "true")
	b.GitIn(b.Root, "config", "--worktree", "core.hooksPath", "/elsewhere")
	b.Files(map[string]string{"home/.home/homebot.yml": "autosave:\n  commit: daily\n# edited\n"})
	r := run(t, b)
	var steps []string
	for _, e := range r.Errors {
		steps = append(steps, e.Step)
	}
	eqStrings(t, "failed steps", steps, []string{"guard", "autosave"})
	const why = "skipped, because the commit guard could not be installed this run (see the guard step)"
	if r.Errors[1].Message != why {
		t.Errorf("autosave error = %q, want %q", r.Errors[1].Message, why)
	}
	if got := strings.TrimSpace(b.GitIn(b.Root, "log", "-1", "--format=%s")); got != "init" {
		t.Errorf("subject = %q, want init: tidy autosaved without the guard", got)
	}
	s := loadState(t, b)
	if len(s.TidyErrors) != 2 || s.TidyErrors[1].Step != "autosave" || s.TidyErrors[1].Message != why {
		t.Errorf("state tidyErrors = %+v", s.TidyErrors)
	}
	if s.LastAutosave != 0 {
		t.Errorf("lastAutosave = %d, want 0", s.LastAutosave)
	}
	if !slices.Contains(journalMsgs(t, b), "autosave failed: "+why) {
		t.Errorf("journal = %q", journalMsgs(t, b))
	}
}

// A failing autosave is stored in English and spoken in the persona's voice.
// The persona has a line for tidy.error.autosave_no_guard and the terminal is
// a TTY, so the failure's transient Speech carries that line, while the
// TidyError in the result and in state.json, and the journal entry, carry the
// neutral text a later run, `homebot status` or the forge sees.
func TestTidyAutosaveFailureStoresNeutralTextAndSpeaksPersona(t *testing.T) {
	b := repo(t, "autosave:\n  commit: daily\n")
	b.GitIn(b.Root, "config", "extensions.worktreeConfig", "true")
	b.GitIn(b.Root, "config", "--worktree", "core.hooksPath", "/elsewhere")
	b.Files(map[string]string{"home/.home/homebot.yml": "autosave:\n  commit: daily\n# edited\n"})
	c := b.Ctx
	yes := true
	c.StdoutTerminal = &yes
	const marker = "PERSONA AUTOSAVE SKIPPED"
	persona.Override(c, &persona.Voice{ID: "hostile", Name: "Hostile", Lines: map[string][]persona.Line{
		"tidy.error.autosave_no_guard": {{Text: marker, Lang: language.English}},
	}})
	r := run(t, b)

	var steps []string
	for _, e := range r.Errors {
		steps = append(steps, e.Step)
	}
	eqStrings(t, "failed steps", steps, []string{"guard", "autosave"})
	neutral := voice.Record(c, msg.TidyErrorAutosaveNoGuard())
	if strings.Contains(neutral, "PERSONA") || neutral == "" {
		t.Fatalf("voice.Record = %q, want the neutral English", neutral)
	}
	if got := r.Errors[1].Message; got != neutral {
		t.Errorf("result autosave error = %q, want %q", got, neutral)
	}
	if got := r.Errors[1].Speech; got != marker {
		t.Errorf("autosave Speech = %q, want the persona line %q", got, marker)
	}
	// Each failure carries its own speech: the guard failed first and has no
	// persona line, so it is said as it is recorded, not as the autosave.
	if got := r.Errors[0].Speech; got != r.Errors[0].Message || got == marker {
		t.Errorf("guard Speech = %q, want its own message %q", got, r.Errors[0].Message)
	}

	s := loadState(t, b)
	if len(s.TidyErrors) != 2 || s.TidyErrors[1].Step != "autosave" || s.TidyErrors[1].Message != neutral {
		t.Errorf("state tidyErrors = %+v, want the autosave step with %q", s.TidyErrors, neutral)
	}
	msgs := journalMsgs(t, b)
	if !slices.Contains(msgs, "autosave failed: "+neutral) {
		t.Errorf("journal = %q, want the neutral autosave failure", msgs)
	}
	for _, m := range msgs {
		if strings.Contains(m, "PERSONA") {
			t.Errorf("voiced journal entry = %q", m)
		}
	}
	for _, e := range s.TidyErrors {
		if strings.Contains(e.Message, "PERSONA") {
			t.Errorf("voiced stored error = %+v", e)
		}
	}
}

// D14: tidy's autosave goes through save, which runs the guard's checks
// itself: here the hook lets everything through (the homebot stub), and
// the plaintext is still refused.
func TestTidyAutosaveIsCheckedByTheGuard(t *testing.T) {
	b := repo(t, "autosave:\n  commit: daily\n")
	b.Files(map[string]string{
		"home/.home/homebot.yml":          "autosave:\n  commit: daily\n# edited\n",
		"home/.home/private/common/token": "abc",
	})
	b.GitIn(b.Root, "add", "-f", "private/common/token")
	r := run(t, b)
	if len(r.Errors) != 1 || r.Errors[0].Step != "autosave" || !strings.HasPrefix(r.Errors[0].Message, "Commit refused: private/common/token is plaintext.") {
		t.Fatalf("Errors = %+v", r.Errors)
	}
	if got := strings.TrimSpace(b.GitIn(b.Root, "log", "-1", "--format=%s")); got != "init" {
		t.Errorf("subject = %q, want init", got)
	}
	if s := loadState(t, b); s.Blocked == nil {
		t.Error("blocked is not recorded")
	}
}

// A Failure says what the terminal prints (Said) and keeps it apart from what
// is recorded.
func TestFailureSaidIsTheSpeechOrTheRecordedMessage(t *testing.T) {
	recorded := state.TidyError{Step: "seal", Message: "neutral English", At: 7}
	cases := map[string]struct {
		f    tidy.Failure
		want string
	}{
		"spoken":          {tidy.Failure{TidyError: recorded, Speech: "PERSONA WORDS"}, "PERSONA WORDS"},
		"never spoken":    {tidy.Failure{TidyError: recorded}, "neutral English"},
		"both empty":      {tidy.Failure{}, ""},
		"speech is empty": {tidy.Failure{TidyError: recorded, Speech: ""}, "neutral English"},
	}
	for name, tc := range cases {
		if got := tc.f.Said(); got != tc.want {
			t.Errorf("%s: Said() = %q, want %q", name, got, tc.want)
		}
	}
}

// Nothing that marshals a Failure, or a TidyError taken out of one, can
// carry the speech into a file.
func TestFailureSpeechIsNeverMarshalled(t *testing.T) {
	f := tidy.Failure{TidyError: state.TidyError{Step: "seal", Message: "neutral English", At: 7}, Speech: "PERSONA WORDS"}
	for name, v := range map[string]any{"the failure": f, "its recorded part": f.TidyError} {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "PERSONA") || strings.Contains(strings.ToLower(string(data)), "speech") {
			t.Errorf("%s marshals as %s", name, data)
		}
	}
}

// Several failures in one run each keep their own speech; the persona
// speaks only the steps it has a line for, and the others are said as they
// are recorded.
func TestEachFailureKeepsItsOwnSpeech(t *testing.T) {
	b := repo(t, "autosave:\n  commit: daily\n")
	b.GitIn(b.Root, "config", "extensions.worktreeConfig", "true")
	b.GitIn(b.Root, "config", "--worktree", "core.hooksPath", "/elsewhere")
	b.Files(map[string]string{"home/.home/homebot.yml": "autosave:\n  commit: daily\n# edited\n"})
	c := b.Ctx
	yes := true
	c.StdoutTerminal = &yes
	persona.Override(c, &persona.Voice{ID: "hostile", Name: "Hostile", Lines: map[string][]persona.Line{
		"tidy.error.autosave_no_guard": {{Text: "PERSONA AUTOSAVE SKIPPED", Lang: language.English}},
	}})
	r := run(t, b)
	if len(r.Errors) != 2 {
		t.Fatalf("Errors = %+v, want the guard and the autosave", r.Errors)
	}
	for _, e := range r.Errors {
		want := e.Message
		if e.Step == "autosave" {
			want = "PERSONA AUTOSAVE SKIPPED"
		}
		if got := e.Said(); got != want {
			t.Errorf("%s: Said() = %q, want %q", e.Step, got, want)
		}
	}
}
