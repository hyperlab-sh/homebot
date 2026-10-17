package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/inbox"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/update"
)

func TestHelperUpgradeJournalsNeutralSuccessAndNotifies(t *testing.T) {
	calls := 0
	lockPath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			t.Errorf("open tidy lock: %v", err)
		} else {
			if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
				t.Errorf("tidy lock still held during update: %v", err)
			}
			_ = lock.Close()
		}
		w.Header().Set("Location", "/releases/tag/v0.1.1")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	b := testutil.Sandbox(t, testutil.Options{Git: true, Config: testutil.BaseConfig + "update: auto\n"})
	lockPath = filepath.Join(b.Ctx.StateDir, "tidy.lock")
	prefix := filepath.Join(b.Dir, "brew")
	versioned := filepath.Join(prefix, "Caskroom", "homebot", "0.1.0", "homebot")
	stable := filepath.Join(prefix, "bin", "homebot")
	brew := filepath.Join(prefix, "bin", "brew")
	for _, path := range []string{versioned, stable, brew} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{versioned, stable} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\necho 0.1.0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	brewBody := fmt.Sprintf("#!/bin/sh\nprintf '#!/bin/sh\\necho 0.1.1\\n' > %q\nchmod 755 %q\n", stable, stable)
	if err := os.WriteFile(brew, []byte(brewBody), 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(b.Dir, "notify.log")
	b.WithStubs(map[string]string{"notify-send": "printf '%s\\n' \"$*\" >> " + fmt.Sprintf("%q", log)})
	b.Ctx.Exe = versioned
	b.Ctx.BuildVersion, b.Ctx.BuildChannel = "0.1.0", "release"
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"HOMEBOT_TEST_RELEASES_URL": srv.URL})
	now := b.Ctx.Now()
	if err := state.Update(b.Ctx, func(s *state.State) {
		s.UpdateCheck = &state.UpdateCheck{At: now.Add(-24 * time.Hour).UnixMilli(), Latest: "0.1.1", FirstSeen: now.Add(-48 * time.Hour).UnixMilli()}
	}); err != nil {
		t.Fatal(err)
	}
	if err := tidyRun(b.Ctx, tidyFlags{helper: true, notify: true, quiet: true}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("release checks = %d, want check plus pre-upgrade guard", calls)
	}
	entries, err := state.ReadJournal(b.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if strings.Contains(entry.Msg, "updated homebot to 0.1.1") {
			found = true
		}
	}
	if !found {
		t.Errorf("neutral update journal missing: %+v", entries)
	}
	data, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(data), "updated to 0.1.1") {
		t.Errorf("notification = %q, %v", data, err)
	}
	st, err := state.Load(b.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.UpdateFailed != nil {
		t.Errorf("failure was not cleared: %+v", st.UpdateFailed)
	}
}

func updateItem(t *testing.T, b *testutil.Box) (inbox.Item, bool) {
	t.Helper()
	items, err := inbox.Collect(b.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.ID == "update-available" {
			return it, true
		}
	}
	return inbox.Item{}, false
}

