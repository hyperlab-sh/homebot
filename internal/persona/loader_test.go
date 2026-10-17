package persona

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/msg/catalog"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func writePersona(t *testing.T, dir, id, body string) {
	t.Helper()
	path := filepath.Join(dir, "personas", id+".toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func hasDiagnostic(ds []Diagnostic, code string) bool {
	for _, d := range ds {
		if d.Text.ID() == "persona.diag."+code {
			return true
		}
	}
	return false
}

const testHeader = "schema = 1\nname = 'Tester'\nemoji = '🧪'\n"

func TestParseAndValidateHeaderDiagnostics(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	cases := []struct{ name, input, code string }{
		{"toml", "schema = [\n", "v1"},
		{"unknown", testHeader + "surprise = true\n", "v2"},
		{"schema", "name = 'Tester'\nemoji = '🧪'\n", "v3"},
		{"schema type", "schema = 'one'\nname = 'Tester'\nemoji = '🧪'\n", "v3"},
		{"id", testHeader + "id = 'other'\n", "v4"},
		{"name", "schema = 1\nemoji = '🧪'\n", "v5"},
		{"emoji", "schema = 1\nname = 'Tester'\nemoji = '1️⃣'\n", "v6"},
		{"tagline", testHeader + "tagline = '$(touch pwned)'\n", "v7"},
		{"extends", testHeader + "extends = 'does-not-exist'\n", "v9"},
		{"unsafe name", "schema = 1\nname = \"Test\\u009b\"\nemoji = '🧪'\n", "v0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, ds := Parse("tester.toml", []byte(tc.input))
			if f != nil {
				ds = append(ds, Validate(ctx, f, ValidateOptions{})...)
			}
			if !hasDiagnostic(ds, tc.code) {
				t.Fatalf("expected %s, got %+v", tc.code, ds)
			}
		})
	}
}

// A [colors] that is not a table of tokens, each a table of light and dark
// values, is never silently ignored (plan S3, V9), and no shape of it panics
// or leaves a half-read persona usable.
func TestMalformedColorsTableIsRefused(t *testing.T) {
	cases := []struct{ name, colors, code, detail string }{
		{"a scalar", "colors = 5\n", "v9", "colors"},
		{"a string", "colors = 'red'\n", "v9", "colors"},
		{"an array", "colors = [1, 2]\n", "v9", "colors"},
		{"an array of tables", "[[colors]]\naccent = 1\n", "v9", "colors"},
		{"a token that is a string", "[colors]\naccent = 'x'\n", "v9", "colors.accent"},
		{"a token that is a number", "[colors]\nok = 5\n", "v9", "colors.ok"},
		{"a token that is an array", "[colors]\nmuted = [1]\n", "v9", "colors.muted"},
		{"a side that is a boolean", "[colors.accent]\nlight = true\n", "v9", "colors.accent.light"},
		{"a side that is a float", "[colors.ok]\ndark = 1.5\n", "v9", "colors.ok.dark"},
		{"a side that is a table", "[colors.accent.light]\nr = 1\n", "v9", "colors.accent.light"},
		{"an unknown token", "[colors]\nbrand = { light = 1 }\n", "v2", "colors.brand"},
		{"an unknown token that is a scalar", "[colors]\nbrand = 1\n", "v2", "colors.brand"},
		{"an unknown side", "[colors.accent]\nmiddle = 1\n", "v2", "colors.accent.middle"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, ds := Parse("tester.toml", []byte(testHeader+tc.colors))
			if f == nil {
				t.Fatalf("no file: %+v", ds)
			}
			want := "persona.diag." + tc.code
			var found bool
			for _, d := range ds {
				found = found || d.Text.ID() == want && d.Severity == Error
			}
			if !found {
				t.Fatalf("no %s in %+v", want, ds)
			}
			if got := len(ds); got != 1 {
				t.Errorf("%d diagnostics, want exactly the one: %+v", got, ds)
			}
			box := testutil.Sandbox(t, testutil.Options{})
			writePersona(t, box.Ctx.ConfigDir, "tester", testHeader+tc.colors)
			v, ds, err := Load(box.Ctx, "tester")
			if err == nil || v != nil || len(headerErrors(ds)) == 0 {
				t.Errorf("Load = %v, %+v, %v: a malformed [colors] left the persona usable", v, ds, err)
			}
		})
	}
	t.Run("well formed", func(t *testing.T) {
		_, ds := Parse("tester.toml", []byte(testHeader+"[colors]\naccent = { light = '#112233', dark = 12 }\n[colors.ok]\nlight = 3\n"))
		if len(ds) != 0 {
			t.Errorf("diagnostics for a good [colors]: %+v", ds)
		}
	})
}

