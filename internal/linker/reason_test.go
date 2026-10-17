package linker

import (
	"os"
	"slices"
	"strings"
	"testing"

	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/msg/catalog"
	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// An Op keeps its reason twice. Reason is the neutral English, the form to
// store or log; ReasonText is the catalog message not yet said, which a print
// path says in the active voice. These tests hold the two apart for every
// reason the planner can give, under a persona that rewords all of them.

// isReasonID is whether id is one of the catalog ids the planner gives as an
// op's reason: the layout.* errors and the conflict.* errors.
func isReasonID(id string) bool {
	return strings.HasPrefix(id, "layout.") || strings.HasPrefix(id, "conflict.")
}

// reasonMarker is what the hostile persona says for id.
func reasonMarker(id string) string { return "PERSONA[" + id + "]" }

// hostileReasons is a persona with a line for every voiced reason id. Each
// line is the id's marker followed by every placeholder the entry declares,
// the optional ones too, so a spoken reason also shows that the values
// survived the rewording.
func hostileReasons() *persona.Voice {
	lines := map[string][]persona.Line{}
	for _, e := range msg.Entries() {
		if !isReasonID(e.ID) || !e.Persona {
			continue
		}
		text := reasonMarker(e.ID)
		for _, s := range e.Slots {
			if s.Name != catalog.Bot && s.Name != catalog.Self {
				text += " {" + s.Name + "}"
			}
		}
		lines[e.ID] = []persona.Line{{Text: text, Lang: language.English}}
	}
	return &persona.Voice{ID: "hostile", Name: "Hostile", Lines: lines}
}

// speakingBox is a box whose Context speaks as the hostile persona on a
// terminal, before anything is planned.
func speakingBox(t *testing.T) *box {
	t.Helper()
	b := newBox(t)
	yes := true
	b.Ctx.StdoutTerminal = &yes
	persona.Override(b.Ctx, hostileReasons())
	return b
}

// reasonCases are repos that give every reason the layout can give, grouped
// by the kind of reason.
var reasonCases = []struct {
	name  string
	files map[string]string
	// want are the reason ids the repo gives, with the persona flag of the
	// entry: layout.inner_error is a fact and is never voiced.
	want []string
}{
	{"invalid, by the name", map[string]string{
		"autolink/common/dot-config/x": "x", // layout.old_name
		"autolink/common/()/x":         "x", // layout.empty_parens
		"autolink/common/((y))/x":      "x", // layout.double_parens
		"autolink/common/(.z)/x":       "x", // layout.top_dot
		"autolink/common/(f)":          "f", // layout.file_parens
		"autolink/common/(d)/(..)/x":   "x", // layout.dot_parens
	}, []string{"layout.old_name", "layout.empty_parens", "layout.double_parens", "layout.top_dot", "layout.file_parens", "layout.dot_parens"}},
	{"invalid, by the layers", map[string]string{
		"autolink/common/x":          "1", // layout.both_layers
		"autolink/linux/x/y":         "2",
		"autolink/common/(claude)/x": "x", // layout.spelling
		"autolink/linux/claude/y":    "y",
		"autolink/common/home/x":     "x", // layout.inside_repo
	}, []string{"layout.both_layers", "layout.spelling", "layout.inside_repo"}},
	{"invalid, a marked folder inside a plain one", map[string]string{
		"autolink/common/a/(b)/c": "c", // layout.must_mark
		"autolink/common/m/(f)":   "f", // layout.inner_file_parens
	}, []string{"layout.must_mark", "layout.inner_file_parens"}},
	{"invalid, the fragments spliced into a line", map[string]string{
		"autolink/common/q/(b)/x":  "x", // layout.must_mark with layout.must_mark.every_layer
		"autolink/linux/q/y":       "y",
		"autolink/common/n/(())/c": "c", // layout.inner_error around layout.double_parens
	}, []string{"layout.must_mark", "layout.inner_error"}},
}

// seenReasonIDs are the ids of the reasons of p's conflicts and invalid ops.
func seenReasonIDs(p Plan) []string {
	var ids []string
	for _, op := range p.Only(Conflict, Invalid) {
		ids = append(ids, op.ReasonText.ID())
	}
	return ids
}

// checkReason holds one op to the rule: the stored reason is the neutral
// English of its text, and the text is said in the persona's words, except
// where its entry is not voiced.
func checkReason(t *testing.T, b *box, op Op) {
	t.Helper()
	id := op.ReasonText.ID()
	if id == "" {
		t.Fatalf("%s op for %s has no ReasonText", op.Kind, op.Target)
	}
	if got, want := op.Reason, voice.Record(b.Ctx, op.ReasonText); got != want {
		t.Errorf("%s: Reason = %q, want the neutral %q", id, got, want)
	}
	if strings.Contains(op.Reason, "PERSONA") {
		t.Errorf("%s: the stored Reason %q holds the persona's words", id, op.Reason)
	}
	said := voice.Plain(b.Ctx, op.ReasonText)
	if !op.ReasonText.Persona() {
		if said != op.Reason {
			t.Errorf("%s: not voiced, so it is said as stored: %q, want %q", id, said, op.Reason)
		}
		return
	}
	if !strings.Contains(said, reasonMarker(id)) {
		t.Errorf("%s: said %q, want the persona's line", id, said)
	}
	if said == op.Reason {
		t.Errorf("%s: the persona's words and the stored Reason are both %q", id, said)
	}
}

// Every reason the planner gives, by class: the layout errors of a bad name,
// of the layers, of a marked folder in a plain one and of the fragments
// spliced into a line; the conflicts with what is in $HOME; and the backstop
// no repo reaches. Each is stored neutral and said in the persona's words.
func TestEveryReasonIsStoredNeutralAndSpokenInTheVoice(t *testing.T) {
	var seen []string
	for _, c := range reasonCases {
		t.Run(c.name, func(t *testing.T) {
			b := speakingBox(t)
			b.repo(c.files)
			p := b.plan()
			ids := seenReasonIDs(p)
			for _, want := range c.want {
				if !slices.Contains(ids, want) {
					t.Errorf("the repo gave %q, want %q among them", ids, want)
				}
			}
			for _, op := range p.Only(Invalid) {
				checkReason(t, b, op)
			}
			seen = append(seen, ids...)
		})
	}
	t.Run("conflict, something real in $HOME", func(t *testing.T) {
		b := speakingBox(t)
		b.Files(map[string]string{
			"home/.home/autolink/common/thing/inner": "x",
			"home/.home/autolink/common/other":       "y",
			"home/.home/autolink/common/differ":      "repo",
			"home/.home/autolink/common/link":        "l",
			"home/.home/autolink/common/dirlink/a":   "a",
			"home/.thing":                            "a file",
			"home/.other/inside":                     "z",
			"home/.differ":                           "mine",
			"home/elsewhere":                         "e",
		})
		must(t, os.Symlink(b.h("elsewhere"), b.h(".link")))
		must(t, os.Symlink("elsewhere", b.h(".dirlink")))
		p := b.plan()
		ids := seenReasonIDs(p.Only(Conflict))
		for _, want := range []string{"conflict.file_vs_folder", "conflict.folder_vs_file", "conflict.different", "conflict.symlink_outside"} {
			if !slices.Contains(ids, want) {
				t.Errorf("the home gave %q, want %q among them", ids, want)
			}
		}
		for _, op := range p.Only(Conflict) {
			checkReason(t, b, op)
			// The values stay in the persona's line: the symlink's destination.
			if op.ReasonText.ID() == "conflict.symlink_outside" && !strings.Contains(voice.Plain(b.Ctx, op.ReasonText), b.h("elsewhere")) {
				t.Errorf("the spoken reason lost the destination: %q", voice.Plain(b.Ctx, op.ReasonText))
			}
		}
		seen = append(seen, ids...)
	})
	t.Run("invalid, the backstop", func(t *testing.T) {
		seen = append(seen, backstopReasons(t)...)
	})

	// A new voiced layout or conflict entry is a new reason: it needs a repo
	// above (or the backstop) that gives it, or this fails. The fragment
	// layout.must_mark.every_layer is persona: false and is spliced into
	// layout.must_mark, so it is never a reason of its own.
	for _, e := range msg.Entries() {
		if isReasonID(e.ID) && e.Persona && !slices.Contains(seen, e.ID) {
			t.Errorf("no test repo gives the reason %s", e.ID)
		}
	}
}

// layout.outside_folder is the backstop for a name that would land outside
// the folder it is in. ParseName refuses those names first, so no repo
// reaches it: it is checked through the planner's own seam.
func backstopReasons(t *testing.T) []string {
	t.Helper()
	b := speakingBox(t)
	pl := &planner{ctx: b.Ctx}
	pl.invalid(b.h("x"), b.r("autolink/common/x"), msg.LayoutOutsideFolder("~/x"))
	if len(pl.ops) != 1 {
		t.Fatalf("ops = %+v", pl.ops)
	}
	checkReason(t, b, pl.ops[0])
	return seenReasonIDs(pl.ops)
}

// The fragments spliced into a reason are English inside the persona's line:
// layout.must_mark.every_layer is not voiced, and layout.inner_error is a
// fact whose inner reason is stored text. The line around them is voiced
// (layout.must_mark) or stays English (layout.inner_error).
func TestFragmentsInsideAReasonStayEnglish(t *testing.T) {
	b := speakingBox(t)
	b.repo(map[string]string{
		"autolink/common/q/(b)/x":  "x",
		"autolink/linux/q/y":       "y",
		"autolink/common/n/(())/c": "c",
	})
	byID := map[string]Op{}
	for _, op := range b.plan().Only(Invalid) {
		byID[op.ReasonText.ID()] = op
	}

	mark, ok := byID["layout.must_mark"]
	if !ok {
		t.Fatalf("no layout.must_mark reason in %v", byID)
	}
	said := voice.Plain(b.Ctx, mark.ReasonText)
	if !strings.HasPrefix(said, reasonMarker("layout.must_mark")) || !strings.Contains(said, " in every layer") || strings.Contains(said, reasonMarker("layout.must_mark.every_layer")) {
		t.Errorf("layout.must_mark said %q, want the persona's line around the English fragment", said)
	}
	if !strings.Contains(mark.Reason, "in every layer") {
		t.Errorf("stored Reason %q lost the fragment", mark.Reason)
	}

	inner, ok := byID["layout.inner_error"]
	if !ok {
		t.Fatalf("no layout.inner_error reason in %v", byID)
	}
	if said := voice.Plain(b.Ctx, inner.ReasonText); said != inner.Reason || strings.Contains(said, "PERSONA") {
		t.Errorf("layout.inner_error said %q, want the stored English %q", said, inner.Reason)
	}
}

// Piped output and --plain stay neutral whatever the persona: what is said
// is what is stored.
func TestAReasonIsSaidNeutralWhenPipedOrPlain(t *testing.T) {
	b := speakingBox(t)
	b.repo(map[string]string{"autolink/common/dot-config/x": "x"})
	ops := b.plan().Only(Invalid)
	if len(ops) != 1 {
		t.Fatalf("ops = %+v", ops)
	}
	op := ops[0]
	if got := voice.Plain(b.Ctx, op.ReasonText); !strings.Contains(got, "PERSONA") {
		t.Fatalf("on a terminal: %q, want the persona", got)
	}

	no := false
	b.Ctx.StdoutTerminal = &no
	if got := voice.Plain(b.Ctx, op.ReasonText); got != op.Reason {
		t.Errorf("piped: %q, want %q", got, op.Reason)
	}

	yes := true
	b.Ctx.StdoutTerminal = &yes
	b.Ctx.Plain = true
	if got := voice.Plain(b.Ctx, op.ReasonText); got != op.Reason {
		t.Errorf("--plain: %q, want %q", got, op.Reason)
	}
}
