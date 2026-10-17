package persona

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// brokenNeatpik is a user personas/neatpik.toml with a header error (V5).
const brokenNeatpik = "schema = 1\nname = ''\nemoji = '🧪'\n"

// embeddedNeatpikLines are the lines of the embedded neatpik as a Voice holds
// them. Every persona inherits them (plan S4: lines merge per id down the
// chain, ending in the embedded neatpik), so a test that wants to know what a
// persona added or leaked compares against them rather than against nothing.
func embeddedNeatpikLines(t *testing.T) map[string][]Line {
	t.Helper()
	f, ds, err := readBuiltin(Default)
	if err != nil || f == nil || len(ds) != 0 {
		t.Fatalf("embedded neatpik: %v %+v", err, ds)
	}
	if len(f.Lines) == 0 {
		t.Fatal("the embedded neatpik has no lines; these tests rely on it having some")
	}
	out := make(map[string][]Line, len(f.Lines))
	for id, variants := range f.Lines {
		for _, text := range variants {
			out[id] = append(out[id], Line{Text: text, Lang: language.English})
		}
	}
	return out
}

// asking is a Context that resolves the persona the way HOMEBOT_PERSONA=id
// does, with no repo config.
func asking(box *testutil.Box, id string) *app.Context {
	ctx := box.Ctx.Fork(map[string]string{"HOMEBOT_PERSONA": id})
	ctx.Config = nil
	return ctx
}

// headerErrors are the diagnostics that make a persona unusable.
func headerErrors(ds []Diagnostic) []Diagnostic {
	var out []Diagnostic
	for _, d := range ds {
		if d.Severity == Error && d.LineID == "" && !d.inLines {
			out = append(out, d)
		}
	}
	return out
}

// The implicit base of every persona is the embedded neatpik (plan S4: "the
// fallback target is always the embedded neatpik"). A broken user
// personas/neatpik.toml makes neatpik unusable as a choice; it must not take
// every other persona down with it.
func TestBrokenUserNeatpikDoesNotPoisonOtherPersonas(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	dir := box.Ctx.ConfigDir
	writePersona(t, dir, Default, brokenNeatpik)
	writePersona(t, dir, "tester", testHeader+"[lines]\n'save.error.refused' = ['Tester says {reason}']\n")
	writePersona(t, dir, "spelled", testHeader+"extends = 'neatpik'\n")
	writePersona(t, dir, "plain", testHeader+"extends = 'none'\n")

	for _, id := range []string{"tester", "spelled", "plain"} {
		t.Run(id, func(t *testing.T) {
			v, ds, err := Load(box.Ctx, id)
			if err != nil || v == nil {
				t.Fatalf("Load(%s) = %v, %+v, %v: a broken neatpik.toml poisoned an unrelated persona", id, v, ds, err)
			}
			if got := headerErrors(ds); len(got) != 0 {
				t.Errorf("header errors from a file this persona does not use: %+v", got)
			}
			if v.ID != id || v.Name != "Tester" {
				t.Errorf("voice = %q %q", v.ID, v.Name)
			}
			// The base is the embedded neatpik's, whose colours are set,
			// unless the persona stopped the chain (plan S3).
			inherited := v.Colors.Accent.Light == "#31466B"
			if inherited == (id == "plain") {
				t.Errorf("colours = %+v: inherited from the embedded neatpik = %v", v.Colors, inherited)
			}
		})
	}

	ctx := asking(box, "tester")
	if got := Active(ctx); got == nil || got.ID != "tester" {
		t.Fatalf("Active = %+v, want tester", got)
	}
	if p := ActiveProblem(ctx); p != nil {
		t.Errorf("ActiveProblem = %+v for a persona that loads", p)
	}
	if _, ok := TakeWarning(ctx); ok {
		t.Error("a warning for a persona that loads")
	}
}

