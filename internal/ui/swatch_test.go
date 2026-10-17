package ui

import (
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func TestSwatchRespectsPlainAndNoColor(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"NO_COLOR": ""})
	terminal := true
	b.Ctx.StdoutTerminal = &terminal
	if got := Swatch(b.Ctx, "#123456"); !strings.Contains(got, "\x1b[") {
		t.Errorf("terminal swatch lacks color: %q", got)
	}
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"NO_COLOR": "1"})
	if got := Swatch(b.Ctx, "#123456"); got != "" {
		t.Errorf("NO_COLOR swatch = %q", got)
	}
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"NO_COLOR": ""})
	b.Ctx.Plain = true
	if got := Swatch(b.Ctx, "#123456"); got != "" {
		t.Errorf("plain swatch = %q", got)
	}
}
