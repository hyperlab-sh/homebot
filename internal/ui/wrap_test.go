package ui_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/ui"
)

// Ported from test/layout.test.ts, "wrapText".

func TestWrapTextWrapsAtTheWidthEveryLineIndented(t *testing.T) {
	text := "Leave it empty and age makes up a strong one and prints it. Save it in your password manager."
	out := ui.WrapText(text, "  ", 40)
	want := []string{"  Leave it empty and age makes up a", "  strong one and prints it. Save it in", "  your password manager."}
	if d := cmp.Diff(want, strings.Split(out, "\n")); d != "" {
		t.Errorf("(-want +got):\n%s", d)
	}
	for _, line := range strings.Split(out, "\n") {
		if len(line) > 40 {
			t.Errorf("%q is longer than 40", line)
		}
	}
}

func TestWrapTextNeverBreaksAURLOrAPath(t *testing.T) {
	url := "https://github.com/some-owner/a-repository-with-a-long-name.git"
	got := strings.Split(ui.WrapText("Can't reach "+url+" today", "", 30), "\n")
	if d := cmp.Diff([]string{"Can't reach", url, "today"}, got); d != "" {
		t.Errorf("(-want +got):\n%s", d)
	}
}

func TestWrapTextKeepsParagraphsAndAnEmptyTextIsOneIndentedLine(t *testing.T) {
	if got := ui.WrapText("one\ntwo", "  ", 80); got != "  one\n  two" {
		t.Errorf("got %q", got)
	}
	if got := ui.WrapText("", "  ", 80); got != "  " {
		t.Errorf("got %q", got)
	}
}

func TestWrapTextAtTheTerminalWidthIsAtMost100Columns(t *testing.T) {
	ctx := newTerminal(t, nil, "").ctx
	words := strings.TrimSpace(strings.Repeat("word ", 80))
	for _, line := range strings.Split(ui.WrapText(words, "", ui.Columns(ctx)), "\n") {
		if len(line) > 100 {
			t.Errorf("%q is longer than 100", line)
		}
	}
}

// What src/ui.ts wrapText returns for the same input.
func TestWrapTextMatchesTypeScript(t *testing.T) {
	for _, c := range []struct {
		text, indent string
		width        int
		want         string
	}{
		{"  leading and   multiple   spaces  ", "", 10, "leading\nand\nmultiple\nspaces"},
		// JavaScript's \s: tab, NBSP, BOM and U+2028 split words; NEL does not.
		{"a\tb\u00a0c\u0085d\uFEFFe\u2028f", "", 80, "a b c\u0085d e f"},
		{"\n\n", "> ", 80, "> \n> \n> "},
		// Widths are UTF-16 code units: the truck counts 2.
		{"emoji 🚚🚚🚚 wide x", "", 8, "emoji\n🚚🚚🚚\nwide x"},
		{"é ü ö ä", "", 3, "é ü\nö ä"},
		{"aaa bbb", "", 7, "aaa bbb"},
		{"aaa bbb", "", 6, "aaa\nbbb"},
		{"aaa bbb", "  ", 9, "  aaa bbb"},
		{"aaa bbb", "  ", 8, "  aaa\n  bbb"},
		{"a b", "", 0, "a\nb"},
		{"word", "", -5, "word"},
		{"x\r\ny", "", 80, "x\ny"},
		{"   ", "  ", 80, "  "},
	} {
		if got := ui.WrapText(c.text, c.indent, c.width); got != c.want {
			t.Errorf("WrapText(%q, %q, %d) = %q, want %q", c.text, c.indent, c.width, got, c.want)
		}
	}
}

func TestWrapTextMeasuresOnlyWhatShows(t *testing.T) {
	p := ui.Styles(newTerminal(t, trueColor, "").ctx)
	words := []string{"Moved", "~/.config/nvim", "into", "the", "repo", "but", "could", "not", "link", "3", "things", "back."}
	styled := slices.Clone(words)
	styled[1], styled[9] = p.Path.Render(words[1]), p.Count.Render(words[9])
	plain, text := strings.Join(words, " "), strings.Join(styled, " ")
	for _, width := range []int{10, 20, 24, 30, 40, 80} {
		if got, want := ansi.Strip(ui.WrapText(text, "  ", width)), ui.WrapText(plain, "  ", width); got != want {
			t.Errorf("width %d: styled words wrap as\n%s\nwant\n%s", width, got, want)
		}
	}
}