func TestLoadChainAndVariantFallback(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	writePersona(t, box.Ctx.ConfigDir, "parent", testHeader+"[lines]\n'save.error.refused' = ['Parent {reason}']\n")
	writePersona(t, box.Ctx.ConfigDir, "tester", testHeader+"extends = 'parent'\n[lines]\n'save.error.refused' = ['Child {reason}', 'Broken {missing}', 'Another {reason}']\n")
	v, ds, err := Load(box.Ctx, "tester")
	if err != nil || !hasDiagnostic(ds, "v16") {
		t.Fatalf("Load: %v, diagnostics %+v", err, ds)
	}
	lines := v.Lines["save.error.refused"]
	if len(lines) != 2 || lines[0].Text != "Child {reason}" || lines[1].Text != "Another {reason}" {
		t.Fatalf("variants = %+v", lines)
	}
	if v.Name != "Tester" {
		t.Errorf("name = %q", v.Name)
	}
	writePersona(t, box.Ctx.ConfigDir, "tester", testHeader+"extends = 'parent'\n")
	v, _, err = Load(box.Ctx, "tester")
	if err != nil || len(v.Lines["save.error.refused"]) != 1 || v.Lines["save.error.refused"][0].Text != "Parent {reason}" {
		t.Fatalf("parent fallback: %+v, %v", v, err)
	}
}

func TestLoadCycleDepthAndShadow(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	for i := 1; i <= 5; i++ {
		next := "none"
		if i < 5 {
			next = fmt.Sprintf("node%d", i+1)
		}
		writePersona(t, box.Ctx.ConfigDir, fmt.Sprintf("node%d", i), testHeader+fmt.Sprintf("extends = '%s'\n", next))
	}
	if _, ds, err := Load(box.Ctx, "node1"); err == nil || !hasDiagnostic(ds, "v9_depth") {
		t.Fatalf("depth 5 accepted: %v, %+v", err, ds)
	}
	if _, _, err := Load(box.Ctx, "node2"); err != nil {
		t.Fatalf("depth 4 refused: %v", err)
	}
	writePersona(t, box.Ctx.ConfigDir, "node2", testHeader+"extends = 'node1'\n")
	if _, ds, err := Load(box.Ctx, "node1"); err == nil || !hasDiagnostic(ds, "v9_cycle") {
		t.Fatalf("cycle accepted: %v, %+v", err, ds)
	}
	writePersona(t, box.Ctx.ConfigDir, Default, "schema = 1\nname = ''\nemoji = '🧪'\n")
	if _, _, err := Load(box.Ctx, Default); err == nil {
		t.Fatal("broken user neatpik accepted")
	}
	if got := Active(box.Ctx.Fork(map[string]string{"HOMEBOT_PERSONA": Default})); got == nil || got.ID != Default || got.Name == "" {
		t.Fatalf("embedded fallback = %+v", got)
	}
}

func TestActiveSelectionAndWarningOnce(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	writePersona(t, box.Ctx.ConfigDir, "tester", testHeader)
	ctx := box.Ctx.Fork(map[string]string{"HOMEBOT_PERSONA": ""})
	if got := Active(ctx); got == nil || got.ID != Default {
		t.Fatalf("unconfigured active = %+v", got)
	}
	ctx.Config = &config.Config{Persona: "tester"}
	if got := Active(ctx); got == nil || got.ID != "tester" {
		t.Fatalf("config after first resolution = %+v", got)
	}
	if p := ActiveProblem(ctx); p != nil {
		t.Fatalf("unexpected config problem: %+v", p)
	}
	env := ctx.Fork(map[string]string{"HOMEBOT_PERSONA": Default})
	if got := Active(env); got == nil || got.ID != Default {
		t.Fatalf("env did not outrank config: %+v", got)
	}
	off := env.Fork(map[string]string{"HOMEBOT_PERSONA": None})
	if got := Active(off); got != nil {
		t.Fatalf("none = %+v", got)
	}
	bad := env.Fork(map[string]string{"HOMEBOT_PERSONA": "../x"})
	if got := Active(bad); got == nil || got.ID != Default {
		t.Fatalf("bad id fallback = %+v", got)
	}
	if p := ActiveProblem(bad); p == nil || p.ID != "../x" || p.Origin != FromEnv {
		t.Fatalf("bad id problem = %+v", p)
	}
	if warning, ok := TakeWarning(bad); !ok || warning.ID() != "persona.warning.unusable" {
		t.Fatalf("first warning = %v, %v", warning.ID(), ok)
	}
	if _, ok := TakeWarning(bad.Fork(nil)); ok {
		t.Fatal("warning emitted after Fork")
	}
}

