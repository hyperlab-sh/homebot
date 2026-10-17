package persona

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// tester is an invented persona (R14: tests never name a built-in but
// neatpik).
var tester = &Voice{ID: "tester", Name: "Tester", Emoji: "🧪"}

// countResolutions replaces resolve with one that returns v and counts its
// calls, until t ends.
func countResolutions(t *testing.T, v *Voice) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	old := resolve
	resolve = func(*app.Context) *Voice { n.Add(1); return v }
	t.Cleanup(func() { resolve = old })
	return &n
}

func TestActiveIsResolvedOncePerConfig(t *testing.T) {
	n := countResolutions(t, tester)
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	// Before LocateRoot.
	ctx.Config = nil
	for range 3 {
		if Active(ctx) != tester {
			t.Fatal("Active is not what resolve gave")
		}
	}
	if got := n.Load(); got != 1 {
		t.Fatalf("resolved %d times before the config, want 1", got)
	}
	// LocateRoot sets Config: the memo misses and the persona is resolved
	// again, now with homebot.yml.
	ctx.Config = config.Default()
	Active(ctx)
	Active(ctx)
	if got := n.Load(); got != 2 {
		t.Fatalf("resolved %d times after the config changed, want 2", got)
	}
	// Another Config (a reload) misses again; the old one is still cached.
	old := ctx.Config
	ctx.Config = config.Default()
	Active(ctx)
	ctx.Config = old
	Active(ctx)
	if got := n.Load(); got != 3 {
		t.Errorf("resolved %d times, want 3", got)
	}
}

func TestActiveIsSharedByCopiesButNotForks(t *testing.T) {
	n := countResolutions(t, tester)
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	Active(ctx)
	Active(ctx.WithContext(context.Background()))
	if got := n.Load(); got != 1 {
		t.Errorf("a WithContext copy resolved again (%d resolutions)", got)
	}
	Active(ctx.Fork(map[string]string{"HOMEBOT_PERSONA": "tester"}))
	if got := n.Load(); got != 2 {
		t.Errorf("a Fork did not resolve again (%d resolutions)", got)
	}
}

func TestNoChoiceLoadsEmbeddedNeatpik(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	ctx.Env = ctx.Env.Without("HOMEBOT_PERSONA")
	yes := true
	ctx.StdoutTerminal = &yes
	if v := Active(ctx); v == nil || v.ID != Default || v.Name != "Neatpik" {
		t.Errorf("Active = %+v, want embedded Neatpik", v)
	}
	if p := ActiveProblem(ctx); p != nil {
		t.Errorf("ActiveProblem = %+v", p)
	}
	if _, ok := TakeWarning(ctx); ok {
		t.Error("TakeWarning handed out a warning")
	}
}

func TestOverrideWinsForTheContextAndItsCopies(t *testing.T) {
	n := countResolutions(t, nil)
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	Override(ctx, tester)
	if Active(ctx) != tester {
		t.Error("Active ignores Override")
	}
	ctx.Config = config.Default()
	if Active(ctx) != tester {
		t.Error("Override is lost when the config changes")
	}
	if Active(ctx.WithContext(context.Background())) != tester {
		t.Error("a WithContext copy does not see Override")
	}
	if got := n.Load(); got != 0 {
		t.Errorf("resolved %d times under Override, want 0", got)
	}
	if Active(ctx.Fork(nil)) != nil {
		t.Error("a Fork sees the original's Override")
	}
	Override(ctx, nil)
	if Active(ctx) != nil {
		t.Error("Override(nil) does not make Active neutral")
	}
}

// Override cannot hold on a Context with no memo, so it says so instead of
// letting the test run with the resolved persona.
func TestOverrideWithoutAMemoPanics(t *testing.T) {
	for name, ctx := range map[string]*app.Context{
		"a Context literal": {},
		"a copy of one":     (&app.Context{}).WithContext(context.Background()),
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("Override did not panic")
				}
			}()
			Override(ctx, tester)
		})
	}
}

// The S1 table's rows for Speaking and NudgeVoice.
func TestWhichVoiceIsInEffect(t *testing.T) {
	type row struct {
		name             string
		active           *Voice // what Active returns (Override)
		plain            bool   // --plain or HOMEBOT_PLAIN
		tty              bool   // stdout is a terminal
		force            bool   // ForceVoice
		env              map[string]string
		speaking, nudges *Voice
	}
	rows := []row{
		{name: "stdout is a terminal", active: tester, tty: true, speaking: tester, nudges: tester},
		{name: "stdout is a pipe or a file", active: tester, speaking: nil, nudges: tester},
		{name: "piped in CI", active: tester, env: map[string]string{"CI": "true", "GITHUB_ACTIONS": "true"}, speaking: nil, nudges: tester},
		{name: "--plain on a terminal", active: tester, plain: true, tty: true, speaking: nil, nudges: nil},
		{name: "--plain piped", active: tester, plain: true, speaking: nil, nudges: nil},
		{name: "--plain beats ForceVoice", active: tester, plain: true, force: true, tty: true, speaking: nil, nudges: nil},
		{name: "persona show, preview, use greeting piped", active: tester, force: true, speaking: tester, nudges: tester},
		{name: "persona show on a terminal", active: tester, force: true, tty: true, speaking: tester, nudges: tester},
		{name: "none on a terminal", active: nil, tty: true, speaking: nil, nudges: nil},
		{name: "none with ForceVoice", active: nil, force: true, speaking: nil, nudges: nil},
		{name: "NO_COLOR keeps the voice", active: tester, tty: true, env: map[string]string{"NO_COLOR": "1"}, speaking: tester, nudges: tester},
		{name: "NO_COLOR unset keeps the voice", active: tester, tty: true, env: map[string]string{"NO_COLOR": ""}, speaking: tester, nudges: tester},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
			ctx.Env = ctx.Env.With(r.env)
			ctx.Plain = r.plain
			ctx.ForceVoice = r.force
			tty := r.tty
			ctx.StdoutTerminal = &tty
			Override(ctx, r.active)
			if got := Speaking(ctx); got != r.speaking {
				t.Errorf("Speaking = %v, want %v", name(got), name(r.speaking))
			}
			if got := NudgeVoice(ctx); got != r.nudges {
				t.Errorf("NudgeVoice = %v, want %v", name(got), name(r.nudges))
			}
		})
	}
}

// HOMEBOT_TEST_TTY drives Speaking the way the TTY seam does.
func TestSpeakingFollowsHomebotTestTTY(t *testing.T) {
	for v, want := range map[string]*Voice{"1": tester, "0": nil} {
		work := t.TempDir()
		vars := testutil.SandboxEnv(work)
		vars["HOMEBOT_TEST_TTY"] = v
		ctx, err := app.New(app.Options{Env: app.NewEnv(vars), Cwd: work, OS: app.Linux})
		if err != nil {
			t.Fatal(err)
		}
		Override(ctx, tester)
		if got := Speaking(ctx); got != want {
			t.Errorf("HOMEBOT_TEST_TTY=%s: Speaking = %v, want %v", v, name(got), name(want))
		}
	}
}

func name(v *Voice) string {
	if v == nil {
		return "neutral"
	}
	return v.Name
}

func TestValidIDIsConfigsSyntax(t *testing.T) {
	for _, s := range []string{"tester", "zork", "none", "", "../x", "Tester", "a-1"} {
		if ValidID(s) != config.ValidPersonaID(s) {
			t.Errorf("ValidID(%q) differs from config.ValidPersonaID", s)
		}
	}
}
