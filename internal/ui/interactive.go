package ui

import (
	"github.com/charmbracelet/x/term"

	"github.com/hyperlab-sh/homebot/internal/app"
)

// Interactive reports whether a person can answer a question: stdin is a
// terminal. Commands that would prompt print what to do instead when it is
// false. A test sets Context.StdinIsTerminal to pick the answer.
func Interactive(c *app.Context) bool {
	if c.StdinIsTerminal != nil {
		return *c.StdinIsTerminal
	}
	f, ok := c.Stdin.(term.File)
	return ok && term.IsTerminal(f.Fd())
}
