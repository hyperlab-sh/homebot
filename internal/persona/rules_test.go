package persona

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// W7 (plan S5, M13): outside its placeholders a variant or tagline holds a
// token its English does not.
func TestAddedFactTokens(t *testing.T) {
	cases := []struct {
		name, english, line string
		fact                bool
	}{
		// What the spec lists.
		{"digit", "a thing", "a thing 5", true},
		{"slash", "a thing", "a thing /x", true},
		{"tilde", "a thing", "a thing ~", true},
		{"double dash", "a thing", "a thing --flag", true},
		{"dash word", "a thing", "a thing -x", true},
		{"dash word first", "a thing", "-x thing", true},
		{"dash word in parentheses", "a thing", "a thing (-x)", true},
		{"homebot", "a thing", "a thing homebot", true},
		{"http", "a thing", "see https://example.org", true},
		{"www", "a thing", "see www.example.org", true},
		{"backtick", "a thing", "run `rm` now", true},
		{"lone backtick", "a thing", "a ` thing", true},
		{"dollar", "a thing", "a thing $", true},
		{"equals", "a thing", "a thing =", true},
		{"at", "a thing", "a thing @", true},
		{"yml", "a thing", "edit it.yml", true},
		{"toml", "a thing", "edit it.toml", true},
		{"age", "a thing", "open it.age", true},
		{"txt", "a thing", "open it.txt", true},
		// A token the English has is not an addition.
		{"backtick in the English", "run `x` first", "first run `y`", false},
		{"slash in the English", "a/b thing", "a thing /", false},
		{"digit in the English", "step 1", "1 step", false},
		// Hyphenated words are words, not flags.
		{"hyphenated word", "a thing", "a well-known thing", false},
		{"two hyphens in a word", "a thing", "a state-of-the-art thing", false},
		{"hyphenated after punctuation", "a thing", "a thing, e-mail", false},
		{"a hyphen inside a word", "a thing", "an up-to-date thing", false},
		// A dash that starts a word is still a flag.
		{"flag after a hyphenated word", "a thing", "a well-known thing -x", true},
		{"nothing", "a thing", "one thing, quite plainly.", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := newFact(tc.line, tc.english) != ""; got != tc.fact {
				t.Errorf("newFact(%q, %q) = %q, want a fact: %v", tc.line, tc.english, newFact(tc.line, tc.english), tc.fact)
			}
		})
	}
}

// The tagline is checked against nothing, so a backtick is an added fact, and
// so is a hyphenated word that is not.
func TestAddedFactInTagline(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	for tagline, want := range map[string]bool{
		"run `x`":                         true,
		"a well-known robot":              false,
		"a tidy home, one step at a time": false,
	} {
		f := &File{ID: "tester", Name: "Tester", Emoji: "🧪", Tagline: tagline, schemaSet: true, schema: 1}
		if got := hasDiagnostic(Validate(ctx, f, ValidateOptions{}), "w7"); got != want {
			t.Errorf("tagline %q: W7 = %v, want %v", tagline, got, want)
		}
	}
}

// V23 (plan S5, L3): placeholders that touch in the English touch in the
// variant. In a run of three or more every neighbouring pair counts:
// {git_prefix}{repo_abs}{abort} is the shell command "git -C <path> rebase
// --abort", and a variant that puts a space between the last two breaks it.
func TestAdjacentPlaceholderRuns(t *testing.T) {
	cases := []struct {
		name, english, line string
		v23                 bool
	}{
		{"pair as is", "{a}{b}", "{a}{b}", false},
		{"pair split", "{a}{b}", "{a} {b}", true},
		{"pair reversed", "{a}{b}", "{b}{a}", true},
		{"run as is", "run {a}{b}{c}", "do {a}{b}{c}", false},
		{"run split at the end", "run {a}{b}{c}", "do {a}{b} {c}", true},
		{"run split at the start", "run {a}{b}{c}", "do {a} {b}{c}", true},
		{"run reordered", "run {a}{b}{c}", "do {c}{b}{a}", true},
		{"run of four, split in the middle", "{a}{b}{c}{d}", "{a}{b} {c}{d}", true},
		{"run of four as is", "{a}{b}{c}{d}", "{a}{b}{c}{d}", false},
		{"run repeating a name", "x{raw}{raw}", "y{raw}{raw}", false},
		{"run repeating a name, split", "x{raw}{raw}", "y{raw} {raw}", true},
		{"selector first", "{n:# x|# y}{b}", "{n:# a|# b}{b}", false},
		{"selector last", "{b}{n:# x|# y}", "{b}{n:# a|# b}", false},
		{"selector split", "{n:# x|# y}{b}", "{n:# a|# b} {b}", true},
		{"separated in the English", "{a} {b}", "{b} {a}", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ds []Diagnostic
			validateLayout(msg.Meta{ID: "fixture", Text: tc.english}, tc.line, 1, &ds)
			if got := hasDiagnostic(ds, "v23"); got != tc.v23 {
				t.Fatalf("V23 for %q against %q = %v, want %v (%+v)", tc.line, tc.english, got, tc.v23, ds)
			}
		})
	}
	// The real entries the run comes from.
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	for id, split := range map[string]string{
		"save.error.rebase_stuck":       "{git_prefix}{repo_abs} {abort}",
		"config.error.export_duplicate": "{exports_open}{i} {exports_close}",
	} {
		meta, ok := msg.Lookup(id)
		if !ok {
			t.Fatalf("no entry %s", id)
		}
		whole := strings.Replace(meta.Text, strings.ReplaceAll(split, " ", ""), split, 1)
		if whole == meta.Text {
			t.Fatalf("%s: %q is not in %q", id, split, meta.Text)
		}
		f := &File{ID: "tester", Name: "Tester", Emoji: "🧪", schemaSet: true, schema: 1,
			Lines: map[string][]string{id: {whole}}}
		if ds := Validate(ctx, f, ValidateOptions{}); !hasDiagnostic(ds, "v23") {
			t.Errorf("%s: %q was accepted: %+v", id, whole, ds)
		}
	}
}

