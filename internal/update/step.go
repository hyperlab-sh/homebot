package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/state"
)

const (
	checkInterval   = 24 * time.Hour
	failureRetry    = 6 * time.Hour
	soakPeriod      = 48 * time.Hour
	attemptInterval = 24 * time.Hour
	tapLagPeriod    = 72 * time.Hour
	DefaultDeadline = 5 * time.Minute
)

// Outcome describes only terminal and notification work for the caller.
// Failures are saved for the inbox and never change tidy's exit code.
type Outcome struct {
	UpdatedVersion string
	BrokenCommand  string
}

// ChecksRelease reports whether the build may query latest unattended.
func ChecksRelease(i Install) bool {
	if i.Method == Source {
		return false
	}
	_, err := parseVersion(i.Version)
	return err == nil
}

// StateVersion compares the running build with the last checked release.
func StateVersion(i Install, check *state.UpdateCheck) bool {
	if check == nil || check.Latest == "" || !ChecksRelease(i) {
		return false
	}
	newer, err := Newer(i.Version, check.Latest)
	return err == nil && newer
}

// checkAt is the first moment Step asks GitHub again: a day after the last
// check, six hours after one that failed.
func checkAt(ch *state.UpdateCheck) int64 {
	interval := checkInterval
	if ch.Failed {
		interval = failureRetry
	}
	return ch.At + interval.Milliseconds()
}

func due(ch *state.UpdateCheck, now int64) bool {
	return ch == nil || ch.At == 0 || now >= checkAt(ch)
}

// attemptAt is the first moment an automatic upgrade of ch.Latest may start:
// 48 hours after the helper first saw it, and a day after the last attempt,
// whatever that attempt's outcome (S5).
func attemptAt(ch *state.UpdateCheck) int64 {
	at := ch.FirstSeen + soakPeriod.Milliseconds()
	if ch.LastAttempt != 0 {
		at = max(at, ch.LastAttempt+attemptInterval.Milliseconds())
	}
	return at
}

// Phase is where a cask or script install running update: auto stands with a
// release that is newer than the running version.
type Phase string

const (
	PhaseUnseen   Phase = "unseen"   // the helper has not recorded this release yet
	PhaseSoaking  Phase = "soaking"  // inside the 48-hour wait
	PhaseFailed   Phase = "failed"   // an attempt failed and has not been cleared
	PhaseRetrying Phase = "retrying" // an attempt left the old version in place (tap lag)
	PhaseReady    Phase = "ready"    // nothing but the helper's next check stands in the way
)

// Gate is AutoGate's answer.
type Gate struct {
	Phase Phase
	// At is the end of the soak for PhaseSoaking. For the phases after it, At
	// is the earliest helper run that can attempt the upgrade: the later of
	// the soak and 24-hour attempt gap, and the next check Step is due for,
	// because an attempt only happens on a check.
	At time.Time
	// Failure is the recorded failure for latest, set for PhaseFailed.
	Failure *state.UpdateFailed
}

// AutoGate says what Step will do about latest for a cask or script install,
// from the same gates Step applies, so version --check cannot drift from the
// helper. It reads state; it asks nothing and writes nothing.
func AutoGate(s *state.State, latest string, now time.Time) Gate {
	ch := s.UpdateCheck
	if ch == nil || ch.Latest != latest || ch.FirstSeen == 0 {
		return Gate{Phase: PhaseUnseen}
	}
	soakEnd := time.UnixMilli(ch.FirstSeen + soakPeriod.Milliseconds())
	if soakEnd.After(now) {
		return Gate{Phase: PhaseSoaking, At: soakEnd}
	}
	g := Gate{Phase: PhaseReady, At: time.UnixMilli(max(attemptAt(ch), checkAt(ch)))}
	switch {
	case s.UpdateFailed != nil && s.UpdateFailed.Version == latest:
		g.Phase, g.Failure = PhaseFailed, s.UpdateFailed
	case ch.LastAttempt != 0 && now.UnixMilli() < ch.LastAttempt+attemptInterval.Milliseconds():
		g.Phase = PhaseRetrying
	}
	return g
}

