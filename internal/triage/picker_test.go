package triage

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func key(text string) tea.KeyPressMsg {
	switch text {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: []rune(text)[0], Text: text}
}

// press sends keys to p and says whether the last one quit.
func press(t *testing.T, p *picker, keys ...string) bool {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		var m tea.Model
		m, cmd = p.Update(key(k))
		if m != p {
			t.Fatal("Update returned another model")
		}
	}
	if cmd == nil {
		return false
	}
	_, quit := cmd().(tea.QuitMsg)
	return quit
}

// view is what p shows, without its styles.
func view(p *picker) string { return ansi.Strip(p.View().Content) }

// fakePicker is a picker over items whose decisions are recorded, not made.
func fakePicker(b *box, items []Candidate) (*picker, *[]string) {
	p := newPicker(b.Ctx, items)
	var made []string
	p.decide = func(c Candidate, a Answer) (string, bool) {
		if a == Skip {
			return "", false
		}
		made = append(made, string(a)+" "+c.Key)
		return "  did " + string(a) + " " + c.Key, true
	}
	return p, &made
}

func TestPickerShowsTheFirstItem(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{".config/foo/conf": "mine\n", ".config/foo/sub/x": "", ".bashrc": "mine\n"})
	p := newPicker(b.Ctx, b.candidates())
	want := `2 things to decide. Each answer is saved right away; q stops.

[1/2] ~/.config/foo  (folder, 5 B)
  conf, sub/
  t track everywhere   o track on linux only   i ignore forever
  s snooze 7 days      enter next              q quit
`
	if got := view(p); got != want {
		t.Errorf("view =\n%s\nwant\n%s", got, want)
	}
}

func TestPickerOneKeyAnswersAndMovesOn(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{".a": "", ".b": "", ".c": "", ".d": ""})
	p, made := fakePicker(b, b.candidates())
	if quit := press(t, p, "t", "enter", "S"); quit {
		t.Fatal("quit before the last item")
	}
	if want := []string{"track ~/.a", "snooze ~/.c"}; !slices.Equal(*made, want) {
		t.Errorf("decided %q, want %q", *made, want)
	}
	v := view(p)
	if !strings.Contains(v, "  did track ~/.a\n  did snooze ~/.c\n\n[4/4] ~/.d  (0 B)\n") {
		t.Errorf("view does not show the decisions, then item 4:\n%s", v)
	}
	if quit := press(t, p, "o"); !quit {
		t.Error("did not quit after the last item")
	}
	if want := []string{"track ~/.a", "snooze ~/.c", "track-os ~/.d"}; !slices.Equal(*made, want) {
		t.Errorf("decided %q, want %q", *made, want)
	}
	if view(p) != "" {
		t.Errorf("the final view is not empty:\n%s", view(p))
	}
	want := "4 things to decide. Each answer is saved right away; q stops.\n" +
		"  did track ~/.a\n  did snooze ~/.c\n  did track-os ~/.d"
	if got := ansi.Strip(p.summary()); got != want {
		t.Errorf("summary =\n%s\nwant\n%s", got, want)
	}
}

// Keys typed ahead reach Update after tea.Quit, before the program stops.
// They must decide nothing, and must not panic past the last item.
func TestPickerIgnoresKeysAfterItQuit(t *testing.T) {
	t.Run("after q", func(t *testing.T) {
		b := newBox(t, false)
		b.home(map[string]string{".a": "", ".b": ""})
		p, made := fakePicker(b, b.candidates())
		if !press(t, p, "q") {
			t.Fatal("q did not quit")
		}
		for _, k := range []string{"t", "i", "o", "s", "enter", "x", "ctrl+c"} {
			if press(t, p, k) {
				t.Errorf("%s after q returned another quit", k)
			}
		}
		if len(*made) != 0 {
			t.Errorf("decided %q after q", *made)
		}
		if p.interrupted {
			t.Error("a Ctrl-C after q counted as an interrupt")
		}
	})
	t.Run("after the last item", func(t *testing.T) {
		b := newBox(t, false)
		b.home(map[string]string{".a": ""})
		p, made := fakePicker(b, b.candidates())
		if !press(t, p, "i") {
			t.Fatal("did not quit after the last item")
		}
		for _, k := range []string{"t", "i", "q", "enter"} {
			press(t, p, k) // panicked on an index out of range before
		}
		if want := []string{"ignore ~/.a"}; !slices.Equal(*made, want) {
			t.Errorf("decided %q, want %q", *made, want)
		}
	})
}

func TestPickerCtrlCIsAnInterrupt(t *testing.T) {
	for k, want := range map[string]bool{"ctrl+c": true, "esc": false, "q": false} {
		b := newBox(t, false)
		b.home(map[string]string{".a": ""})
		p, _ := fakePicker(b, b.candidates())
		press(t, p, k)
		if p.interrupted != want {
			t.Errorf("%s: interrupted = %v, want %v", k, p.interrupted, want)
		}
	}
}

