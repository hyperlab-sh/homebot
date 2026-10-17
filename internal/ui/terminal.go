package ui

import (
	"io"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"

	"github.com/hyperlab-sh/homebot/internal/app"
)

// stream names one of the Context's output streams, as a key of app.Once.
type stream string

const (
	stdoutStream stream = "stdout"
	stderrStream stream = "stderr"
)

// detect is what w can show: whether it is a terminal, then NO_COLOR,
// CLICOLOR_FORCE, TERM and COLORTERM. Each stream is detected on its own, so
// piping stdout does not strip the colours of errors on a terminal.
//
// NO_COLOR counts when it is not empty, as no-color.org and src/ui.ts have
// it, and turns colours off even with CLICOLOR_FORCE; bold and faint stay.
// CLICOLOR_FORCE counts when it is set and not 0, as bixense.com/clicolors
// has it. colorprofile reads both as Go booleans, so they are given to it as
// 1 or nothing.
func detect(w io.Writer, env app.Env) colorprofile.Profile {
	noColor := env.Get("NO_COLOR") != ""
	force := env.Get("CLICOLOR_FORCE")
	forced := force != "" && force != "0"
	p := colorprofile.Detect(w, env.With(map[string]string{
		"NO_COLOR":       flag(noColor),
		"CLICOLOR_FORCE": flag(forced),
	}).Environ())
	if noColor && p > colorprofile.ASCII {
		p = colorprofile.ASCII
	}
	return p
}

func flag(on bool) string {
	if on {
		return "1"
	}
	return ""
}

// Columns is the width prose wraps at: the terminal's, at most 100 columns,
// and 80 when stdout is not a terminal (app.Context.StdoutIsTerminal) or its
// size is unknown.
func Columns(ctx *app.Context) int {
	if !ctx.StdoutIsTerminal() {
		return 80
	}
	if f, ok := ctx.Stdout.(term.File); ok {
		if w, _, err := term.GetSize(f.Fd()); err == nil && w > 0 {
			return min(w, 100)
		}
	}
	return 80
}