// Ported from test/layout.test.ts, "inbox details": the lines under an item.

func TestDetailALongDetailWrapsUnderItsItem(t *testing.T) {
	detail := "gitleaks found something that looks like a secret. Move it to private/ (homebot pack --private <path>) or unstage it, then save again so the autosave can go on."
	lines := strings.Split(ui.Detail(detail, 80), "\n")
	if len(lines) < 2 {
		t.Fatalf("not wrapped: %q", lines)
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "       ") || len(line) > 80 {
			t.Errorf("%q is not indented under the item or is too long", line)
		}
	}
}

func TestDetailShowsUpToThreeLinesWithoutStoredColours(t *testing.T) {
	got := ui.Detail("Commit refused: private/a is plaintext. Unstage it:\n  \x1b[36mgit rm --cached private/a\x1b[39m\nthird\nfourth", 80)
	want := []string{
		"       Commit refused: private/a is plaintext. Unstage it:",
		"         git rm --cached private/a",
		"       third",
	}
	if d := cmp.Diff(want, strings.Split(got, "\n")); d != "" {
		t.Errorf("(-want +got):\n%s", d)
	}
}

func TestDetailEdges(t *testing.T) {
	for _, c := range []struct{ detail, want string }{
		{"one line", "       one line"},
		{"\x1b[1;31mbold red\x1b[0m", "       bold red"},
		// Only colour sequences are stored colours; other escapes stay.
		{"\x1b]8;;x\x07link", "       \x1b]8;;x\x07link"},
		{"first\n\nthird", "       first\n       \n       third"},
		{"", "       "},
	} {
		if got := ui.Detail(c.detail, 80); got != c.want {
			t.Errorf("Detail(%q) = %q, want %q", c.detail, got, c.want)
		}
	}
}

