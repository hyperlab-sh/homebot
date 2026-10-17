package cli

import (
	"errors"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// ExitCode is the exit code of a run that ended with err: 0 without one, a
// *fail.Exit's code, and 1 for anything else.
func ExitCode(err error) int {
	var exit *fail.Exit
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.Code
	}
	return 1
}

// Report prints a failed command's error as the TypeScript did: the first
// line after "homebot: ", in the error style, then the remaining lines (what
// a tool said, a command to run) with their own layout. A mistake on the
// command line is printed whole in the error style, then the pointer to the
// command's help. Nothing is printed for a nil error or a *fail.Exit,
// wherever it is in err's chain. A fail.Say error is said in c's voice,
// wherever it is in err's chain. Finding no home repo is followed by a
// line pointing to homebot setup. It prints through ui.Err(c), so a pipe
// gets plain text.
//
// The styles are looked up only to print, and only when stderr shows
// colour (ui.ErrStyles), so a run that ends without an error to print, or
// prints it plain, never asks the terminal for its background.
func Report(c *app.Context, err error) {
	var exit *fail.Exit
	var opt *optionError
	switch {
	case err == nil, errors.As(err, &exit):
	case errors.As(err, &opt):
		st := ui.ErrStyles(c)
		ui.Eprintln(c, st.Error.Render(voice.Plain(c, msg.CLIErrorArgs(opt.message))))
		ui.Eprintln(c, st.Muted.Render(opt.hint))
	default:
		first, rest, more := strings.Cut(voice.ErrorText(c, err), "\n")
		ui.Eprintln(c, ui.ErrStyles(c).Error.Render(voice.Plain(c, msg.CLIErrorPrefix(first))))
		if more {
			ui.Eprintln(c, rest)
		}
		if isNoRoot(err) {
			ui.Eprintln(c, ui.ErrStyles(c).Muted.Render(voice.Plain(c, msg.ContextHintSetup())))
		}
	}
}

// isNoRoot reports whether err is the error of finding no home repo
// (context.error.no_config), wherever it is in err's chain. Report follows
// it with the pointer to homebot setup, and setup offers to make one.
func isNoRoot(err error) bool {
	var ue *fail.UserError
	if !errors.As(err, &ue) {
		return false
	}
	t, ok := ue.Text()
	return ok && t.ID() == msg.ContextErrorNoConfig().ID()
}

// optionError is a mistake on the command line, such as an unknown option:
// the message, and the pointer to the page of the command it was given to.
type optionError struct {
	message string
	hint    string
}

func (e *optionError) Error() string { return e.message }

// newOptionError is message about command's command line, with the pointer
// to its page in c's voice. The inbox, and the root (""), point at the
// overview: "See: homebot --help".
func newOptionError(c *app.Context, command, message string) *optionError {
	space := ""
	if command != "inbox" && command != "" {
		space = command + " "
	}
	return &optionError{message: message, hint: voice.Plain(c, msg.CLIErrorArgsHint(space))}
}
