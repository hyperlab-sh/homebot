package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func personaBox(t *testing.T) *testutil.Box {
	t.Helper()
	return testutil.Sandbox(t, testutil.Options{})
}

func runPersona(b *testutil.Box, args ...string) (int, string, string) {
	b.Stdout.Reset()
	b.Stderr.Reset()
	code := execute(b.Ctx, append([]string{"persona"}, args...)...)
	return code, b.Stdout.String(), b.Stderr.String()
}

func writePersona(t *testing.T, b *testutil.Box, id, body string) string {
	t.Helper()
	dir := filepath.Join(b.Ctx.ConfigDir, "personas")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const testerPersona = `schema = 1
id = "tester"
name = "Tester"
emoji = "🧪"
tagline = "A careful helper"
extends = "neatpik"
[lines]
"save.success.committed" = ["Saved {n:# file|# files}", "Filed {n:# file|# files}"]
`

func TestPersonaListShowsSourcesAndBrokenFiles(t *testing.T) {
	b := personaBox(t)
	writePersona(t, b, "tester", testerPersona)
	overrideID := ""
	for _, id := range persona.Builtins() {
		if id != persona.Default {
			overrideID = id
			break
		}
	}
	writePersona(t, b, overrideID, `schema = 1
name = "An override"
emoji = "🧪"
extends = "neatpik"
`)
	writePersona(t, b, "broken", `schema = 1
name = "Bad"
tagline = "$(touch /tmp/pwned)"
`)
	code, out, stderr := runPersona(b, "list")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	for _, want := range []string{"tester  Tester  [user]", overrideID + "  An override  [override]", "broken  [unusable]", "neatpik  Neatpik  [built-in]"} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q in %q", want, out)
		}
	}
}

func TestPersonaShowAndValidateRefuseHostileHeader(t *testing.T) {
	b := personaBox(t)
	path := writePersona(t, b, "hostile", "schema = 1\nid = \"hostile\"\nname = \"Hostile\"\nemoji = \"🧪\"\ntagline = \"$(touch "+filepath.Join(b.Dir, "pwned")+")\"\nextends = \"neatpik\"\n")
	for _, args := range [][]string{{"show", "hostile"}, {"validate", "hostile"}, {"validate", path}, {"use", "hostile"}} {
		code, _, stderr := runPersona(b, args...)
		if code != 1 || !strings.Contains(stderr, "Persona") {
			t.Errorf("%q: exit %d stderr %q", args, code, stderr)
		}
	}
	if _, err := os.Lstat(filepath.Join(b.Dir, "pwned")); !os.IsNotExist(err) {
		t.Errorf("hostile tagline executed: %v", err)
	}
	code, out, _ := runPersona(b, "show", "neatpik")
	if code != 0 || !strings.Contains(out, "Neatpik") || !strings.Contains(out, "light #") {
		t.Errorf("show: exit %d out %q", code, out)
	}
}

func TestPersonaUsePreservesCommentsAndWarnsOnEnv(t *testing.T) {
	b := personaBox(t)
	writePersona(t, b, "tester", testerPersona)
	path := filepath.Join(b.Root, "homebot.yml")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append([]byte("# keep this comment\n"), before...), 0o644); err != nil {
		t.Fatal(err)
	}
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"HOMEBOT_PERSONA": "neatpik"})
	code, out, stderr := runPersona(b, "use", "tester")
	if code != 0 || stderr != "" {
		t.Fatalf("use: %d %q %q", code, out, stderr)
	}
	if !strings.Contains(out, "HOMEBOT_PERSONA overrides") {
		t.Errorf("missing env warning: %q", out)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "# keep this comment") || !strings.Contains(string(got), "persona: tester") {
		t.Errorf("config: %q", got)
	}
	code, _, _ = runPersona(b, "use", "none")
	if code != 0 {
		t.Errorf("none exit %d", code)
	}
	got, _ = os.ReadFile(path)
	if !strings.Contains(string(got), "persona: none") {
		t.Errorf("none config: %q", got)
	}
}

func TestPersonaUseRefusesInvalidVariantWhilePreviewDropsIt(t *testing.T) {
	b := personaBox(t)
	bad := `schema = 1
id = "tester"
name = "Tester"
emoji = "🧪"
extends = "none"
[lines]
"save.success.committed" = ["No count here"]
`
	// extends = "none": what is left once the variant is dropped is the
	// catalog's own wording, whatever Neatpik says for this id.
	writePersona(t, b, "tester", bad)
	before, err := os.ReadFile(filepath.Join(b.Root, "homebot.yml"))
	if err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runPersona(b, "use", "tester")
	if code != 1 || !strings.Contains(stderr, "unusable") {
		t.Errorf("use invalid variant: %d %q", code, stderr)
	}
	after, _ := os.ReadFile(filepath.Join(b.Root, "homebot.yml"))
	if string(after) != string(before) {
		t.Error("invalid persona changed config")
	}
	code, out, stderr := runPersona(b, "preview", "tester", "--event", "save.success.committed")
	// The finding goes to stderr, the preview of what is left to stdout.
	if code != 0 || !strings.Contains(stderr, "Missing") || strings.Contains(out, "Missing") || !strings.Contains(out, "committed 3 files") {
		t.Errorf("preview dropped variant: %d %q %q", code, out, stderr)
	}
}