// ... but neatpik as a choice is unusable, and the fallback still speaks
// with the embedded one (plan S4, m11).
func TestBrokenUserNeatpikIsUnusableAsAChoice(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	writePersona(t, box.Ctx.ConfigDir, Default, brokenNeatpik)
	if v, ds, err := Load(box.Ctx, Default); err == nil || v != nil || len(headerErrors(ds)) == 0 {
		t.Fatalf("Load(neatpik) = %v, %+v, %v: the broken shadow was accepted", v, ds, err)
	}
	ctx := asking(box, Default)
	got := Active(ctx)
	if got == nil || got.ID != Default || got.Name != "Neatpik" {
		t.Fatalf("fallback = %+v, want the embedded Neatpik", got)
	}
	if p := ActiveProblem(ctx); p == nil || p.Origin != FromEnv || p.ID != Default {
		t.Errorf("ActiveProblem = %+v", p)
	}
	if _, ok := TakeWarning(ctx); !ok {
		t.Error("no warning for an unusable HOMEBOT_PERSONA")
	}
}

// extends = "neatpik" is the default (plan S3), so it is the embedded neatpik
// whether or not it is written, and a user's neatpik.toml, broken or not,
// is never what a persona extends. It is a choice, not a parent.
func TestExplicitExtendsOfNeatpikIsTheEmbeddedOne(t *testing.T) {
	for name, body := range map[string]string{
		"broken": brokenNeatpik,
		// Two distinctive lines: one for an id the embedded neatpik has no
		// line for, one for an id it does, so a leak shows either way.
		"valid": testHeader + "[lines]\n'save.error.refused' = ['Mine {reason}']\n'inbox.header.clear' = ['Mine, all clear']\n",
	} {
		t.Run(name, func(t *testing.T) {
			box := testutil.Sandbox(t, testutil.Options{})
			dir := box.Ctx.ConfigDir
			writePersona(t, dir, Default, body)
			writePersona(t, dir, "tester", testHeader+"extends = 'neatpik'\n")
			writePersona(t, dir, "silent", testHeader)

			v, ds, err := Load(box.Ctx, "tester")
			if err != nil || v == nil || len(headerErrors(ds)) != 0 {
				t.Fatalf("Load(tester) = %v, %+v, %v", v, ds, err)
			}
			// Lines inherit down the chain, so tester has exactly the embedded
			// neatpik's lines (it has none of its own): none of the user
			// neatpik.toml's lines, and none of the embedded ones missing.
			if !reflect.DeepEqual(v.Lines, embeddedNeatpikLines(t)) || v.Colors.Accent.Light != "#31466B" {
				t.Errorf("tester took the user's neatpik.toml, not the embedded one: %+v", v)
			}
			for id, lines := range v.Lines {
				for _, l := range lines {
					if strings.HasPrefix(l.Text, "Mine") {
						t.Errorf("the user's neatpik line leaked into tester: %s = %q", id, l.Text)
					}
				}
			}
			// The validator says the same: the parent exists.
			f, _, _ := Read(box.Ctx, "tester")
			if hasDiagnostic(Validate(box.Ctx, f, ValidateOptions{}), "v9") {
				t.Error("V9 for extends = 'neatpik'")
			}
			// ... and spelling it out changes nothing against saying nothing.
			silent, _, err := Load(box.Ctx, "silent")
			if err != nil || silent.Colors != v.Colors || !reflect.DeepEqual(silent.Lines, v.Lines) {
				t.Errorf("silent = %+v, %v differs from tester = %+v", silent, err, v)
			}
		})
	}
}

// A user's neatpik.toml may name the default itself. That is the embedded
// base, not a file extending itself (V9) or a cycle.
func TestAUserNeatpikMayExtendNeatpik(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	writePersona(t, box.Ctx.ConfigDir, Default, testHeader+"extends = 'neatpik'\n")
	v, ds, err := Load(box.Ctx, Default)
	if err != nil || v == nil || v.Name != "Tester" || v.Colors.Accent.Light != "#31466B" {
		t.Fatalf("Load(neatpik) = %+v, %+v, %v", v, ds, err)
	}
	f, _, _ := Read(box.Ctx, Default)
	if got := Validate(box.Ctx, f, ValidateOptions{}); hasDiagnostic(got, "v9") {
		t.Errorf("V9 for a neatpik.toml that extends neatpik: %+v", got)
	}
}

