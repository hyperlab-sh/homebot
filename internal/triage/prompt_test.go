package triage

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/ui"
)

// answers is an ask that hands out the given answers, then q.
func answers(list ...string) func(string) string {
	return func(string) string {
		if len(list) == 0 {
			return "q"
		}
		a := list[0]
		list = list[1:]
		return a
	}
}

func (b *box) prompt(ask func(string) string) error {
	b.t.Helper()
	return Prompt(b.Ctx, b.candidates(), ask)
}

func TestPromptAppliesAnswersAndStopsOnQ(t *testing.T) {
	b := newBox(t, true)
	b.home(map[string]string{".config/a/x": "", ".config/b/x": "", ".config/c/x": ""})
	b.commit()
	if err := b.prompt(answers("x", "t", "i", "q")); err != nil {
		t.Fatal(err)
	}
	if !b.isLink(b.h(".config/a")) {
		t.Error("~/.config/a was not packed")
	}
	if got := b.ignores(); len(got) != 1 || got[0] != "~/.config/b" {
		t.Errorf("LoadIgnores = %q, want ~/.config/b", got)
	}
	if !b.isDir(b.h(".config/c")) {
		t.Error("~/.config/c changed after q")
	}
}

func TestPromptWithEmptyAnswersSkipsInsteadOfLoopingForever(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{".config/a/x": ""})
	if err := b.prompt(func(string) string { return "" }); err != nil {
		t.Fatal(err)
	}
	if !b.isDir(b.h(".config/a")) {
		t.Error("~/.config/a changed on an empty answer")
	}
}

// The TypeScript's prompt with stdin closed, byte for byte (the parity
// scenario triage-noninteractive).
func TestPromptPrintsWhatTheTypeScriptPrinted(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{".bashrc": "mine\n", ".config/foo/conf": "mine\n"})
	b.Ctx.Stdin = strings.NewReader("")
	if err := b.prompt(func(q string) string { return ui.Ask(b.Ctx, q) }); err != nil {
		t.Fatal(err)
	}
	want := "2 things to decide. Each answer is saved right away; q stops.\n" +
		"\n" +
		"[1/2] ~/.config/foo  (folder, 5 B)\n" +
		"  t track everywhere   o track on linux only   i ignore forever\n" +
		"  s snooze 7 days      enter next              q quit\n" +
		"> \n" +
		"[2/2] ~/.bashrc  (5 B)\n" +
		"  t track everywhere   o track on linux only   i ignore forever\n" +
		"  s snooze 7 days      enter next              q quit\n" +
		"> "
	if got := b.Stdout.String(); got != want {
		t.Errorf("stdout =\n%s\nwant\n%s", got, want)
	}
}

func TestPromptAsksAgainAfterAWrongAnswer(t *testing.T) {
	b := newBox(t, true)
	b.repo(map[string]string{"notes.txt": "x"})
	// o (this OS only) means nothing for a repo file; letters are not case
	// sensitive.
	if err := b.prompt(answers("o", "what", "T")); err != nil {
		t.Fatal(err)
	}
	want := `1 things to decide. Each answer is saved right away; q stops.

[1/1] notes.txt  (1 B)
  t track (git add)    i ignore (.gitignore)   s snooze 7 days
  enter next           q quit
  pick one of the letters above
  pick one of the letters above
  staged notes.txt
`
	if got := b.Stdout.String(); got != want {
		t.Errorf("stdout =\n%s\nwant\n%s", got, want)
	}
}

func TestPromptPrintsAFailedDecisionAndGoesOn(t *testing.T) {
	b := newBox(t, true)
	b.repo(map[string]string{"b.txt": "x"})
	// git add fails on a path that is not there.
	items := []Candidate{repoItem(b, "gone.txt"), repoItem(b, "b.txt")}
	if err := Prompt(b.Ctx, items, answers("t", "t")); err != nil {
		t.Fatal(err)
	}
	out := b.Stdout.String()
	if !strings.Contains(out, "\n  git add gone.txt failed.\n  pathspec 'gone.txt' did not match any files\n") {
		t.Errorf("stdout does not show the failure:\n%s", out)
	}
	if !strings.Contains(out, "  staged b.txt\n") {
		t.Errorf("the next item was not decided:\n%s", out)
	}
}

func TestPromptStopsOnCtrlC(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{".config/a/x": "", ".config/b/x": ""})
	ctx, cancel := context.WithCancelCause(context.Background())
	t.Cleanup(func() { cancel(nil) })
	b.Ctx = b.Ctx.WithContext(ctx)
	asked := 0
	err := b.prompt(func(string) string {
		asked++
		cancel(fail.Silently(130))
		return "i"
	})
	var exit *fail.Exit
	if !errors.As(err, &exit) || exit.Code != 130 {
		t.Fatalf("err = %v, want the exit 130 cause", err)
	}
	if asked != 1 {
		t.Errorf("asked %d times, want 1", asked)
	}
	if got := b.ignores(); len(got) != 0 {
		t.Errorf("an answer given as Ctrl-C came was carried out: %q", got)
	}
}

func TestRunWithNothingToSort(t *testing.T) {
	b := newBox(t, false)
	if err := Run(b.Ctx); err != nil {
		t.Fatal(err)
	}
	if got := b.Stdout.String(); got != "Nothing to sort. Everything has a decision.\n" {
		t.Errorf("stdout = %q", got)
	}
}

func TestRunWithoutATerminalOnStdoutUsesTheLinePrompt(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{".bashrc": "mine\n"})
	yes := true
	b.Ctx.StdinIsTerminal = &yes // stdout is still a buffer
	b.Ctx.Stdin = strings.NewReader("i\n")
	if err := Run(b.Ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(b.Stdout.String(), "> "+"  will never ask about ~/.bashrc again\n") {
		t.Errorf("stdout = %q", b.Stdout.String())
	}
}