func updateLock(c *app.Context) (func(), bool, error) {
	if err := os.MkdirAll(c.StateDir, 0o700); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(filepath.Join(c.StateDir, "update.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() { _ = f.Close() }, true, nil
}

func saveCheck(c *app.Context, release Release, now int64, failed bool) error {
	return state.Update(c, func(s *state.State) {
		if s.UpdateCheck == nil {
			s.UpdateCheck = &state.UpdateCheck{}
		}
		ch := s.UpdateCheck
		ch.At, ch.Failed = now, failed
		if failed {
			return
		}
		if ch.Latest != release.Version {
			ch.Latest, ch.FirstSeen = release.Version, now
			ch.FirstAttempt, ch.LastAttempt = 0, 0
			s.UpdateFailed = nil
		}
	})
}

func saveAttempt(c *app.Context, now int64) error {
	return state.Update(c, func(s *state.State) {
		if s.UpdateCheck != nil {
			s.UpdateCheck.LastAttempt = now
		}
	})
}

func saveFailure(c *app.Context, version string, now int64, err error) error {
	reason := FailureReason(err)
	return state.Update(c, func(s *state.State) {
		s.UpdateFailed = &state.UpdateFailed{Version: version, At: now, Reason: reason, Error: firstLine(err.Error())}
	})
}

// FailureReason is the small persisted classification of an upgrade failure.
func FailureReason(err error) string {
	switch {
	case errors.Is(err, ErrBrokenInstall):
		return "broken"
	case errors.Is(err, ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, ErrCosign):
		return "cosign"
	case errors.Is(err, ErrVerify):
		return "verify"
	default:
		return "upgrade"
	}
}

var cosignGitVersion = regexp.MustCompile(`(?m)GitVersion:\s*v?([0-9]+\.[0-9]+\.[0-9]+)\s*$`)

// CosignReady mirrors the embedded installer's version gate before any
// unattended script download. The installer still verifies the signature.
func CosignReady(c *app.Context) bool {
	minimum := CosignMin()
	if minimum == "" {
		return false
	}
	r := proc.Run(c, []string{"cosign", "version"}, proc.Options{Timeout: 5 * time.Second, Group: true})
	if r.Code != 0 {
		return false
	}
	m := cosignGitVersion.FindStringSubmatch(r.Stdout)
	if m == nil {
		return false
	}
	newer, err := Newer(m[1], minimum)
	return err == nil && !newer
}

// Step runs only after tidy has returned and released tidy.lock. It uses its
// own nonblocking interprocess lock and one deadline for checking and upgrade.
func Step(c *app.Context, b Build, info *debug.BuildInfo, mode config.UpdateMode, deadline time.Duration) (Outcome, error) {
	if mode != config.UpdateNotify && mode != config.UpdateAuto {
		return Outcome{}, nil
	}
	i := Detect(c, b, info)
	if !ChecksRelease(i) {
		return Outcome{}, nil
	}
	releaseLock, held, err := updateLock(c)
	if err != nil {
		return Outcome{}, err
	}
	if !held {
		c.Debug.Debug("update: lock held, skipping")
		return Outcome{}, nil
	}
	defer releaseLock()
	if deadline <= 0 {
		deadline = DefaultDeadline
	}
	ctx, cancel := context.WithTimeout(c.Context(), deadline)
	defer cancel()
	stepCtx := c.WithContext(ctx)
	now := c.Now().UnixMilli()
	st, err := state.Load(c)
	if err != nil {
		return Outcome{}, err
	}
	if !due(st.UpdateCheck, now) {
		return Outcome{}, nil
	}
	r, err := Latest(stepCtx)
	if err != nil {
		c.Debug.Debug("update: latest check failed", "error", err)
		return Outcome{}, saveCheck(c, Release{}, now, true)
	}
	if err := saveCheck(c, r, now, false); err != nil {
		return Outcome{}, err
	}
	newer, err := Newer(i.Version, r.Version)
	if err != nil || !newer {
		if err != nil {
			return Outcome{}, err
		}
		return Outcome{}, state.Update(c, func(s *state.State) { s.UpdateFailed = nil })
	}
	if mode == config.UpdateNotify || i.Method != Cask && i.Method != Script {
		return Outcome{}, nil
	}
	st, err = state.Load(c)
	if err != nil {
		return Outcome{}, err
	}
	ch := st.UpdateCheck
	if ch == nil || now < attemptAt(ch) {
		return Outcome{}, nil
	}
	if i.Method == Cask {
		fresh, err := Latest(stepCtx)
		if err != nil {
			c.Debug.Debug("update: pre-upgrade latest check failed", "error", err)
			return Outcome{}, saveCheck(c, Release{}, now, true)
		}
		if fresh.Version != r.Version {
			return Outcome{}, saveCheck(c, fresh, now, false)
		}
	}
	if err := saveAttempt(c, now); err != nil {
		return Outcome{}, err
	}
	u := Plan(i, r)
	if i.Method == Script && testingBase(c) != "" {
		u.BaseURL = testingBase(c)
	}
	found := ""
	u.Found = &found
	if i.Method == Script && !CosignReady(stepCtx) {
		err = fmt.Errorf("%w: cosign %s or newer is required; rerun setup if PATH changed", ErrCosign, CosignMin())
	} else {
		err = RunUnattended(ctx, stepCtx, u)
	}
	if err == nil {
		if found == "" {
			found = r.Version
		}
		return Outcome{UpdatedVersion: found}, state.Update(c, func(s *state.State) {
			s.UpdateFailed = nil
			if s.UpdateCheck != nil {
				s.UpdateCheck.FirstAttempt = 0
			}
		})
	}
	if errors.Is(err, ErrNotYetAvailable) {
		return Outcome{}, state.Update(c, func(s *state.State) {
			if s.UpdateCheck != nil && s.UpdateCheck.FirstAttempt == 0 {
				s.UpdateCheck.FirstAttempt = now
			}
		})
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) && !errors.Is(err, ErrBrokenInstall) {
		err = errors.Join(ErrTimeout, err)
	}
	c.Debug.Debug("update: unattended upgrade failed", "error", err)
	if saveErr := saveFailure(c, r.Version, now, err); saveErr != nil {
		return Outcome{}, saveErr
	}
	if errors.Is(err, ErrBrokenInstall) {
		return Outcome{BrokenCommand: "brew reinstall --cask hyperlab-sh/tap/homebot"}, nil
	}
	return Outcome{}, nil
}

func testingBase(c *app.Context) string {
	if v := releasesBase(c); v != releaseBase {
		return strings.TrimSuffix(v, "/")
	}
	return ""
}