// extends = "none" stops the chain (plan S3): the embedded neatpik supplies
// neither colours nor lines, and the catalog's English says what the persona
// does not. Without it, the embedded neatpik is under every persona.
func TestExtendsNoneStopsTheChain(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	dir := box.Ctx.ConfigDir
	lines := "[lines]\n'save.error.refused' = ['Tester says {reason}']\n"
	colors := "[colors]\naccent = { light = '#112233' }\n"
	writePersona(t, dir, "stops", testHeader+"extends = 'none'\n"+colors+lines)
	writePersona(t, dir, "inherits", testHeader+colors+lines)
	writePersona(t, dir, "child", testHeader+"extends = 'stops'\n")

	stops, ds, err := Load(box.Ctx, "stops")
	if err != nil || stops == nil || len(headerErrors(ds)) != 0 {
		t.Fatalf("Load(stops) = %v, %+v, %v", stops, ds, err)
	}
	if stops.Colors.Accent.Light != "#112233" || stops.Colors.Accent.Dark != "" || stops.Colors.OK != (Pair{}) || stops.Colors.Muted != (Pair{}) {
		t.Errorf("stops.Colors = %+v, want only its own accent.light", stops.Colors)
	}
	inherits, _, err := Load(box.Ctx, "inherits")
	if err != nil || inherits.Colors.Accent.Dark != "#AFC4E9" || inherits.Colors.OK.Light != "#245D38" {
		t.Errorf("inherits.Colors = %+v, %v, want the embedded neatpik under its own accent.light", inherits.Colors, err)
	}
	// A child of a persona that stops the chain stops with it: it has its parent's colours and nothing of the embedded neatpik's.
	child, _, err := Load(box.Ctx, "child")
	if err != nil || child.Colors != stops.Colors {
		t.Errorf("child.Colors = %+v, %v: the embedded neatpik came back under a parent that stopped the chain", child.Colors, err)
	}
}

// Whatever breaks a file in the chain, the diagnostic names that file, not
// the persona that was asked for.
func TestChainFailuresNameTheirFile(t *testing.T) {
	cases := []struct{ name, parent, code string }{
		{"header", "schema = 1\nname = ''\nemoji = '🧪'\n", "v5"},
		{"syntax", "schema = [\n", "v1"},
		{"unknown key", testHeader + "surprise = true\n", "v2"},
		{"emoji", "schema = 1\nname = 'Parent'\nemoji = 'x'\n", "v6"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			box := testutil.Sandbox(t, testutil.Options{})
			dir := box.Ctx.ConfigDir
			writePersona(t, dir, "parent", tc.parent)
			writePersona(t, dir, "child", testHeader+"extends = 'parent'\n")
			file := dir + "/personas/parent.toml"
			v, ds, err := Load(box.Ctx, "child")
			if err == nil || v != nil {
				t.Fatalf("Load(child) = %v, %+v, %v", v, ds, err)
			}
			if !strings.Contains(err.Error(), file) {
				t.Errorf("error %q does not name %s", err, file)
			}
			var found bool
			for _, d := range headerErrors(ds) {
				if d.Path != file {
					t.Errorf("%s has Path %q, want %s", d.Text.ID(), d.Path, file)
				}
				found = found || d.Text.ID() == "persona.diag."+tc.code
			}
			if !found {
				t.Errorf("no %s in %+v", tc.code, ds)
			}
		})
	}
}

// Nothing configured, so a broken neatpik.toml is not a config problem: the
// inbox turns FromConfig into a persona_broken item (plan S4), and nobody
// asked for a persona in homebot.yml.
func TestNothingConfiguredIsNotAConfigProblem(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	writePersona(t, box.Ctx.ConfigDir, Default, brokenNeatpik)

	for name, cfg := range map[string]*config.Config{
		"no repo":           nil,
		"a repo, no choice": {},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := asking(box, "")
			ctx.Config = cfg
			got := Active(ctx)
			if got == nil || got.ID != Default || got.Name != "Neatpik" {
				t.Fatalf("Active = %+v, want the embedded Neatpik", got)
			}
			if !reflect.DeepEqual(got.Lines, embeddedNeatpikLines(t)) {
				t.Errorf("the fallback voice does not speak the embedded Neatpik's lines: %+v", got.Lines)
			}
			p := ActiveProblem(ctx)
			if p == nil {
				t.Fatal("the broken file is not reported at all")
			}
			if p.Origin != FromDefault {
				t.Errorf("origin = %v, want FromDefault", p.Origin)
			}
			if _, ok := TakeWarning(ctx); ok {
				t.Error("a warning about a variable nobody set")
			}
			if p.Diag.Path != box.Ctx.ConfigDir+"/personas/neatpik.toml" {
				t.Errorf("the problem names %q, not the broken file", p.Diag.Path)
			}
		})
	}

	// The same file, chosen in homebot.yml, is the user's config problem.
	ctx := asking(box, "")
	ctx.Config = &config.Config{Persona: Default}
	if p := ActiveProblem(ctx); p == nil || p.Origin != FromConfig {
		t.Errorf("persona: neatpik in homebot.yml: %+v", p)
	}
}