// diagnose is what persona validate says of a file's text: Parse, then
// Validate.
func diagnose(ctx *app.Context, source string) []Diagnostic {
	f, ds := Parse("tester.toml", []byte(source))
	if f != nil {
		ds = append(ds, Validate(ctx, f, ValidateOptions{})...)
	}
	return ds
}

// errorOf is the first error with the code ("v6"), if there is one.
func errorOf(ds []Diagnostic, code string) (Diagnostic, bool) {
	for _, d := range ds {
		if d.Severity == Error && d.Text.ID() == "persona.diag."+code {
			return d, true
		}
	}
	return Diagnostic{}, false
}

// loadsAs reports whether Load can use a persona whose file is source.
func loadsAs(t *testing.T, box *testutil.Box, source string) bool {
	t.Helper()
	writePersona(t, box.Ctx.ConfigDir, "tester", source)
	v, _, err := Load(box.Ctx, "tester")
	if (err == nil) != (v != nil) {
		t.Fatalf("Load returned %v and %v", v, err)
	}
	return err == nil
}

// V6 (plan S5, C1): the emoji is one grapheme, with no ASCII in it. The
// values here are the ones that would hurt if an emoji were ever pasted into
// a shell, a prompt or a format string.
func TestHostileEmojiAreRefused(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	for _, emoji := range []string{
		"$(touch pwned)", "`id`", "%F{red}", `\`, `"`, "a", "1️⃣", "🍋🚲",
		"", " ", "🧪 ", "$", "'", "%", "🧪a", "a🧪", "{bot}", "🧪\n",
	} {
		t.Run(fmt.Sprintf("%q", emoji), func(t *testing.T) {
			source := "schema = 1\nname = 'Tester'\nemoji = " + strconv.Quote(emoji) + "\n"
			if _, found := errorOf(diagnose(box.Ctx, source), "v6"); !found {
				t.Errorf("no V6 error: %+v", diagnose(box.Ctx, source))
			}
			if loadsAs(t, box, source) {
				t.Error("a persona with this emoji loads")
			}
		})
	}
	for _, emoji := range []string{"🧪", "🧹", "☔", "🏳️‍🌈"} {
		t.Run("fine/"+emoji, func(t *testing.T) {
			source := "schema = 1\nname = 'Tester'\nemoji = " + strconv.Quote(emoji) + "\n"
			if _, found := errorOf(diagnose(box.Ctx, source), "v6"); found {
				t.Errorf("V6 for %q", emoji)
			}
			if !loadsAs(t, box, source) {
				t.Error("a persona with this emoji does not load")
			}
		})
	}
}

// V7 (plan S5, C1, I11): the tagline is one line of at most 100 characters
// with no $ and no backtick. % is allowed.
func TestTaglineRules(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	cases := []struct {
		name, tagline string
		bad           bool
	}{
		{"newline", "one\ntwo", true},
		{"trailing newline", "one\n", true},
		{"carriage return", "one\rtwo", true},
		{"101 characters", strings.Repeat("x", 101), true},
		{"101 multibyte characters", strings.Repeat("é", 101), true},
		{"command substitution", "$(touch pwned)", true},
		{"backtick", "`id`", true},
		{"a lone backtick", "it`s", true},
		{"a dollar", "costs $5", true},
		{"100 characters", strings.Repeat("x", 100), false},
		{"100 multibyte characters", strings.Repeat("é", 100), false},
		{"percent", "100%", false},
		{"a format verb", "50% %s %F{red}", false},
		{"quotes and a backslash", `say "hi" \o/`, false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := testHeader + "tagline = " + strconv.Quote(tc.tagline) + "\n"
			_, found := errorOf(diagnose(box.Ctx, source), "v7")
			if found != tc.bad {
				t.Errorf("V7 for %q = %v, want %v", tc.tagline, found, tc.bad)
			}
			// V7 is a header error: the persona is unusable, not just
			// quieter.
			if loads := loadsAs(t, box, source); loads == tc.bad {
				t.Errorf("Load of %q = %v, want %v", tc.tagline, loads, !tc.bad)
			}
		})
	}
}