func TestListIncludesValidationDiagnostics(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	writePersona(t, box.Ctx.ConfigDir, "tester", testHeader+"[lines]\n'not.a.catalog.id' = ['one']\n")
	files, diagnostics, err := List(box.Ctx)
	if err != nil || len(files) != len(Builtins())+1 || !hasDiagnostic(diagnostics["tester"], "v11") {
		t.Fatalf("List = %v, %v, %v", files, diagnostics, err)
	}
}

func TestUnsafeFieldsAndLineKey(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	unsafe := []string{
		"\x00", "\x1b", "\x7f", "\u009b", "\u200b", "\u200d", "\n", "\r", "\t",
		"\u200e", "\u202e", "\u2067", "\ufeff", "\ufe0f",
		`\e`, `\E`, `\033`, `\0033`, `\x1b`, `\u001b`, `\U0000001b`, `\c[`, `^[`,
	}
	for _, sample := range unsafe {
		for _, field := range []string{"name", "emoji", "tagline", "extends", "lang", "color", "key", "variant"} {
			if field == "variant" && sample == "\n" {
				continue // Variant newlines are checked against English by V24.
			}
			if field == "emoji" && sample == "\ufe0f" {
				continue // VS16 directly after the emoji base is allowed.
			}
			t.Run(fmt.Sprintf("%q/%s", sample, field), func(t *testing.T) {
				f := &File{ID: "tester", Name: "Tester", Emoji: "🧪", Lines: map[string][]string{}, schemaSet: true, schema: 1}
				switch field {
				case "name":
					f.Name += sample
				case "emoji":
					f.Emoji += sample
				case "tagline":
					f.Tagline = sample
				case "extends":
					f.Extends = sample
				case "lang":
					f.Lang = sample
				case "color":
					f.Colors.Accent.Light = sample
				case "key":
					f.Lines["save.error.refused"+sample] = []string{"Hello {reason}"}
				case "variant":
					f.Lines["save.error.refused"] = []string{"Hello {reason}" + sample}
				}
				if ds := Validate(ctx, f, ValidateOptions{}); !hasDiagnostic(ds, "v0") {
					t.Fatalf("missing V0 for %q in %s: %+v", sample, field, ds)
				}
			})
		}
	}
}

func TestEmojiExceptionsAndRefusals(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	for _, emoji := range []string{"👨‍👩‍👧", "🏳️‍🌈", "🇧🇷", "☀️"} {
		t.Run("valid/"+emoji, func(t *testing.T) {
			f := &File{ID: "tester", Name: "Tester", Emoji: emoji, schemaSet: true, schema: 1}
			ds := Validate(ctx, f, ValidateOptions{})
			if hasDiagnostic(ds, "v0") || hasDiagnostic(ds, "v6") {
				t.Fatalf("valid emoji %q: %+v", emoji, ds)
			}
		})
	}
	for _, emoji := range []string{"\u200d🧪", "🧪\u200d", "🧪\u200d\u200d🧪", "a\u200d🧪", "\ufe0f🧪", "a\ufe0f", "1️⃣", "🍋🚲"} {
		t.Run("invalid/"+emoji, func(t *testing.T) {
			f := &File{ID: "tester", Name: "Tester", Emoji: emoji, schemaSet: true, schema: 1}
			if ds := Validate(ctx, f, ValidateOptions{}); !hasDiagnostic(ds, "v6") {
				t.Fatalf("accepted invalid emoji %q: %+v", emoji, ds)
			}
		})
	}
}

