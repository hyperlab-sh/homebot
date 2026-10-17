package triage

import (
	"context"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// keys maps what the person types to an answer. Enter alone moves on, so a
// closed stdin cannot loop forever.
var keys = map[string]Answer{"t": Track, "o": TrackOS, "i": Ignore, "s": Snooze, "n": Skip, "": Skip, "q": Quit}

// answerFor is the answer key stands for about cand; false when key means
// nothing there (o, track on this OS only, means nothing in the repo).
func answerFor(cand Candidate, key string) (Answer, bool) {
	a, ok := keys[strings.ToLower(key)]
	if a == TrackOS && cand.Kind == Repo {
		return "", false
	}
	return a, ok
}

// Run asks about every candidate, one at a time, and carries out each
// answer right away. On a terminal it draws a picker that takes one key per
// answer; otherwise it prints the TypeScript's line prompt and reads a line
// per answer.
func Run(c *app.Context) error {
	items, err := Candidates(c)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		ui.Println(c, ui.Styles(c).OK.Render(voice.Render(c, msg.TriageSuccessNone())))
		return nil
	}
	if ui.Interactive(c) && stdoutIsTerminal(c) {
		return runPicker(c, items)
	}
	return Prompt(c, items, func(q string) string { return ui.Ask(c, q) })
}

// stdoutIsTerminal says whether the picker would draw where someone sees it:
// homebot triage | tee log gets the line prompt.
func stdoutIsTerminal(c *app.Context) bool {
	_, out, _ := ui.Attached(c)
	f, ok := out.(term.File)
	return ok && term.IsTerminal(f.Fd())
}

// Prompt asks about items on plain lines, reading each answer with ask, as
// the TypeScript's interactiveTriage did: a header, then per item its key
// and size, the menu, and "> ". An answer that means nothing asks again; q
// stops. A failed decision is printed and the next item asked about. A
// Ctrl-C stops before the next question or decision.
func Prompt(c *app.Context, items []Candidate, ask func(question string) string) error {
	st := ui.Styles(c)
	ui.Println(c, header(c, len(items)))
	for i, cand := range items {
		if err := stopped(c); err != nil {
			return err
		}
		ui.Println(c)
		ui.Println(c, itemLine(c, i+1, len(items), cand))
		ui.Println(c, st.Muted.Render(voice.Render(c, menu(c, cand))))
		var answer Answer
		for answer == "" {
			key := ask(voice.Plain(c, msg.TriagePromptInput()))
			if err := stopped(c); err != nil {
				return err
			}
			if a, ok := answerFor(cand, key); ok {
				answer = a
			} else {
				ui.Println(c, st.Muted.Render(voice.Render(c, msg.TriageHintPick())))
			}
		}
		if answer == Quit {
			break
		}
		if line, ok := result(c, cand, answer); ok {
			ui.Println(c, line)
		}
	}
	return nil
}

// stopped is why the run was told to stop (Ctrl-C), or nil.
func stopped(c *app.Context) error {
	if ctx := c.Context(); ctx.Err() != nil {
		return context.Cause(ctx)
	}
	return nil
}

// header is the first line: how many things there are to decide.
func header(c *app.Context, n int) string {
	st := ui.Styles(c)
	return st.Accent.Render(voice.Render(c, msg.TriageStatusHeader(n))) + st.Muted.Render(voice.Render(c, msg.TriageHintHeaderSuffix()))
}

// itemLine is "[i/n] key  (description)", the counter bold and the
// description dim.
func itemLine(c *app.Context, i, n int, cand Candidate) string {
	st := ui.Styles(c)
	line := voice.Render(c, msg.TriagePromptItem(i, n, cand.Key, st.Muted.Render(Describe(c, cand))))
	counter, rest, _ := strings.Cut(line, " ")
	return st.Accent.Render(counter) + " " + rest
}

// menu is the letters cand can be answered with.
func menu(c *app.Context, cand Candidate) msg.Text {
	if cand.Kind == Repo {
		return msg.TriageMenuRepo()
	}
	return msg.TriageMenuHome(string(c.OS))
}

// result carries out answer and is the line saying how it went: green on
// success, red on failure; false when there was nothing to do.
func result(c *app.Context, cand Candidate, answer Answer) (string, bool) {
	st := ui.Styles(c)
	said, done, err := Decide(c, cand, answer)
	if err != nil {
		return st.Error.Render(voice.Render(c, msg.TriageStatusResult(voice.ErrorText(c, err)))), true
	}
	if !done {
		return "", false
	}
	return st.OK.Render(voice.Render(c, msg.TriageStatusResult(voice.Render(c, said)))), true
}
