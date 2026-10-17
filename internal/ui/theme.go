package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"

	"github.com/hyperlab-sh/homebot/internal/app"
)

type themeKey struct{}

// isDark reports whether the terminal's background is dark, resolved once
// per Context, from the first of these that knows:
//
//   - HOMEBOT_THEME, light or dark;
//   - COLORFGBG, as rxvt, Konsole and others set it (colorfgbgDark);
//   - the terminal itself (terminalIsDark), asked only when stdin and stdout
//     are terminals, stdout shows colour, and homebot is not running over
//     ssh, where the answer can come back after the wait is over and land at
//     the shell's prompt.
//
// In every other case, and when the terminal does not answer in time, the
// background is taken to be dark.
func isDark(ctx *app.Context) bool {
	return app.Once(ctx, themeKey{}, func() bool {
		switch theme := ctx.Env.Get("HOMEBOT_THEME"); theme {
		case "dark":
			return true
		case "light":
			return false
		case "":
		default:
			if ctx.Debug != nil {
				ctx.Debug.Debug("ui: ignoring HOMEBOT_THEME; want light or dark", "value", theme)
			}
		}
		if dark, ok := colorfgbgDark(ctx.Env.Get("COLORFGBG")); ok {
			return dark
		}
		in, inOK := ctx.Stdin.(term.File)
		out, outOK := ctx.Stdout.(term.File)
		if !inOK || !outOK || writer(ctx, stdoutStream).Profile <= colorprofile.ASCII ||
			!term.IsTerminal(in.Fd()) || !term.IsTerminal(out.Fd()) || overSSH(ctx.Env) {
			return true
		}
		return terminalIsDark(in, out)
	})
}

// colorfgbgDark reads COLORFGBG, "fg;bg" or "fg;pixmap;bg", whose last
// field is the background's colour index: 0 to 6 and 8 are dark, 7 and 9
// to 15 light. ok is false for any other value, which is then ignored.
func colorfgbgDark(value string) (dark, ok bool) {
	fields := strings.Split(value, ";")
	if len(fields) < 2 || len(fields) > 3 {
		return false, false
	}
	bg, err := strconv.Atoi(fields[len(fields)-1])
	if err != nil || bg < 0 || bg > 15 {
		return false, false
	}
	return bg <= 6 || bg == 8, true
}

// overSSH reports whether homebot runs in an ssh session.
func overSSH(env app.Env) bool {
	return env.Get("SSH_CONNECTION") != "" || env.Get("SSH_TTY") != ""
}