// A styled span that a break falls inside: each line carries its own
// codes. The line ends with a reset, and the next starts, after its indent,
// with the codes that were open.
func TestWrapTextStyledSpanBrokenAcrossLines(t *testing.T) {
	for _, c := range []struct {
		name, text, indent string
		width              int
		want               string
	}{
		{
			name: "a long span over three lines",
			text: "see \x1b[33mgit rm --cached x\x1b[39m now", indent: "  ", width: 14,
			want: "  see \x1b[33mgit rm\x1b[0m\n  \x1b[33m--cached x\x1b[39m\n  now",
		},
		{
			name: "a span longer than a line, closed on the last",
			text: "\x1b[36maa bb cc dd ee\x1b[39m", width: 5,
			want: "\x1b[36maa bb\x1b[0m\n\x1b[36mcc dd\x1b[0m\n\x1b[36mee\x1b[39m",
		},
		{
			name: "the break falls where the span opens: nothing to close",
			text: "ab \x1b[1mcd ef\x1b[22m", width: 4,
			want: "ab\n\x1b[1mcd\x1b[0m\n\x1b[1mef\x1b[22m",
		},
		{
			name: "the break falls where the span closes",
			text: "\x1b[1mab cd\x1b[22m ef", width: 4,
			want: "\x1b[1mab\x1b[0m\n\x1b[1mcd\x1b[22m\nef",
		},
		{
			name: "two styles reopen in the order they were opened, an indent stays plain",
			text: "\x1b[1m\x1b[38;5;208mone two\x1b[39m three\x1b[22m four", indent: "> ", width: 8,
			want: "> \x1b[1m\x1b[38;5;208mone\x1b[0m\n> \x1b[1m\x1b[38;5;208mtwo\x1b[39m\x1b[0m\n> \x1b[1mthree\x1b[22m\n> four",
		},
		{
			name: "a true colour is reopened whole",
			text: "\x1b[38;2;1;2;3maa bb\x1b[39m", width: 3,
			want: "\x1b[38;2;1;2;3maa\x1b[0m\n\x1b[38;2;1;2;3mbb\x1b[39m",
		},
		{
			name: "a combined sequence opens two styles",
			text: "\x1b[1;31maa bb\x1b[0m cc", width: 3,
			want: "\x1b[1;31maa\x1b[0m\n\x1b[1m\x1b[31mbb\x1b[0m\ncc",
		},
		{
			name: "both reset forms end every style",
			text: "\x1b[1maa\x1b[m bb\n\x1b[4mcc dd\x1b[0m ee", width: 2,
			want: "\x1b[1maa\x1b[m\nbb\n\x1b[4mcc\x1b[0m\n\x1b[4mdd\x1b[0m\nee",
		},
		{
			name: "a span that runs over a paragraph break is the text's own",
			text: "\x1b[3maa bb\ncc dd\x1b[23m", width: 2,
			want: "\x1b[3maa\x1b[0m\n\x1b[3mbb\ncc\x1b[0m\n\x1b[3mdd\x1b[23m",
		},
		{
			name: "bold and faint together stay together (1;2), 22 ends both",
			text: "\x1b[1;2maa bb\x1b[22m cc", width: 3,
			want: "\x1b[1;2maa\x1b[0m\n\x1b[1m\x1b[2mbb\x1b[22m\ncc",
		},
		{
			name: "bold then faint in separate sequences are both reopened",
			text: "\x1b[1m\x1b[2maa bb\x1b[22m", width: 3,
			want: "\x1b[1m\x1b[2maa\x1b[0m\n\x1b[1m\x1b[2mbb\x1b[22m",
		},
		{
			name: "24 ends underline and double underline, 25 ends both blinks",
			text: "\x1b[4m\x1b[21maa\x1b[24m bb \x1b[5mcc\x1b[6m dd\x1b[25m ee", width: 2,
			want: "\x1b[4m\x1b[21maa\x1b[24m\nbb\n\x1b[5mcc\x1b[6m\x1b[0m\n\x1b[5m\x1b[6mdd\x1b[25m\nee",
		},
		{
			name: "26 is no blink: it stays open until a reset",
			text: "\x1b[26maa bb\x1b[25m cc", width: 2,
			want: "\x1b[26maa\x1b[0m\n\x1b[26mbb\x1b[25m\x1b[0m\n\x1b[26mcc",
		},
		{
			name: "a sub-parameter style (4:3) is one unit and counts for no width",
			text: "\x1b[4:3maa bb\x1b[24m", width: 2,
			want: "\x1b[4:3maa\x1b[0m\n\x1b[4:3mbb\x1b[24m",
		},
		{
			name: "4:0 ends a plain underline",
			text: "\x1b[4maa\x1b[4:0m bb", width: 2,
			want: "\x1b[4maa\x1b[4:0m\nbb",
		},
		{
			name: "4:0 ends a double underline, as 24 does",
			text: "\x1b[21maa\x1b[4:0m bb", width: 2,
			want: "\x1b[21maa\x1b[4:0m\nbb",
		},
		{
			name: "4:0 ends a curly underline",
			text: "\x1b[4:3maa\x1b[4:0m bb", width: 2,
			want: "\x1b[4:3maa\x1b[4:0m\nbb",
		},
		{
			name: "23 ends italic",
			text: "\x1b[3maa\x1b[23m bb", width: 2,
			want: "\x1b[3maa\x1b[23m\nbb",
		},
		{
			name: "23 ends Fraktur",
			text: "\x1b[20maa\x1b[23m bb", width: 2,
			want: "\x1b[20maa\x1b[23m\nbb",
		},
		{
			name: "a colon true colour with the colour space left empty (38:2::r:g:b) is one unit",
			text: "\x1b[38:2::1:2:3maa bb\x1b[39m", width: 3,
			want: "\x1b[38:2::1:2:3maa\x1b[0m\n\x1b[38:2::1:2:3mbb\x1b[39m",
		},
		{
			name: "a colon true colour without the colour space (38:2:r:g:b) is one unit",
			text: "\x1b[38:2:1:2:3maa bb\x1b[39m", width: 3,
			want: "\x1b[38:2:1:2:3maa\x1b[0m\n\x1b[38:2:1:2:3mbb\x1b[39m",
		},
		{
			name: "a colon true colour inside a combined sequence takes no other field",
			text: "\x1b[1;38:2::1:2:3;4maa bb\x1b[0m", width: 3,
			want: "\x1b[1;38:2::1:2:3;4maa\x1b[0m\n\x1b[1m\x1b[38:2::1:2:3m\x1b[4mbb\x1b[0m",
		},
		{
			name: "a colon true colour is replaced by a later colour of its kind",
			text: "\x1b[38:2::1:2:3m\x1b[31maa bb\x1b[39m", width: 3,
			want: "\x1b[38:2::1:2:3m\x1b[31maa\x1b[0m\n\x1b[31mbb\x1b[39m",
		},
		{
			name: "background and underline colours in true colour are reopened whole",
			text: "\x1b[48;2;4;5;6m\x1b[58:2::7:8:9maa bb\x1b[49m\x1b[59m", width: 3,
			want: "\x1b[48;2;4;5;6m\x1b[58:2::7:8:9maa\x1b[0m\n\x1b[48;2;4;5;6m\x1b[58:2::7:8:9mbb\x1b[49m\x1b[59m",
		},
		{
			name: "no escapes: unchanged",
			text: "aa bb cc", width: 5,
			want: "aa bb\ncc",
		},
		{
			name: "a span that fits is untouched",
			text: "\x1b[33mgit rm\x1b[39m x", width: 20,
			want: "\x1b[33mgit rm\x1b[39m x",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := ui.WrapText(c.text, c.indent, c.width); got != c.want {
				t.Errorf("WrapText(%q, %q, %d)\n got %q\nwant %q", c.text, c.indent, c.width, got, c.want)
			}
		})
	}
}

