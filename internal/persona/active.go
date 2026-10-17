package persona

import (
	"errors"
	"fmt"
	"sync/atomic"

	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/msg"
)

// The order things happen in (plan S1, I2), which Active, Speaking and
// ui.Styles rely on:
//
//  1. app.New reads the environment (HOMEBOT_PLAIN, HOMEBOT_DEBUG).
//     HOMEBOT_PERSONA is read later, when the persona is first resolved.
//  2. Dispatch applies --plain (ctx.Plain).
//  3. Commands that need the repo run LocateRoot, which sets ctx.Config.
//  4. The persona is resolved the first time a voiced text, a {bot} or a
//     persona colour is needed. Active memoizes per ctx.Config pointer, so
//     a resolution made before step 3 (nil Config: only HOMEBOT_PERSONA or
//     neatpik) is made again once the config is loaded. ui.Styles memoizes
//     per Speaking's answer, so the palette follows the same rule.
//  5. Commands that never locate the root (version, help) speak with
//     HOMEBOT_PERSONA or neatpik; help text itself is never voiced.

// activeKey is Active's memo key: one resolution per Config.
type activeKey struct{ cfg *config.Config }

// resolve is replaceable in the API pin tests that count resolutions.
var resolve = resolveActive

// Active is the persona this home speaks as, whatever stdout is (plan S1):
// HOMEBOT_PERSONA (empty counts as unset), else persona: in homebot.yml,
// else neatpik; nil for none. An id that fails ValidID, is unknown or is
// unusable falls back to neatpik (ActiveProblem says why).
//
// It is memoized with app.Once keyed on the ctx.Config pointer: a Context
// that resolves the persona before LocateRoot (nil Config) resolves it again
// after, once homebot.yml can name it. Override, for tests, wins over all of
// it.
//
// Most code wants Speaking (printed output) or NudgeVoice (caches and
// notifications), which apply the rest of S1's table.
func Active(ctx *app.Context) *Voice {
	if o := overrideOf(ctx); o.set.Load() {
		return o.v.Load()
	}
	return app.Once(ctx, activeKey{ctx.Config}, func() *Voice { return resolve(ctx) })
}

// Speaking is the voice of what ctx prints now (plan S1); nil is neutral.
// It is nil with --plain or HOMEBOT_PLAIN, which beat everything; nil when
// stdout is not a terminal (a pipe, a file, CI) unless ctx.ForceVoice; and
// Active otherwise. NO_COLOR does not change the voice.
func Speaking(ctx *app.Context) *Voice {
	if ctx.Plain {
		return nil
	}
	if !ctx.ForceVoice && !ctx.StdoutIsTerminal() {
		return nil
	}
	return Active(ctx)
}

// NudgeVoice is the voice of the nudge cache, the symbol cache and desktop
// notifications (plan S1): Active whatever stdout is, as the helper writes
// them with no terminal; nil with --plain or HOMEBOT_PLAIN, or for none.
func NudgeVoice(ctx *app.Context) *Voice {
	if ctx.Plain {
		return nil
	}
	return Active(ctx)
}

// override is what Override set for a Context.
type override struct {
	set atomic.Bool
	v   atomic.Pointer[Voice]
}

type overrideKey struct{}

func overrideOf(ctx *app.Context) *override {
	return app.Once(ctx, overrideKey{}, func() *override { return &override{} })
}

// Override makes Active return v for ctx (nil for the neutral voice), for
// tests in other packages; forbidigo keeps it out of non-test code. It
// holds for ctx and its copies (they share its memo), for any Config, but
// not for a Fork, which starts afresh. It panics on a Context with no memo
// (one not built by app.New or app.ForReport, such as a Context literal),
// where it could not hold even for ctx and the test would silently get
// the resolved persona instead.
func Override(ctx *app.Context, v *Voice) {
	o := overrideOf(ctx)
	if overrideOf(ctx) != o {
		panic("persona: Override on a Context with no memo (not built by app.New or app.ForReport) would have no effect")
	}
	o.v.Store(v)
	o.set.Store(true)
}

type resolution struct {
	voice   *Voice
	problem *Problem
}
type resolutionKey struct{ cfg *config.Config }

func resolutionFor(ctx *app.Context) *resolution {
	return app.Once(ctx, resolutionKey{ctx.Config}, func() *resolution { return selectVoice(ctx) })
}

func resolveActive(ctx *app.Context) *Voice { return resolutionFor(ctx).voice }

func selectVoice(ctx *app.Context) *resolution {
	id, origin := Default, FromDefault
	if ctx.Config != nil && ctx.Config.Persona != "" {
		id, origin = ctx.Config.Persona, FromConfig
	}
	if envID := ctx.Env.Get("HOMEBOT_PERSONA"); envID != "" {
		id, origin = envID, FromEnv
	}
	if id == None {
		return &resolution{}
	}
	var v *Voice
	var ds []Diagnostic
	var err error
	if ValidID(id) {
		v, ds, err = Load(ctx, id)
	} else {
		ds, err = []Diagnostic{diagnostic("v4", id, 0, 0, "", 0)}, errInvalidChoice
	}
	if err == nil {
		return &resolution{voice: v}
	}
	problem := &Problem{ID: id, Origin: origin}
	if d, found := firstHeaderError(ds); found {
		problem.Diag = d
	} else if len(ds) > 0 {
		problem.Diag = ds[0]
	} else {
		problem.Diag = diagnostic("v9", id, 0, 0, "", 0)
	}
	if ctx.Debug != nil {
		ctx.Debug.Debug("persona: selected persona unusable", "id", id, "error", err)
	}
	return &resolution{voice: embeddedDefault(ctx), problem: problem}
}

var errInvalidChoice = errors.New("persona: invalid selection")

// embeddedDefault is the voice homebot falls back to when the chosen persona
// is unusable: the embedded neatpik, lines included (plan S4, m11). It reads
// lines the way load does, so it matches what an unextended persona
// inherits, and it never fails: a nil voice only if the embedded file itself
// cannot be read.
func embeddedDefault(ctx *app.Context) *Voice {
	f, _, err := readBuiltin(Default)
	if err != nil || f == nil {
		return nil
	}
	v := &Voice{ID: Default, Name: f.Name, Emoji: f.Emoji, Lang: language.English,
		Colors: f.Colors, Lines: map[string][]Line{}}
	addLines(ctx, v, f)
	return v
}

// ActiveProblem is why Active fell back to neatpik, or nil when it did not.
func ActiveProblem(ctx *app.Context) *Problem {
	if overrideOf(ctx).set.Load() {
		return nil
	}
	return resolutionFor(ctx).problem
}

// TakeWarning hands out the warning about an unusable HOMEBOT_PERSONA (plan
// S4) once per process: a flag kept with app.OncePerProcess, which every
// copy and Fork of ctx shares, guards it. persona never prints it: voice
// does, the first time it resolves a voice on a terminal. Until the loader
// exists there is never one.
type warningKey struct{}

func TakeWarning(ctx *app.Context) (msg.Text, bool) {
	p := ActiveProblem(ctx)
	if p == nil || p.Origin != FromEnv {
		return msg.Text{}, false
	}
	flag := app.OncePerProcess(ctx, warningKey{}, func() *atomic.Bool { return &atomic.Bool{} })
	if !flag.CompareAndSwap(false, true) {
		return msg.Text{}, false
	}
	return msg.PersonaWarningUnusable(fmt.Sprintf("%q", msg.StripUnsafe(p.ID))), true
}