func TestPickerShowsTheDecisionsThatFit(t *testing.T) {
	b := newBox(t, false)
	files := map[string]string{}
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		files["."+n] = ""
	}
	b.home(files)
	p, _ := fakePicker(b, b.candidates())
	press(t, p, "t", "t", "t", "t", "t", "t")
	if got := strings.Count(view(p), "  did track"); got != 6 {
		t.Errorf("with no height known the view shows %d decisions, want all 6", got)
	}
	// header + 2 decisions + blank, item, menu (2 lines) = 7
	p.Update(tea.WindowSizeMsg{Width: 80, Height: 7})
	v := view(p)
	if strings.Count(v, "\n") != 7 {
		t.Errorf("the view is %d lines, want 7:\n%s", strings.Count(v, "\n"), v)
	}
	if !strings.Contains(v, "q stops.\n  did track ~/.e\n  did track ~/.f\n\n[7/8] ~/.g") {
		t.Errorf("the view does not show the two latest decisions:\n%s", v)
	}
	p.Update(tea.WindowSizeMsg{Width: 80, Height: 3})
	if strings.Contains(view(p), "did track") {
		t.Errorf("no room, yet the view shows decisions:\n%s", view(p))
	}
	press(t, p, "t", "t")
	if got := strings.Count(ansi.Strip(p.summary()), "  did track"); got != 8 {
		t.Errorf("the summary holds %d decisions, want all 8", got)
	}
}

func TestPickerSaysWhenAKeyMeansNothing(t *testing.T) {
	b := newBox(t, true)
	b.repo(map[string]string{"notes.txt": "x"})
	p, made := fakePicker(b, b.candidates())
	// o (this OS only) means nothing in the repo.
	for _, k := range []string{"o", "x", "space"} {
		if k == "space" {
			if _, cmd := p.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}); cmd != nil {
				t.Fatal("space did something")
			}
		} else if press(t, p, k) {
			t.Fatalf("%s quit", k)
		}
		if !strings.HasSuffix(view(p), "  enter next           q quit\n  pick one of the letters above\n") {
			t.Errorf("after %s the view does not say to pick a letter:\n%s", k, view(p))
		}
	}
	if len(*made) != 0 {
		t.Errorf("decided %q on keys that mean nothing", *made)
	}
	press(t, p, "i")
	if strings.Contains(view(p), "pick one") {
		t.Error("the hint stays after a good key")
	}
}

func TestPickerQuitsOnQEscAndCtrlC(t *testing.T) {
	for _, k := range []string{"q", "Q", "esc", "ctrl+c"} {
		b := newBox(t, false)
		b.home(map[string]string{".a": "", ".b": ""})
		p, made := fakePicker(b, b.candidates())
		press(t, p, "t")
		if !press(t, p, k) {
			t.Errorf("%s did not quit", k)
		}
		if !slices.Equal(*made, []string{"track ~/.a"}) {
			t.Errorf("%s: decided %q", k, *made)
		}
		want := "2 things to decide. Each answer is saved right away; q stops.\n  did track ~/.a"
		if got := ansi.Strip(p.summary()); got != want || view(p) != "" {
			t.Errorf("%s: summary =\n%s\nwant\n%s\nand an empty view, not\n%s", k, got, want, view(p))
		}
	}
}

func TestPickerDecidesForReal(t *testing.T) {
	b := newBox(t, true)
	b.home(map[string]string{".config/a/x": "", ".config/b/x": ""})
	b.commit()
	p := newPicker(b.Ctx, b.candidates())
	if !press(t, p, "t", "i") {
		t.Fatal("did not quit after the last item")
	}
	if !b.isLink(b.h(".config/a")) {
		t.Error("~/.config/a was not packed")
	}
	if got := b.ignores(); !slices.Equal(got, []string{"~/.config/b"}) {
		t.Errorf("LoadIgnores = %q", got)
	}
	want := "2 things to decide. Each answer is saved right away; q stops.\n" +
		"  packed ~/.config/a into autolink/common/config/a\n" +
		"  will never ask about ~/.config/b again"
	if got := ansi.Strip(p.summary()); got != want {
		t.Errorf("summary =\n%s\nwant\n%s", got, want)
	}
}

func TestPickerShowsAFailedDecisionAndGoesOn(t *testing.T) {
	b := newBox(t, true)
	b.repo(map[string]string{"b.txt": "x"})
	p := newPicker(b.Ctx, []Candidate{repoItem(b, "gone.txt"), repoItem(b, "b.txt")})
	press(t, p, "t")
	if v := view(p); !strings.Contains(v, "  git add gone.txt failed.\n") || !strings.Contains(v, "[2/2] b.txt") {
		t.Errorf("view =\n%s", v)
	}
}

func TestPickerPeekFitsTheTerminal(t *testing.T) {
	b := newBox(t, false)
	files := map[string]string{}
	for _, n := range []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india", "juliet"} {
		files[".config/app/"+n] = ""
	}
	b.home(files)
	p := newPicker(b.Ctx, b.candidates())
	if got := p.peekLine(); got != "  alpha, bravo, charlie, delta, echo, foxtrot, golf, hotel, ..." {
		t.Errorf("peek = %q", got)
	}
	p.Update(tea.WindowSizeMsg{Width: 30, Height: 10})
	if got := p.peekLine(); got != "  alpha, bravo, charlie, ..." || len(got) > 30 {
		t.Errorf("peek at 30 columns = %q", got)
	}
	p.Update(tea.WindowSizeMsg{Width: 5, Height: 10})
	if got := p.peekLine(); got != "  alpha, ..." {
		t.Errorf("peek at 5 columns = %q: the first name always shows", got)
	}
}

func TestPickerShowsNoPeekForAFile(t *testing.T) {
	b := newBox(t, false)
	b.home(map[string]string{".netrc": "machine x password secret\n"})
	p := newPicker(b.Ctx, b.candidates())
	if strings.Contains(view(p), "secret") || len(p.peek) != 0 {
		t.Errorf("the picker shows a file's contents:\n%s", view(p))
	}
}
