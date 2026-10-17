package ui_test

import (
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/ui"
)

func TestPersonaAccentAndNoColor(t *testing.T) {
	for _, noColor := range []bool{false, true} {
		box := testutil.Sandbox(t, testutil.Options{})
		ctx := box.Ctx
		yes := true
		ctx.StdoutTerminal = &yes
		color := ""
		if noColor {
			color = "1"
		}
		ctx.Env = ctx.Env.With(map[string]string{
			"CLICOLOR_FORCE": "1", "TERM": "xterm-256color", "COLORTERM": "truecolor", "NO_COLOR": color,
		})
		persona.Override(ctx, &persona.Voice{ID: "tester", Name: "Tester", Colors: persona.Colors{
			Accent: persona.Pair{Light: "#123456", Dark: "#123456"},
		}})
		if got := ui.NeutralStyles(ctx).Accent.Render("neutral"); strings.Contains(got, "18;52;86") {
			t.Errorf("neutral palette adopted persona accent: %q", got)
		}
		ui.Println(ctx, ui.Styles(ctx).Accent.Render("Tester"))
		got := box.Stdout.String()
		if noColor {
			if got != "Tester\n" {
				t.Errorf("NO_COLOR output = %q", got)
			}
		} else if !strings.Contains(got, "18;52;86") {
			t.Errorf("accent RGB missing: %q", got)
		}
	}
}

// Under NO_COLOR the neutral Accent keeps its bold (NO_COLOR drops colour, not
// weight) and a persona's accent leaves no escape at all (plan M2). The
// difference is on purpose; this pins both halves so neither drifts.
func TestAccentUnderNoColorNeutralKeepsBoldPersonaLeavesNothing(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	ctx := box.Ctx
	yes := true
	ctx.StdoutTerminal = &yes
	ctx.Env = ctx.Env.With(map[string]string{
		"CLICOLOR_FORCE": "1", "TERM": "xterm-256color", "COLORTERM": "truecolor", "NO_COLOR": "1",
	})
	persona.Override(ctx, &persona.Voice{ID: "tester", Name: "Tester", Colors: persona.Colors{
		Accent: persona.Pair{Light: "#123456", Dark: "#123456"},
	}})
	ui.Println(ctx, ui.NeutralStyles(ctx).Accent.Render("neutral"))
	ui.Println(ctx, ui.Styles(ctx).Accent.Render("persona"))
	if want := "\x1b[1mneutral\x1b[m\npersona\n"; box.Stdout.String() != want {
		t.Errorf("NO_COLOR output = %q, want %q", box.Stdout.String(), want)
	}
}
