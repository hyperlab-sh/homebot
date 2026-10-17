package ui_test

import (
	"io"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/hyperlab-sh/homebot/internal/ui"
)

func TestStyleKeepsTheLayout(t *testing.T) {
	term := newTerminal(t, trueColor, "")
	text := "short\n\na much longer line\n\tindented by a tab\n  padded  \n"
	got := ui.Styles(term.ctx).Muted.Render(text)
	if plain := ansi.Strip(got); plain != text {
		t.Errorf("text changed under the style: %q", plain)
	}
	lines := strings.Split(got, "\n")
	for i, line := range lines {
		plain := ansi.Strip(line)
		switch {
		case plain == "" && line != "":
			t.Errorf("line %d is empty but styled: %q", i, line)
		case plain != "" && (!strings.HasPrefix(line, "\x1b[") || !strings.HasSuffix(line, "m")):
			t.Errorf("line %d is not styled on its own: %q", i, line)
		}
	}
	if ui.Styles(term.ctx).Accent.Render("") != "" {
		t.Error("an empty text should stay empty")
	}
}

func TestANestedSpanKeepsTheOuterStyle(t *testing.T) {
	term := newTerminal(t, trueColor, "")
	p := ui.Styles(term.ctx)
	const todo = "38;2;250;204;21"
	line := p.Todo.Render(p.Path.Render("~/a") + " and " + p.Path.Render("~/b") + " are not repos, " + p.Path.Render("~/c"))
	if _, err := io.WriteString(ui.Out(term.ctx), line+"\n"); err != nil {
		t.Fatal(err)
	}
	got := term.stdout.String()
	if plain := ansi.Strip(got); plain != "~/a and ~/b are not repos, ~/c\n" {
		t.Fatalf("text changed: %q", plain)
	}
	for _, between := range [][2]string{{"~/a", " and "}, {"~/b", " are not"}} {
		i, j := strings.Index(got, between[0]), strings.Index(got, between[1])
		if gap := got[i+len(between[0]) : j]; !strings.Contains(gap, todo) {
			t.Errorf("the todo colour is off after %s: %q", between[0], gap)
		}
	}
	if tail := got[strings.Index(got, "~/c")+len("~/c"):]; strings.Contains(tail, todo) {
		t.Errorf("the outer style reopens after the last span only to close: %q", tail)
	}

	// Bold, with or without a colour, is on again after an inner span.
	for _, tc := range []struct {
		name, line, inner, after string
		want                     []string
	}{
		{"bold and colour", p.Alarm.Render("Refused: " + p.Path.Render("~/x") + " holds a secret"), "~/x", " holds", []string{"1", "38;2;255;107;107"}},
		{"bold alone", p.Path.Render("~/" + p.Todo.Render("notes") + "/a.md"), "notes", "/a.md", []string{"1"}},
	} {
		i := strings.Index(tc.line, tc.inner) + len(tc.inner)
		gap := tc.line[i:strings.Index(tc.line, tc.after)]
		if got := sgrOn(gap); !strings.HasPrefix(gap, "\x1b[m") || !slices.Equal(got, tc.want) {
			t.Errorf("%s: after the inner span %q turns on %q, want a reset then %q", tc.name, gap, got, tc.want)
		}
	}
}

// sgrOn is what the SGR sequences in s leave on, in order, each extended
// colour (38;2;r;g;b or 38;5;n) as one entry. A reset clears the list.
func sgrOn(s string) []string {
	var on []string
	for _, m := range regexp.MustCompile(`\x1b\[([0-9;]*)m`).FindAllStringSubmatch(s, -1) {
		params := strings.Split(m[1], ";")
		for i := 0; i < len(params); i++ {
			p := params[i]
			switch {
			case p == "" || p == "0":
				on = nil
				continue
			case (p == "38" || p == "48" || p == "58") && i+1 < len(params) && params[i+1] == "2" && i+4 < len(params):
				p, i = strings.Join(params[i:i+5], ";"), i+4
			case (p == "38" || p == "48" || p == "58") && i+1 < len(params) && params[i+1] == "5" && i+2 < len(params):
				p, i = strings.Join(params[i:i+3], ";"), i+2
			}
			on = append(on, p)
		}
	}
	return on
}

func TestPaletteColours(t *testing.T) {
	const (
		bold, faint = "\x1b[1m", "\x1b[2m"
		green, blue = "\x1b[32m", "\x1b[34m"
	)
	cases := []struct {
		theme                 string
		urgent, todo, command string
	}{
		{"dark", "38;2;255;107;107", "38;2;250;204;21", "38;2;103;232;249"},
		{"light", "38;2;198;40;40", "38;2;161;98;7", "38;2;14;116;144"},
	}
	for _, c := range cases {
		t.Run(c.theme, func(t *testing.T) {
			p := ui.Styles(newTerminal(t, with(trueColor, map[string]string{"HOMEBOT_THEME": c.theme}), "").ctx)
			for name, check := range map[string]struct {
				style ui.Style
				want  []string
			}{
				"accent":  {p.Accent, []string{bold}},
				"path":    {p.Path, []string{bold}},
				"ok":      {p.OK, []string{green}},
				"muted":   {p.Muted, []string{faint}},
				"notice":  {p.Notice, []string{blue}},
				"urgent":  {p.Urgent, []string{c.urgent}},
				"error":   {p.Error, []string{c.urgent}},
				"alarm":   {p.Alarm, []string{c.urgent, "1"}},
				"todo":    {p.Todo, []string{c.todo}},
				"command": {p.Command, []string{c.command}},
			} {
				got := check.style.Render("x")
				for _, w := range check.want {
					if !strings.Contains(got, w) {
						t.Errorf("%s renders %q, want it to hold %q", name, got, w)
					}
				}
			}
		})
	}
}

