package ui

import (
	"charm.land/lipgloss/v2"

	"github.com/hyperlab-sh/homebot/internal/app"
)

// Swatch shows a color beside its printed value in persona show. The value
// remains visible without terminal color, so the swatch is only an aid.
func Swatch(ctx *app.Context, value string) string {
	if value == "" || ctx.Plain || ctx.Env.Get("NO_COLOR") != "" || !ctx.StdoutIsTerminal() {
		return ""
	}
	return lipgloss.NewStyle().Background(lipgloss.Color(value)).Render("  ")
}
