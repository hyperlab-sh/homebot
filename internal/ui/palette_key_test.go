package ui

import (
	"testing"

	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// Styles is memoized per voice in effect: the key follows persona.Speaking.
func TestThePaletteIsKeptPerVoiceInEffect(t *testing.T) {
	tester := &persona.Voice{ID: "tester", Name: "Tester", Emoji: "🧪"}
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	yes, no := true, false
	ctx.StdoutTerminal = &yes
	neutral := paletteKeyOf(ctx)
	if neutral != (paletteKey{}) {
		t.Fatalf("the neutral voice's key = %+v, want the zero key", neutral)
	}
	persona.Override(ctx, tester)
	if got := paletteKeyOf(ctx); got != (paletteKey{tester}) {
		t.Errorf("a persona on a terminal: key = %+v, want the persona's", got)
	}
	// LocateRoot loading the config does not change the key by itself.
	ctx.Config = config.Default()
	if got := paletteKeyOf(ctx); got != (paletteKey{tester}) {
		t.Errorf("after the config loaded: key = %+v, want the persona's", got)
	}
	ctx.StdoutTerminal = &no
	if got := paletteKeyOf(ctx); got != neutral {
		t.Errorf("piped: key = %+v, want the neutral one", got)
	}
	ctx.ForceVoice = true
	if got := paletteKeyOf(ctx); got != (paletteKey{tester}) {
		t.Errorf("piped with ForceVoice: key = %+v, want the persona's", got)
	}
	ctx.Plain = true
	if got := paletteKeyOf(ctx); got != neutral {
		t.Errorf("--plain: key = %+v, want the neutral one", got)
	}
	// Styles works for every key.
	_ = Styles(ctx)
	ctx.Plain = false
	_ = Styles(ctx)
}