func TestPersonaNewScaffoldAndNoOverwrite(t *testing.T) {
	b := personaBox(t)
	code, _, stderr := runPersona(b, "new", "tester")
	if code != 0 {
		t.Fatalf("new exit %d: %q", code, stderr)
	}
	path := filepath.Join(b.Ctx.ConfigDir, "personas", "tester.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"schema = 1", "id = \"tester\"", "[lines]", `# "save.success.committed"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("scaffold lacks %q", want)
		}
	}
	if code, _, stderr := runPersona(b, "validate", "tester"); code != 0 {
		t.Errorf("new scaffold is invalid: %d %q", code, stderr)
	}
	code, _, stderr = runPersona(b, "new", "tester")
	if code != 1 || !strings.Contains(stderr, "already exists") {
		t.Errorf("overwrite: %d %q", code, stderr)
	}
	if got, _ := os.ReadFile(path); string(got) != string(data) {
		t.Error("existing file changed")
	}
	writePersona(t, b, "source", strings.ReplaceAll(testerPersona, "tester", "source"))
	code, _, stderr = runPersona(b, "new", "copy", "--from", "source")
	if code != 0 {
		t.Fatalf("from exit %d: %q", code, stderr)
	}
	copied, _ := os.ReadFile(filepath.Join(b.Ctx.ConfigDir, "personas", "copy.toml"))
	if !strings.Contains(string(copied), `"save.success.committed" = [`) {
		t.Errorf("copy lacks line: %q", copied)
	}
}

func TestPersonaPreviewEventAllVariantsAndPlain(t *testing.T) {
	b := personaBox(t)
	writePersona(t, b, "tester", testerPersona)
	code, out, stderr := runPersona(b, "preview", "tester", "--event", "save.success.committed", "--all-variants")
	if code != 0 || stderr != "" || !strings.Contains(out, "Saved 3 files") || !strings.Contains(out, "Filed 3 files") {
		t.Errorf("variants: %d %q %q", code, out, stderr)
	}
	for _, want := range []string{"light theme", "dark theme"} {
		if !strings.Contains(out, want) {
			t.Errorf("event preview lacks %q", want)
		}
	}
	if strings.Count(out, "Saved 3 files") != 2 || strings.Count(out, "Filed 3 files") != 2 {
		t.Errorf("each theme should show both variants: %q", out)
	}
	code, out, _ = runPersona(b, "--plain", "preview", "tester", "--event", "save.success.committed")
	if code != 0 || !strings.Contains(out, "committed 3 files") {
		t.Errorf("plain: %d %q", code, out)
	}
	code, _, stderr = runPersona(b, "preview", "tester", "--event", "guard.plaintext")
	if code != 1 || !strings.Contains(stderr, "No voiced message id") {
		t.Errorf("non-voice: %d %q", code, stderr)
	}
}

func TestPersonaIDsIncludesVoiceSlots(t *testing.T) {
	b := personaBox(t)
	code, out, stderr := runPersona(b, "ids")
	if code != 0 || stderr != "" || !strings.Contains(out, "save.success.committed [success]") || !strings.Contains(out, "{n}: int") {
		t.Errorf("ids: %d %q %q", code, out, stderr)
	}
	if strings.Contains(out, "guard.plaintext") {
		t.Error("ids lists a fact")
	}
}

func TestPersonaUseRewritesTheShellCachesAtOnce(t *testing.T) {
	b := personaBox(t)
	b.Ctx.Env = b.Ctx.Env.Without("HOMEBOT_PERSONA")
	writePersona(t, b, "tester", testerPersona)
	read := func(name string) (string, bool) {
		data, err := os.ReadFile(filepath.Join(b.Ctx.CacheDir, name))
		if os.IsNotExist(err) {
			return "", false
		}
		if err != nil {
			t.Fatal(err)
		}
		return string(data), true
	}
	if code, out, stderr := runPersona(b, "use", "tester"); code != 0 {
		t.Fatalf("use tester: %d %q %q", code, out, stderr)
	}
	nudge, _ := read("nudge")
	symbol, ok := read("symbol")
	if !strings.HasPrefix(nudge, "🧪 Tester: ") || !ok || symbol != "🧪\n" {
		t.Errorf("after use tester: nudge %q symbol %q (exists %v)", nudge, symbol, ok)
	}
	if code, out, stderr := runPersona(b, "use", "none"); code != 0 {
		t.Fatalf("use none: %d %q %q", code, out, stderr)
	}
	nudge, _ = read("nudge")
	if !strings.HasPrefix(nudge, "🚚 homebot: ") {
		t.Errorf("after use none: nudge %q", nudge)
	}
	if symbol, ok := read("symbol"); ok {
		t.Errorf("after use none: symbol %q still there", symbol)
	}
}
