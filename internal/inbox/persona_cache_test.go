package inbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// Refresh holds nothing between runs: every run resolves the persona again
// and rewrites the nudge and the symbol, so a changed choice or a changed
// persona file shows on the next run. What starts that run is another matter
// (persona use does; see the cli tests).

func writeTester(t *testing.T, b *box, name, emoji string) string {
	t.Helper()
	path := filepath.Join(b.Ctx.ConfigDir, "personas", "tester.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("schema = 1\nid = \"tester\"\nname = %q\nemoji = %q\ntagline = \"t\"\nextends = \"neatpik\"\n", name, emoji)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func setConfig(t *testing.T, b *box, yml string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(b.Root, "homebot.yml"), []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	b.reload()
}

// refreshFresh runs Refresh the way a new process would: a Context with no
// memo, so nothing resolved earlier is reused.
func refreshFresh(t *testing.T, b *box) (nudge string, symbol string, hasSymbol bool) {
	t.Helper()
	if _, err := Refresh(b.Ctx.Fork(nil), Options{}); err != nil {
		t.Fatal(err)
	}
	nudge = readCache(t, b.Ctx, "nudge")
	data, err := os.ReadFile(filepath.Join(b.Ctx.CacheDir, "symbol"))
	if os.IsNotExist(err) {
		return nudge, "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return nudge, string(data), true
}

func TestRefreshFollowsAChangedPersonaChoiceAndFile(t *testing.T) {
	b := newBox(t, testutil.Options{Config: testutil.BaseConfig + "persona: tester\n"})
	b.Ctx.Env = b.Ctx.Env.Without("HOMEBOT_PERSONA")
	file := writeTester(t, b, "Tester", "🧪")

	check := func(step, nudgePrefix, wantSymbol string) {
		t.Helper()
		nudge, symbol, has := refreshFresh(t, b)
		if !strings.HasPrefix(nudge, nudgePrefix) {
			t.Errorf("%s: nudge %q, want it to start %q", step, nudge, nudgePrefix)
		}
		if wantSymbol == "" && has {
			t.Errorf("%s: symbol %q, want none", step, symbol)
		}
		if wantSymbol != "" && symbol != wantSymbol {
			t.Errorf("%s: symbol %q (exists %v), want %q", step, symbol, has, wantSymbol)
		}
	}
	check("configured persona", "🧪 Tester: ", "🧪\n")

	// The persona file changes in place, and so does its mtime.
	writeTester(t, b, "Scope", "🔭")
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(file, later, later); err != nil {
		t.Fatal(err)
	}
	check("edited persona file", "🔭 Scope: ", "🔭\n")

	// The choice in homebot.yml changes to none.
	setConfig(t, b, testutil.BaseConfig+"persona: none\n")
	check("persona: none", "🚚 homebot: ", "")

	// HOMEBOT_PERSONA wins over the file, both ways.
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"HOMEBOT_PERSONA": "tester"})
	check("environment picks tester", "🔭 Scope: ", "🔭\n")
	setConfig(t, b, testutil.BaseConfig+"persona: tester\n")
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"HOMEBOT_PERSONA": "none"})
	check("environment picks none", "🚚 homebot: ", "")
}