// Through the writers a wrapped styled span shows what the terminal can: in
// colour each line is styled on its own, and under NO_COLOR the text wraps
// exactly as the same words without styles.
func TestWrapTextStyledSpanThroughTheWriters(t *testing.T) {
	const words = "run git rm --cached secrets now please"
	build := func(ctx *app.Context) (styled, plain string) {
		cmd := ui.Styles(ctx).Command.Render("git rm --cached secrets")
		return strings.Replace(words, "git rm --cached secrets", cmd, 1), words
	}
	t.Run("colour: every line is styled on its own", func(t *testing.T) {
		term := newTerminal(t, trueColor, "")
		styled, plain := build(term.ctx)
		wrapped := ui.WrapText(styled, "  ", 20)
		if got, want := ansi.Strip(wrapped), ui.WrapText(plain, "  ", 20); got != want {
			t.Fatalf("styled text wraps as\n%s\nwant\n%s", got, want)
		}
		endsClosed := regexp.MustCompile(`\x1b\[(0?|39)m$`)
		spanWords := strings.Fields("git rm --cached secrets")
		for _, line := range strings.Split(wrapped, "\n") {
			visible := strings.Fields(ansi.Strip(line))
			if !slices.Contains(spanWords, visible[0]) {
				continue
			}
			// A line that starts inside the span opens its own style, after
			// the indent, and one that ends inside it closes it.
			if !strings.HasPrefix(strings.TrimLeft(line, " "), "\x1b[") {
				t.Errorf("line %q starts in the span without a style of its own", line)
			}
			if slices.Contains(spanWords, visible[len(visible)-1]) && !endsClosed.MatchString(line) {
				t.Errorf("line %q ends in the span without closing it", line)
			}
		}
	})
	t.Run("not a terminal: the same lines as the unstyled text", func(t *testing.T) {
		term := newTerminal(t, nil, "")
		styled, plain := build(term.ctx)
		ui.Println(term.ctx, ui.WrapText(styled, "  ", 20))
		if got, want := term.stdout.String(), ui.WrapText(plain, "  ", 20)+"\n"; got != want {
			t.Errorf("stdout = %q, want %q", got, want)
		}
	})
	t.Run("NO_COLOR on a terminal: no colour, and the text of the unstyled wrap", func(t *testing.T) {
		term := newTerminal(t, with(trueColor, map[string]string{"NO_COLOR": "1"}), "")
		styled, plain := build(term.ctx)
		ui.Println(term.ctx, ui.WrapText(styled, "  ", 20))
		got := term.stdout.String()
		if want := ui.WrapText(plain, "  ", 20) + "\n"; ansi.Strip(got) != want {
			t.Errorf("text = %q, want %q", ansi.Strip(got), want)
		}
		colour := regexp.MustCompile(`\x1b\[(?:[0-9;:]*;)?(?:3[0-8]|4[0-8]|9[0-7]|10[0-7])[;:m]`)
		if colour.MatchString(got) {
			t.Errorf("stdout = %q, want no colour", got)
		}
	})
}

// Unstyled text comes out as it went in: WrapText adds no escape sequence of
// its own where the input has none.
func TestWrapTextAddsNoEscapesToUnstyledText(t *testing.T) {
	text := "run git rm --cached secrets now please, and then keep going for a while\nsecond paragraph with words"
	for _, width := range []int{1, 5, 12, 20, 80} {
		if got := ui.WrapText(text, "> ", width); strings.Contains(got, "\x1b") {
			t.Errorf("width %d: %q has an escape sequence", width, got)
		}
	}
}