// V9 (plan S5): a lang that does not parse is an error, and so is a colour
// that is neither #RRGGBB nor 0 to 255, whatever its TOML type.
func TestBadLanguageIsRefused(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	for _, lang := range []string{"not a language", "e", "!!", "en--US", "toolongsubtag1", "12345", "en-"} {
		t.Run(fmt.Sprintf("%q", lang), func(t *testing.T) {
			source := testHeader + "lang = " + strconv.Quote(lang) + "\n"
			d, found := errorOf(diagnose(box.Ctx, source), "v9")
			if !found {
				t.Fatalf("no V9 error: %+v", diagnose(box.Ctx, source))
			}
			if said := msg.Speak(msg.Neutral{}, d.Text, nil); !strings.Contains(said, "lang") {
				t.Errorf("V9 does not say it is about lang: %q", said)
			}
			if loadsAs(t, box, source) {
				t.Error("a persona with this lang loads")
			}
		})
	}
	for _, lang := range []string{"en", "pt", "pt-BR", "pl", "zh-Hant"} {
		t.Run("fine/"+lang, func(t *testing.T) {
			source := testHeader + "lang = " + strconv.Quote(lang) + "\n"
			if _, found := errorOf(diagnose(box.Ctx, source), "v9"); found {
				t.Errorf("V9 for %q", lang)
			}
			if !loadsAs(t, box, source) {
				t.Errorf("lang %q does not load", lang)
			}
		})
	}
}

func TestBadColoursAreRefused(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	positions := []struct{ token, side string }{
		{"accent", "light"}, {"accent", "dark"}, {"ok", "light"}, {"ok", "dark"}, {"muted", "light"}, {"muted", "dark"},
	}
	bad := map[string]string{
		"a short hex":             `"#FFF"`,
		"a long hex":              `"#1234567"`,
		"a hex with no digits":    `"#GGGGGG"`,
		"a hex with no hash":      `"ffffff"`,
		"a name":                  `"red"`,
		"an index past the end":   `256`,
		"a negative index":        `-1`,
		"a far index":             `999`,
		"an index as a string":    `"256"`,
		"a padded index":          `"007"`,
		"a decimal":               `"1.5"`,
		"a hex index":             `"0x10"`,
		"a boolean":               `true`,
		"a float":                 `1.5`,
		"an array":                `[1, 2]`,
		"a date":                  `2026-10-15`,
		"a table":                 `{ r = 1 }`,
		"spaces around an index":  `" 12"`,
		"a hex with a bad digit":  `"#12345g"`,
		"a hex with a long digit": `"#12345678"`,
	}
	for name, value := range bad {
		t.Run(name, func(t *testing.T) {
			for _, p := range positions {
				source := testHeader + "[colors." + p.token + "]\n" + p.side + " = " + value + "\n"
				d, found := errorOf(diagnose(box.Ctx, source), "v9")
				if !found {
					t.Fatalf("no V9 for %s.%s = %s: %+v", p.token, p.side, value, diagnose(box.Ctx, source))
				}
				if said := msg.Speak(msg.Neutral{}, d.Text, nil); !strings.Contains(said, "colors."+p.token+"."+p.side) {
					t.Errorf("V9 does not say where: %q", said)
				}
				if loadsAs(t, box, source) {
					t.Errorf("a persona with %s.%s = %s loads", p.token, p.side, value)
				}
			}
		})
	}
	for name, value := range map[string]string{
		"a hex": `"#1a2b3c"`, "an upper-case hex": `"#1A2B3C"`, "index 0": `0`, "index 255": `255`, "an index": `17`,
		"an index as a string": `"16"`,
	} {
		t.Run("fine/"+name, func(t *testing.T) {
			source := testHeader + "[colors.accent]\nlight = " + value + "\ndark = " + value + "\n"
			if _, found := errorOf(diagnose(box.Ctx, source), "v9"); found {
				t.Errorf("V9 for %s: %+v", value, diagnose(box.Ctx, source))
			}
			if !loadsAs(t, box, source) {
				t.Errorf("a persona with %s does not load", value)
			}
		})
	}
}
