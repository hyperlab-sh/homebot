// Package fail holds the error a user can act on and the helpers that format
// a tool's failure. From homebot it imports only internal/msg, which imports
// nothing else of homebot's, so every package, including config below app,
// can return its errors.
package fail

import (
	"regexp"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/msg"
)

// UserError is a failure the user can act on. The CLI prints its first line
// as "homebot: <line>" and the rest verbatim, then exits 1.
type UserError struct {
	Msg string
	// text is the message a Say error was made from; the zero Text for User.
	text msg.Text
	// said is Msg as Say set it, so an edited Msg is noticed.
	said string
}

// Error returns Msg: the neutral English for an error made by Say, and
// whatever text it was given for one made by User, which may already be
// voiced. Storage sites use Error(), never a terminal rendering, so a path
// whose error can reach state, a journal or the forge must build it with Say
// (or voice.Nest and voice.Attach); fail.User(voice.Plain(...)) there would
// store persona text.
func (e *UserError) Error() string { return e.Msg }

// User returns a *UserError holding message, text a voice has already said
// (voice.Plain). Message text comes from the catalog (internal/msg); there is
// deliberately no Errorf form.
func User(message string) *UserError { return &UserError{Msg: message} }

// Say returns a *UserError holding t, for code that has no *app.Context to
// say t with, such as config.Parse and profile.Parse. Its Error() is t in the
// neutral voice, so tests, logs and wrapping errors read right; the CLI's
// error handler says t in the Context's voice instead. Code that has a
// Context returns Say(t) when the error may reach stored state; the CLI
// renders it for the terminal through voice.ErrorText.
func Say(t msg.Text) *UserError {
	said := msg.Speak(msg.Neutral{}, t, nil)
	return &UserError{Msg: said, text: t, said: said}
}

// Text is the message e was made from by Say, and whether there is one.
// Once Msg is changed, Msg is the message: Text reports none, so the CLI
// prints Msg as it prints a User error.
func (e *UserError) Text() (msg.Text, bool) {
	if e.text.ID() == "" || e.Msg != e.said {
		return msg.Text{}, false
	}
	return e.text, true
}

var toolErrorPrefix = regexp.MustCompile(`(?i)^(fatal|error):\s*`)

// ToolSaid returns the last max non-empty lines of a tool's output, trimmed
// and without a leading "fatal:" or "error:".
func ToolSaid(output string, max int) []string {
	var lines []string
	for _, l := range strings.Split(output, "\n") {
		l = toolErrorPrefix.ReplaceAllString(strings.TrimSpace(l), "")
		if l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > max {
		lines = lines[len(lines)-max:]
	}
	return lines
}

// Failure formats a failure: the summary, then the last three lines the tool
// said, each indented under it and prefixed with "<tool>: " when tool is set
// (format.failure.tool_line, a fact every voice says alike).
func Failure(summary, output, tool string) string {
	parts := []string{summary}
	for _, l := range ToolSaid(output, 3) {
		if tool == "" {
			parts = append(parts, "  "+l)
			continue
		}
		parts = append(parts, msg.Speak(msg.Neutral{}, msg.FormatFailureToolLine(tool, l), nil))
	}
	return strings.Join(parts, "\n")
}
