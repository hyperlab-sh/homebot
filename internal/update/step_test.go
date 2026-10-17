package update

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func stepServer(t *testing.T, latest func(int) (int, string)) (*httptest.Server, *int) {
	t.Helper()
	calls := new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		if r.URL.Path != "/releases/latest" {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		status, version := latest(*calls)
		if version != "" {
			w.Header().Set("Location", "/releases/tag/v"+version)
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, calls
}

type stepFixture struct {
	box                   *testutil.Box
	now                   time.Time
	brew, stable, counter string
}

func caskStepFixture(t *testing.T, server string) *stepFixture {
	t.Helper()
	b := testutil.Sandbox(t, testutil.Options{})
	prefix := filepath.Join(b.Dir, "brew")
	versioned := filepath.Join(prefix, "Caskroom", "homebot", "0.1.0", "homebot")
	writeBinary(t, versioned)
	stable := filepath.Join(prefix, "bin", "homebot")
	writeBinary(t, stable)
	brew := filepath.Join(prefix, "bin", "brew")
	counter := filepath.Join(b.Dir, "brew-runs")
	b.Ctx.Exe = versioned
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{testReleasesURL: server})
	now := time.UnixMilli(testutil.TestNow)
	f := &stepFixture{box: b, now: now, brew: brew, stable: stable, counter: counter}
	f.setBrew(t, "echo 0.1.1")
	b.Ctx.Now = func() time.Time { return f.now }
	return f
}

func (f *stepFixture) setBrew(t *testing.T, installedLine string) {
	t.Helper()
	body := fmt.Sprintf("#!/bin/sh\nprintf 'run\\n' >> %q\nprintf '#!/bin/sh\\n%s\\n' > %q\nchmod 755 %q\n", f.counter, installedLine, f.stable, f.stable)
	if err := os.WriteFile(f.brew, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(f.brew, 0o755); err != nil {
		t.Fatal(err)
	}
}

func (f *stepFixture) runs(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(f.counter)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "run\n")
}

func (f *stepFixture) at(hours int) {
	f.now = time.UnixMilli(testutil.TestNow).Add(time.Duration(hours) * time.Hour)
}

// atDuration moves the clock to d after the first check.
func (f *stepFixture) atDuration(d time.Duration) { f.now = time.UnixMilli(testutil.TestNow).Add(d) }

// rewindCheck makes the next check due without moving the clock, as a clock
// that jumped or a state file edited by hand would. It is the only way to
// reach the 24-hour attempt gate: attempts only happen on a check, checks are
// a day apart, and an attempt stamps the check, so the cadence alone always
// stops a second attempt in the same day first.
func rewindCheck(t *testing.T, f *stepFixture) {
	t.Helper()
	if err := state.Update(f.box.Ctx, func(s *state.State) { s.UpdateCheck.At = f.now.Add(-checkInterval).UnixMilli() }); err != nil {
		t.Fatal(err)
	}
}

func (f *stepFixture) step(t *testing.T, mode config.UpdateMode) Outcome {
	t.Helper()
	out, err := Step(f.box.Ctx, Build{Version: "0.1.0", Channel: "release"}, nil, mode, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestStepCadenceSoakAndUpgrade(t *testing.T) {
	srv, calls := stepServer(t, func(int) (int, string) { return 302, "0.1.1" })
	f := caskStepFixture(t, srv.URL)
	if out := f.step(t, config.UpdateAuto); out.UpdatedVersion != "" || *calls != 1 || f.runs(t) != 0 {
		t.Fatalf("first: %+v calls=%d runs=%d", out, *calls, f.runs(t))
	}
	f.at(23)
	f.step(t, config.UpdateAuto)
	if *calls != 1 {
		t.Errorf("checked again before 24h: %d", *calls)
	}
	f.at(24)
	f.step(t, config.UpdateAuto)
	if *calls != 2 || f.runs(t) != 0 {
		t.Errorf("24h: calls=%d runs=%d", *calls, f.runs(t))
	}
	f.at(48)
	out := f.step(t, config.UpdateAuto)
	if out.UpdatedVersion != "0.1.1" || *calls != 4 || f.runs(t) != 1 {
		t.Errorf("48h: %+v calls=%d runs=%d", out, *calls, f.runs(t))
	}
	st, err := state.Load(f.box.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.UpdateFailed != nil || st.UpdateCheck.LastAttempt != f.now.UnixMilli() {
		t.Errorf("state after upgrade = %+v", st)
	}
}

func TestStepMovingReleaseResetsSoak(t *testing.T) {
	srv, calls := stepServer(t, func(n int) (int, string) {
		if n >= 4 {
			return 302, "0.1.2"
		}
		return 302, "0.1.1"
	})
	f := caskStepFixture(t, srv.URL)
	f.step(t, config.UpdateAuto)
	f.at(24)
	f.step(t, config.UpdateAuto)
	f.at(48)
	f.step(t, config.UpdateAuto)
	st, err := state.Load(f.box.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 4 || f.runs(t) != 0 || st.UpdateCheck.Latest != "0.1.2" || st.UpdateCheck.FirstSeen != f.now.UnixMilli() {
		t.Errorf("moving release: calls=%d runs=%d state=%+v", *calls, f.runs(t), st.UpdateCheck)
	}
}

func TestStepFailedCheckRetriesAfterSixHours(t *testing.T) {
	srv, calls := stepServer(t, func(n int) (int, string) {
		if n == 1 {
			return 500, ""
		}
		return 302, "0.1.1"
	})
	f := caskStepFixture(t, srv.URL)
	f.step(t, config.UpdateNotify)
	f.at(5)
	f.step(t, config.UpdateNotify)
	if *calls != 1 {
		t.Errorf("retried before 6h: %d", *calls)
	}
	f.at(6)
	f.step(t, config.UpdateNotify)
	st, err := state.Load(f.box.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 2 || st.UpdateCheck.Failed || st.UpdateCheck.FirstSeen != f.now.UnixMilli() || st.UpdateFailed != nil {
		t.Errorf("retry: calls=%d state=%+v", *calls, st)
	}
}

func failingBrew(t *testing.T, f *stepFixture) {
	t.Helper()
	if err := os.WriteFile(f.brew, []byte(fmt.Sprintf("#!/bin/sh\nprintf 'run\\n' >> %q\nexit 1\n", f.counter)), 0o755); err != nil {
		t.Fatal(err)
	}
}

// Spec "Attempts": an attempt fails at T; none at T+1h (and no request), none
// at T+23h59, the next at T+24h00. That is the check cadence, which is what
// stops the helper within the day.
func TestStepFailedUpgradeAttemptWaitsTwentyFourHours(t *testing.T) {
	srv, calls := stepServer(t, func(int) (int, string) { return 302, "0.1.1" })
	f := caskStepFixture(t, srv.URL)
	failingBrew(t, f)
	f.step(t, config.UpdateAuto)
	f.at(48)
	f.step(t, config.UpdateAuto)
	if f.runs(t) != 1 || *calls != 3 {
		t.Fatalf("first attempt: runs=%d calls=%d, want 1 and 3 (check, guard)", f.runs(t), *calls)
	}
	for _, wait := range []time.Duration{time.Hour, 23*time.Hour + 59*time.Minute} {
		f.atDuration(48*time.Hour + wait)
		f.step(t, config.UpdateAuto)
		if f.runs(t) != 1 || *calls != 3 {
			t.Errorf("A+%s: runs=%d calls=%d, want no attempt and no request", wait, f.runs(t), *calls)
		}
	}
	f.at(72)
	f.step(t, config.UpdateAuto)
	if f.runs(t) != 2 || *calls != 5 {
		t.Errorf("A+24h: runs=%d calls=%d, want the next attempt", f.runs(t), *calls)
	}
	st, err := state.Load(f.box.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.UpdateFailed == nil || st.UpdateFailed.Reason != "upgrade" {
		t.Errorf("failure = %+v", st.UpdateFailed)
	}
}

// The attempt gate (updateCheck.lastAttempt) is separate from the check
// cadence. With a check due again inside the day the helper asks GitHub, finds
// the soaked release and still makes no second attempt until 24h00 after the
// first, whatever the first one's outcome.
func TestStepAttemptGateHoldsWhenACheckIsDue(t *testing.T) {
	for _, tc := range []struct {
		name  string
		brew  func(*testing.T, *stepFixture)
		state string
	}{
		{"failed", failingBrew, "failed"},
		{"tap lag", func(t *testing.T, f *stepFixture) { f.setBrew(t, "echo 0.1.0") }, "lag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, calls := stepServer(t, func(int) (int, string) { return 302, "0.1.1" })
			f := caskStepFixture(t, srv.URL)
			tc.brew(t, f)
			f.step(t, config.UpdateAuto)
			f.at(48)
			f.step(t, config.UpdateAuto)
			if f.runs(t) != 1 {
				t.Fatalf("first attempt count = %d", f.runs(t))
			}
			asked := *calls
			for _, wait := range []time.Duration{time.Hour, 6 * time.Hour, 23*time.Hour + 59*time.Minute} {
				f.atDuration(48*time.Hour + wait)
				rewindCheck(t, f)
				f.step(t, config.UpdateAuto)
				asked++
				if *calls != asked || f.runs(t) != 1 {
					t.Fatalf("A+%s: calls=%d (want %d, the check ran) runs=%d (want 1, the gate held)", wait, *calls, asked, f.runs(t))
				}
			}
			f.atDuration(72 * time.Hour)
			rewindCheck(t, f)
			f.step(t, config.UpdateAuto)
			if f.runs(t) != 2 {
				t.Errorf("A+24h00: runs=%d, want the second attempt", f.runs(t))
			}
		})
	}
}

// AutoGate's retrying case uses the same strict comparison as attemptAt: an
// attempt at A holds the gate until A+24h00m00s exactly, and releases on it.
// version --check reads this, so a one-second drift would tell the user the
// helper is waiting when it is not, or the reverse.
func TestAutoGateAttemptBoundary(t *testing.T) {
	first := time.UnixMilli(testutil.TestNow)
	attempt := first.Add(soakPeriod + time.Hour) // the attempt came an hour after the soak ended
	st := &state.State{UpdateCheck: &state.UpdateCheck{
		At:          first.UnixMilli(),
		Latest:      "0.1.1",
		FirstSeen:   first.UnixMilli(),
		LastAttempt: attempt.UnixMilli(),
	}}
	for _, tc := range []struct {
		name string
		wait time.Duration
		want Phase
	}{
		{"same instant", 0, PhaseRetrying},
		{"1h", time.Hour, PhaseRetrying},
		{"one second before 24h", 24*time.Hour - time.Second, PhaseRetrying},
		{"one millisecond before 24h", 24*time.Hour - time.Millisecond, PhaseRetrying},
		{"exactly 24h", 24 * time.Hour, PhaseReady},
		{"one millisecond after 24h", 24*time.Hour + time.Millisecond, PhaseReady},
		{"30h", 30 * time.Hour, PhaseReady},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := AutoGate(st, "0.1.1", attempt.Add(tc.wait)).Phase; got != tc.want {
				t.Errorf("AutoGate at attempt+%s = %q, want %q", tc.wait, got, tc.want)
			}
		})
	}
}

func TestStepTapLagFourAttemptsThenItemState(t *testing.T) {
	srv, _ := stepServer(t, func(int) (int, string) { return 302, "0.1.1" })
	f := caskStepFixture(t, srv.URL)
	f.setBrew(t, "echo 0.1.0") // brew succeeds but the stable binary is still old.
	f.step(t, config.UpdateAuto)
	for _, hour := range []int{48, 72, 96, 120} {
		f.at(hour)
		if out := f.step(t, config.UpdateAuto); out.UpdatedVersion != "" || out.BrokenCommand != "" {
			t.Errorf("hour %d: %+v", hour, out)
		}
		st, err := state.Load(f.box.Ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st.UpdateFailed != nil || st.UpdateCheck.FirstAttempt != time.UnixMilli(testutil.TestNow).Add(48*time.Hour).UnixMilli() {
			t.Errorf("hour %d: state=%+v", hour, st)
		}
	}
	if f.runs(t) != 4 {
		t.Errorf("brew runs = %d, want 4", f.runs(t))
	}
}

func TestStepSuccessfulCaskMayInstallNewerThanRelease(t *testing.T) {
	srv, _ := stepServer(t, func(int) (int, string) { return 302, "0.1.1" })
	f := caskStepFixture(t, srv.URL)
	f.setBrew(t, "echo 0.1.2")
	f.step(t, config.UpdateAuto)
	f.at(48)
	if out := f.step(t, config.UpdateAuto); out.UpdatedVersion != "0.1.2" {
		t.Errorf("newer stable binary: %+v", out)
	}
}

func TestScriptAutoRefusesMissingAndOldCosignWithoutInstalling(t *testing.T) {
	srv, calls := stepServer(t, func(int) (int, string) { return 302, "0.1.1" })
	b := testutil.Sandbox(t, testutil.Options{})
	exe := filepath.Join(b.Dir, "script-bin", "homebot")
	writeBinary(t, exe)
	b.Ctx.Exe = exe
	if err := os.MkdirAll(b.Ctx.DataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.Ctx.DataDir, "install-receipt"), []byte("method=script\nbin="+exe+"\nversion=0.1.0\ninstalled=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{testReleasesURL: srv.URL, "PATH": b.Bin})
	now := time.UnixMilli(testutil.TestNow)
	b.Ctx.Now = func() time.Time { return now }
	step := func() {
		t.Helper()
		if out, err := Step(b.Ctx, Build{Version: "0.1.0", Channel: "release"}, nil, config.UpdateAuto, time.Second); err != nil || out.UpdatedVersion != "" {
			t.Fatalf("script step = %+v, %v", out, err)
		}
	}
	step()
	now = now.Add(48 * time.Hour)
	step()
	st, err := state.Load(b.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.UpdateFailed == nil || st.UpdateFailed.Reason != "cosign" || st.UpdateCheck.LastAttempt != now.UnixMilli() {
		t.Errorf("missing cosign state = %+v", st)
	}
	b.WithStubs(map[string]string{"cosign": "echo 'GitVersion: v0.1.0'"})
	now = now.Add(24 * time.Hour)
	step()
	st, err = state.Load(b.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.UpdateFailed == nil || st.UpdateFailed.Reason != "cosign" || st.UpdateCheck.LastAttempt != now.UnixMilli() || *calls != 3 {
		t.Errorf("old cosign state = %+v; calls=%d", st, *calls)
	}
	if data, err := os.ReadFile(exe); err != nil || !strings.Contains(string(data), "echo 0.1.0") {
		t.Errorf("script binary changed: %q, %v", data, err)
	}
}

func TestStepHeldUpdateLockMakesNoRequest(t *testing.T) {
	srv, calls := stepServer(t, func(int) (int, string) { return 302, "0.1.1" })
	f := caskStepFixture(t, srv.URL)
	if err := os.MkdirAll(f.box.Ctx.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(f.box.Ctx.StateDir, "update.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	f.step(t, config.UpdateAuto)
	if *calls != 0 {
		t.Errorf("held lock still checked release: %d", *calls)
	}
}

func TestStepDeadlineRecordsTimeoutWithoutBreakingInstall(t *testing.T) {
	srv, _ := stepServer(t, func(int) (int, string) { return 302, "0.1.1" })
	f := caskStepFixture(t, srv.URL)
	f.step(t, config.UpdateAuto)
	f.at(48)
	if err := os.WriteFile(f.brew, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := Step(f.box.Ctx, Build{Version: "0.1.0", Channel: "release"}, nil, config.UpdateAuto, 200*time.Millisecond)
	if err != nil || out.BrokenCommand != "" {
		t.Errorf("timeout outcome = %+v, %v", out, err)
	}
	st, err := state.Load(f.box.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.UpdateFailed == nil || st.UpdateFailed.Reason != "timeout" {
		t.Errorf("timeout failure = %+v", st.UpdateFailed)
	}
}

func TestStepBrokenCaskNeedsReinstall(t *testing.T) {
	srv, _ := stepServer(t, func(int) (int, string) { return 302, "0.1.1" })
	f := caskStepFixture(t, srv.URL)
	f.step(t, config.UpdateAuto)
	f.at(48)
	if err := os.WriteFile(f.brew, []byte("#!/bin/sh\n/bin/rm -f "+fmt.Sprintf("%q", f.stable)+"\n/bin/sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := Step(f.box.Ctx, Build{Version: "0.1.0", Channel: "release"}, nil, config.UpdateAuto, 200*time.Millisecond)
	if err != nil || out.BrokenCommand != "brew reinstall --cask hyperlab-sh/tap/homebot" {
		t.Errorf("broken cask outcome = %+v, %v", out, err)
	}
	st, err := state.Load(f.box.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.UpdateFailed == nil || st.UpdateFailed.Reason != "broken" {
		t.Errorf("failure = %+v", st.UpdateFailed)
	}
}

func TestStepNoReleaseChecksForSourceOrDisabledMode(t *testing.T) {
	srv, calls := stepServer(t, func(int) (int, string) { t.Error("unexpected release request"); return 500, "" })
	f := caskStepFixture(t, srv.URL)
	if _, err := Step(f.box.Ctx, Build{Version: "dev"}, nil, config.UpdateAuto, time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := Step(f.box.Ctx, Build{Version: "0.1.0", Channel: "release"}, nil, config.UpdateOff, time.Second); err != nil {
		t.Fatal(err)
	}
	if *calls != 0 {
		t.Errorf("disabled builds made %d requests", *calls)
	}
}

// Keep a cancelled parent context from becoming an unattended successful run.
func TestStepCancelledParentMakesNoUpgrade(t *testing.T) {
	srv, _ := stepServer(t, func(int) (int, string) { return 302, "0.1.1" })
	f := caskStepFixture(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.box.Ctx = f.box.Ctx.WithContext(ctx)
	f.step(t, config.UpdateAuto)
	if f.runs(t) != 0 {
		t.Error("cancelled helper ran brew")
	}
}

// Spec "Soak": a newer release appearing while the wait runs restarts it. The
// wait that would have ended at T+48h for 0.1.1 is now 0.1.2's, seen at T+24h,
// so nothing installs at T+48h and the first upgrade is at T+72h, to 0.1.2.
func TestStepNewerReleaseAtT24ResetsSoak(t *testing.T) {
	srv, calls := stepServer(t, func(n int) (int, string) {
		if n == 1 {
			return 302, "0.1.1"
		}
		return 302, "0.1.2"
	})
	f := caskStepFixture(t, srv.URL)
	f.setBrew(t, "echo 0.1.2")
	f.step(t, config.UpdateAuto)
	f.at(24)
	f.step(t, config.UpdateAuto)
	st, err := state.Load(f.box.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.UpdateCheck.Latest != "0.1.2" || st.UpdateCheck.FirstSeen != f.now.UnixMilli() {
		t.Fatalf("T+24h: latest=%q firstSeen=%d, want 0.1.2 seen now (%d)", st.UpdateCheck.Latest, st.UpdateCheck.FirstSeen, f.now.UnixMilli())
	}
	f.at(48)
	if out := f.step(t, config.UpdateAuto); out.UpdatedVersion != "" || f.runs(t) != 0 {
		t.Fatalf("T+48h: %+v runs=%d, want nothing: 0.1.2 has soaked 24h", out, f.runs(t))
	}
	if *calls != 3 {
		t.Errorf("T+48h: %d requests, want a check and no guard", *calls)
	}
	if data, err := os.ReadFile(f.stable); err != nil || !strings.Contains(string(data), "echo 0.1.0") {
		t.Errorf("binary changed at T+48h: %q, %v", data, err)
	}
	f.at(71)
	f.step(t, config.UpdateAuto)
	if f.runs(t) != 0 {
		t.Errorf("T+71h: brew ran during the soak")
	}
	f.at(72)
	out := f.step(t, config.UpdateAuto)
	if out.UpdatedVersion != "0.1.2" || f.runs(t) != 1 {
		t.Errorf("T+72h: %+v runs=%d, want one upgrade to 0.1.2", out, f.runs(t))
	}
}

// scriptFixture is a script install on a fake release server: GitHub's
// latest redirect, the release files install.sh downloads, and a cosign that
// answers `version` and counts `verify-blob`.
type scriptFixture struct {
	box       *testutil.Box
	now       time.Time
	bin       string
	oldBinary []byte
	newBinary []byte
	srv       *httptest.Server

	mu       sync.Mutex
	offline  bool
	latest   int
	requests map[string]int
	verifies string
}

func scriptStepFixture(t *testing.T, verify string) *scriptFixture {
	t.Helper()
	b := testutil.Sandbox(t, testutil.Options{})
	f := &scriptFixture{box: b, now: time.UnixMilli(testutil.TestNow), requests: map[string]int{}}
	f.bin = filepath.Join(b.Bin, "homebot")
	writeBinary(t, f.bin)
	f.oldBinary, _ = os.ReadFile(f.bin)
	f.newBinary = []byte("#!/bin/sh\ncase \"${1:-}\" in --version|version) echo 0.1.1 ;; *) exit 2 ;; esac\n")
	files := releaseFiles(t, "0.1.1", f.newBinary)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path == "/releases/latest" {
			f.latest++
			if f.offline {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Location", "/releases/tag/v0.1.1")
			w.WriteHeader(http.StatusFound)
			return
		}
		data, ok := files[r.URL.Path]
		if !ok {
			t.Errorf("unexpected request %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		f.requests[r.URL.Path]++
		_, _ = w.Write(data)
	}))
	t.Cleanup(f.srv.Close)
	f.verifies = filepath.Join(b.Dir, "cosign-verifies")
	b.WithStubs(map[string]string{
		"uname": `case "$1" in -s) echo Linux ;; -m) echo x86_64 ;; *) exit 2 ;; esac`,
		"cosign": fmt.Sprintf(`case "$1" in
  version) echo 'GitVersion: v2.4.3' ;;
  verify-blob) printf 'verify\n' >> %q
%s ;;
  *) exit 2 ;;
esac`, f.verifies, verify),
	})
	b.Ctx.Exe = f.bin
	if err := os.MkdirAll(b.Ctx.DataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.receipt(), []byte("method=script\nbin="+f.bin+"\nversion=0.1.0\ninstalled=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{testReleasesURL: f.srv.URL})
	b.Ctx.Now = func() time.Time { return f.now }
	return f
}

func (f *scriptFixture) receipt() string { return filepath.Join(f.box.Ctx.DataDir, "install-receipt") }

func (f *scriptFixture) atDuration(d time.Duration) { f.now = time.UnixMilli(testutil.TestNow).Add(d) }

func (f *scriptFixture) step(t *testing.T) Outcome {
	t.Helper()
	out, err := Step(f.box.Ctx, Build{Version: "0.1.0", Channel: "release"}, nil, config.UpdateAuto, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (f *scriptFixture) setOffline(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offline = v
}

// asked is how many times GitHub's latest redirect was requested.
func (f *scriptFixture) asked() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.latest
}

// downloads is how many release files install.sh fetched, summed.
func (f *scriptFixture) downloads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.requests {
		n += c
	}
	return n
}

func (f *scriptFixture) verified(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(f.verifies)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "verify\n")
}

func (f *scriptFixture) state(t *testing.T) *state.State {
	t.Helper()
	st, err := state.Load(f.box.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// unchanged says the install is exactly as the fixture made it: the old binary
// and a receipt for 0.1.0.
func (f *scriptFixture) unchanged(t *testing.T) {
	t.Helper()
	if data, err := os.ReadFile(f.bin); err != nil || !bytes.Equal(data, f.oldBinary) {
		t.Errorf("binary changed: %q, %v", data, err)
	}
	if data, err := os.ReadFile(f.receipt()); err != nil || !strings.Contains(string(data), "version=0.1.0\n") {
		t.Errorf("receipt changed: %q, %v", data, err)
	}
}

// Spec "Attempts" for a script install: nothing installs while 0.1.1 soaks,
// then the helper downloads the signed release, verifies it, replaces the
// binary named by the receipt and writes the new version into the receipt.
func TestStepScriptUpgradesAfterSoakThroughTheEmbeddedInstaller(t *testing.T) {
	f := scriptStepFixture(t, "exit 0")
	if out := f.step(t); out.UpdatedVersion != "" || f.downloads() != 0 || f.verified(t) != 0 {
		t.Fatalf("first check: %+v downloads=%d verifies=%d, want none", out, f.downloads(), f.verified(t))
	}
	f.atDuration(24 * time.Hour)
	f.step(t)
	if f.downloads() != 0 {
		t.Fatalf("T+24h downloaded during the soak")
	}
	f.unchanged(t)

	f.atDuration(48 * time.Hour)
	out := f.step(t)
	if out.UpdatedVersion != "0.1.1" || out.BrokenCommand != "" {
		t.Fatalf("T+48h outcome = %+v, want an upgrade to 0.1.1", out)
	}
	if data, err := os.ReadFile(f.bin); err != nil || !bytes.Equal(data, f.newBinary) {
		t.Errorf("binary is not the verified release: %q, %v", data, err)
	}
	receipt, err := os.ReadFile(f.receipt())
	if err != nil || !strings.Contains(string(receipt), "method=script\nbin="+f.bin+"\nversion=0.1.1\n") {
		t.Errorf("receipt = %q, %v", receipt, err)
	}
	f.mu.Lock()
	for path, n := range f.requests {
		if n != 1 {
			t.Errorf("%s requested %d times, want once", path, n)
		}
	}
	if len(f.requests) != 3 {
		t.Errorf("installer fetched %d files, want checksums, bundle and archive", len(f.requests))
	}
	f.mu.Unlock()
	if f.verified(t) != 1 {
		t.Errorf("cosign verified %d times, want once", f.verified(t))
	}
	st := f.state(t)
	if st.UpdateFailed != nil || st.UpdateCheck.LastAttempt != f.now.UnixMilli() || st.UpdateCheck.FirstAttempt != 0 {
		t.Errorf("state after upgrade = %+v %+v", st.UpdateCheck, st.UpdateFailed)
	}
	f.box.Ctx.Exe = f.bin
	if got := Detect(f.box.Ctx, Build{Version: "0.1.1", Channel: "release"}, nil); got.Method != Script || got.Binary != f.bin {
		t.Errorf("after the upgrade Detect = %+v", got)
	}
}

// Spec "Attempts" and S5, for a script install whose signature does not
// verify: the failure is recorded as "verify" (the inbox says "signature
// failed"), nothing on disk changes, there is no second attempt before 24h
// and none at all while the check is not due; a later attempt retries, and an
// offline check in between keeps the failure.
func TestStepScriptFailingCosignRecordsVerifyAndRetriesDaily(t *testing.T) {
	f := scriptStepFixture(t, "echo 'Error: none of the expected identities matched' >&2; exit 1")
	f.step(t)
	f.atDuration(48 * time.Hour)
	if out := f.step(t); out != (Outcome{}) {
		t.Fatalf("outcome = %+v, a failed signature is the inbox's to tell", out)
	}
	st := f.state(t)
	if st.UpdateFailed == nil || st.UpdateFailed.Version != "0.1.1" || st.UpdateFailed.Reason != "verify" {
		t.Fatalf("failure = %+v, want 0.1.1 / verify", st.UpdateFailed)
	}
	if !strings.HasPrefix(st.UpdateFailed.Error, ErrVerify.Error()) {
		t.Errorf("failure text = %q, want it to say %q", st.UpdateFailed.Error, ErrVerify)
	}
	attempt := f.now
	if st.UpdateCheck.LastAttempt != attempt.UnixMilli() || f.verified(t) != 1 {
		t.Errorf("lastAttempt=%d verifies=%d, want the attempt just made", st.UpdateCheck.LastAttempt, f.verified(t))
	}
	f.unchanged(t)

	asked := f.asked()
	for _, wait := range []time.Duration{time.Hour, 23*time.Hour + 59*time.Minute} {
		f.atDuration(48*time.Hour + wait)
		f.step(t)
		if f.asked() != asked || f.verified(t) != 1 {
			t.Errorf("A+%s: asked=%d (want %d) verifies=%d (want 1)", wait, f.asked(), asked, f.verified(t))
		}
	}
	f.atDuration(72 * time.Hour)
	f.step(t)
	if f.asked() != asked+1 || f.verified(t) != 2 {
		t.Fatalf("A+24h: asked=%d (want %d) verifies=%d (want 2)", f.asked(), asked+1, f.verified(t))
	}
	f.unchanged(t)

	f.setOffline(true)
	f.atDuration(96 * time.Hour)
	f.step(t)
	st = f.state(t)
	if !st.UpdateCheck.Failed || st.UpdateCheck.Latest != "0.1.1" || st.UpdateFailed == nil || st.UpdateFailed.Reason != "verify" {
		t.Fatalf("after an offline check: %+v %+v, want the failure and the release kept", st.UpdateCheck, st.UpdateFailed)
	}
	if f.verified(t) != 2 {
		t.Errorf("an offline check attempted an upgrade")
	}
	f.setOffline(false)
	f.atDuration(96*time.Hour + 5*time.Hour + 59*time.Minute)
	f.step(t)
	if f.verified(t) != 2 {
		t.Errorf("retried before the six hours were up")
	}
	f.atDuration(96*time.Hour + 6*time.Hour)
	f.step(t)
	if f.verified(t) != 3 {
		t.Errorf("verifies = %d, want the third attempt six hours after the offline check", f.verified(t))
	}
	f.unchanged(t)
}
