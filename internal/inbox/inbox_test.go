package inbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/text/language"

	"github.com/charmbracelet/x/ansi"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/cleanup"
	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/guard"
	"github.com/hyperlab-sh/homebot/internal/helper"
	"github.com/hyperlab-sh/homebot/internal/linker"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/profile"
	"github.com/hyperlab-sh/homebot/internal/selfname"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/unpack"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// box is a sandbox whose Context has its homebot.yml loaded, as LocateRoot
// would have done.
type box struct {
	*testutil.Box
	t *testing.T
}

func helperPathFixture(t *testing.T, b *box, exe string) {
	t.Helper()
	files := helper.Files(b.Ctx)
	if b.Ctx.OS == app.Linux {
		service, timer := helper.SystemdUnits(b.Ctx, exe, b.Bin)
		b.Files(map[string]string{
			strings.TrimPrefix(files[0], b.Dir+string(filepath.Separator)): service,
			strings.TrimPrefix(files[1], b.Dir+string(filepath.Separator)): timer,
		})
		return
	}
	b.Files(map[string]string{strings.TrimPrefix(files[0], b.Dir+string(filepath.Separator)): helper.LaunchdPlist(b.Ctx, exe, b.Bin)})
}

func TestUpdateItemUsesLocalStateAndManualMethod(t *testing.T) {
	b := newBox(t, testutil.Options{})
	b.Ctx.BuildVersion, b.Ctx.BuildChannel = "0.1.0", "release"
	b.Ctx.Config.Update = config.UpdateNotify
	b.update(func(s *state.State) {
		s.UpdateCheck = &state.UpdateCheck{At: b.Ctx.Now().UnixMilli(), Latest: "0.1.1", FirstSeen: b.Ctx.Now().UnixMilli()}
	})
	item := b.mustFind("update-available")
	if !strings.Contains(item.Detail, "0.1.1") || item.Level != Todo {
		t.Errorf("notify item = %+v", item)
	}

	b.Ctx.Config.Update = config.UpdateAuto
	item = b.mustFind("update-available")
	if !strings.Contains(item.Detail, "manual") {
		t.Errorf("manual method item = %+v", item)
	}

	b.Ctx.BuildVersion = "0.1.1"
	contains(t, b.ids(), "update-available", false)
	b.Ctx.BuildVersion = "0.1.0"
	b.Ctx.Config.Update = config.UpdateOff
	contains(t, b.ids(), "update-available", false)
}

func TestUpdateItemCaskSoakFailureAndTapLag(t *testing.T) {
	b := newBox(t, testutil.Options{})
	b.Ctx.BuildVersion, b.Ctx.BuildChannel = "0.1.0", "release"
	b.Ctx.Config.Update = config.UpdateAuto
	exe := filepath.Join(b.Dir, "brew", "Caskroom", "homebot", "0.1.0", "homebot")
	executableFixture(t, exe)
	b.Ctx.Exe = exe
	now := b.Ctx.Now().UnixMilli()
	b.update(func(s *state.State) { s.UpdateCheck = &state.UpdateCheck{At: now, Latest: "0.1.1", FirstSeen: now} })
	contains(t, b.ids(), "update-available", false)
	b.update(func(s *state.State) {
		s.UpdateFailed = &state.UpdateFailed{Version: "0.1.1", At: now, Reason: "verify", Error: "bad signature"}
	})
	if got := b.mustFind("update-available"); !strings.Contains(got.Detail, "Verification") {
		t.Errorf("failure item = %+v", got)
	}
	b.update(func(s *state.State) {
		s.UpdateFailed = nil
		s.UpdateCheck.FirstAttempt = now - (72 * time.Hour).Milliseconds()
	})
	if got := b.mustFind("update-available"); !strings.Contains(got.Detail, "72 hours") {
		t.Errorf("tap lag item = %+v", got)
	}
	// An offline check afterwards must not hide the tap-lag item.
	b.update(func(s *state.State) { s.UpdateCheck.Failed = true })
	if got := b.mustFind("update-available"); !strings.Contains(got.Detail, "72 hours") {
		t.Errorf("tap lag item after a failed check = %+v", got)
	}
}

// S5: a failed check "raises no item", and "the item goes away once the running
// version is at least the latest". Nothing else removes it, so an offline
// laptop keeps the item, including the one that says to report a bad signature.
func TestUpdateItemSurvivesFailedCheck(t *testing.T) {
	failedCheck := func(s *state.State) { s.UpdateCheck.Failed = true }
	cask := func(t *testing.T, b *box) {
		t.Helper()
		exe := filepath.Join(b.Dir, "brew", "Caskroom", "homebot", "0.1.0", "homebot")
		executableFixture(t, exe)
		b.Ctx.Exe = exe
	}
	for _, tc := range []struct {
		name   string
		mode   config.UpdateMode
		method func(*testing.T, *box)
		failed *state.UpdateFailed
		want   string
	}{
		{"notify", config.UpdateNotify, func(*testing.T, *box) {}, nil, "Version 0.1.1 is available"},
		{"auto and manual", config.UpdateAuto, func(*testing.T, *box) {}, nil, "manual upgrade"},
		{"signature failed", config.UpdateAuto, cask, &state.UpdateFailed{Version: "0.1.1", Reason: "verify", Error: "bad signature"}, "report this failure"},
		{"cosign", config.UpdateAuto, cask, &state.UpdateFailed{Version: "0.1.1", Reason: "cosign", Error: "no cosign"}, "cosign"},
		{"timeout", config.UpdateAuto, cask, &state.UpdateFailed{Version: "0.1.1", Reason: "timeout"}, "timed out"},
		{"upgrade failed", config.UpdateAuto, cask, &state.UpdateFailed{Version: "0.1.1", Reason: "upgrade", Error: "brew exploded"}, "brew exploded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newBox(t, testutil.Options{})
			b.Ctx.BuildVersion, b.Ctx.BuildChannel = "0.1.0", "release"
			b.Ctx.Config.Update = tc.mode
			tc.method(t, b)
			now := b.Ctx.Now().UnixMilli()
			b.update(func(s *state.State) {
				s.UpdateCheck = &state.UpdateCheck{At: now, Latest: "0.1.1", FirstSeen: now - (72 * time.Hour).Milliseconds()}
				if tc.failed != nil {
					tc.failed.At = now
					s.UpdateFailed = tc.failed
				}
			})
			before := b.mustFind("update-available")
			if !strings.Contains(before.Detail, tc.want) {
				t.Fatalf("item before the failed check = %+v, want %q", before, tc.want)
			}
			b.update(failedCheck)
			after, ok := b.find("update-available")
			if !ok {
				t.Fatalf("a failed check hid the item: %v", b.ids())
			}
			if after.Detail != before.Detail || FixText(b.Ctx, after.Fix) != FixText(b.Ctx, before.Fix) {
				t.Errorf("a failed check changed the item:\nbefore %+v\nafter  %+v", before, after)
			}
			// A later success clears the flag; the item is unchanged.
			b.update(func(s *state.State) { s.UpdateCheck.Failed = false })
			if again := b.mustFind("update-available"); again.Detail != before.Detail {
				t.Errorf("item after a good check = %+v", again)
			}
			// Only the running version catching up removes it, offline or not.
			b.update(failedCheck)
			b.Ctx.BuildVersion = "0.1.1"
			contains(t, b.ids(), "update-available", false)
			b.Ctx.BuildVersion = "0.2.0"
			contains(t, b.ids(), "update-available", false)
		})
	}
}

// If no check ever succeeded there is no latest version, so there is no item.
func TestUpdateItemNeedsAGoodAnswerOnce(t *testing.T) {
	b := newBox(t, testutil.Options{})
	b.Ctx.BuildVersion, b.Ctx.BuildChannel = "0.1.0", "release"
	b.Ctx.Config.Update = config.UpdateNotify
	b.update(func(s *state.State) {
		s.UpdateCheck = &state.UpdateCheck{At: b.Ctx.Now().UnixMilli(), Failed: true}
	})
	contains(t, b.ids(), "update-available", false)
}