// Whatever makes the chosen persona unusable, the fallback speaks with the
// embedded Neatpik: its name, its colours and its lines (plan S4, m11), not
// just the name over the catalog's wording.
func TestFallbackVoiceSpeaksTheEmbeddedNeatpik(t *testing.T) {
	want := embeddedNeatpikLines(t)
	cases := map[string]struct {
		neatpik string // a user personas/neatpik.toml, or "" for none
		choose  string // HOMEBOT_PERSONA
	}{
		"a persona that does not exist":      {choose: "ghost"},
		"an id that is not an id":            {choose: "../x"},
		"a broken neatpik.toml, chosen":      {neatpik: brokenNeatpik, choose: Default},
		"a broken neatpik.toml, by default":  {neatpik: brokenNeatpik},
		"a broken neatpik.toml, other asked": {neatpik: brokenNeatpik, choose: "ghost"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			box := testutil.Sandbox(t, testutil.Options{})
			if tc.neatpik != "" {
				writePersona(t, box.Ctx.ConfigDir, Default, tc.neatpik)
			}
			ctx := asking(box, tc.choose)
			if ActiveProblem(ctx) == nil {
				t.Fatal("the persona is not reported as unusable, so this is not the fallback")
			}
			got := Active(ctx)
			if got == nil || got.ID != Default || got.Name != "Neatpik" {
				t.Fatalf("Active = %+v, want the embedded Neatpik", got)
			}
			if got.Colors.Accent.Light == "" {
				t.Errorf("the fallback has no colours: %+v", got.Colors)
			}
			if !reflect.DeepEqual(got.Lines, want) {
				t.Errorf("fallback lines = %+v, want the embedded Neatpik's", got.Lines)
			}
		})
	}
}

// A valid user neatpik.toml shadows the built-in when nothing is configured:
// neatpik is the default choice, and a user file with its id overrides it.
func TestValidUserNeatpikShadowsTheDefaultChoice(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	writePersona(t, box.Ctx.ConfigDir, Default, "schema = 1\nname = 'My tidy'\nemoji = '🧪'\n")
	ctx := asking(box, "")
	if got := Active(ctx); got == nil || got.ID != Default || got.Name != "My tidy" {
		t.Fatalf("Active = %+v, want the user's neatpik", got)
	}
	if p := ActiveProblem(ctx); p != nil {
		t.Errorf("ActiveProblem = %+v", p)
	}
}

// writeChain writes nodes node1 to nodeN, each extending the next. The last
// carries tail, the rest of its header: "" says nothing about extends, so it
// takes the implicit neatpik; "extends = 'none'\n" stops the chain.
func writeChain(t *testing.T, dir string, n int, tail string) {
	t.Helper()
	for i := 1; i <= n; i++ {
		extra := tail
		if i < n {
			extra = fmt.Sprintf("extends = 'node%d'\n", i+1)
		}
		writePersona(t, dir, fmt.Sprintf("node%d", i), testHeader+extra)
	}
}

