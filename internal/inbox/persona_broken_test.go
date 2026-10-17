package inbox

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// brokenBox selects a persona in homebot.yml whose file does not exist.
func brokenBox(t *testing.T) *box {
	t.Helper()
	b := newBox(t, testutil.Options{Config: testutil.BaseConfig + "persona: tester\n"})
	b.Ctx.Env = b.Ctx.Env.Without("HOMEBOT_PERSONA")
	if p := persona.ActiveProblem(b.Ctx); p == nil || p.Origin != persona.FromConfig {
		t.Fatalf("persona problem = %+v, want one from homebot.yml", p)
	}
	return b
}

func TestBrokenConfiguredPersonaIsNeutralInTheNudgeSymbolAndNotification(t *testing.T) {
	b := brokenBox(t)
	b.WithStubs(map[string]string{"notify-send": `printf '%s|' "$@" >> "$WORK_SENT"; echo >> "$WORK_SENT"`})
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"WORK_SENT": filepath.Join(b.Dir, "sent")})
	// A stale symbol from before the persona broke must go.
	if err := os.MkdirAll(b.Ctx.CacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.Ctx.CacheDir, "symbol"), []byte("🧹\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	items := []Item{{ID: "a", Level: Urgent, Title: "Bad"}, {ID: "b", Level: Todo, Title: "Meh"}}

	emoji, text := NudgeLine(b.Ctx, items)
	eq(t, emoji, "🚚")
	eq(t, text, "homebot: 2 things need you (1 urgent), run: homebot")
	title, body := NotificationText(b.Ctx, items)
	eq(t, title, "homebot needs you")
	eq(t, body, "Bad. Run: homebot")

	if err := WriteCache(b.Ctx, items); err != nil {
		t.Fatal(err)
	}
	eq(t, readCache(t, b.Ctx, "nudge"), "🚚 homebot: 2 things need you (1 urgent), run: homebot\n")
	if _, err := os.Stat(filepath.Join(b.Ctx.CacheDir, "symbol")); !os.IsNotExist(err) {
		t.Errorf("broken persona left a symbol file: %v", err)
	}

	shown, err := Notify(b.Ctx, items)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, shown, true)
	if got, want := sent(t, b), []string{"--app-name=homebot|homebot needs you|Bad. Run: homebot|"}; !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}

	// What the person reads in a terminal still falls back to Neatpik.
	b.Ctx.ForceVoice = true
	if got := ansi.Strip(Render(b.Ctx, nil, nil, false)); !strings.HasPrefix(got, "🧹 Neatpik · testhost · ") {
		t.Errorf("terminal header = %q, want Neatpik's", got)
	}
}

func TestABrokenEnvironmentPersonaStillSpeaksAsNeatpikInTheBackground(t *testing.T) {
	b := newBox(t, testutil.Options{})
	c := b.Ctx.Fork(map[string]string{"HOMEBOT_PERSONA": "missing"})
	if p := persona.ActiveProblem(c); p == nil || p.Origin != persona.FromEnv {
		t.Fatalf("persona problem = %+v, want one from the environment", p)
	}
	items := []Item{{ID: "a", Level: Urgent, Title: "Bad"}}
	emoji, text := NudgeLine(c, items)
	eq(t, emoji, "🧹")
	eq(t, text, "Neatpik: 1 thing needs you (1 urgent), run: homebot")
	// The fallback is the embedded Neatpik with its lines (plan S4, m11), so
	// the notification says what a working setup says, not the catalog's
	// "Neatpik needs you".
	title, body := NotificationText(c, items)
	wantTitle, wantBody := NotificationText(b.Ctx.Fork(map[string]string{"HOMEBOT_PERSONA": "neatpik"}), items)
	eq(t, title, wantTitle)
	eq(t, body, wantBody)
	if !strings.HasPrefix(title, "🧹 Neatpik ") || !strings.HasSuffix(body, "homebot") {
		t.Errorf("notification = %q / %q, want Neatpik's", title, body)
	}
}
