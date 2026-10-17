package catalog

import (
	"math"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"golang.org/x/text/language"
)

func mustParse(t *testing.T, text string) Template {
	t.Helper()
	tmpl, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse(%q): %v", text, err)
	}
	return tmpl
}

func TestFormatPlaceholdersAndBraces(t *testing.T) {
	cases := []struct {
		text   string
		values map[string]any
		want   string
	}{
		{"plain", nil, "plain"},
		{"", nil, ""},
		{"{a} and {b}", map[string]any{"a": "x", "b": 2}, "x and 2"},
		{"{a}{a}", map[string]any{"a": "z"}, "zz"},
		{"the command did not write {out}, the file given to it as {{out}}", map[string]any{"out": "/tmp/o"}, "the command did not write /tmp/o, the file given to it as {out}"},
		{`- brew: "{{os}}/Brewfile"`, nil, `- brew: "{os}/Brewfile"`},
		{"}} and {{", nil, "} and {"},
		{"{{{a}}}", map[string]any{"a": "v"}, "{v}"},
		{"{a}", map[string]any{"a": ""}, ""},
		{"multi\nline {a}\n", map[string]any{"a": "x"}, "multi\nline x\n"},
		{"🚚 {bot}: hi", map[string]any{"bot": "homebot"}, "🚚 homebot: hi"},
	}
	for _, c := range cases {
		if got := mustParse(t, c.text).Format(language.English, c.values, nil); got != c.want {
			t.Errorf("Format(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestFormatStylesOnlyTheValues(t *testing.T) {
	mark := func(name, value string) string { return "[" + name + ":" + value + "]" }
	cases := []struct {
		text   string
		values map[string]any
		want   string
	}{
		{"{a} and {n:# thing|# things}, {{a}}", map[string]any{"a": "x", "n": 2}, "[a:x] and [n:2] things, {a}"},
		{"{n:# of #|#/#}", map[string]any{"n": 4}, "[n:4]/[n:4]"},
		{"{n:one=no # here|other=none}", map[string]any{"n": 5}, "none"},
		{"{a} {missing} {n:# a|# b}", map[string]any{"a": "", "n": "3"}, "[a:] {missing} {n:# a|# b}"},
		{"plain", nil, "plain"},
	}
	for _, c := range cases {
		if got := mustParse(t, c.text).Format(language.English, c.values, mark); got != c.want {
			t.Errorf("Format(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestEnglishPluralsMatchUIPlural(t *testing.T) {
	// ui.ts plural(): the singular only for exactly 1.
	tmpl := mustParse(t, "{n:# thing needs|# things need} you")
	for n, want := range map[int]string{
		0:          "0 things need you",
		1:          "1 thing needs you",
		2:          "2 things need you",
		11:         "11 things need you",
		21:         "21 things need you",
		101:        "101 things need you",
		10_000_001: "10000001 things need you",
		-1:         "-1 thing needs you",
		-2:         "-2 things need you",
	} {
		if got := tmpl.Format(language.English, map[string]any{"n": n}, nil); got != want {
			t.Errorf("n=%d: %q, want %q", n, got, want)
		}
	}
}

func TestPluralHashIsEveryOccurrence(t *testing.T) {
	got := mustParse(t, "{n:# of #|#/#}").Format(language.English, map[string]any{"n": 4}, nil)
	if got != "4/4" {
		t.Errorf("got %q", got)
	}
}

func TestNamedCategoriesFallBackToOther(t *testing.T) {
	// Polish: 1 is one, 2-4 few, 5-21 many.
	pl := language.Polish
	named := mustParse(t, "{n:one=# a|few=# b|other=# c}")
	for n, want := range map[int]string{1: "1 a", 3: "3 b", 5: "5 c", 22: "22 b"} {
		if got := named.Format(pl, map[string]any{"n": n}, nil); got != want {
			t.Errorf("pl n=%d: %q, want %q", n, got, want)
		}
	}
	// The shorthand a|b is one|other in every language.
	short := mustParse(t, "{n:# a|# b}")
	for n, want := range map[int]string{1: "1 a", 3: "3 b", 5: "5 b"} {
		if got := short.Format(pl, map[string]any{"n": n}, nil); got != want {
			t.Errorf("pl shorthand n=%d: %q, want %q", n, got, want)
		}
	}
}

func TestPluralOfExtremeCounts(t *testing.T) {
	tmpl := mustParse(t, "{n:#x|#y}")
	for _, n := range []int{math.MinInt, math.MaxInt} {
		if got := tmpl.Format(language.English, map[string]any{"n": n}, nil); got == "" {
			t.Errorf("n=%d rendered nothing", n)
		}
	}
}

func TestMissingOrWrongValuesShowTheGap(t *testing.T) {
	cases := []struct {
		text   string
		values map[string]any
		want   string
	}{
		{"a {x} b", nil, "a {x} b"},
		{"{n:# a|# b}", nil, "{n:# a|# b}"},
		{"{n:# a|# b}", map[string]any{"n": "3"}, "{n:# a|# b}"},
		{"{v}", map[string]any{"v": 1.5}, "1.5"},
	}
	for _, c := range cases {
		if got := mustParse(t, c.text).Format(language.English, c.values, nil); got != c.want {
			t.Errorf("Format(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

func TestRefs(t *testing.T) {
	got := mustParse(t, "{b} {{x}} {n:# a|# b} {b} {k:one=x|other=y}").Refs()
	want := []Ref{
		{Name: "b"},
		{Name: "n", Forms: []Form{{"one", "# a"}, {"other", "# b"}}},
		{Name: "b"},
		{Name: "k", Forms: []Form{{"one", "x"}, {"other", "y"}}},
	}
	if d := cmp.Diff(want, got, cmpopts.IgnoreUnexported(Ref{})); d != "" {
		t.Errorf("Refs (-want +got):\n%s", d)
	}
}

func TestParseRejects(t *testing.T) {
	for _, text := range []string{
		"{a", "a}", "{{a}", "{a{b}}", "}{", "{}", "{A}", "{1a}", "{a-b}", "{a b}",
		"{n:# a}", "{n:# a|# b|# c}", "{n:one=a|two=b}", "{n:one=a|one=b|other=c}",
		"{n:a|}", "{n:|b}", "{n:one=a|b}", "{n:a|one=b}", "{n:colour=a|other=b}", "{n:one=|other=b}",
	} {
		if _, err := Parse(text); err == nil {
			t.Errorf("Parse(%q) should fail", text)
		}
	}
}

func TestParseAccepts(t *testing.T) {
	for _, text := range []string{
		"{n:# a|# b}", "{n:one=# a|other=# b}", "{n:one=a|few=b|other=c}", "{n:zero=z|other=o}",
		"{n:a=b|c}", "{a_b1}",
	} {
		if _, err := Parse(text); err != nil {
			t.Errorf("Parse(%q): %v", text, err)
		}
	}
}
