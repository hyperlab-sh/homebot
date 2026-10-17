package cli

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/hyperlab-sh/homebot/internal/persona"
)

var updatePersonaGoldens = flag.Bool("update-persona-goldens", false, "record persona list and preview goldens")

func checkPersonaGolden(t *testing.T, path, got string) {
	t.Helper()
	if *updatePersonaGoldens {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s changed; record deliberately with -update-persona-goldens\nwant:\n%s\ngot:\n%s", path, want, got)
	}
}

func TestPersonaListGolden(t *testing.T) {
	b := personaBox(t)
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"HOMEBOT_PERSONA": "", "NO_COLOR": "1"})
	code, out, stderr := runPersona(b, "list")
	if code != 0 || stderr != "" {
		t.Fatalf("list: %d %q", code, stderr)
	}
	checkPersonaGolden(t, filepath.Join("..", "persona", "builtin", "list.golden"), out)
}

func TestPersonaIDsGolden(t *testing.T) {
	b := personaBox(t)
	code, out, stderr := runPersona(b, "ids")
	if code != 0 || stderr != "" {
		t.Fatalf("ids: %d %q", code, stderr)
	}
	checkPersonaGolden(t, filepath.Join("testdata", "persona-preview", "ids.golden"), out)
}

// TestPersonaNewScaffoldGolden records the whole file persona new writes for a
// fresh id: the header, the empty colour table and one commented-out line for
// every voice id, with its English, placeholders and meanings. It is the one
// place a change to the scaffold or to the set of voiced ids shows as a diff.
func TestPersonaNewScaffoldGolden(t *testing.T) {
	b := personaBox(t)
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"HOMEBOT_PERSONA": ""})
	code, out, stderr := runPersona(b, "new", "tester")
	if code != 0 || stderr != "" {
		t.Fatalf("new: %d %q %q", code, out, stderr)
	}
	data, err := os.ReadFile(filepath.Join(b.Ctx.ConfigDir, "personas", "tester.toml"))
	if err != nil {
		t.Fatal(err)
	}
	checkPersonaGolden(t, filepath.Join("testdata", "persona-preview", "new.golden"), string(data))
	// The scaffold is a persona that loads and validates as it is.
	if code, out, stderr := runPersona(b, "validate", "tester"); code != 0 {
		t.Errorf("scaffold does not validate: %d %q %q", code, out, stderr)
	}
}

func TestPersonaPreviewGoldens(t *testing.T) {
	for _, id := range persona.Builtins() {
		path := filepath.Join("..", "persona", "builtin", id, "preview.golden")
		if id != persona.Default {
			if _, err := os.Stat(path); os.IsNotExist(err) {
				continue
			}
		}
		t.Run(id, func(t *testing.T) {
			b := personaBox(t)
			b.Ctx.Env = b.Ctx.Env.With(map[string]string{"HOMEBOT_PERSONA": "", "NO_COLOR": "1"})
			code, out, stderr := runPersona(b, "preview", id)
			if code != 0 || stderr != "" {
				t.Fatalf("preview: %d %q", code, stderr)
			}
			checkPersonaGolden(t, path, out)
		})
	}
	t.Run("tester", func(t *testing.T) {
		b := personaBox(t)
		b.Ctx.Env = b.Ctx.Env.With(map[string]string{"HOMEBOT_PERSONA": "", "NO_COLOR": "1"})
		writePersona(t, b, "tester", testerPersona)
		code, out, stderr := runPersona(b, "preview", "tester")
		if code != 0 || stderr != "" {
			t.Fatalf("preview: %d %q", code, stderr)
		}
		checkPersonaGolden(t, filepath.Join("testdata", "persona-preview", "tester.golden"), out)
	})
}

func TestDefaultPersonaIsNeatpik(t *testing.T) {
	b := personaBox(t)
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"HOMEBOT_PERSONA": ""})
	terminal := true
	b.Ctx.StdoutTerminal = &terminal
	v, _, err := persona.Load(b.Ctx, persona.Default)
	if err != nil {
		t.Fatal(err)
	}
	b.Stdout.Reset()
	b.Stderr.Reset()
	if code := execute(b.Ctx, "--all"); code != 0 {
		t.Fatalf("--all: %d %q", code, b.Stderr.String())
	}
	first, _, _ := strings.Cut(ansi.Strip(b.Stdout.String()), "\n")
	if !strings.HasPrefix(first, v.Emoji+" "+v.Name) {
		t.Errorf("default inbox does not start with %s %s: %q", v.Emoji, v.Name, first)
	}
}
