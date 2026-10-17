package inbox

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// hostileName holds ', " and a backslash (not one that starts an ESC form,
// which the renderer refuses outright).
const hostileName = `T'es"t\r`

// hostile is a persona whose every inbox line carries a quote, a double quote,
// a backslash and a line break, with two nudge variants to rotate between.
func hostile(emoji string) *persona.Voice {
	en := language.English
	return &persona.Voice{ID: "tester", Name: hostileName, Emoji: emoji, Lines: map[string][]persona.Line{
		"inbox.nudge": {
			{Text: "{bot}: it's one \"{n}\" \\ {urgent}\nok", Lang: en},
			{Text: "{bot}: it's two \"{n}\" \\ {urgent}\nok", Lang: en},
		},
		"inbox.notify.title": {{Text: "{bot} it's \"on\" \\ \nnow", Lang: en}},
		"inbox.notify.body":  {{Text: "{title}{more} isn't \"ok\" \\ \nnext.", Lang: en}},
	}}
}

// hostileNudge is what NudgeLine says in the hostile persona's variant v
// ("one" or "two") for n items of which urgent are urgent.
func hostileNudge(v string, n, urgent int) string {
	suffix := ""
	if urgent > 0 {
		suffix = fmt.Sprintf(" (%d urgent)", urgent)
	}
	return fmt.Sprintf("%s: it's %s \"%d\" \\ %s\nok homebot", hostileName, v, n, suffix)
}

func todoItems(ids ...string) []Item {
	var items []Item
	for _, id := range ids {
		items = append(items, Item{ID: id, Level: Todo, Title: "x"})
	}
	return items
}

func TestHostilePersonaNudgeKeepsItsTextRotatesOnItemsAndKeepsUrgency(t *testing.T) {
	b := newBox(t, testutil.Options{})
	persona.Override(b.Ctx, hostile("🧪"))
	seen := map[string]bool{}
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		emoji, text := NudgeLine(b.Ctx, todoItems(id))
		eq(t, emoji, "🧪")
		if text != hostileNudge("one", 1, 0) && text != hostileNudge("two", 1, 0) {
			t.Fatalf("nudge for %q = %q", id, text)
		}
		// The same inbox says the same thing again.
		_, again := NudgeLine(b.Ctx, todoItems(id))
		eq(t, again, text)
		seen[text] = true
	}
	if len(seen) != 2 {
		t.Errorf("eight different inboxes all got %v: the pick is not keyed on the item ids", seen)
	}

	// The urgency suffix is the code's, whichever variant is picked.
	urgent := []Item{{ID: "u1", Level: Urgent, Title: "Bad"}, {ID: "u2", Level: Urgent, Title: "Worse"}, {ID: "t", Level: Todo}}
	_, text := NudgeLine(b.Ctx, urgent)
	if text != hostileNudge("one", 3, 2) && text != hostileNudge("two", 3, 2) {
		t.Errorf("urgent nudge = %q", text)
	}

	// The file holds the one line: the break became a space and nothing else changed.
	if err := WriteCache(b.Ctx, urgent); err != nil {
		t.Fatal(err)
	}
	eq(t, readCache(t, b.Ctx, "nudge"), "🧪 "+strings.ReplaceAll(text, "\n", " ")+"\n")
	eq(t, readCache(t, b.Ctx, "symbol"), "🧪\n")
	eq(t, readCache(t, b.Ctx, "count"), "3!\n")
}

func TestAPersonaLineThatDropsTheUrgencySlotIsNotUsed(t *testing.T) {
	b := newBox(t, testutil.Options{})
	persona.Override(b.Ctx, &persona.Voice{ID: "tester", Name: "Tester", Emoji: "🧪", Lines: map[string][]persona.Line{
		"inbox.nudge": {{Text: "{bot}: all calm, {n}", Lang: language.English}},
	}})
	emoji, text := NudgeLine(b.Ctx, []Item{{ID: "u", Level: Urgent, Title: "Bad"}})
	eq(t, emoji, "🧪")
	eq(t, text, "Tester: 1 thing needs you (1 urgent), run: homebot")
}

func TestHostilePersonaHeaderIsExact(t *testing.T) {
	b := newBox(t, testutil.Options{})
	b.Ctx.ForceVoice = true
	items := []Item{{ID: "u", Level: Urgent, Title: "Bad"}, {ID: "t", Level: Todo, Title: "Meh"}}
	persona.Override(b.Ctx, hostile("🧪"))
	eq(t, ansi.Strip(Render(b.Ctx, nil, nil, false)), "🧪 "+hostileName+" · testhost · nothing needs you. Home is in order.")
	eq(t, ansi.Strip(Render(b.Ctx, items, nil, false)), "🧪 "+hostileName+" · testhost · 2 things need you (1 urgent)\n\n  1  ! Bad\n  2  • Meh")

	// An emoji the cache writers would refuse is not drawn either.
	persona.Override(b.Ctx, hostile("$(touch pwned)"))
	eq(t, ansi.Strip(Render(b.Ctx, nil, nil, false)), hostileName+" · testhost · nothing needs you. Home is in order.")
}

func TestHostilePersonaNotificationTextIsExact(t *testing.T) {
	b := newBox(t, testutil.Options{})
	persona.Override(b.Ctx, hostile("🧪"))
	one := []Item{{ID: "a", Level: Urgent, Title: "Bad"}}
	title, body := NotificationText(b.Ctx, one)
	// The title is one line; the body keeps its break, which the senders quote.
	eq(t, title, "🧪 "+hostileName+" it's \"on\" \\  now")
	eq(t, body, "Bad isn't \"ok\" \\ \nnext. homebot")
	two := []Item{{ID: "a", Level: Urgent, Title: "Bad"}, {ID: "b", Level: Urgent, Title: "Worse"}}
	_, body = NotificationText(b.Ctx, two)
	eq(t, body, "Bad (+1 more) isn't \"ok\" \\ \nnext. homebot")

	// An unsafe emoji is dropped from the title and nothing else changes.
	persona.Override(b.Ctx, hostile("\x1b]52;c;eA==\x07"))
	title, _ = NotificationText(b.Ctx, one)
	eq(t, title, hostileName+" it's \"on\" \\  now")
}