func TestUpdateItemGoInstallShowsGoInstallCommand(t *testing.T) {
	before := readBuildInfo
	readBuildInfo = func() (*debug.BuildInfo, bool) { return &debug.BuildInfo{Main: debug.Module{Sum: "h1:example"}}, true }
	t.Cleanup(func() { readBuildInfo = before })
	b := newBox(t, testutil.Options{})
	b.Ctx.BuildVersion, b.Ctx.BuildChannel = "0.1.0", "source"
	for _, mode := range []config.UpdateMode{config.UpdateNotify, config.UpdateAuto} {
		b.Ctx.Config.Update = mode
		b.update(func(s *state.State) {
			s.UpdateCheck = &state.UpdateCheck{At: b.Ctx.Now().UnixMilli(), Latest: "0.1.1", FirstSeen: b.Ctx.Now().UnixMilli()}
		})
		item := b.mustFind("update-available")
		if got := FixText(b.Ctx, item.Fix); got != "go install github.com/hyperlab-sh/homebot/cmd/homebot@v0.1.1" {
			t.Errorf("%s: go install command = %q", mode, got)
		}
	}
	// A development build of the same module never gets the item.
	b.Ctx.BuildVersion = "dev"
	contains(t, b.ids(), "update-available", false)
}

func executableFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestHelperBinaryPathInbox(t *testing.T) {
	for _, osKind := range []app.OS{app.Linux, app.MacOS} {
		t.Run(string(osKind), func(t *testing.T) {
			for _, tc := range []struct {
				name, path               string
				exists, executable, want bool
			}{
				{"missing", "bin/missing/homebot", false, false, true},
				{"stable", "bin/homebot", true, true, false},
				{"not executable", "bin/noexec/homebot", true, false, true},
				{"versioned", "Cellar/homebot/1.2/bin/homebot", true, true, true},
				{"quoted", "bin space/100%/$$x & /homebot", true, true, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					b := newBox(t, testutil.Options{OS: osKind})
					path := filepath.Join(b.Dir, tc.path)
					if tc.exists {
						executableFixture(t, path)
						if !tc.executable {
							if err := os.Chmod(path, 0o644); err != nil {
								t.Fatal(err)
							}
						}
					}
					helperPathFixture(t, b, path)
					it, ok := b.find("helper-path")
					if ok != tc.want {
						t.Fatalf("helper-path present = %v, want %v; items = %v", ok, tc.want, b.ids())
					}
					if ok && (it.Level != Todo || FixText(b.Ctx, it.Fix) != "homebot setup" || !strings.Contains(it.Detail, path)) {
						t.Errorf("helper-path = %+v", it)
					}
				})
			}
		})
	}
}

func TestHelperBinaryPathAbsentUnreadableAndGuard(t *testing.T) {
	b := newBox(t, testutil.Options{})
	contains(t, b.ids(), "helper-path", false)
	b.WithStubs(map[string]string{"homebot": "exit 0"})
	if _, err := guard.InstallHooks(b.Ctx); err != nil {
		t.Fatal(err)
	}
	contains(t, b.ids(), "helper-path", false)
	if err := os.Remove(filepath.Join(b.Bin, "homebot")); err != nil {
		t.Fatal(err)
	}
	it := b.mustFind("helper-path")
	if !strings.Contains(it.Detail, filepath.Join(b.Bin, "homebot")) {
		t.Errorf("guard path not named: %+v", it)
	}

	// A second distinct broken path is named in the same item.
	missing := filepath.Join(b.Dir, "other", "homebot")
	helperPathFixture(t, b, missing)
	items := b.collect()
	var found []Item
	for _, item := range items {
		if item.ID == "helper-path" {
			found = append(found, item)
		}
	}
	if len(found) != 1 || !strings.Contains(found[0].Detail, missing) || !strings.Contains(found[0].Detail, filepath.Join(b.Bin, "homebot")) {
		t.Errorf("combined paths = %+v", found)
	}
}

func TestGuardVersionedRecordedPathIsFlaggedEvenWhenExecutable(t *testing.T) {
	b := newBox(t, testutil.Options{})
	b.WithStubs(map[string]string{"homebot": "exit 0"})
	if _, err := guard.InstallHooks(b.Ctx); err != nil {
		t.Fatal(err)
	}
	versioned := filepath.Join(b.Dir, "Cellar", "homebot", "1.2", "bin", "homebot")
	executableFixture(t, versioned)
	shim := filepath.Join(b.Ctx.DataDir, "hooks", "pre-commit")
	content, err := os.ReadFile(shim)
	if err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(b.Bin, "homebot")
	if !strings.Contains(string(content), old) {
		t.Fatalf("installed shim did not record %q", old)
	}
	if err := os.WriteFile(shim, []byte(strings.Replace(string(content), old, versioned, 1)), 0o755); err != nil {
		t.Fatal(err)
	}
	it := b.mustFind("helper-path")
	if !strings.Contains(it.Detail, versioned) {
		t.Errorf("guard versioned path not named: %+v", it)
	}
}

func TestUnreadableHelperUnitIsDebugOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Fatal("this permission test must run as a non-root user")
	}
	b := newBox(t, testutil.Options{})
	helperPathFixture(t, b, filepath.Join(b.Dir, "missing", "homebot"))
	service := helper.Files(b.Ctx)[0]
	if err := os.Chmod(service, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(service, 0o644) })
	var debug bytes.Buffer
	b.Ctx.Debug = slog.New(slog.NewTextHandler(&debug, &slog.HandlerOptions{Level: slog.LevelDebug}))
	contains(t, b.ids(), "helper-path", false)
	if !strings.Contains(debug.String(), "cannot read helper binary path") {
		t.Errorf("debug = %q", debug.String())
	}
}

func TestInstalledHelperFIFOIsSkippedWithoutBlocking(t *testing.T) {
	for _, osKind := range []app.OS{app.Linux, app.MacOS} {
		t.Run(string(osKind), func(t *testing.T) {
			b := newBox(t, testutil.Options{OS: osKind})
			helperPathFixture(t, b, filepath.Join(b.Dir, "missing", "homebot"))
			file := helper.Files(b.Ctx)[0]
			if err := os.Remove(file); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(file, 0o600); err != nil {
				t.Fatal(err)
			}
			var debug bytes.Buffer
			b.Ctx.Debug = slog.New(slog.NewTextHandler(&debug, &slog.HandlerOptions{Level: slog.LevelDebug}))
			type outcome struct {
				items []Item
				err   error
			}
			done := make(chan outcome, 1)
			go func() { items, err := Collect(b.Ctx); done <- outcome{items, err} }()
			select {
			case result := <-done:
				if result.err != nil {
					t.Fatal(result.err)
				}
				for _, item := range result.items {
					if item.ID == "helper-path" {
						t.Errorf("FIFO produced path item: %+v", item)
					}
				}
				if !strings.Contains(debug.String(), "cannot read helper binary path") {
					t.Errorf("debug = %q", debug.String())
				}
			case <-time.After(15 * time.Second):
				t.Fatal("inbox blocked while reading a FIFO helper file")
			}
		})
	}
}

func TestRegularHelperSymlinkIsRead(t *testing.T) {
	b := newBox(t, testutil.Options{})
	path := filepath.Join(b.Dir, "missing", "homebot")
	helperPathFixture(t, b, path)
	service := helper.Files(b.Ctx)[0]
	target := service + ".installed"
	if err := os.Rename(service, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, service); err != nil {
		t.Fatal(err)
	}
	if it := b.mustFind("helper-path"); !strings.Contains(it.Detail, path) {
		t.Errorf("regular service symlink was not read: %+v", it)
	}
}

func TestBinaryNeedsEffectiveExecutePermission(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Fatal("this permission test must run as a non-root user")
	}
	path := filepath.Join(t.TempDir(), "homebot")
	executableFixture(t, path)
	// Owner permissions take precedence over "other": the previous any-bit
	// check accepted this file even though its owner cannot execute it.
	if err := os.Chmod(path, 0o001); err != nil {
		t.Fatal(err)
	}
	if !badBinaryPath(path) {
		t.Error("owner with no execute permission was accepted")
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if badBinaryPath(path) {
		t.Error("owner-executable file was rejected")
	}
}

func newBox(t *testing.T, opts testutil.Options) *box {
	t.Helper()
	opts.Git = true
	b := &box{Box: testutil.Sandbox(t, opts), t: t}
	b.reload()
	return b
}

// reload reads homebot.yml again, after a test rewrote it.
func (b *box) reload() {
	b.t.Helper()
	cfg, err := config.Load(b.Root)
	if err != nil {
		b.t.Fatal(err)
	}
	b.Ctx.Config = cfg
}

func (b *box) collect() []Item {
	b.t.Helper()
	items, err := Collect(b.Ctx)
	if err != nil {
		b.t.Fatalf("Collect: %v", err)
	}
	return items
}

func (b *box) ids() []string {
	b.t.Helper()
	var ids []string
	for _, it := range b.collect() {
		ids = append(ids, it.ID)
	}
	return ids
}