// Spec "Soak" and "Notify": update: notify shows the item the moment the
// helper first sees a release, with no wait; the wait belongs to automatic
// upgrades. A newer release at T+24h replaces it, and notify never upgrades.
func TestHelperNotifyShowsTheItemAtOnceAndFollowsTheNewestRelease(t *testing.T) {
	latest, calls := "0.1.1", 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Location", "/releases/tag/v"+latest)
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	b := testutil.Sandbox(t, testutil.Options{Git: true, Config: testutil.BaseConfig + "update: notify\n"})
	prefix := filepath.Join(b.Dir, "brew")
	versioned := filepath.Join(prefix, "Caskroom", "homebot", "0.1.0", "homebot")
	brew := filepath.Join(prefix, "bin", "brew")
	for _, path := range []string{versioned, brew} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(versioned, []byte("#!/bin/sh\necho 0.1.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	brewRuns := filepath.Join(b.Dir, "brew-runs")
	if err := os.WriteFile(brew, []byte(fmt.Sprintf("#!/bin/sh\necho run >> %q\n", brewRuns)), 0o755); err != nil {
		t.Fatal(err)
	}
	b.WithStubs(map[string]string{"notify-send": "exit 0"})
	b.Ctx.Exe = versioned
	b.Ctx.BuildVersion, b.Ctx.BuildChannel = "0.1.0", "release"
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"HOMEBOT_TEST_RELEASES_URL": srv.URL})
	start := b.Ctx.Now()
	clock := start
	b.Ctx.Now = func() time.Time { return clock }
	helper := func(after time.Duration) {
		t.Helper()
		clock = start.Add(after)
		if err := tidyRun(b.Ctx, tidyFlags{helper: true, notify: true, quiet: true}); err != nil {
			t.Fatal(err)
		}
	}
	wantItem := func(when, version string) {
		t.Helper()
		it, ok := updateItem(t, b)
		if !ok {
			t.Fatalf("%s: no update-available item", when)
		}
		if !strings.Contains(it.Detail, "Version "+version+" is available") {
			t.Errorf("%s: detail = %q, want version %s", when, it.Detail, version)
		}
		if fix := inbox.FixText(b.Ctx, it.Fix); !strings.Contains(fix, "upgrade --cask hyperlab-sh/tap/homebot") {
			t.Errorf("%s: fix = %q, want the brew command", when, fix)
		}
	}
	if _, ok := updateItem(t, b); ok {
		t.Fatal("an item before any check")
	}
	helper(0)
	wantItem("T", "0.1.1")
	helper(23 * time.Hour)
	if calls != 1 {
		t.Errorf("T+23h: %d requests, want the day's check only", calls)
	}
	wantItem("T+23h", "0.1.1")
	latest = "0.1.2"
	helper(24 * time.Hour)
	if calls != 2 {
		t.Errorf("T+24h: %d requests, want the second daily check", calls)
	}
	wantItem("T+24h", "0.1.2")
	helper(72 * time.Hour)
	wantItem("T+72h", "0.1.2")
	if _, err := os.Stat(brewRuns); !os.IsNotExist(err) {
		t.Errorf("notify upgraded: %v", err)
	}
}

// Spec "Attempts" for a script install, end to end through the helper's state
// and the inbox: a release whose signature does not verify leaves the old
// binary and an item that says so, not silence.
func TestHelperScriptSignatureFailureReachesTheInbox(t *testing.T) {
	srv := latestServer(t)
	b := testutil.Sandbox(t, testutil.Options{Git: true, Config: testutil.BaseConfig + "update: auto\n"})
	scriptInstall(t, b, srv.URL)
	b.WithStubs(map[string]string{"cosign": "echo 'GitVersion: v2.4.3'"})
	before := update.InstallScript
	// CosignMin is read from these bytes, so the stub carries the real minimum.
	update.InstallScript = []byte("#!/bin/sh\nCOSIGN_MIN=" + update.CosignMin() + "\necho 'signature verification failed' >&2\nexit 5\n")
	t.Cleanup(func() { update.InstallScript = before })
	b.Ctx.BuildVersion, b.Ctx.BuildChannel = "0.1.0", "release"
	start := b.Ctx.Now()
	clock := start
	b.Ctx.Now = func() time.Time { return clock }
	step := func(after time.Duration) {
		t.Helper()
		clock = start.Add(after)
		out, err := update.Step(b.Ctx, update.Build{Version: "0.1.0", Channel: "release"}, nil, config.UpdateAuto, 30*time.Second)
		if err != nil || out != (update.Outcome{}) {
			t.Fatalf("step at +%s = %+v, %v", after, out, err)
		}
	}
	step(0)
	if _, ok := updateItem(t, b); ok {
		t.Fatal("an auto script install in its soak must stay quiet")
	}
	step(48 * time.Hour)
	it, ok := updateItem(t, b)
	if !ok {
		t.Fatal("no item after the signature failed")
	}
	if !strings.Contains(it.Detail, "Verification of version 0.1.1 failed") || !strings.Contains(it.Detail, "report this failure") {
		t.Errorf("detail = %q, want the signature failure", it.Detail)
	}
	if fix := inbox.FixText(b.Ctx, it.Fix); !strings.Contains(fix, "--version 'v0.1.1'") && !strings.Contains(fix, "--version v0.1.1") {
		t.Errorf("fix = %q, want the signed installer command for v0.1.1", fix)
	}
	// An offline check later keeps the item: the answer is old, not wrong.
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"HOMEBOT_TEST_RELEASES_URL": "http://127.0.0.1:1"})
	step(72 * time.Hour)
	if st, err := state.Load(b.Ctx); err != nil || st.UpdateCheck == nil || !st.UpdateCheck.Failed {
		t.Fatalf("offline check not recorded: %+v, %v", st, err)
	}
	if it, ok := updateItem(t, b); !ok || !strings.Contains(it.Detail, "Verification of version 0.1.1 failed") {
		t.Errorf("item after an offline check = %+v, %v", it, ok)
	}
}