func TestLineDiagnostics(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	cases := []struct {
		name, id string
		lines    []string
		code     string
	}{
		{"unknown", "missing.message", []string{"hello"}, "v11"},
		{"unvoiced", "cli.version", []string{"{version}"}, "v12"},
		{"empty", "save.error.refused", nil, "v13"},
		{"syntax", "save.error.refused", []string{"Bad {"}, "v14"},
		{"missing placeholder", "save.error.refused", []string{"Bad"}, "v15"},
		{"unknown placeholder", "save.error.refused", []string{"Bad {reason} {other}"}, "v16"},
		{"bad selector type", "save.error.refused", []string{"Bad {reason:# one|# many}"}, "v17"},
		{"duplicate", "save.error.refused", []string{"Bad {reason}", "Bad {reason}"}, "w4"},
		{"overlong", "save.error.refused", []string{strings.Repeat("x", 300) + " {reason}"}, "v26"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &File{ID: "tester", Name: "Tester", Emoji: "🧪", schemaSet: true, schema: 1,
				Lines: map[string][]string{tc.id: tc.lines}}
			if ds := Validate(ctx, f, ValidateOptions{}); !hasDiagnostic(ds, tc.code) {
				t.Fatalf("expected %s: %+v", tc.code, ds)
			}
		})
	}
}

// TestCountShownIsNotHidden: V18 raises only when the variant uses the
// placeholder and none of its references shows the count, bare or with a '#'.
// "{n} {n:thing|things}" shows it in the bare {n}; the selector picks the noun.
func TestCountShownIsNotHidden(t *testing.T) {
	cases := []struct {
		name, english, line string
		hidden              bool
	}{
		{"bare English, bare and a selector", "{n} things", "{n} {n:thing|things}", false},
		{"selector English, bare and a selector", "{n:# thing|# things}", "{n} {n:thing|things}", false},
		{"bare English, selector with #", "{n} things", "{n:# thing|# things}", false},
		{"selector first, bare after", "{n} things", "{n:thing|things}: {n}", false},
		{"two selectors, one with #", "{n:# thing|# things}", "{n:no|some} {n:# thing|# things}", false},
		{"bare English, selector alone", "{n} things", "{n:thing|things}", true},
		{"selector English, selector alone", "{n:# thing|# things}", "{n:thing|things}", true},
		{"two selectors, none with #", "{n} things", "{n:no|some} {n:thing|things}", true},
		{"count not used at all", "{n} things", "things", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ds []Diagnostic
			meta := msg.Meta{ID: "fixture", Text: tc.english, Slots: []msg.Slot{{Name: "n", Type: catalog.Int}}}
			validateLayout(meta, tc.line, 1, &ds)
			if got := hasDiagnostic(ds, "v18"); got != tc.hidden {
				t.Fatalf("v18 = %v, want %v for %q as a variant of %q: %+v", got, tc.hidden, tc.line, tc.english, ds)
			}
		})
	}
}

func TestLayoutDiagnostics(t *testing.T) {
	cases := []struct{ name, english, line, append, code string }{
		{"whitespace", "x", " x", "", "v19"},
		{"position", "{path}: failed", "failed {path}", "", "v20"},
		{"punctuation", ", {n} blocked", "{n} blocked", "", "v21"},
		{"append", "try:", "try.", " command", "v22"},
		{"adjacent", "{left}{right}", "{left} {right}", "", "v23"},
		{"newline", "first\nsecond", "first second", "", "v24"},
		{"long", "short", strings.Repeat("x", 225), "", "v26"},
		{"length warning", "short", strings.Repeat("x", 86), "", "w5"}, // the cap is max(3 × 5, 5 + 80) = 85
		{"fact warning", "hello", "hello --danger", "", "w7"},
		{"hidden plural count", "{n:# thing|# things}", "{n:thing|things}", "", "v18"},
		{"hidden bare count", "{n} things", "{n:thing|things}", "", "v18"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ds []Diagnostic
			meta := msg.Meta{ID: "fixture", Text: tc.english, Append: tc.append,
				Slots: []msg.Slot{{Name: "n", Type: catalog.Int}}}
			validateLayout(meta, tc.line, 1, &ds)
			if !hasDiagnostic(ds, tc.code) {
				t.Fatalf("expected %s: %+v", tc.code, ds)
			}
		})
	}
}

func TestHeaderWarnings(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	f := &File{ID: "tester", Name: "Tester", Emoji: "☀️", Lang: "pl", schemaSet: true, schema: 1}
	f.Colors.Accent.Light = "#FFFFFF"
	ds := Validate(ctx, f, ValidateOptions{})
	for _, code := range []string{"w1", "w2", "w6"} {
		if !hasDiagnostic(ds, code) {
			t.Errorf("missing %s: %+v", code, ds)
		}
	}
}

