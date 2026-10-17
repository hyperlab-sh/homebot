package inbox

import (
	"strings"
	"testing"

	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// The link-invalid item's detail lists the first reasons of the layout errors.
// Each reason is said in the active voice, as the item's title is; with no
// persona to speak, on a pipe or under --plain it is the neutral English that
// the planner stores.

// speaksLayoutSpelling is a persona with its own line for layout.spelling.
func speaksLayoutSpelling() *persona.Voice {
	return &persona.Voice{ID: "hostile", Name: "Hostile", Lines: map[string][]persona.Line{
		"layout.spelling": {{Text: "PERSONA SPELLING {marked}/{plain} in {where_marked} and {where_plain}", Lang: language.English}},
	}}
}

func spellingRepo(b *box) {
	b.Files(map[string]string{
		"home/.home/autolink/common/(claude)/x": "x",
		"home/.home/autolink/linux/claude/y":    "y",
	})
}

func TestLinkInvalidDetailSaysTheReasonInThePersonasVoice(t *testing.T) {
	b := newBox(t, testutil.Options{})
	yes := true
	b.Ctx.StdoutTerminal = &yes
	persona.Override(b.Ctx, speaksLayoutSpelling())
	spellingRepo(b)

	it := b.mustFind("link-invalid")
	if !strings.Contains(it.Detail, "PERSONA SPELLING (claude)/claude in autolink/common/(claude) and autolink/linux/claude") {
		t.Errorf("detail = %q, want the persona's line with its values", it.Detail)
	}
	if strings.Contains(it.Detail, "use one spelling everywhere") {
		t.Errorf("detail = %q still holds the neutral English", it.Detail)
	}
}

func TestLinkInvalidDetailStaysNeutralWhenPipedOrPlain(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(b *box)
	}{
		{"piped", func(b *box) {
			no := false
			b.Ctx.StdoutTerminal = &no
		}},
		{"--plain", func(b *box) {
			yes := true
			b.Ctx.StdoutTerminal = &yes
			b.Ctx.Plain = true
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newBox(t, testutil.Options{})
			persona.Override(b.Ctx, speaksLayoutSpelling())
			tc.setup(b)
			spellingRepo(b)

			it := b.mustFind("link-invalid")
			if strings.Contains(it.Detail, "PERSONA") {
				t.Errorf("detail = %q holds the persona's words", it.Detail)
			}
			if !strings.Contains(it.Detail, `spelled "(claude)" in autolink/common/(claude) but "claude" in autolink/linux/claude; use one spelling everywhere`) {
				t.Errorf("detail = %q, want the neutral reason", it.Detail)
			}
		})
	}
}
