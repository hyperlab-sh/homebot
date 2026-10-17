package catalog

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestLoad(t *testing.T) {
	f, err := Load([]byte(`
version: 1
conventions:
  plural: "ignored"
messages:
  - id: inbox.next_hint
    kind: hint
    text: "  Do the first one now:"
    append: " homebot next"
    placeholders: []
    persona: true
    class: voice
    source: "src/inbox.ts:275"
    notes: null
  - id: save.success.pushed
    kind: success
    text: "pushed {n:# commit|# commits}"
    placeholders:
      - name: n
        type: int
        meaning: "a count"
        forms: "# commit|# commits"
      - name: bot
        type: string
        optional: true
        meaning: "persona"
`))
	if err != nil {
		t.Fatal(err)
	}
	want := &File{Version: 1, Conventions: map[string]any{"plural": "ignored"}, Messages: []Entry{
		{ID: "inbox.next_hint", Kind: "hint", Text: "  Do the first one now:", Append: " homebot next", Persona: true, Class: "voice", Source: "src/inbox.ts:275", Placeholders: []Placeholder{}},
		{ID: "save.success.pushed", Kind: "success", Text: "pushed {n:# commit|# commits}", Placeholders: []Placeholder{
			{Name: "n", Type: Int, Meaning: "a count", Forms: "# commit|# commits"},
			{Name: "bot", Type: String, Optional: true, Meaning: "persona"},
		}},
	}}
	if d := cmp.Diff(want, f); d != "" {
		t.Errorf("Load (-want +got):\n%s", d)
	}
}

func TestLoadReadsAFixedValue(t *testing.T) {
	f, err := Load([]byte(`messages: [{id: a.b, text: "see: {cmd}", placeholders: [{name: cmd, type: string, value: "homebot link", meaning: "the command (fixed)"}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	value := "homebot link"
	want := []Placeholder{{Name: "cmd", Type: String, Value: &value, Meaning: "the command (fixed)"}}
	if d := cmp.Diff(want, f.Messages[0].Placeholders); d != "" {
		t.Errorf("placeholders (-want +got):\n%s", d)
	}
	if v, ok := f.Messages[0].Placeholders[0].Fixed(); !ok || v != value {
		t.Errorf("Fixed() = %q, %v", v, ok)
	}
}

// An empty value is a value: Load keeps it apart from none, so the
// generator can refuse it.
func TestLoadKeepsAnEmptyValueApart(t *testing.T) {
	f, err := Load([]byte(`messages: [{id: a.b, text: "{a}{b}", placeholders: [{name: a, type: string, value: ""}, {name: b, type: string}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	ph := f.Messages[0].Placeholders
	if v, ok := ph[0].Fixed(); !ok || v != "" {
		t.Errorf("value: \"\" reads as Fixed() = %q, %v", v, ok)
	}
	if _, ok := ph[1].Fixed(); ok {
		t.Error("no value reads as fixed")
	}
}

func TestLoadRefusesEmptyAndBroken(t *testing.T) {
	for _, src := range []string{"", "version: 1\n", "messages: [\n"} {
		if _, err := Load([]byte(src)); err == nil {
			t.Errorf("Load(%q) should fail", src)
		}
	}
}

func TestLoadRefusesUnknownFields(t *testing.T) {
	for _, c := range []struct{ name, src, field string }{
		{"top level", "mesages: []\nmessages: [{id: a.b, text: x}]", "mesages"},
		{"entry", "messages: [{id: a.b, text: x, apend: \" homebot\"}]", "apend"},
		{"placeholder", "messages: [{id: a.b, text: \"{n}\", placeholders: [{name: n, type: int, optinal: true}]}]", "optinal"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load([]byte(c.src))
			if err == nil || !strings.Contains(err.Error(), c.field) {
				t.Errorf("err = %v, want it to name %q", err, c.field)
			}
		})
	}
}

func TestLoadKeepsTheDocumentationSections(t *testing.T) {
	f, err := Load([]byte("conventions: {plural: a}\npalette: {heading: {ansi: bold}}\nglyphs: [{codepoint: U+2192}]\nmessages: [{id: a.b, text: x}]"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Conventions == nil || f.Palette == nil || f.Glyphs == nil {
		t.Errorf("sections lost: %+v", f)
	}
}

func TestTypeValid(t *testing.T) {
	for _, ty := range []Type{Int, String, Path, Duration} {
		if !ty.Valid() {
			t.Errorf("%s should be valid", ty)
		}
	}
	for _, ty := range []Type{"", "number", "Int"} {
		if ty.Valid() {
			t.Errorf("%q should not be valid", ty)
		}
	}
}