// The extends chain is at most four levels (plan S4: "the active persona, its
// extends chain (at most 4 levels, cycles refused), neatpik, the catalog").
// neatpik is listed apart from the four, so how the last file ends cannot
// change whether the chain is accepted.
func TestExtendsLimitDoesNotCountTheImplicitNeatpik(t *testing.T) {
	for name, tail := range map[string]string{
		"ends with no extends":      "",
		"ends with extends none":    "extends = 'none'\n",
		"ends with extends neatpik": "extends = 'neatpik'\n",
	} {
		t.Run(name, func(t *testing.T) {
			box := testutil.Sandbox(t, testutil.Options{})
			dir := box.Ctx.ConfigDir
			writeChain(t, dir, 4, tail)
			v, ds, err := Load(box.Ctx, "node1")
			if err != nil || v == nil || v.ID != "node1" {
				t.Fatalf("four files refused: %v, %+v, %v", v, ds, err)
			}
			if hasDiagnostic(ds, "v9") || hasDiagnostic(ds, "v9_depth") {
				t.Errorf("V9 for a chain of four: %+v", ds)
			}

			// A fifth file is one too many, however it ends.
			writeChain(t, dir, 5, tail)
			v, ds, err = Load(box.Ctx, "node1")
			if err == nil || v != nil || !hasDiagnostic(ds, "v9_depth") {
				t.Fatalf("five files accepted: %v, %+v, %v", v, ds, err)
			}
			// The diagnostic names the file whose extends line goes too far.
			want := dir + "/personas/node4.toml"
			for _, d := range ds {
				if d.Text.ID() == "persona.diag.v9_depth" && d.Path != want {
					t.Errorf("V9 (depth) is reported for %q, want %s", d.Path, want)
				}
			}
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q does not name %s", err, want)
			}
			// ... and a chain of three and of two are fine.
			if _, _, err := Load(box.Ctx, "node3"); err != nil {
				t.Errorf("three files refused: %v", err)
			}
			if _, _, err := Load(box.Ctx, "node4"); err != nil {
				t.Errorf("two files refused: %v", err)
			}
		})
	}
}

// A user's neatpik.toml is never a file of someone else's chain, so it does
// not count toward the four either.
func TestExtendsLimitIgnoresAUserNeatpik(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	dir := box.Ctx.ConfigDir
	writePersona(t, dir, Default, testHeader)
	writeChain(t, dir, 4, "extends = 'neatpik'\n")
	if v, ds, err := Load(box.Ctx, "node1"); err != nil || v == nil {
		t.Fatalf("node1 to node4 are four files: %+v, %v", ds, err)
	}
	writeChain(t, dir, 5, "extends = 'neatpik'\n")
	if _, ds, err := Load(box.Ctx, "node1"); err == nil || !hasDiagnostic(ds, "v9_depth") {
		t.Fatalf("node1 to node5 are five files: %+v, %v", ds, err)
	}
}

func TestExtendsCyclesAreRefused(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	dir := box.Ctx.ConfigDir
	file := func(id string) string { return dir + "/personas/" + id + ".toml" }

	// Two files extending each other.
	writePersona(t, dir, "ping", testHeader+"extends = 'pong'\n")
	writePersona(t, dir, "pong", testHeader+"extends = 'ping'\n")
	// A file that extends itself.
	writePersona(t, dir, "me", testHeader+"extends = 'me'\n")
	// A cycle at the end of a long tail, so the depth limit is not what
	// stops it.
	writePersona(t, dir, "tail1", testHeader+"extends = 'ping'\n")

	// Not a cycle: extends = "neatpik" is the embedded neatpik, not the
	// user's neatpik.toml that extends the persona that extends it.
	writePersona(t, dir, Default, testHeader+"extends = 'tester'\n")
	writePersona(t, dir, "tester", testHeader+"extends = 'neatpik'\n")
	for _, id := range []string{"tester", Default} {
		if v, ds, err := Load(box.Ctx, id); err != nil || v == nil {
			t.Errorf("Load(%s) = %v, %+v, %v: the embedded neatpik is not a loop", id, v, ds, err)
		}
	}

	for _, tc := range []struct{ id, closes string }{
		{"ping", file("pong")},
		{"pong", file("ping")},
		{"me", ""},
		{"tail1", file("pong")},
	} {
		t.Run(tc.id, func(t *testing.T) {
			v, ds, err := Load(box.Ctx, tc.id)
			if err == nil || v != nil || !hasDiagnostic(ds, "v9_cycle") {
				t.Fatalf("cycle accepted: %v, %+v, %v", v, ds, err)
			}
			if tc.closes == "" {
				return
			}
			var named bool
			for _, d := range ds {
				named = named || d.Text.ID() == "persona.diag.v9_cycle" && d.Path == tc.closes
			}
			if !named {
				t.Errorf("no V9 cycle finding naming %s, the file that closes the loop: %+v", tc.closes, ds)
			}
		})
	}
}

