package ui_test

import (
	"testing"

	"github.com/hyperlab-sh/homebot/internal/ui"
)

// Ported from test/layout.test.ts, "ask". Bun's prompt() writes the question
// and one space; ask trims the question's trailing spaces first.

func TestAskShowsTheQuestionWithoutTrailingSpacesThenOneSpace(t *testing.T) {
	term := newTerminal(t, nil, "  answer  \n  answer  \n  answer  \n")
	for _, q := range []string{"Name [home]: ", "> ", "Switch to it? [Y/n]"} {
		if got := ui.Ask(term.ctx, q); got != "answer" {
			t.Errorf("Ask(%q) = %q, want answer", q, got)
		}
	}
	if got, want := term.stdout.String(), "Name [home]: > Switch to it? [Y/n] "; got != want {
		t.Errorf("questions shown as %q, want %q", got, want)
	}
}

func TestAskOnAClosedStdinIsAnEmptyAnswer(t *testing.T) {
	if got := ui.Ask(newTerminal(t, nil, "").ctx, "> "); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestAskReadsOneLineAtATime(t *testing.T) {
	term := newTerminal(t, nil, "one\r\n\ntwo\n\u00a0three\u00a0\nunfinished")
	// Bun's prompt() drops a last line the input ends without finishing.
	for i, want := range []string{"one", "", "two", "three", "", ""} {
		if got := ui.Ask(term.ctx, "?"); got != want {
			t.Errorf("answer %d = %q, want %q", i, got, want)
		}
	}
}

func TestAskQuestionStylesFollowTheTerminal(t *testing.T) {
	term := newTerminal(t, nil, "y\n")
	ui.Ask(term.ctx, ui.Styles(term.ctx).Accent.Render("Go?"))
	if got := term.stdout.String(); got != "Go? " {
		t.Errorf("without a terminal the question is %q, want it plain", got)
	}
}

// AskOrEnd tells an answer, an empty one included, from the input ending.
func TestAskOrEndSaysWhenTheInputEnded(t *testing.T) {
	term := newTerminal(t, nil, "yes\n\nunfinished")
	for i, want := range []struct {
		answer string
		ok     bool
	}{{"yes", true}, {"", true}, {"", false}, {"", false}} {
		got, ok := ui.AskOrEnd(term.ctx, "?")
		if got != want.answer || ok != want.ok {
			t.Errorf("answer %d = %q, %v; want %q, %v", i, got, ok, want.answer, want.ok)
		}
	}
	if got, want := term.stdout.String(), "? ? ? ? "; got != want {
		t.Errorf("questions shown as %q, want %q", got, want)
	}
}
