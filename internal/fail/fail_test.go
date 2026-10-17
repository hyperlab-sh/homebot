package fail

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hyperlab-sh/homebot/internal/msg"
)

func TestToolSaid(t *testing.T) {
	cases := []struct {
		name   string
		output string
		max    int
		want   []string
	}{
		{"empty", "", 3, nil},
		{"blank lines dropped", "\n  \n\n", 3, nil},
		{"trimmed", "  a  \n\tb\t", 3, []string{"a", "b"}},
		{"prefixes dropped, any case", "fatal: x\nERROR:  y\nError:z\nwarning: w", 4, []string{"x", "y", "z", "warning: w"}},
		{"only a leading prefix", "fatal: error: nested", 3, []string{"error: nested"}},
		{"keeps the last max", "1\n2\n3\n4\n5", 3, []string{"3", "4", "5"}},
		{"fewer than max", "1\n2", 3, []string{"1", "2"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if d := cmp.Diff(c.want, ToolSaid(c.output, c.max)); d != "" {
				t.Errorf("ToolSaid (-want +got):\n%s", d)
			}
		})
	}
}

func TestFailure(t *testing.T) {
	cases := []struct {
		name, summary, output, tool, want string
	}{
		{"no output", "Push failed.", "", "", "Push failed."},
		{"no tool", "Push failed.", "fatal: no remote\n", "", "Push failed.\n  no remote"},
		{"tool", "Could not decrypt private/x.age.", "age: error: bad\nlast\n", "age", "Could not decrypt private/x.age.\n  age: age: error: bad\n  age: last"},
		{"last three", "S", "a\nb\nc\nd", "git", "S\n  git: b\n  git: c\n  git: d"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Failure(c.summary, c.output, c.tool); got != c.want {
				t.Errorf("Failure = %q, want %q", got, c.want)
			}
		})
	}
}

// Ported from test/layout.test.ts, "what a failed tool said".

func TestToolSaidKeepsTheLastLinesWithoutGitLabels(t *testing.T) {
	stderr := "hint: something\nerror: src refspec main does not match any\nerror: failed to push some refs to 'origin'\n"
	if d := cmp.Diff([]string{"hint: something", "src refspec main does not match any", "failed to push some refs to 'origin'"}, ToolSaid(stderr, 3)); d != "" {
		t.Errorf("(-want +got):\n%s", d)
	}
	if d := cmp.Diff([]string{"failed to push some refs to 'origin'"}, ToolSaid(stderr, 1)); d != "" {
		t.Errorf("max 1 (-want +got):\n%s", d)
	}
	if got := ToolSaid("  \n\n", 3); len(got) != 0 {
		t.Errorf("blank output said %q", got)
	}
}

func TestFailureIsASentenceThenTheToolsLines(t *testing.T) {
	for _, c := range []struct{ summary, output, tool, want string }{
		{"Push failed.", "fatal: Authentication failed\n", "", "Push failed.\n  Authentication failed"},
		{"Could not encrypt private/x.", "age: no recipients\n", "age", "Could not encrypt private/x.\n  age: age: no recipients"},
		{"git add failed.", "", "", "git add failed."},
	} {
		if got := Failure(c.summary, c.output, c.tool); got != c.want {
			t.Errorf("Failure(%q, %q, %q) = %q, want %q", c.summary, c.output, c.tool, got, c.want)
		}
	}
}

func TestUserErrorUnwrapsWithErrorsAs(t *testing.T) {
	var err error = User("a\nb")
	var ue *UserError
	if !errors.As(err, &ue) || ue.Msg != "a\nb" || err.Error() != "a\nb" {
		t.Fatalf("errors.As or Error() lost the message: %#v", err)
	}
}

func TestASayErrorReadsAsTheNeutralVoice(t *testing.T) {
	for _, c := range []struct {
		text msg.Text
		want string
	}{
		{msg.ConfigErrorProfiles("homebot.yml:2:1"), "homebot.yml:2:1: profiles must be a mapping"},
		{msg.ConfigErrorExportDuplicate("homebot.yml:9:3", 1, "apt"), `homebot.yml:9:3: exports[1]: duplicate export name "apt" (give each one profiles that don't overlap)`},
		// Errors keep the literal homebot; nothing fills it from the voice.
		{msg.ConfigErrorOldName("/r/homebot.yaml"), "homebot reads homebot.yml now: rename /r/homebot.yaml to homebot.yml"},
		// A value is never read as catalog syntax.
		{msg.ManifestErrorUnknownVariable("p.yml step 3", "{nope}", "{os}, {host}"), "p.yml step 3: unknown variable {nope} (available: {os}, {host})"},
		{msg.ManifestErrorOneActionNone("p.yml step 1"), "p.yml step 1: each step needs exactly one action, found none"},
	} {
		err := Say(c.text)
		if err.Error() != c.want || err.Msg != c.want {
			t.Errorf("%s: Error() = %q, Msg = %q, want %q", c.text.ID(), err.Error(), err.Msg, c.want)
		}
		got, ok := err.Text()
		if !ok || got.ID() != c.text.ID() {
			t.Errorf("%s: Text() = %s, %v", c.text.ID(), got, ok)
		}
		var ue *UserError
		if wrapped := fmt.Errorf("loading: %w", err); !errors.As(wrapped, &ue) || ue != err || wrapped.Error() != "loading: "+c.want {
			t.Errorf("%s: wrapped, the error reads %q", c.text.ID(), wrapped)
		}
	}
}

func TestOnlyASayErrorHoldsAText(t *testing.T) {
	if got, ok := User("Push failed.").Text(); ok || got.ID() != "" {
		t.Errorf("User error holds %s", got)
	}
	if got, ok := Say(msg.Text{}).Text(); ok || got.ID() != "" {
		t.Errorf("Say of the zero Text holds %s", got)
	}
}

func TestAnEditedMsgIsTheMessage(t *testing.T) {
	err := Say(msg.ConfigErrorProfiles("homebot.yml:2:1"))
	err.Msg = "homebot.yml: something else"
	if got, ok := err.Text(); ok {
		t.Errorf("Text() = %s after Msg changed; the CLI would print it instead of Msg", got)
	}
	if err.Error() != "homebot.yml: something else" {
		t.Errorf("Error() = %q", err.Error())
	}
	err.Msg = "homebot.yml:2:1: profiles must be a mapping"
	if _, ok := err.Text(); !ok {
		t.Error("Msg back as Say set it, and Text() is gone")
	}
}

func TestFailureLinesUseTheCatalogLayout(t *testing.T) {
	line := msg.FormatFailureToolLine("git", "no remote")
	if line.ID() != "format.failure.tool_line" || line.Persona() {
		t.Fatalf("%s may be reworded; Failure says it neutrally", line.ID())
	}
	if got := Failure("S", "{x}\n#", "git"); got != "S\n  git: {x}\n  git: #" {
		t.Errorf("tool output is read as catalog syntax: %q", got)
	}
}