func TestLineBreakRules(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	f := &File{ID: "tester", Name: "Tester", Emoji: "🧪", schemaSet: true, schema: 1,
		Lines: map[string][]string{"action.link.error.layout": {"{action} {n:a rule|# rules}:\n  {reasons}"}}}
	ds := Validate(ctx, f, ValidateOptions{})
	if hasDiagnostic(ds, "v0") || hasDiagnostic(ds, "v24") {
		t.Fatalf("matching newline refused: %+v", ds)
	}
	f.Lines["action.link.error.layout"] = []string{"{action} {n:a rule|# rules}: {reasons}"}
	if ds := Validate(ctx, f, ValidateOptions{}); !hasDiagnostic(ds, "v24") {
		t.Fatalf("mismatched newline accepted: %+v", ds)
	}
	f.Lines["action.link.error.layout"] = []string{"{action} {n:a rule|# rules}:\r  {reasons}"}
	if ds := Validate(ctx, f, ValidateOptions{}); !hasDiagnostic(ds, "v0") {
		t.Fatalf("CR accepted: %+v", ds)
	}
}

func TestUnsafeLineKeyWarnsAndDrops(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	unsafeID := "save.error.refused\u202e"
	f := &File{ID: "tester", Name: "Tester", Emoji: "🧪", schemaSet: true, schema: 1,
		Lines: map[string][]string{unsafeID: {"hello {reason}"}}}
	ds := Validate(ctx, f, ValidateOptions{})
	if !hasDiagnostic(ds, "v0") || len(ds) == 0 || ds[0].Severity != Warning {
		t.Fatalf("unsafe key diagnostics = %+v", ds)
	}
	if got := validVariants(ctx, f); len(got) != 0 {
		t.Fatalf("unsafe key loaded: %+v", got)
	}
}

func TestSupplierLanguageAndPluralFallback(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	writePersona(t, box.Ctx.ConfigDir, "tester", testHeader+"lang = 'pl'\nextends = 'none'\n[lines]\n'action.link.status.changed' = ['{action} {n:one=# jedna|few=# kilka|many=# wielu|other=# inne} changed']\n")
	v, ds, err := Load(box.Ctx, "tester")
	if err != nil || len(v.Lines["action.link.status.changed"]) != 1 {
		t.Fatalf("Load Polish line: %+v, %+v, %v", v, ds, err)
	}
	line := v.Lines["action.link.status.changed"][0]
	if line.Lang != language.Polish {
		t.Fatalf("supplier language = %s", line.Lang)
	}
	tmpl, err := catalog.Parse(line.Text)
	if err != nil {
		t.Fatal(err)
	}
	if got := tmpl.Format(line.Lang, map[string]any{"n": 2, "action": "link:"}, nil); got != "link: 2 kilka changed" {
		t.Fatalf("Polish few = %q", got)
	}
	if got := tmpl.Format(line.Lang, map[string]any{"n": 5, "action": "link:"}, nil); got != "link: 5 wielu changed" {
		t.Fatalf("Polish many = %q", got)
	}
	writePersona(t, box.Ctx.ConfigDir, "tester", testHeader+"lang = 'pl'\n[lines]\n'action.link.status.changed' = ['{action} {n:one=# jedna|other=# inne} changed']\n")
	v, _, err = Load(box.Ctx, "tester")
	if err != nil {
		t.Fatal(err)
	}
	tmpl, _ = catalog.Parse(v.Lines["action.link.status.changed"][0].Text)
	if got := tmpl.Format(language.Polish, map[string]any{"n": 2, "action": "link:"}, nil); got != "link: 2 inne changed" {
		t.Fatalf("missing few should use other, got %q", got)
	}
}

func TestBuiltinsValid(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	ids := Builtins()
	if !slices.Contains(ids, Default) {
		t.Fatalf("builtins = %v", ids)
	}
	for _, id := range ids {
		f, parsed, err := readBuiltin(id)
		if err != nil || len(parsed) != 0 {
			t.Fatalf("%s: %v, %+v", id, err, parsed)
		}
		if ds := Validate(ctx, f, ValidateOptions{}); len(ds) != 0 {
			t.Errorf("%s: %+v", id, ds)
		}
	}
}