func TestStyledTextReachesTheTerminalAsItCanShowIt(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		// colour and bold report whether the output keeps them.
		colour, bold bool
	}{
		{"sandbox: not a terminal, NO_COLOR", nil, false, false},
		{"not a terminal", map[string]string{"NO_COLOR": unset, "TERM": "xterm-256color"}, false, false},
		{"truecolor terminal", trueColor, true, true},
		{"NO_COLOR=1 keeps bold, drops colour", with(trueColor, map[string]string{"NO_COLOR": "1"}), false, true},
		{"NO_COLOR with any value", with(trueColor, map[string]string{"NO_COLOR": "yes"}), false, true},
		{"empty NO_COLOR is unset", with(trueColor, map[string]string{"NO_COLOR": ""}), true, true},
		{"TERM=dumb", with(trueColor, map[string]string{"TERM": "dumb", "COLORTERM": unset}), false, false},
		{"no TERM", with(trueColor, map[string]string{"TERM": unset, "COLORTERM": unset}), false, false},
		{"CLICOLOR_FORCE on a pipe", map[string]string{"NO_COLOR": unset, "CLICOLOR_FORCE": "1", "TERM": "xterm-256color"}, true, true},
		{"NO_COLOR beats CLICOLOR_FORCE", map[string]string{"CLICOLOR_FORCE": "1", "TERM": "xterm-256color"}, false, true},
		{"NO_COLOR=0 is still set", with(trueColor, map[string]string{"NO_COLOR": "0"}), false, true},
		{"CLICOLOR_FORCE=yes on a pipe", map[string]string{"NO_COLOR": unset, "CLICOLOR_FORCE": "yes", "TERM": "xterm-256color"}, true, true},
		{"CLICOLOR_FORCE=false is set and not 0", map[string]string{"NO_COLOR": unset, "CLICOLOR_FORCE": "false", "TERM": "xterm-256color"}, true, true},
		{"CLICOLOR_FORCE=0 on a pipe", map[string]string{"NO_COLOR": unset, "CLICOLOR_FORCE": "0", "TERM": "xterm-256color"}, false, false},
		{"empty CLICOLOR_FORCE on a pipe", map[string]string{"NO_COLOR": unset, "CLICOLOR_FORCE": "", "TERM": "xterm-256color"}, false, false},
		{"CLICOLOR_FORCE=0 on a terminal", with(trueColor, map[string]string{"CLICOLOR_FORCE": "0"}), true, true},
		{"NO_COLOR=yes beats CLICOLOR_FORCE=yes", map[string]string{"NO_COLOR": "yes", "CLICOLOR_FORCE": "yes", "TERM": "xterm-256color"}, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			term := newTerminal(t, c.env, "")
			p := ui.Styles(term.ctx)
			line := p.Accent.Render("homebot") + " " + p.Command.Render("homebot save") + "\n"
			for _, out := range []struct {
				w   io.Writer
				buf interface{ String() string }
			}{{ui.Out(term.ctx), term.stdout}, {ui.Err(term.ctx), term.stderr}} {
				if _, err := io.WriteString(out.w, line); err != nil {
					t.Fatal(err)
				}
				got := out.buf.String()
				if ansi.Strip(got) != "homebot homebot save\n" {
					t.Errorf("text changed: %q", got)
				}
				if hasColour := strings.Contains(got, "[3") || strings.Contains(got, "[9") || strings.Contains(got, ";3"); hasColour != c.colour {
					t.Errorf("colour = %v, want %v: %q", hasColour, c.colour, got)
				}
				if hasBold := strings.Contains(got, "\x1b[1"); hasBold != c.bold {
					t.Errorf("bold = %v, want %v: %q", hasBold, c.bold, got)
				}
			}
		})
	}
}

func TestTheProfileIsDecidedOncePerContext(t *testing.T) {
	term := newTerminal(t, trueColor, "")
	ui.Out(term.ctx)
	term.ctx.Env = term.ctx.Env.With(map[string]string{"NO_COLOR": "1"})
	if _, err := io.WriteString(ui.Out(term.ctx), ui.Styles(term.ctx).OK.Render("ok")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(term.stdout.String(), "\x1b[32m") {
		t.Errorf("the profile was detected again: %q", term.stdout.String())
	}
}

func TestThemeWithoutATerminalIsDark(t *testing.T) {
	for _, theme := range []string{unset, "", "solarized"} {
		p := ui.Styles(newTerminal(t, with(trueColor, map[string]string{"HOMEBOT_THEME": theme}), "").ctx)
		if got := p.Urgent.Render("x"); !strings.Contains(got, "255;107;107") {
			t.Errorf("HOMEBOT_THEME=%q: %q, want the dark urgent colour", theme, got)
		}
	}
}

func TestAnUnknownThemeIsLogged(t *testing.T) {
	term := newTerminal(t, map[string]string{"HOMEBOT_THEME": "solarized", "HOMEBOT_DEBUG": "1"}, "")
	ui.Styles(term.ctx)
	if !strings.Contains(term.stderr.String(), "HOMEBOT_THEME") || !strings.Contains(term.stderr.String(), "solarized") {
		t.Errorf("debug output %q does not mention the ignored theme", term.stderr.String())
	}
}

func TestColumnsWithoutATerminal(t *testing.T) {
	if got := ui.Columns(newTerminal(t, trueColor, "").ctx); got != 80 {
		t.Errorf("Columns = %d, want 80", got)
	}
}