// Parse derives a persona's id from its file name. persona.toml is the file
// name of a built-in (builtin/<id>/persona.toml, where the folder is the id)
// and must not turn a user's personas/persona.toml into the persona
// "personas".
func TestAPersonaNamedPersona(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	writePersona(t, box.Ctx.ConfigDir, "persona", testHeader+"[lines]\n'save.error.refused' = ['Persona {reason}']\n")
	f, ds, err := Read(box.Ctx, "persona")
	if err != nil || f == nil || f.ID != "persona" {
		t.Fatalf("Read = %+v, %+v, %v", f, ds, err)
	}
	if ds := Validate(box.Ctx, f, ValidateOptions{}); len(ds) != 0 {
		t.Errorf("Validate = %+v", ds)
	}
	v, ds, err := Load(box.Ctx, "persona")
	if err != nil || v == nil || v.ID != "persona" || len(v.Lines["save.error.refused"]) != 1 {
		t.Fatalf("Load = %+v, %+v, %v", v, ds, err)
	}
	// By path, as persona validate <file> will.
	if f, _, err := ReadFile(box.Ctx, box.Ctx.ConfigDir+"/personas/persona.toml"); err != nil || f.ID != "persona" {
		t.Errorf("ReadFile: %+v, %v", f, err)
	}
	// The built-in layout keeps working: the folder is the id.
	if f, _, err := readBuiltin(Default); err != nil || f.ID != Default {
		t.Errorf("readBuiltin: %+v, %v", f, err)
	}
	if f, ds := Parse("builtin/sample/persona.toml", []byte(testHeader)); f == nil || f.ID != "sample" || hasDiagnostic(ds, "v4") {
		t.Errorf("Parse of the built-in layout: %+v, %+v", f, ds)
	}
}

// A bad [lines] key drops that key; it never makes the persona unusable (plan
// S4, M9). That holds for the empty key too, whose diagnostic has no LineID
// to tell it from a header error by.
func TestLineKeyProblemsNeverMakeThePersonaUnusable(t *testing.T) {
	for name, lines := range map[string]string{
		"an empty key":                   "'' = ['hello {reason}']\n",
		"an empty key and an empty list": "'' = []\n",
		"an unknown id":                  "'not.a.message' = ['hello']\n",
		"an unvoiced id":                 "'cli.version' = ['{version}']\n",
		"an empty variant list":          "'action.link.status.changed' = []\n",
		"a key with a control code":      "\"save.error.refused\\u202e\" = ['hello {reason}']\n",
		"no entries at all":              "",
	} {
		t.Run(name, func(t *testing.T) {
			box := testutil.Sandbox(t, testutil.Options{})
			writePersona(t, box.Ctx.ConfigDir, "tester", testHeader+"[lines]\n"+lines+"'save.error.refused' = ['Kept {reason}']\n")
			v, ds, err := Load(box.Ctx, "tester")
			if err != nil || v == nil {
				t.Fatalf("Load = %v, %+v, %v: a bad line key made the persona unusable", v, ds, err)
			}
			if got := headerErrors(ds); len(got) != 0 {
				t.Errorf("header errors for a line key: %+v", got)
			}
			if got := v.Lines["save.error.refused"]; len(got) != 1 || got[0].Text != "Kept {reason}" {
				t.Errorf("the good key was lost: %+v", v.Lines)
			}
			// The embedded neatpik's ids are inherited, not bad keys: only an
			// id outside its set and the good key would be a leak.
			inherited := embeddedNeatpikLines(t)
			for id := range v.Lines {
				if _, ok := inherited[id]; id != "save.error.refused" && !ok {
					t.Errorf("a bad key loaded: %q", id)
				}
			}
			for id, want := range inherited {
				if !reflect.DeepEqual(v.Lines[id], want) {
					t.Errorf("the inherited lines of %q changed: %+v", id, v.Lines[id])
				}
			}
			if name != "no entries at all" && len(ds) == 0 {
				t.Error("the bad key is not reported")
			}
		})
	}
	t.Run("an empty [lines] table", func(t *testing.T) {
		box := testutil.Sandbox(t, testutil.Options{})
		writePersona(t, box.Ctx.ConfigDir, "tester", testHeader+"[lines]\n")
		v, ds, err := Load(box.Ctx, "tester")
		if err != nil || v == nil || len(ds) != 0 || !reflect.DeepEqual(v.Lines, embeddedNeatpikLines(t)) {
			t.Fatalf("Load = %+v, %+v, %v", v, ds, err)
		}
	})
}