func (b *box) find(id string) (Item, bool) {
	b.t.Helper()
	for _, it := range b.collect() {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

func (b *box) mustFind(id string) Item {
	b.t.Helper()
	it, ok := b.find(id)
	if !ok {
		b.t.Fatalf("no %s item in %q", id, b.ids())
	}
	return it
}

func (b *box) update(change func(*state.State)) {
	b.t.Helper()
	if err := state.Update(b.Ctx, change); err != nil {
		b.t.Fatal(err)
	}
}

func (b *box) at(when string) {
	b.t.Helper()
	now, err := time.Parse(time.RFC3339, when)
	if err != nil {
		b.t.Fatal(err)
	}
	b.Ctx.Now = func() time.Time { return now }
}

func (b *box) h(rel string) string { return filepath.Join(b.Home, rel) }

func (b *box) commit() {
	b.t.Helper()
	b.Files(map[string]string{"home/.home/autolink/common/zshrc": "x\n"})
	b.GitIn(b.Root, "add", "-A")
	b.GitIn(b.Root, "commit", "-q", "-m", "init")
}

func contains(t *testing.T, ids []string, id string, want bool) {
	t.Helper()
	if slices.Contains(ids, id) != want {
		t.Errorf("ids %q: holding %s is %v, want %v", ids, id, !want, want)
	}
}

func eq[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

// --- collect ---

func TestAFreshRepoAsksForTheBasicsUrgentFirst(t *testing.T) {
	b := newBox(t, testutil.Options{})
	items := b.collect()
	lastUrgent, firstTodo := -1, len(items)
	for i, it := range items {
		if it.Level == Urgent {
			lastUrgent = i
		} else if firstTodo == len(items) {
			firstTodo = i
		}
	}
	if firstTodo < lastUrgent {
		t.Errorf("a todo item before an urgent one: %+v", items)
	}
	ids := b.ids()
	for _, id := range []string{"no-guard", "no-keys", "no-remote", "missing-profile"} {
		contains(t, ids, id, true)
	}
	b.Files(map[string]string{"home/.home/manifests/profiles/workstation.yml": "steps: []\n"})
	contains(t, b.ids(), "never-unpacked", true)
}

func TestEveryItemSaysWhatToRun(t *testing.T) {
	b := newBox(t, testutil.Options{})
	b.Files(map[string]string{"home/.home/manifests/profiles/workstation.yml": "steps: ["})
	now := b.Ctx.Now().UnixMilli()
	b.update(func(s *state.State) {
		s.Blocked = &state.Blocked{Reason: "x", At: now}
		s.TidyErrors = []state.TidyError{{Step: "export", Message: "boom", At: now}}
	})
	items := b.collect()
	if len(items) <= 5 {
		t.Errorf("%d items, want more than 5", len(items))
	}
	for _, it := range items {
		if it.Fix == nil || FixText(b.Ctx, it.Fix) == "" {
			t.Errorf("%s has no fix", it.ID)
		}
	}
}

func TestNoProfileChosenStillSaysWhatToDo(t *testing.T) {
	b := newBox(t, testutil.Options{Config: "profiles: {}\n"})
	it := b.mustFind("no-profile")
	if !strings.Contains(it.Fix.Shell, "homebot.yml") {
		t.Errorf("fix = %+v", it.Fix)
	}
	eq(t, it.Title, "No profile chosen for testhost")
}

func TestReportsSecretStates(t *testing.T) {
	b := newBox(t, testutil.Options{})
	contains(t, b.ids(), "no-keys", true)
	testutil.TestKeys(t, b.Root)
	b.Files(map[string]string{"home/.home/private/common/a.age": "x"})
	ids := b.ids()
	contains(t, ids, "secret-locked", true)
	contains(t, ids, "no-keys", false)
	// Go seals in-process: there is no age to miss.
	contains(t, ids, "no-age", false)
}

func TestReportsLayoutErrorsAsUrgentWithTheFixInTheDetail(t *testing.T) {
	b := newBox(t, testutil.Options{})
	b.Files(map[string]string{
		"home/.home/autolink/common/(claude)/x": "x",
		"home/.home/autolink/linux/claude/y":    "y",
	})
	it := b.mustFind("link-invalid")
	eq(t, it.Level, Urgent)
	if !strings.Contains(it.Detail, "use one spelling everywhere") {
		t.Errorf("detail = %q", it.Detail)
	}
	eq(t, FixText(b.Ctx, it.Fix), "homebot link --dry-run")
	contains(t, b.ids(), "link-conflict", false)
}

func TestReportsBlockedCommitsAndFailedChores(t *testing.T) {
	b := newBox(t, testutil.Options{})
	now := b.Ctx.Now().UnixMilli()
	b.update(func(s *state.State) {
		s.Blocked = &state.Blocked{Reason: "too big", At: now}
		s.TidyErrors = []state.TidyError{
			{Step: "export", Message: "boom", At: now - 2*3600_000},
			{Step: "autosave", Message: "refused", At: now},
		}
	})
	items := b.collect()
	eq(t, items[0].ID, "blocked")
	eq(t, items[0].Level, Urgent)
	eq(t, items[0].Detail, "too big")
	tidy := b.mustFind("tidy:export")
	eq(t, tidy.Title, "Automatic export failed 2h ago")
	eq(t, tidy.Detail, "boom")
	eq(t, FixText(b.Ctx, tidy.Fix), "homebot tidy")
	// A refused autosave is the blocked item already.
	contains(t, b.ids(), "tidy:autosave", false)
}

func TestABrokenProfileShowsUpAsAnItemInsteadOfCrashing(t *testing.T) {
	b := newBox(t, testutil.Options{})
	b.Files(map[string]string{"home/.home/manifests/profiles/workstation.yml": "steps: ["})
	it := b.mustFind("broken:profile")
	eq(t, it.Level, Urgent)
	eq(t, it.Title, "Could not check profile")
	if !strings.Contains(it.Detail, "invalid YAML") {
		t.Errorf("detail = %q", it.Detail)
	}
	eq(t, FixText(b.Ctx, it.Fix), "homebot unpack --dry-run")
}

func TestKnowsWhenTheProfileChangedSinceTheLastUnpack(t *testing.T) {
	b := newBox(t, testutil.Options{})
	b.Files(map[string]string{"home/.home/manifests/profiles/workstation.yml": "steps:\n  - run: 'true'\n"})
	if err := unpack.Run(b.Ctx, "", unpack.Options{}); err != nil {
		t.Fatal(err)
	}
	ids := b.ids()
	contains(t, ids, "never-unpacked", false)
	contains(t, ids, "profile-changed", false)
	b.Files(map[string]string{"home/.home/manifests/profiles/workstation.yml": "steps:\n  - run: 'true'\n  - run: 'echo more'\n"})
	it := b.mustFind("profile-changed")
	eq(t, it.Title, "Profile workstation changed since the last unpack")
	eq(t, it.Detail, "It runs every step without asking; to see them first: homebot unpack --dry-run")
	eq(t, FixText(b.Ctx, it.Fix), "homebot unpack")
}

func TestADifferentProfileUnpackedLastCountsAsNeverUnpacked(t *testing.T) {
	b := newBox(t, testutil.Options{})
	b.Files(map[string]string{
		"home/.home/manifests/profiles/container.yml":   "steps:\n  - run: 'true'\n",
		"home/.home/manifests/profiles/workstation.yml": "steps:\n  - run: 'true'\n",
	})
	if err := unpack.Run(b.Ctx, "container", unpack.Options{}); err != nil {
		t.Fatal(err)
	}
	it := b.mustFind("never-unpacked")
	eq(t, it.Title, "Profile workstation was never unpacked here (last unpacked: container)")
	contains(t, b.ids(), "profile-changed", false)
}

func TestTheProfileHashIsTheResolvedSteps(t *testing.T) {
	b := newBox(t, testutil.Options{})
	b.Files(map[string]string{"home/.home/manifests/profiles/workstation.yml": "steps:\n  - run: 'true'\n"})
	steps, err := profile.ResolveFor(b.Ctx, "workstation")
	if err != nil {
		t.Fatal(err)
	}
	b.update(func(s *state.State) {
		s.Unpack = &state.Unpack{Profile: "workstation", Hash: profile.StepsHash(steps), At: 1}
	})
	ids := b.ids()
	contains(t, ids, "never-unpacked", false)
	contains(t, ids, "profile-changed", false)
	b.update(func(s *state.State) { s.Unpack.Hash = "stale" })
	contains(t, b.ids(), "profile-changed", true)
}

func TestAFailingExportIsAnItem(t *testing.T) {
	b := newBox(t, testutil.Options{Config: "profiles:\n  default: workstation\nexports:\n  - name: list\n    out: list.txt\n    run: echo hi\n  - name: other\n    out: other.txt\n    run: echo hi\n"})
	contains(t, b.ids(), "export:list", false)
	b.update(func(s *state.State) {
		s.Exports.Set("list", state.ExportResult{At: 1, OK: false, Error: "brew: not found"})
		s.Exports.Set("other", state.ExportResult{At: 1, OK: true})
	})
	it := b.mustFind("export:list")
	eq(t, it.Level, Todo)
	eq(t, it.Title, "Export list is failing")
	eq(t, it.Detail, "brew: not found")
	eq(t, FixText(b.Ctx, it.Fix), "homebot export list")
	contains(t, b.ids(), "export:other", false)
}

// --- the helper ---

func installHelper(t *testing.T, b *box) {
	t.Helper()
	for _, f := range helper.Files(b.Ctx) {
		b.Files(map[string]string{strings.TrimPrefix(f, b.Dir+"/"): "x"})
	}
}

func TestTheHelperItems(t *testing.T) {
	t.Run("not installed", func(t *testing.T) {
		b := newBox(t, testutil.Options{})
		it := b.mustFind("no-helper")
		eq(t, it.Level, Todo)
		eq(t, it.Title, "The background helper is not installed")
		eq(t, it.Detail, "It seals, exports, links and reminds you every hour")
		eq(t, FixText(b.Ctx, it.Fix), "homebot setup")
		contains(t, b.ids(), "no-agent", false)
	})
	t.Run("installed and running", func(t *testing.T) {
		b := newBox(t, testutil.Options{})
		installHelper(t, b)
		b.WithStubs(map[string]string{"loginctl": "echo Linger=yes"})
		now := b.Ctx.Now().UnixMilli()
		b.update(func(s *state.State) { s.LastTidy = now - 3600_000 })
		ids := b.ids()
		for _, id := range []string{"no-helper", "helper-stale", "helper-no-linger"} {
			contains(t, ids, id, false)
		}
	})
	t.Run("stale after six hours", func(t *testing.T) {
		b := newBox(t, testutil.Options{})
		installHelper(t, b)
		now := b.Ctx.Now().UnixMilli()
		b.update(func(s *state.State) { s.LastTidy = now - 6*3600_000 })
		contains(t, b.ids(), "helper-stale", false)
		b.update(func(s *state.State) { s.LastTidy = now - 7*3600_000 })
		it := b.mustFind("helper-stale")
		eq(t, it.Title, "The background helper last ran 7h ago")
		eq(t, FixText(b.Ctx, it.Fix), "homebot setup")
		contains(t, b.ids(), "agent-stale", false)
	})
	t.Run("headless without lingering", func(t *testing.T) {
		b := newBox(t, testutil.Options{})
		b.WithStubs(map[string]string{"loginctl": "echo Linger=no"})
		// Not installed: no-helper says it all.
		contains(t, b.ids(), "helper-no-linger", false)
		installHelper(t, b)
		it := b.mustFind("helper-no-linger")
		eq(t, it.Level, Todo)
		eq(t, it.Title, voice.Plain(b.Ctx, msg.InboxHelperNoLingerTitle()))
		eq(t, it.Detail, voice.Plain(b.Ctx, msg.InboxHelperNoLingerDetail()))
		eq(t, it.Fix.Shell, "loginctl enable-linger $USER")
		// It is the last item.
		items := b.collect()
		eq(t, items[len(items)-1].ID, "helper-no-linger")
	})
}

// --- ordering ---

func TestChecksRunTogetherAndKeepTheTypeScriptOrder(t *testing.T) {
	b := newBox(t, testutil.Options{})
	e := &env{c: b.Ctx, st: &state.State{}, now: b.Ctx.Now()}
	todo := func(id string) Item { return Item{ID: id, Level: Todo, Title: id} }
	slow := make(chan struct{})
	fake := []check{
		// The first check finishes last, after the others had to start.
		{run: func(*env) ([]Item, error) { <-slow; return []Item{todo("first")}, nil }},
		{id: "middle", retry: args("link", "--dry-run"), run: func(*env) ([]Item, error) {
			return []Item{todo("found-before-failing")}, errors.New("disk on fire")
		}},
		{run: func(*env) ([]Item, error) {
			close(slow)
			return []Item{{ID: "late-urgent", Level: Urgent, Title: "u"}, todo("last")}, nil
		}},
	}
	items, err := collect(e, fake)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	want := []string{"broken:middle", "late-urgent", "first", "found-before-failing", "last"}
	if !slices.Equal(ids, want) {
		t.Fatalf("ids = %q, want %q", ids, want)
	}
	broken := items[0]
	eq(t, broken.Title, "Could not check middle")
	eq(t, broken.Detail, "disk on fire")
	eq(t, FixText(b.Ctx, broken.Fix), "homebot link --dry-run")
}

func TestAFailureOutsideTheSafeChecksFailsTheInbox(t *testing.T) {
	b := newBox(t, testutil.Options{})
	e := &env{c: b.Ctx, st: &state.State{}, now: b.Ctx.Now()}
	boom := errors.New("boom")
	_, err := collect(e, []check{
		{run: func(*env) ([]Item, error) { return []Item{{ID: "a", Level: Todo}}, nil }},
		{run: func(*env) ([]Item, error) { return nil, boom }},
	})
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want boom", err)
	}
}

func TestStoredErrorDetailKeepsHomebotWithAnEffectiveName(t *testing.T) {
	b := newBox(t, testutil.Options{})
	stable := filepath.Join(b.Bin, "homebot")
	if err := os.WriteFile(stable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b.Ctx.Exe = stable
	if err := selfname.Link(b.Ctx, "zork"); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(b.Home, ".local", "bin")
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": bin + string(filepath.ListSeparator) + b.Bin})
	if got := selfname.Effective(b.Ctx); got != "zork" {
		t.Fatalf("effective name = %q, want zork", got)
	}
	e := &env{c: b.Ctx, st: &state.State{}, now: b.Ctx.Now()}
	items, err := collect(e, []check{{id: "pack", retry: args("pack"), run: func(*env) ([]Item, error) {
		return nil, fail.Say(msg.PackErrorNoArgs())
	}}})
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %+v, err = %v", items, err)
	}
	if got := items[0].Detail; !strings.Contains(got, "homebot pack") || strings.Contains(got, "zork") {
		t.Errorf("stored Detail = %q", got)
	}
	if got := FixText(b.Ctx, items[0].Fix); got != "zork pack" {
		t.Errorf("printed fix = %q, want zork pack", got)
	}
}

func TestTheChecksAreInTheTypeScriptOrder(t *testing.T) {
	// Every check, with the id and retry fix a safely check carries, in the
	// order src/inbox.ts adds its items (collectInbox, lines 58-240), plus
	// the Go naming check after exports.
	want := []struct{ run, id, retry string }{
		{run: "identityItems"},
		{run: "signingKeyItems"},
		{run: "gitconfigItems"},
		{run: "blockedItems"},
		{run: "tidyErrorItems"},
		{run: "repoItems"},
		{run: "secretItems", id: "secrets", retry: "homebot lock"},
		{run: "linkItems", id: "links", retry: "homebot link --dry-run"},
		{run: "triageItems", id: "triage", retry: "homebot triage"},
		{run: "gitItems", id: "git", retry: "git -C ~/.home status"},
		{run: "profileItems", id: "profile", retry: "homebot unpack --dry-run"},
		{run: "exportItems"},
		{run: "nameItems", id: "name", retry: "homebot name"},
		{run: "personaBrokenItems"},
		{run: "helperItems"},
		{run: "binaryPathItems"},
		{run: "updateItems"},
	}
	b := newBox(t, testutil.Options{})
	e := &env{c: b.Ctx, repo: "~/.home"}
	if len(checks) != len(want) {
		t.Fatalf("%d checks, want %d", len(checks), len(want))
	}
	for i, ch := range checks {
		full := runtime.FuncForPC(reflect.ValueOf(ch.run).Pointer()).Name()
		run := full[strings.LastIndex(full, ".")+1:]
		retry := ""
		if ch.retry != nil {
			retry = FixText(b.Ctx, ch.retry(e))
		}
		if run != want[i].run || ch.id != want[i].id || retry != want[i].retry {
			t.Errorf("check %d is {%s %q %q}, want {%s %q %q}", i+1, run, ch.id, retry, want[i].run, want[i].id, want[i].retry)
		}
	}
}

// --- git and the remote ---

func TestGitIdentityComesBeforeTheRefusedCommit(t *testing.T) {
	b := newBox(t, testutil.Options{})
	b.GitIn(b.Root, "config", "--unset", "user.email")
	b.GitIn(b.Root, "config", "--unset", "user.name")
	b.Files(map[string]string{"gitconfig": "[init]\n\tdefaultBranch = main\n"})
	b.update(func(s *state.State) { s.Blocked = &state.Blocked{Reason: "Author identity unknown", At: 1} })
	ids := b.ids()
	i, j := slices.Index(ids, "no-identity"), slices.Index(ids, "blocked")
	if i < 0 || j < 0 || i > j {
		t.Fatalf("ids = %q, want no-identity before blocked", ids)
	}
	it := b.mustFind("no-identity")
	eq(t, it.Level, Urgent)
	if !strings.Contains(it.Fix.Shell, "git config --file ~/.config/git/config user.email") {
		t.Errorf("fix = %q", it.Fix.Shell)
	}
	t.Run("a name alone is not enough", func(t *testing.T) {
		b.Files(map[string]string{"gitconfig": "[user]\n\tname = Ada\n"})
		contains(t, b.ids(), "no-identity", true)
	})
	t.Run("the global config counts", func(t *testing.T) {
		b.Files(map[string]string{"gitconfig": "[user]\n\tname = Ada\n\temail = ada@example.com\n"})
		contains(t, b.ids(), "no-identity", false)
	})
}

func TestAMissingSigningKeyIsUrgentWithThePerMachineWayOut(t *testing.T) {
	b := newBox(t, testutil.Options{})
	b.GitIn(b.Root, "config", "commit.gpgsign", "true")
	b.GitIn(b.Root, "config", "gpg.format", "ssh")
	b.GitIn(b.Root, "config", "user.signingkey", b.h(".ssh/id_rsa"))
	it := b.mustFind("no-signing-key")
	eq(t, it.Level, Urgent)
	eq(t, it.Title, "Commits are signed with ~/.ssh/id_rsa, which this machine doesn't have")
	eq(t, it.Fix.Shell, voice.Plain(b.Ctx, msg.GitSigningOff()))
	if !strings.Contains(it.Fix.Shell, "--file ~/.config/git/config.local") {
		t.Errorf("fix = %q", it.Fix.Shell)
	}
	b.Files(map[string]string{"home/.ssh/id_rsa": "KEY"})
	contains(t, b.ids(), "no-signing-key", false)
}

func TestAGitconfigBesideTheReposConfig(t *testing.T) {
	fromRepo := func(b *box) {
		b.Files(map[string]string{"home/.home/autolink/common/config/(git)/config": "[user]\n\tname = Repo\n"})
		if err := os.MkdirAll(b.h(".config/git"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(b.Root, "autolink/common/config/(git)/config"), b.h(".config/git/config")); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("a real ~/.gitconfig is flagged", func(t *testing.T) {
		b := newBox(t, testutil.Options{})
		fromRepo(b)
		b.Files(map[string]string{"home/.gitconfig": "[user]\n\temail = old@example.com\n"})
		it := b.mustFind("gitconfig-shadow")
		eq(t, it.Level, Todo)
		eq(t, it.Title, "~/.gitconfig overrides the git config from the repo")
		eq(t, it.Fix.Shell, voice.Plain(b.Ctx, msg.GitGitconfigAside()))
		if !strings.Contains(it.Detail, "~/.config/git/config.local") {
			t.Errorf("detail = %q", it.Detail)
		}
	})
	t.Run("nothing without a ~/.gitconfig", func(t *testing.T) {
		b := newBox(t, testutil.Options{})
		fromRepo(b)
		contains(t, b.ids(), "gitconfig-shadow", false)
	})
}

func TestTheRemoteNoneThenNeverPushedThenNothing(t *testing.T) {
	b := newBox(t, testutil.Options{})
	b.commit()
	eq(t, FixText(b.Ctx, b.mustFind("no-remote").Fix), "homebot remote")
	bare := filepath.Join(b.Dir, "remote.git")
	b.GitIn(b.Dir, "init", "-q", "--bare", bare)
	b.GitIn(b.Root, "remote", "add", "origin", bare)
	never := b.mustFind("never-pushed")
	eq(t, never.Title, "Not backed up yet: nothing pushed to the remote")
	eq(t, never.Detail, bare)
	eq(t, FixText(b.Ctx, never.Fix), "homebot remote")
	// Task 18 runs homebot remote; pushing has the same end state.
	b.GitIn(b.Root, "push", "-q", "-u", "origin", "main")
	ids := b.ids()
	contains(t, ids, "never-pushed", false)
	contains(t, ids, "no-remote", false)

	t.Run("unreachable for more than three days", func(t *testing.T) {
		now := b.Ctx.Now().UnixMilli()
		b.update(func(s *state.State) { s.LastSync = now - 3*24*3600_000 })
		contains(t, b.ids(), "unreachable", false)
		b.update(func(s *state.State) { s.LastSync = now - 4*24*3600_000 })
		it := b.mustFind("unreachable")
		// Decision D6: not "since 4d ago".
		eq(t, it.Title, "Last reached the remote 4d ago")
		eq(t, FixText(b.Ctx, it.Fix), "homebot save")
	})
	t.Run("ahead, behind, diverged", func(t *testing.T) {
		b.Files(map[string]string{"home/.home/autolink/common/vimrc": "x\n"})
		b.GitIn(b.Root, "add", "-A")
		b.GitIn(b.Root, "commit", "-q", "-m", "local")
		eq(t, b.mustFind("unpushed").Title, "1 commit not pushed")
		b.GitIn(b.Root, "push", "-q")
		b.GitIn(b.Root, "reset", "-q", "--hard", "HEAD~1")
		eq(t, b.mustFind("behind").Title, "1 new commit on the remote")
		b.Files(map[string]string{"home/.home/autolink/common/inputrc": "x\n"})
		b.GitIn(b.Root, "add", "-A")
		b.GitIn(b.Root, "commit", "-q", "-m", "other")
		it := b.mustFind("diverged")
		eq(t, it.Level, Urgent)
		eq(t, it.Fix.Shell, "git -C ~/.home pull --rebase")
	})
}

func TestUncommittedChangesWhenNothingAutosaves(t *testing.T) {
	b := newBox(t, testutil.Options{})
	b.commit()
	b.Files(map[string]string{"home/.home/autolink/common/zshrc": "changed\n"})
	// autosave.commit is off by default.
	eq(t, b.mustFind("uncommitted").Title, "1 uncommitted change")
	b.Files(map[string]string{"home/.home/homebot.yml": "profiles:\n  default: workstation\nautosave:\n  commit: hourly\n"})
	b.reload()
	b.GitIn(b.Root, "add", "homebot.yml")
	b.GitIn(b.Root, "commit", "-q", "-m", "config")
	now := b.Ctx.Now().UnixMilli()
	b.update(func(s *state.State) { s.LastAutosave = now - 3600_000 })
	contains(t, b.ids(), "uncommitted", false)
	b.update(func(s *state.State) { s.LastAutosave = now - 3*24*3600_000 })
	contains(t, b.ids(), "uncommitted", true)
}

func TestNotARepo(t *testing.T) {
	b := &box{Box: testutil.Sandbox(t, testutil.Options{}), t: t}
	b.reload()
	it := b.mustFind("no-repo")
	eq(t, it.Level, Urgent)
	eq(t, it.Title, "~/.home is not a git repository")
	eq(t, it.Fix.Shell, "git -C ~/.home init")
	ids := b.ids()
	for _, id := range []string{"no-guard", "no-gitleaks", "no-remote"} {
		contains(t, ids, id, false)
	}
}

// --- backups (cleanup-signing) ---

func backedUp(t *testing.T, b *box) {
	t.Helper()
	b.at("2026-10-12T10:00:00Z")
	b.Files(map[string]string{"home/.home/autolink/common/a": "r", "home/.a": "m"})
	plan, err := linker.PlanLinks(b.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := linker.BackupConflicts(b.Ctx, plan); err != nil {
		t.Fatal(err)
	}
	if plan, err = linker.PlanLinks(b.Ctx); err != nil {
		t.Fatal(err)
	}
	if err := linker.ApplyLinks(b.Ctx, plan, linker.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestTheInboxListsBackupsWithCleanupAsTheFix(t *testing.T) {
	b := newBox(t, testutil.Options{})
	backedUp(t, b)
	it := b.mustFind("backups")
	eq(t, it.Level, Todo)
	eq(t, FixText(b.Ctx, it.Fix), "homebot cleanup")
	if !strings.HasPrefix(it.Title, "1 backup that homebot link moved aside") {
		t.Errorf("title = %q", it.Title)
	}
	eq(t, it.Detail, "~/.a.homebot-backup-20261012-100000")
}

func TestNewThingsInHomeAreOneTriageItemNamingTheFirstThree(t *testing.T) {
	b := newBox(t, testutil.Options{})
	b.commit() // homebot.yml, untracked in the repo, is a candidate too
	contains(t, b.ids(), "triage", false)
	for _, name := range []string{".aa", ".bb", ".cc", ".dd"} {
		if err := os.WriteFile(b.h(name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	it := b.mustFind("triage")
	eq(t, it.Level, Todo)
	eq(t, it.Title, "4 new things to sort: track, ignore or snooze")
	eq(t, it.Detail, "~/.aa, ~/.bb, ~/.cc, ...")
	eq(t, FixText(b.Ctx, it.Fix), "homebot triage")
}

func TestNoBackupsItemOnceTheyAreGone(t *testing.T) {
	b := newBox(t, testutil.Options{})
	backedUp(t, b)
	// Deleted the way homebot cleanup deletes it: the review, answering delete.
	backups, err := cleanup.FindBackups(b.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("backups = %v, want the one link moved aside", backups)
	}
	deleteIt := func(cleanup.Backup, cleanup.Question) (cleanup.Answer, error) { return cleanup.Delete, nil }
	r, err := cleanup.Review(b.Ctx, backups, deleteIt, nil)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, r.Deleted, 1)
	contains(t, b.ids(), "backups", false)
}

func TestFirstThreeSaysWhenMoreWereLeftOut(t *testing.T) {
	names := []string{"a", "b", "c", "d"}
	eq(t, firstThree(names, true), "a, b, c, ...")
	eq(t, firstThree(names, false), "a, b, c")
	eq(t, firstThree(names[:3], true), "a, b, c")
	eq(t, firstThree(names[:1], true), "a")
}

// --- render ---

func TestRendersAShortListWithFixesAndWhatWasDoneAutomatically(t *testing.T) {
	b := newBox(t, testutil.Options{})
	var items []Item
	for i := range 7 {
		level := Todo
		if i == 0 {
			level = Urgent
		}
		items = append(items, Item{ID: "i" + string(rune('0'+i)), Level: level, Title: "Thing " + string(rune('0'+i)), Fix: &Fix{Args: []string{"link"}}})
	}
	if err := state.Journal(b.Ctx, state.Entry{Msg: "sealed 2 secrets"}); err != nil {
		t.Fatal(err)
	}
	journal, err := state.ReadJournal(b.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	text := ansi.Strip(Render(b.Ctx, items, journal, false))
	for _, want := range []string{
		"7 things need you (1 urgent)",
		"→ homebot link",
		"...and 2 more",
		"  Done for you since you last looked:\n    sealed 2 secrets\n",
		"homebot next",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("render lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Thing 5") {
		t.Errorf("the sixth item shows without --all:\n%s", text)
	}
	if all := ansi.Strip(Render(b.Ctx, items, nil, true)); !strings.Contains(all, "  7  • Thing 6") || strings.Contains(all, "more") {
		t.Errorf("--all:\n%s", all)
	}
	if empty := ansi.Strip(Render(b.Ctx, nil, nil, false)); !strings.Contains(empty, "nothing needs you") {
		t.Errorf("empty inbox: %q", empty)
	}
}

// Render's styles are the terminal's business (ui.Println); without them the
// text is the TypeScript's, byte for byte.
func TestRenderInTheNeutralVoiceIsTheTypeScriptsBytes(t *testing.T) {
	b := newBox(t, testutil.Options{})
	items := []Item{
		{ID: "no-repo", Level: Urgent, Title: "~/.home is not a git repository", Fix: &Fix{Shell: "git -C ~/.home init"}},
		{ID: "x", Level: Todo, Title: "Profile workstation does not exist yet", Detail: "Write the steps", Fix: &Fix{Args: []string{"unpack"}}},
		{ID: "y", Level: Todo, Title: "No fix"},
	}
	done := []state.Entry{{At: 1, Msg: "failed", Error: true}}
	for i := range 8 {
		done = append(done, state.Entry{At: int64(i + 2), Msg: "did " + string(rune('a'+i))})
	}
	want := "homebot · testhost · 3 things need you (1 urgent)\n" +
		"\n" +
		"  1  ! ~/.home is not a git repository\n" +
		"       → git -C ~/.home init\n" +
		"  2  • Profile workstation does not exist yet\n" +
		"       Write the steps\n" +
		"       → homebot unpack\n" +
		"  3  • No fix\n" +
		"\n" +
		"  Done for you since you last looked:\n" +
		"    did c\n    did d\n    did e\n    did f\n    did g\n    did h"
	eq(t, ansi.Strip(Render(b.Ctx, items, done, false)), want)
	eq(t, ansi.Strip(Render(b.Ctx, nil, nil, false)), "homebot · testhost · nothing needs you. Home is in order.")
	one := []Item{{ID: "a", Level: Todo, Title: "One", Fix: &Fix{Args: []string{"save"}}}}
	eq(t, ansi.Strip(Render(b.Ctx, one, nil, false)), "homebot · testhost · 1 thing needs you\n\n  1  • One\n       → homebot save\n\n  Do the first one now: homebot next")
}

func TestPersonaHeaderAndPipedNeutralHeader(t *testing.T) {
	b := newBox(t, testutil.Options{})
	persona.Override(b.Ctx, &persona.Voice{ID: "tester", Name: "Tester", Emoji: "🧪"})
	items := []Item{
		{ID: "a", Level: Urgent, Title: "Bad", Fix: &Fix{Args: []string{"save"}}},
		{ID: "b", Level: Todo, Title: "Meh"},
	}
	b.Ctx.ForceVoice = true
	eq(t, ansi.Strip(Render(b.Ctx, nil, nil, false)), "🧪 Tester · testhost · nothing needs you. Home is in order.")
	eq(t, ansi.Strip(Render(b.Ctx, items, nil, false)),
		"🧪 Tester · testhost · 2 things need you (1 urgent)\n\n  1  ! Bad\n       → homebot save\n  2  • Meh\n\n  Do the first one now: homebot next")
	b.Ctx.ForceVoice = false
	eq(t, ansi.Strip(Render(b.Ctx, nil, nil, false)), "homebot · testhost · nothing needs you. Home is in order.")
	eq(t, ansi.Strip(Render(b.Ctx, items, nil, false)),
		"homebot · testhost · 2 things need you (1 urgent)\n\n  1  ! Bad\n       → homebot save\n  2  • Meh\n\n  Do the first one now: homebot next")
}

// layout.test.ts "inbox details"
func renderDetail(t *testing.T, detail string) []string {
	t.Helper()
	b := newBox(t, testutil.Options{})
	it := Item{ID: "blocked", Level: Urgent, Title: "A commit was refused", Detail: detail, Fix: &Fix{Args: []string{"save"}}}
	lines := strings.Split(ansi.Strip(Render(b.Ctx, []Item{it}, nil, false)), "\n")
	return lines[2 : len(lines)-2]
}

func TestALongDetailWrapsUnderItsItem(t *testing.T) {
	lines := renderDetail(t, "gitleaks found something that looks like a secret. Move it to private/ (homebot pack --private <path>) or unstage it, then save again so the autosave can go on.")
	if len(lines) <= 3 {
		t.Fatalf("lines = %q", lines)
	}
	for _, line := range lines[1 : len(lines)-1] {
		if !strings.HasPrefix(line, "       ") || len(line) > 100 {
			t.Errorf("line %q", line)
		}
	}
}

func TestTheFixUnderAReasonIsShownUpToThreeLinesWithoutStoredColors(t *testing.T) {
	lines := renderDetail(t, "Commit refused: private/a is plaintext. Unstage it:\n  \x1b[36mgit rm --cached private/a\x1b[39m\nthird\nfourth")
	want := []string{
		"  1  ! A commit was refused",
		"       Commit refused: private/a is plaintext. Unstage it:",
		"         git rm --cached private/a",
		"       third",
		"       → homebot save",
	}
	if !slices.Equal(lines, want) {
		t.Errorf("lines = %q\nwant %q", lines, want)
	}
}

// --- nudge, cache, notifications ---

func readCache(t *testing.T, c *app.Context, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(c.CacheDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestTheNudgeLineAndCacheFiles(t *testing.T) {
	b := newBox(t, testutil.Options{})
	one := []Item{{ID: "a", Level: Todo, Title: "x"}}
	emoji, text := NudgeLine(b.Ctx, nil)
	eq(t, emoji, "")
	eq(t, text, "")
	emoji, text = NudgeLine(b.Ctx, one)
	eq(t, emoji, "🚚")
	eq(t, text, "homebot: 1 thing needs you, run: homebot")
	if err := WriteCache(b.Ctx, append(one, Item{ID: "b", Level: Urgent, Title: "y"})); err != nil {
		t.Fatal(err)
	}
	eq(t, readCache(t, b.Ctx, "count"), "2!\n")
	eq(t, readCache(t, b.Ctx, "nudge"), "🚚 homebot: 2 things need you (1 urgent), run: homebot\n")
	if err := WriteCache(b.Ctx, one); err != nil {
		t.Fatal(err)
	}
	eq(t, readCache(t, b.Ctx, "count"), "1\n")
	if err := WriteCache(b.Ctx, nil); err != nil {
		t.Fatal(err)
	}
	eq(t, readCache(t, b.Ctx, "nudge"), "")
	eq(t, readCache(t, b.Ctx, "count"), "")
	if _, err := os.Stat(filepath.Join(b.Ctx.CacheDir, "symbol")); !os.IsNotExist(err) {
		t.Errorf("neutral symbol exists: %v", err)
	}
}

func TestPersonaNudgeSymbolAndStablePick(t *testing.T) {
	b := newBox(t, testutil.Options{})
	persona.Override(b.Ctx, &persona.Voice{ID: "tester", Name: "Tester", Emoji: "🧪", Lines: map[string][]persona.Line{
		"inbox.nudge": {{Text: "{bot}: one {n}{urgent}", Lang: language.English}, {Text: "{bot}: two {n}{urgent}", Lang: language.English}},
	}})
	nudge := func(ids ...string) string {
		t.Helper()
		var items []Item
		for _, id := range ids {
			items = append(items, Item{ID: id, Level: Todo})
		}
		if err := WriteCache(b.Ctx, items); err != nil {
			t.Fatal(err)
		}
		return readCache(t, b.Ctx, "nudge")
	}
	seen := map[string]bool{}
	for _, ids := range [][]string{{"a"}, {"b"}, {"c"}, {"d"}, {"e"}, {"f"}, {"g"}, {"h"}} {
		first := nudge(ids...)
		if first != "🧪 Tester: one 1 homebot\n" && first != "🧪 Tester: two 1 homebot\n" {
			t.Fatalf("persona nudge for %q = %q", ids, first)
		}
		// Refreshing with the same inbox keeps the wording.
		eq(t, nudge(ids...), first)
		seen[first] = true
	}
	// A different inbox may pick the other variant; with the old empty key
	// every inbox would pick the same one.
	if len(seen) != 2 {
		t.Errorf("eight different inboxes all got one of %v: the pick is not keyed on the item ids", seen)
	}
	eq(t, readCache(t, b.Ctx, "symbol"), "🧪\n")
	eq(t, readCache(t, b.Ctx, "count"), "1\n")
}

func TestDefaultNudgeUsesLoadedPersonaAndNoneIsNeutral(t *testing.T) {
	b := newBox(t, testutil.Options{})
	b.Ctx.Env = b.Ctx.Env.Without("HOMEBOT_PERSONA")
	v, _, err := persona.Load(b.Ctx, "neatpik")
	if err != nil {
		t.Fatal(err)
	}
	items := []Item{{ID: "a", Level: Todo}}
	if err := WriteCache(b.Ctx, items); err != nil {
		t.Fatal(err)
	}
	if got := readCache(t, b.Ctx, "nudge"); !strings.HasPrefix(got, v.Emoji+" ") {
		t.Errorf("default nudge = %q, want emoji %q", got, v.Emoji)
	}
	none := b.Ctx.Fork(map[string]string{"HOMEBOT_PERSONA": "none"})
	if err := WriteCache(none, items); err != nil {
		t.Fatal(err)
	}
	if got := readCache(t, none, "nudge"); got != "🚚 homebot: 1 thing needs you, run: homebot\n" {
		t.Errorf("none nudge = %q", got)
	}
	if _, err := os.Stat(filepath.Join(none.CacheDir, "symbol")); !os.IsNotExist(err) {
		t.Errorf("none left symbol: %v", err)
	}
}

func TestPersonaBrokenOnlyForConfiguredSelection(t *testing.T) {
	b := newBox(t, testutil.Options{Config: testutil.BaseConfig + "persona: tester\n"})
	b.Ctx.Env = b.Ctx.Env.Without("HOMEBOT_PERSONA")
	it := b.mustFind("persona_broken")
	if it.Level != Todo || it.Fix == nil || !slices.Equal(it.Fix.Args, []string{"persona", "validate", "tester"}) {
		t.Errorf("broken persona item = %+v", it)
	}
	env := b.Ctx.Fork(map[string]string{"HOMEBOT_PERSONA": "missing"})
	items, err := Collect(env)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == "persona_broken" {
			t.Fatal("bad environment selection made a configured-persona item")
		}
	}
}

func TestPersonaFIFODoesNotBlock(t *testing.T) {
	for _, kind := range []string{"fifo", "zero"} {
		t.Run(kind, func(t *testing.T) {
			b := newBox(t, testutil.Options{Config: testutil.BaseConfig + "persona: tester\n"})
			b.Ctx.Env = b.Ctx.Env.Without("HOMEBOT_PERSONA")
			path := filepath.Join(b.Ctx.ConfigDir, "personas", "tester.toml")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			var err error
			if kind == "fifo" {
				err = syscall.Mkfifo(path, 0o600)
			} else {
				err = os.Symlink("/dev/zero", path)
			}
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				items, err := Refresh(b.Ctx, Options{})
				if err == nil {
					found := false
					for _, it := range items {
						found = found || it.ID == "persona_broken"
					}
					if !found {
						err = errors.New("missing persona_broken item")
					}
				}
				done <- err
			}()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("inbox refresh blocked on persona path")
			}
			// The whole line is the neutral one for the items Refresh found.
			items, err := Collect(b.Ctx)
			if err != nil {
				t.Fatal(err)
			}
			urgent := ""
			if u := countUrgent(items); u > 0 {
				urgent = fmt.Sprintf(" (%d urgent)", u)
			}
			want := fmt.Sprintf("🚚 homebot: %d things need you%s, run: homebot\n", len(items), urgent)
			if len(items) < 2 {
				t.Fatalf("expected a fresh repo to need several things, got %d items", len(items))
			}
			eq(t, readCache(t, b.Ctx, "nudge"), want)
			if _, err := os.Stat(filepath.Join(b.Ctx.CacheDir, "symbol")); !os.IsNotExist(err) {
				t.Errorf("unsafe persona left a symbol file: %v", err)
			}
		})
	}
}

func TestRefreshWritesTheCacheFromWhatItCollects(t *testing.T) {
	b := newBox(t, testutil.Options{})
	items, err := Refresh(b.Ctx, Options{})
	if err != nil {
		t.Fatal(err)
	}
	emoji, text := NudgeLine(b.Ctx, items)
	eq(t, readCache(t, b.Ctx, "nudge"), emoji+" "+text+"\n")
	st, err := state.Load(b.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Notify != nil {
		t.Errorf("Refresh without Notify recorded %+v", st.Notify)
	}
}

// sent is what the notification stub was called with, one call per line.
func sent(t *testing.T, b *box) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(b.Dir, "sent"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func notifyKey(t *testing.T, b *box) *state.Notify {
	t.Helper()
	st, err := state.Load(b.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	return st.Notify
}

func TestNotifiesOncePerNewSetOfUrgentItemsThenDaily(t *testing.T) {
	b := newBox(t, testutil.Options{})
	b.WithStubs(map[string]string{"notify-send": `printf '%s|' "$@" >> "$WORK_SENT"; echo >> "$WORK_SENT"`})
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"WORK_SENT": filepath.Join(b.Dir, "sent")})
	urgent := []Item{{ID: "a", Level: Urgent, Title: "Bad"}, {ID: "t", Level: Todo, Title: "meh"}}
	t0 := b.Ctx.Now()
	notify := func(items []Item, at time.Duration) bool {
		t.Helper()
		b.Ctx.Now = func() time.Time { return t0.Add(at) }
		shown, err := Notify(b.Ctx, items)
		if err != nil {
			t.Fatal(err)
		}
		return shown
	}
	eq(t, notify(urgent, 0), true)
	eq(t, notify(urgent, time.Hour), false)
	eq(t, notifyKey(t, b).Key, "a")
	eq(t, notifyKey(t, b).At, t0.UnixMilli())
	worse := append([]Item{{ID: "b", Level: Urgent, Title: "Worse"}}, urgent...)
	eq(t, notify(worse, 2*time.Hour), true)
	eq(t, notifyKey(t, b).Key, "a,b")
	// The same set again a day later.
	eq(t, notify(worse, 26*time.Hour-time.Millisecond), false)
	eq(t, notify(worse, 26*time.Hour), true)
	eq(t, notify(nil, 27*time.Hour), false)
	if k := notifyKey(t, b); k != nil {
		t.Errorf("notify = %+v, want it cleared", k)
	}
	want := []string{
		"--app-name=homebot|homebot needs you|Bad. Run: homebot|",
		"--app-name=homebot|homebot needs you|Worse (+1 more). Run: homebot|",
		"--app-name=homebot|homebot needs you|Worse (+1 more). Run: homebot|",
	}
	if got := sent(t, b); !slices.Equal(got, want) {
		t.Errorf("sent %q\nwant %q", got, want)
	}
}

func TestNotifySendEscapesBodyMarkupOnly(t *testing.T) {
	b := newBox(t, testutil.Options{})
	b.WithStubs(map[string]string{"notify-send": `printf '%s|' "$@" >> "$WORK_SENT"; echo >> "$WORK_SENT"`})
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"WORK_SENT": filepath.Join(b.Dir, "sent")})
	if _, err := Notify(b.Ctx, []Item{{ID: "x", Level: Urgent, Title: "A < B & C > D"}}); err != nil {
		t.Fatal(err)
	}
	want := "--app-name=homebot|homebot needs you|A &lt; B &amp; C &gt; D. Run: homebot|"
	if got := sent(t, b); len(got) != 1 || got[0] != want {
		t.Errorf("notify-send arguments = %q, want %q", got, want)
	}
}

func TestANotificationThatFailsIsStillRecorded(t *testing.T) {
	b := newBox(t, testutil.Options{})
	b.WithStubs(map[string]string{"notify-send": "exit 1"})
	shown, err := Notify(b.Ctx, []Item{{ID: "a", Level: Urgent, Title: "Bad"}})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, shown, false)
	eq(t, notifyKey(t, b).Key, "a")
}

func TestNotificationsWithoutNotifySendUseGdbus(t *testing.T) {
	b := newBox(t, testutil.Options{})
	if lookPath(b.Ctx, "notify-send") {
		t.Skip("notify-send is installed outside the sandbox's bin")
	}
	b.WithStubs(map[string]string{"gdbus": `printf '%s|' "$@" >> "$WORK_SENT"; echo >> "$WORK_SENT"`})
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"WORK_SENT": filepath.Join(b.Dir, "sent")})
	if _, err := Notify(b.Ctx, []Item{{ID: "a", Level: Urgent, Title: `Say "hi"`}}); err != nil {
		t.Fatal(err)
	}
	want := []string{`call|--session|--dest|org.freedesktop.Notifications|--object-path|/org/freedesktop/Notifications|--method|org.freedesktop.Notifications.Notify|"homebot"|0|""|"homebot needs you"|"Say \"hi\". Run: homebot"|[]|{}|10000|`}
	if got := sent(t, b); !slices.Equal(got, want) {
		t.Errorf("sent %q\nwant %q", got, want)
	}
}

// gdbus parses each argument as GVariant text, so a persona's quote, backslash
// or line break must not end the string early, escape what follows or split
// the argument.
func TestGdbusNotificationSurvivesAHostilePersonaLine(t *testing.T) {
	b := newBox(t, testutil.Options{})
	if lookPath(b.Ctx, "notify-send") {
		t.Skip("notify-send is installed outside the sandbox's bin")
	}
	b.WithStubs(map[string]string{"gdbus": `printf '%s|' "$@" >> "$WORK_SENT"; echo >> "$WORK_SENT"`})
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"WORK_SENT": filepath.Join(b.Dir, "sent")})
	persona.Override(b.Ctx, &persona.Voice{ID: "tester", Name: "Tester", Emoji: "🧪", Lines: map[string][]persona.Line{
		"inbox.notify.title": {{Text: "{bot} it's \"on\" C:\\path\nnow", Lang: language.English}},
		"inbox.notify.body":  {{Text: "{title}{more} isn't \"ok\" <b> \\\nnext.", Lang: language.English}},
	}})
	if _, err := Notify(b.Ctx, []Item{{ID: "a", Level: Urgent, Title: "Bad"}}); err != nil {
		t.Fatal(err)
	}
	want := []string{`call|--session|--dest|org.freedesktop.Notifications|--object-path|/org/freedesktop/Notifications|--method|org.freedesktop.Notifications.Notify|"homebot"|0|""|"🧪 Tester it's \"on\" C:\\path now"|"Bad isn't \"ok\" &lt;b&gt; \\\nnext. homebot"|[]|{}|10000|`}
	if got := sent(t, b); !slices.Equal(got, want) {
		t.Errorf("sent %q\nwant %q", got, want)
	}
}

func TestNotificationsOnMacOSGoThroughOsascript(t *testing.T) {
	b := newBox(t, testutil.Options{OS: app.MacOS})
	b.WithStubs(map[string]string{"osascript": `printf '%s|' "$@" >> "$WORK_SENT"; echo >> "$WORK_SENT"`})
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"WORK_SENT": filepath.Join(b.Dir, "sent")})
	items := []Item{{ID: "a", Level: Urgent, Title: `Say "hi" <&>`}, {ID: "b", Level: Urgent, Title: "b"}}
	if _, err := Notify(b.Ctx, items); err != nil {
		t.Fatal(err)
	}
	want := []string{`-e|display notification "Say \"hi\" <&> (+1 more). Run: homebot" with title "homebot needs you"|`}
	if got := sent(t, b); !slices.Equal(got, want) {
		t.Errorf("sent %q\nwant %q", got, want)
	}
}

func TestNoNotifierMeansNothingShown(t *testing.T) {
	b := newBox(t, testutil.Options{})
	if lookPath(b.Ctx, "notify-send") || lookPath(b.Ctx, "gdbus") {
		t.Skip("a notifier is installed outside the sandbox's bin")
	}
	shown, err := Notify(b.Ctx, []Item{{ID: "a", Level: Urgent, Title: "Bad"}})
	if err != nil {
		t.Fatal(err)
	}
	eq(t, shown, false)
	eq(t, notifyKey(t, b).Key, "a")
}

// lookPath says name is installed outside the sandbox's own bin.
func lookPath(c *app.Context, name string) bool { return proc.HasCommand(c, name) }

func TestJSStringLeavesHTMLAlone(t *testing.T) {
	eq(t, jsString(`a "b" <c> & \ é`), `"a \"b\" <c> & \\ é"`)
}

func TestJSStringEscapesLineBreaksAsJSONStringifyDoes(t *testing.T) {
	// \n, \r and \t are escapes AppleScript string literals read too.
	eq(t, jsString("a\nb"), `"a\nb"`)
	eq(t, jsString("a\r\tb"), `"a\r\tb"`)
	// Other control characters get JSON's \u form, as with JSON.stringify.
	eq(t, jsString("a\x01b"), `"a\u0001b"`)
}

func TestANewlineInATitleStaysInsideTheAppleScriptString(t *testing.T) {
	b := newBox(t, testutil.Options{OS: app.MacOS})
	b.WithStubs(map[string]string{"osascript": `printf '%s|' "$@" >> "$WORK_SENT"; echo >> "$WORK_SENT"`})
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"WORK_SENT": filepath.Join(b.Dir, "sent")})
	if _, err := Notify(b.Ctx, []Item{{ID: "a", Level: Urgent, Title: "line one\nline \"two\""}}); err != nil {
		t.Fatal(err)
	}
	want := []string{`-e|display notification "line one\nline \"two\". Run: homebot" with title "homebot needs you"|`}
	if got := sent(t, b); !slices.Equal(got, want) {
		t.Errorf("sent %q\nwant %q", got, want)
	}
}
