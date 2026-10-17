package persona

import (
	"bytes"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// voiceSpeaker says what a Voice has and nothing else: the first variant of a
// line, in the language of the persona that supplied it. It stands in for
// internal/voice, which imports this package.
type voiceSpeaker struct{ v *Voice }

func (s voiceSpeaker) Name() string { return s.v.Name }
func (s voiceSpeaker) Line(id string) (string, language.Tag, bool) {
	lines := s.v.Lines[id]
	if len(lines) == 0 {
		return "", language.Und, false
	}
	return lines[0].Text, lines[0].Lang, true
}

// say builds id from values and has v say it.
func say(t *testing.T, v *Voice, id string, values map[string]any) string {
	t.Helper()
	text, err := msg.Build(id, values)
	if err != nil {
		t.Fatal(err)
	}
	return msg.Speak(voiceSpeaker{v}, text, nil)
}

// neutral is what the catalog says for the same build.
func neutral(t *testing.T, id string, values map[string]any) string {
	t.Helper()
	text, err := msg.Build(id, values)
	if err != nil {
		t.Fatal(err)
	}
	return msg.Speak(msg.Neutral{}, text, nil)
}

// A user file with a built-in's id shadows it: Read, List, Load and the
// selection all see the user's, the embedded copy is untouched, and a
// persona that says nothing about its base still extends the embedded one
// (plan S4).
func TestUserFileShadowsAValidBuiltin(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	dir := box.Ctx.ConfigDir
	writePersona(t, dir, Default, "schema = 1\nname = 'My tidy'\nemoji = '🧪'\ntagline = 'mine'\n"+
		"[lines]\n'save.error.refused' = ['Mine: {reason}']\n'inbox.header.clear' = ['Mine: all clear']\n")
	writePersona(t, dir, "plain", testHeader)
	writePersona(t, dir, "asks", testHeader+"extends = 'neatpik'\n")

	f, ds, err := Read(box.Ctx, Default)
	if err != nil || f == nil || len(ds) != 0 {
		t.Fatalf("Read = %+v, %v, %v", f, ds, err)
	}
	if f.Source != UserOverridesBuiltin || f.Name != "My tidy" || f.Path != dir+"/personas/neatpik.toml" {
		t.Errorf("Read = source %v, name %q, path %q", f.Source, f.Name, f.Path)
	}
	if built, _, err := readBuiltin(Default); err != nil || built.Name != "Neatpik" || built.Source != Builtin {
		t.Errorf("the embedded copy changed: %+v, %v", built, err)
	}
	if !slices.Contains(Builtins(), Default) {
		t.Errorf("Builtins = %v", Builtins())
	}

	files, _, err := List(box.Ctx)
	var listed []string
	for _, f := range files {
		if f.ID == Default {
			listed = append(listed, f.Name)
		}
	}
	if err != nil || !slices.Equal(listed, []string{"My tidy"}) {
		t.Errorf("List has neatpik as %q (%v), want the user's one, once", listed, err)
	}

	v, _, err := Load(box.Ctx, Default)
	if err != nil || v.Name != "My tidy" || len(v.Lines["save.error.refused"]) != 1 {
		t.Fatalf("Load(neatpik) = %+v, %v", v, err)
	}
	// The user's file has no colours: they come from the embedded neatpik.
	if v.Colors.Accent.Light == "" || v.Colors.OK.Dark == "" {
		t.Errorf("colours = %+v", v.Colors)
	}
	for name, ctx := range map[string]string{"by name": Default, "by default": ""} {
		if got := Active(asking(box, ctx)); got == nil || got.Name != "My tidy" {
			t.Errorf("Active %s = %+v", name, got)
		}
	}

	// ... and only as a choice. extends = "neatpik" is the default (plan
	// S3), so spelling it out is the same as saying nothing: both have the
	// embedded neatpik under them and inherit its lines, and neither reaches
	// the user's file, whose lines are all distinctive ("Mine").
	embedded := embeddedNeatpikLines(t)
	for _, id := range []string{"plain", "asks"} {
		v, _, err := Load(box.Ctx, id)
		if err != nil || !reflect.DeepEqual(v.Lines, embedded) {
			t.Errorf("%s = %+v, %v: not the embedded neatpik's lines", id, v, err)
			continue
		}
		for lineID, lines := range v.Lines {
			for _, l := range lines {
				if strings.HasPrefix(l.Text, "Mine") {
					t.Errorf("%s: the user's neatpik leaked into a persona that extends the default: %s = %q", id, lineID, l.Text)
				}
			}
		}
	}
}

// An id the persona has no line for is said by the catalog, as the neutral
// voice says it; an id it has is said by the persona (plan S4).
func TestALineFallsThroughToTheCatalog(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	writePersona(t, box.Ctx.ConfigDir, "tester", testHeader+"[lines]\n'save.error.refused' = ['Tester says no: {reason}']\n")
	v, _, err := Load(box.Ctx, "tester")
	if err != nil {
		t.Fatal(err)
	}
	refused := map[string]any{"reason": "disk full"}
	if got := say(t, v, "save.error.refused", refused); got != "Tester says no: disk full" {
		t.Errorf("its own line = %q", got)
	}
	for _, id := range []string{"save.status.nothing", "save.success.committed"} {
		values := map[string]any{}
		if id == "save.success.committed" {
			values["n"] = 3
		}
		got := say(t, v, id, values)
		if got == "" || got != neutral(t, id, values) {
			t.Errorf("%s = %q, want the catalog's %q", id, got, neutral(t, id, values))
		}
	}

	// A persona whose only variant is invalid has no line either: the whole
	// chain is asked, then the catalog.
	writePersona(t, box.Ctx.ConfigDir, "bad", testHeader+"[lines]\n'save.error.refused' = ['Bad {missing}']\n")
	v, ds, err := Load(box.Ctx, "bad")
	if err != nil || !hasDiagnostic(ds, "v16") {
		t.Fatalf("Load = %v, %v, %v", v, codes(ds), err)
	}
	if got := say(t, v, "save.error.refused", refused); got != neutral(t, "save.error.refused", refused) || got != "Commit refused: disk full" {
		t.Errorf("the dropped line is said as %q", got)
	}
}

// M5: a line is formatted with the plural rules of the persona that supplied
// it, and a line the catalog supplies with English rules, whatever lang the
// persona has. In Portuguese 0 is "one"; in English it is "other".
func TestPortuguesePersonaFallsThroughWithEnglishPlurals(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	dir := box.Ctx.ConfigDir
	// save.success.pushed is "pushed {n:# commit|# commits}".
	writePersona(t, dir, "english", testHeader+"[lines]\n'save.success.pushed' = ['sent {n:# commit|# commits}']\n")
	writePersona(t, dir, "lusophone", testHeader+"lang = 'pt'\nextends = 'english'\n"+
		"[lines]\n'save.success.committed' = ['gravou {n:one=# arquivo|other=# arquivos}']\n")

	v, ds, err := Load(box.Ctx, "lusophone")
	if err != nil || v == nil {
		t.Fatalf("Load = %v, %v, %v", v, codes(ds), err)
	}
	if v.Lang != language.Portuguese {
		t.Errorf("Lang = %s", v.Lang)
	}
	if l := v.Lines["save.success.committed"]; len(l) != 1 || l[0].Lang != language.Portuguese {
		t.Errorf("its own line = %+v, want Portuguese", l)
	}
	if l := v.Lines["save.success.pushed"]; len(l) != 1 || l[0].Lang != language.English {
		t.Errorf("the parent's line = %+v, want English: a line keeps its supplier's language", l)
	}

	for _, tc := range []struct {
		id   string
		n    int
		want string
	}{
		// Its own line, Portuguese rules: 0 and 1 are "one".
		{"save.success.committed", 0, "gravou 0 arquivo"},
		{"save.success.committed", 1, "gravou 1 arquivo"},
		{"save.success.committed", 2, "gravou 2 arquivos"},
		// The parent's line, English rules: 0 is "other".
		{"save.success.pushed", 0, "sent 0 commits"},
		{"save.success.pushed", 1, "sent 1 commit"},
		// The catalog's, English rules.
		{"save.success.pulled", 0, "pulled 0 commits"},
		{"save.success.pulled", 1, "pulled 1 commit"},
		{"save.success.pulled", 2, "pulled 2 commits"},
	} {
		if got := say(t, v, tc.id, map[string]any{"n": tc.n}); got != tc.want {
			t.Errorf("%s with %d = %q, want %q", tc.id, tc.n, got, tc.want)
		}
	}
}

// V15 on one of five variants: the other four load, in order, and Validate
// names the bad one by id and index (plan S4, S5).
func TestFiveVariantsOneBad(t *testing.T) {
	const id = "save.error.refused"
	for bad := 1; bad <= 5; bad++ {
		t.Run(string(rune('0'+bad)), func(t *testing.T) {
			box := testutil.Sandbox(t, testutil.Options{})
			var variants, good []string
			for i := 1; i <= 5; i++ {
				line := "Variant " + string(rune('A'+i-1)) + ": {reason}"
				if i == bad {
					line = "Variant without it"
				} else {
					good = append(good, line)
				}
				variants = append(variants, "'"+line+"'")
			}
			writePersona(t, box.Ctx.ConfigDir, "tester", testHeader+"[lines]\n'"+id+"' = ["+strings.Join(variants, ", ")+"]\n")

			var debug bytes.Buffer
			ctx := box.Ctx.Fork(nil)
			ctx.Debug = slog.New(slog.NewTextHandler(&debug, &slog.HandlerOptions{Level: slog.LevelDebug}))

			v, ds, err := Load(ctx, "tester")
			if err != nil || v == nil {
				t.Fatalf("Load = %v, %v, %v", v, codes(ds), err)
			}
			var kept []string
			for _, l := range v.Lines[id] {
				kept = append(kept, l.Text)
			}
			if !slices.Equal(kept, good) {
				t.Errorf("kept %q, want %q", kept, good)
			}
			if !strings.Contains(debug.String(), "dropped invalid variant") {
				t.Errorf("no debug line for the dropped variant: %q", debug.String())
			}

			f, _, err := Read(box.Ctx, "tester")
			if err != nil {
				t.Fatal(err)
			}
			var errs []Diagnostic
			for _, d := range Validate(box.Ctx, f, ValidateOptions{}) {
				if d.Severity == Error {
					errs = append(errs, d)
				}
			}
			if len(errs) != 1 || errs[0].Text.ID() != "persona.diag.v15" || errs[0].LineID != id || errs[0].Variant != bad {
				t.Errorf("Validate errors = %+v, want V15 for %s variant %d", errs, id, bad)
			}
		})
	}
}

// A persona that is not there. A missing user file is an unknown id: the
// fallback speaks, the problem says who asked, and only HOMEBOT_PERSONA gets
// a warning (plan S4).
func TestAMissingPersonaFile(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	if v, ds, err := Load(box.Ctx, "ghost"); v != nil || err == nil || len(headerErrors(ds)) == 0 && !hasDiagnostic(ds, "v9") {
		t.Errorf("Load(ghost) = %v, %v, %v", v, codes(ds), err)
	}
	if f, _, err := Read(box.Ctx, "ghost"); f != nil || err == nil {
		t.Errorf("Read(ghost) = %+v, %v", f, err)
	}
	if files, _, _ := List(box.Ctx); slices.ContainsFunc(files, func(f *File) bool { return f.ID == "ghost" }) {
		t.Error("List has ghost")
	}

	t.Run("from the environment", func(t *testing.T) {
		ctx := asking(box, "ghost")
		if got := Active(ctx); got == nil || got.ID != Default || got.Name != "Neatpik" {
			t.Fatalf("Active = %+v", got)
		}
		if p := ActiveProblem(ctx); p == nil || p.ID != "ghost" || p.Origin != FromEnv {
			t.Errorf("ActiveProblem = %+v", p)
		}
		if w, ok := TakeWarning(ctx); !ok || w.ID() != "persona.warning.unusable" {
			t.Errorf("TakeWarning = %v, %v", w.ID(), ok)
		}
	})
	t.Run("from homebot.yml", func(t *testing.T) {
		ctx := asking(box, "")
		ctx.Config = &config.Config{Persona: "ghost"}
		if got := Active(ctx); got == nil || got.ID != Default {
			t.Fatalf("Active = %+v", got)
		}
		if p := ActiveProblem(ctx); p == nil || p.ID != "ghost" || p.Origin != FromConfig {
			t.Errorf("ActiveProblem = %+v", p)
		}
		if _, ok := TakeWarning(ctx); ok {
			t.Error("a warning for persona: in homebot.yml; only HOMEBOT_PERSONA warns")
		}
	})
	t.Run("the environment wins over a bad config", func(t *testing.T) {
		writePersona(t, box.Ctx.ConfigDir, "tester", testHeader)
		ctx := asking(box, "tester")
		ctx.Config = &config.Config{Persona: "ghost"}
		if got := Active(ctx); got == nil || got.ID != "tester" {
			t.Fatalf("Active = %+v", got)
		}
		if p := ActiveProblem(ctx); p != nil {
			t.Errorf("ActiveProblem = %+v: the config was never consulted", p)
		}
	})
}

// No repo, so a nil Config: the environment still picks, and with no choice
// at all it is the embedded neatpik, with no problem and no personas
// directory needed.
func TestNoConfigAtAll(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	// The sandbox has never made <ConfigDir>/personas.
	ctx := asking(box, "")
	if ctx.Config != nil {
		t.Fatal("asking left a Config")
	}
	got := Active(ctx)
	if got == nil || got.ID != Default || got.Name != "Neatpik" || got.Colors.Accent.Light == "" {
		t.Fatalf("Active = %+v, want the embedded Neatpik", got)
	}
	if p := ActiveProblem(ctx); p != nil {
		t.Errorf("ActiveProblem = %+v", p)
	}
	if _, ok := TakeWarning(ctx); ok {
		t.Error("a warning with nothing configured")
	}
	if files, findings, err := List(ctx); err != nil || len(files) != len(Builtins()) || len(findings[Default]) != 0 {
		t.Errorf("List = %d files, %v, %v", len(files), findings, err)
	}

	writePersona(t, box.Ctx.ConfigDir, "tester", testHeader)
	if got := Active(asking(box, "tester")); got == nil || got.ID != "tester" {
		t.Errorf("env with no Config: Active = %+v", got)
	}
	if got := Active(asking(box, None)); got != nil {
		t.Errorf("none with no Config: Active = %+v", got)
	}
}