// A Context with no ConfigDir (app.ForReport) has no user files at all, and
// the embedded neatpik is still there: nothing is configured, so nothing is
// broken, and no persona may be looked up relative to the working directory
// (where a cloned repo could ship one).
func TestWithoutAConfigDirOnlyTheBuiltinsExist(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	cwd := t.TempDir()
	writePersona(t, cwd, "tester", testHeader)
	writePersona(t, cwd, Default, testHeader)
	t.Chdir(cwd)

	t.Run("nothing configured", func(t *testing.T) {
		ctx := asking(box, "")
		ctx.ConfigDir = ""
		got := Active(ctx)
		if got == nil || got.ID != Default || got.Name != "Neatpik" {
			t.Fatalf("Active = %+v, want the embedded Neatpik (not the one in %s)", got, cwd)
		}
		if p := ActiveProblem(ctx); p != nil {
			t.Errorf("ActiveProblem = %+v: nothing is wrong when nothing is configured", p)
		}
		f, ds, err := Read(ctx, Default)
		if err != nil || f == nil || f.Source != Builtin {
			t.Errorf("Read(neatpik) = %+v, %+v, %v, want the built-in", f, ds, err)
		}
		files, findings, err := List(ctx)
		if _, listed := findings["tester"]; err != nil || listed || len(files) != len(Builtins()) {
			t.Errorf("List = %d files, %v, %v: it listed ./personas of %s", len(files), findings, err, cwd)
		}
	})
	t.Run("a persona that is not a built-in", func(t *testing.T) {
		ctx := asking(box, "tester")
		ctx.ConfigDir = ""
		if f, _, err := Read(ctx, "tester"); err == nil || f != nil {
			t.Errorf("Read(tester) = %+v, %v: read relative to the working directory", f, err)
		}
		if got := Active(ctx); got == nil || got.ID != Default {
			t.Fatalf("Active = %+v, want the embedded Neatpik", got)
		}
		if p := ActiveProblem(ctx); p == nil || p.Origin != FromEnv || p.ID != "tester" {
			t.Errorf("ActiveProblem = %+v", p)
		}
	})
}

// LoadFile judges a file that is not installed yet the way Load will judge it
// once it is: the whole chain, the cap of four files and cycles.
func TestLoadFileJudgesTheWholeChain(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	dir := box.Ctx.ConfigDir
	draft := func(name, body string) string {
		path := filepath.Join(t.TempDir(), name+".toml")
		if err := os.WriteFile(path, []byte("schema = 1\nid = '"+name+"'\nname = 'Draft'\nemoji = '🧪'\n"+body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	t.Run("valid", func(t *testing.T) {
		writePersona(t, dir, "base", testHeader)
		v, ds, err := LoadFile(box.Ctx, draft("fresh", "extends = 'base'\n"))
		if err != nil || v == nil || v.ID != "fresh" || headerErrors(ds) != nil {
			t.Fatalf("LoadFile = %v, %+v, %v", v, ds, err)
		}
	})
	t.Run("extends nothing installed", func(t *testing.T) {
		v, ds, err := LoadFile(box.Ctx, draft("lost", "extends = 'not-there'\n"))
		if err == nil || v != nil || headerErrors(ds) == nil {
			t.Fatalf("missing parent accepted: %v, %+v, %v", v, ds, err)
		}
	})
	t.Run("extends a broken persona", func(t *testing.T) {
		writePersona(t, dir, "wreck", brokenNeatpik)
		if v, _, err := LoadFile(box.Ctx, draft("heir", "extends = 'wreck'\n")); err == nil || v != nil {
			t.Fatalf("broken parent accepted: %v, %v", v, err)
		}
	})
	t.Run("one file too deep", func(t *testing.T) {
		writeChain(t, dir, 4, "")
		// The draft and node1 to node4 are five files.
		v, ds, err := LoadFile(box.Ctx, draft("tall", "extends = 'node1'\n"))
		if err == nil || v != nil || !hasDiagnostic(ds, "v9_depth") {
			t.Fatalf("five files accepted: %v, %+v, %v", v, ds, err)
		}
		writeChain(t, dir, 3, "")
		if _, _, err := LoadFile(box.Ctx, draft("fit", "extends = 'node1'\n")); err != nil {
			t.Fatalf("four files refused: %v", err)
		}
	})
	t.Run("closes a loop", func(t *testing.T) {
		// The installed copy of the draft's name exists, so the loop is a real
		// one and not a missing parent; the draft is what is judged.
		writePersona(t, dir, "looped", testHeader)
		writePersona(t, dir, "ping", testHeader+"extends = 'looped'\n")
		v, ds, err := LoadFile(box.Ctx, draft("looped", "extends = 'ping'\n"))
		if err == nil || v != nil || !hasDiagnostic(ds, "v9_cycle") || !strings.Contains(err.Error(), "cycle") {
			t.Fatalf("cycle accepted: %v, %+v, %v", v, ds, err)
		}
	})
	t.Run("a file that is not there", func(t *testing.T) {
		v, ds, err := LoadFile(box.Ctx, filepath.Join(t.TempDir(), "ghost.toml"))
		if err == nil || v != nil {
			t.Fatalf("missing file accepted: %v, %+v, %v", v, ds, err)
		}
	})
}

// List gives each persona the verdict Load gives it, whole chain included: a
// persona in a five-file chain, in a cycle, or under a broken parent is
// unusable there too, and a good chain is not. Judged file by file it would
// list as usable what `persona use` refuses.
func TestListJudgesTheWholeChain(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	dir := box.Ctx.ConfigDir
	writeChain(t, dir, 5, "") // node1 is the first of five files
	for id, body := range map[string]string{
		"ring-a": testHeader + "extends = 'ring-b'\n", // a cycle
		"ring-b": testHeader + "extends = 'ring-a'\n",
		"wreck":  "schema = 1\nname = 'Wreck'\n", // no emoji
		"heir":   testHeader + "extends = 'wreck'\n",
		"stray":  testHeader + "mystery = 1\n", // an unknown key
		"ward":   testHeader + "extends = 'stray'\n",
		"base":   testHeader,
		"good":   testHeader + "extends = 'base'\n",
		"lone":   testHeader + "[lines]\n'save.error.refused' = ['No']\n", // a bad line only
	} {
		writePersona(t, dir, id, body)
	}
	files, findings, err := List(box.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, f := range files {
		listed[f.ID] = true
	}
	for _, tc := range []struct {
		id     string
		reason string // the code of the error the verdict rests on; "" for a usable persona
		loads  bool   // Load accepts it: the error is a line, which load drops
	}{
		{"node1", "v9_depth", false},
		{"node2", "", true}, // four files
		{"ring-a", "v9_cycle", false},
		{"ring-b", "v9_cycle", false},
		{"wreck", "v6", false},
		{"heir", "v6", false},
		{"stray", "v2", false},
		{"ward", "v2", false},
		{"good", "", true},
		{"base", "", true},
		{"lone", "v15", true},
	} {
		t.Run(tc.id, func(t *testing.T) {
			_, loaded, loadErr := Load(box.Ctx, tc.id)
			ds := findings[tc.id]
			if len(ds) != len(loaded) {
				t.Errorf("List found %d things, Load %d:\n%+v\n%+v", len(ds), len(loaded), ds, loaded)
			}
			if (loadErr == nil) != tc.loads {
				t.Errorf("Load error = %v, want accepted = %v", loadErr, tc.loads)
			}
			var unusable bool
			for _, d := range ds {
				unusable = unusable || d.Severity == Error
			}
			if unusable != (tc.reason != "") {
				t.Errorf("List says unusable = %v, want %v: %+v", unusable, tc.reason != "", ds)
			}
			if tc.reason != "" && !hasDiagnostic(ds, tc.reason) {
				t.Errorf("no %s among %+v", tc.reason, ds)
			}
			if tc.reason == "" && len(ds) != 0 {
				t.Errorf("a good chain has findings: %+v", ds)
			}
			if !listed[tc.id] {
				t.Errorf("%s is not listed", tc.id)
			}
		})
	}
}
