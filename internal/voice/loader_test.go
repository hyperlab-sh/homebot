package voice_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// These tests run the voice engine over the real persona loader: persona files
// on disk, selected the way users select them (HOMEBOT_PERSONA), with no
// persona.Override and no SpeakAs.

const testerHeader = "schema = 1\nname = 'Tester'\nemoji = '🧪'\n"

// testerPersona rewords the inbox header and nothing else. It extends none,
// so its nudge, which is voiced and has {bot}, falls through to the catalog
// with the persona's name rather than to Neatpik's wording.
const testerPersona = testerHeader + "extends = 'none'\n[lines]\n'inbox.header.count' = ['{n:# note|# notes} (Tester)']\n"

const (
	neutralNudge = "homebot: 2 things need you, run: homebot"
	testerNudge  = "Tester: 2 things need you, run: homebot"
	// neatpikNudge is the variant of Neatpik's nudge that seed 1 picks; the
	// tests depend on the seed and the rotation.
	neatpikNudge = "Neatpik: 2 things are waiting, run: homebot"
)

func writePersonaFile(t *testing.T, ctx *app.Context, id, body string) {
	t.Helper()
	path := filepath.Join(ctx.ConfigDir, "personas", id+".toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// speaking is a Fork of the sandbox's Context that selects persona id, with
// stdout a terminal (tty) or not. The sandbox itself selects none.
func speaking(box *testutil.Box, id string, tty bool) *app.Context {
	ctx := box.Ctx.Fork(map[string]string{"HOMEBOT_PERSONA": id})
	ctx.StdoutTerminal = &tty
	return ctx
}

func plainOf(ctx *app.Context, t msg.Text) string  { return voice.Plain(ctx, t) }
func renderOf(ctx *app.Context, t msg.Text) string { return ansi.Strip(voice.Render(ctx, t)) }

// The end-to-end case: a persona file with lines, selected by
// HOMEBOT_PERSONA, is what Render, Plain and Nudge say, with its name for
// {bot}. Record stays neutral.
func TestARealPersonaFileSpeaks(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	writePersonaFile(t, box.Ctx, "tester", testerPersona)
	ctx := speaking(box, "tester", true)

	header := msg.InboxHeaderCount(2)
	nudge := msg.InboxNudge(2, "")
	for name, got := range map[string]string{
		"Render header": renderOf(ctx, header),
		"Plain header":  plainOf(ctx, header),
	} {
		if want := "2 notes (Tester)"; got != want {
			t.Errorf("%s = %q, want the persona's line %q", name, got, want)
		}
	}
	for name, got := range map[string]string{
		"Render nudge": renderOf(ctx, nudge),
		"Plain nudge":  plainOf(ctx, nudge),
		"Nudge":        voice.Nudge(ctx, nudge, "k"),
	} {
		if got != testerNudge {
			t.Errorf("%s = %q, want %q: the catalog's words with {bot} as the persona", name, got, testerNudge)
		}
	}
	for name, got := range map[string]string{"header": voice.Record(ctx, header), "nudge": voice.Record(ctx, nudge)} {
		if strings.Contains(got, "Tester") || strings.Contains(got, "notes (") {
			t.Errorf("Record %s = %q, want the neutral voice", name, got)
		}
	}
	if got := voice.Record(ctx, nudge); got != neutralNudge {
		t.Errorf("Record nudge = %q, want %q", got, neutralNudge)
	}
	if got := box.Stderr.String(); got != "" {
		t.Errorf("a usable persona printed a warning: %q", got)
	}
}

// With nothing selected, the embedded Neatpik speaks on a terminal: its own
// lines under its name, which is a visible change from the neutral voice that
// nothing else pins.
func TestTheDefaultPersonaIsNeatpik(t *testing.T) {
	for _, env := range []string{"", "neatpik"} {
		t.Run("HOMEBOT_PERSONA="+env, func(t *testing.T) {
			box := testutil.Sandbox(t, testutil.Options{})
			ctx := speaking(box, env, true)
			nudge := msg.InboxNudge(2, "")
			if got := renderOf(ctx, nudge); got != neatpikNudge {
				t.Errorf("Render = %q, want %q", got, neatpikNudge)
			}
			if got := plainOf(ctx, msg.InboxHeaderCount(2)); got != "2 things need you" {
				t.Errorf("header = %q, want Neatpik's first variant, which reads like the catalog's words", got)
			}
			if got := voice.Record(ctx, nudge); got != neutralNudge {
				t.Errorf("Record = %q, want %q", got, neutralNudge)
			}
			if got := box.Stderr.String(); got != "" {
				t.Errorf("warned: %q", got)
			}
		})
	}
}

// S1's table, one row per line, over the real loader. The want is the header
// text that Plain says; a persona line ends in "(Tester)".
func TestVoiceSelectionTable(t *testing.T) {
	voiced := "2 notes (Tester)"
	neutral := "2 things need you"
	cases := []struct {
		name    string
		persona string
		tty     bool
		force   bool
		plain   bool
		env     map[string]string
		want    string
	}{
		{"terminal", "tester", true, false, false, nil, voiced},
		{"piped", "tester", false, false, false, nil, neutral},
		{"piped, persona show forces the voice", "tester", false, true, false, nil, voiced},
		{"--plain beats the terminal", "tester", true, false, true, nil, neutral},
		{"--plain beats the forced voice", "tester", false, true, true, nil, neutral},
		{"HOMEBOT_PLAIN beats the terminal", "tester", true, false, false, map[string]string{"HOMEBOT_PLAIN": "1"}, neutral},
		{"none on a terminal", "none", true, false, false, nil, neutral},
		{"none, forced", "none", false, true, false, nil, neutral},
		{"NO_COLOR leaves the voice unchanged", "tester", true, false, false, map[string]string{"NO_COLOR": "1"}, voiced},
		{"NO_COLOR, piped", "tester", false, false, false, map[string]string{"NO_COLOR": "1"}, neutral},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			box := testutil.Sandbox(t, testutil.Options{})
			writePersonaFile(t, box.Ctx, "tester", testerPersona)
			vars := map[string]string{"HOMEBOT_PERSONA": c.persona}
			for k, v := range c.env {
				vars[k] = v
			}
			ctx := box.Ctx.Fork(vars)
			ctx.StdoutTerminal = &c.tty
			ctx.ForceVoice = c.force
			ctx.Plain = c.plain || c.env["HOMEBOT_PLAIN"] != ""
			if got := plainOf(ctx, msg.InboxHeaderCount(2)); got != c.want {
				t.Errorf("Plain = %q, want %q", got, c.want)
			}
			if got := renderOf(ctx, msg.InboxHeaderCount(2)); got != c.want {
				t.Errorf("Render = %q, want %q", got, c.want)
			}
			if got := voice.Record(ctx, msg.InboxHeaderCount(2)); got != neutral {
				t.Errorf("Record = %q, want the neutral %q", got, neutral)
			}
		})
	}
}

// {bot} (N4): the speaker's name where the voice is in effect, homebot where
// it is not. Record is always homebot; Nudge uses the persona whatever stdout
// is, unless --plain or none.
func TestBotIsTheVoiceNameOrHomebot(t *testing.T) {
	// identity.warning.unknown is a persona entry whose only argument is {bot}.
	identity := msg.IdentityWarningUnknown()
	nudge := msg.InboxNudge(2, "")
	const (
		idTester  = "Git doesn't know who you are yet, and Tester commits as you."
		idNeutral = "Git doesn't know who you are yet, and homebot commits as you."
	)
	cases := []struct {
		name                       string
		persona                    string
		tty, plain                 bool
		wantRender, wantPlain      string
		wantNudge, wantRecord      string
		wantIdentity, wantIdRecord string
	}{
		{"terminal", "tester", true, false, testerNudge, testerNudge, testerNudge, neutralNudge, idTester, idNeutral},
		{"piped", "tester", false, false, neutralNudge, neutralNudge, testerNudge, neutralNudge, idNeutral, idNeutral},
		{"--plain", "tester", true, true, neutralNudge, neutralNudge, neutralNudge, neutralNudge, idNeutral, idNeutral},
		{"none", "none", true, false, neutralNudge, neutralNudge, neutralNudge, neutralNudge, idNeutral, idNeutral},
		{"none, piped", "none", false, false, neutralNudge, neutralNudge, neutralNudge, neutralNudge, idNeutral, idNeutral},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			box := testutil.Sandbox(t, testutil.Options{})
			writePersonaFile(t, box.Ctx, "tester", testerPersona)
			ctx := speaking(box, c.persona, c.tty)
			ctx.Plain = c.plain
			for _, g := range []struct{ what, got, want string }{
				{"Render nudge", renderOf(ctx, nudge), c.wantRender},
				{"Plain nudge", plainOf(ctx, nudge), c.wantPlain},
				{"Nudge", voice.Nudge(ctx, nudge, "k"), c.wantNudge},
				{"Record nudge", voice.Record(ctx, nudge), c.wantRecord},
				{"Render identity", renderOf(ctx, identity), c.wantIdentity},
				{"Plain identity", plainOf(ctx, identity), c.wantIdentity},
				{"Record identity", voice.Record(ctx, identity), c.wantIdRecord},
			} {
				if g.got != g.want {
					t.Errorf("%s = %q, want %q", g.what, g.got, g.want)
				}
			}
		})
	}
}

// warning is the one line voice prints for an unusable HOMEBOT_PERSONA.
func warning(id string) string {
	return "Persona " + id + " could not be loaded; using Neatpik. Run homebot persona validate " + id + ".\n"
}

// An unusable HOMEBOT_PERSONA warns exactly once per process (S4, I1): not
// again when ctx.Config changes (as LocateRoot does), nor on a Fork. A
// piped run says nothing. A broken file the user did not name in the
// environment (their neatpik.toml, chosen by default) never warns.
func TestUnusablePersonaWarnsOnce(t *testing.T) {
	header := msg.InboxHeaderCount(2)
	nudge := msg.InboxNudge(2, "")

	t.Run("terminal: one line across a Config change and a Fork", func(t *testing.T) {
		for _, c := range []struct{ name, id, quoted string }{
			{"missing", "nosuch", `"nosuch"`},
			{"invalid id", "Bad Id!", `"Bad Id!"`},
			{"broken file", "broken", `"broken"`},
			{"unsafe id", "bad\x1b[31mid", `"bad[31mid"`},
		} {
			t.Run(c.name, func(t *testing.T) {
				box := testutil.Sandbox(t, testutil.Options{})
				writePersonaFile(t, box.Ctx, "broken", "schema = [\n")
				ctx := speaking(box, c.id, true)
				want := warning(c.quoted)

				// The fallback is the embedded Neatpik, lines included (the
				// variant seed 1 picks), not the catalog's "2 things need you".
				if got := plainOf(ctx, header); got != "2 things are waiting for you" {
					t.Errorf("the fallback did not speak as the embedded Neatpik: %q", got)
				}
				if got := box.Stderr.String(); got != want {
					t.Fatalf("after the first voiced text, stderr = %q, want %q", got, want)
				}
				// LocateRoot sets ctx.Config; the persona resolves again.
				ctx.Config = &config.Config{}
				renderOf(ctx, header)
				renderOf(ctx, nudge)
				forked := ctx.Fork(nil)
				renderOf(forked, header)
				voice.Plain(forked, nudge)
				if got := box.Stderr.String(); got != want {
					t.Errorf("after a Config change and a Fork, stderr = %q, want the one line %q", got, want)
				}
				if got := box.Stdout.String(); got != "" {
					t.Errorf("the warning went to stdout: %q", got)
				}
				if n := strings.Count(box.Stderr.String(), "\n"); n != 1 {
					t.Errorf("%d lines, want 1", n)
				}
			})
		}
	})

	t.Run("a Fork warns first when the original never spoke", func(t *testing.T) {
		box := testutil.Sandbox(t, testutil.Options{})
		ctx := speaking(box, "nosuch", true)
		fork := ctx.Fork(nil)
		renderOf(fork, header)
		renderOf(ctx, header)
		if got, want := box.Stderr.String(), warning(`"nosuch"`); got != want {
			t.Errorf("stderr = %q, want %q", got, want)
		}
	})

	t.Run("piped: nothing, even forced", func(t *testing.T) {
		for _, force := range []bool{false, true} {
			box := testutil.Sandbox(t, testutil.Options{})
			ctx := speaking(box, "nosuch", false)
			ctx.ForceVoice = force
			renderOf(ctx, header)
			plainOf(ctx, header)
			voice.Nudge(ctx, nudge, "k")
			if got := box.Stderr.String(); got != "" {
				t.Errorf("forced=%v: piped run warned: %q", force, got)
			}
		}
	})

	t.Run("the first terminal run after a piped one still warns", func(t *testing.T) {
		box := testutil.Sandbox(t, testutil.Options{})
		ctx := speaking(box, "nosuch", false)
		plainOf(ctx, header)
		if got := box.Stderr.String(); got != "" {
			t.Fatalf("piped run warned: %q", got)
		}
		yes := true
		ctx.StdoutTerminal = &yes
		plainOf(ctx, header)
		plainOf(ctx, header)
		if got, want := box.Stderr.String(), warning(`"nosuch"`); got != want {
			t.Errorf("stderr = %q, want %q", got, want)
		}
	})

	t.Run("nothing to say: unvoiced text, --plain, none, a usable persona", func(t *testing.T) {
		for _, c := range []struct {
			name  string
			id    string
			plain bool
			text  msg.Text
		}{
			{"unvoiced text", "nosuch", false, msg.FormatAgoHours(3)},
			{"--plain", "nosuch", true, header},
			{"none", "none", false, header},
			{"a usable persona", "tester", false, header},
		} {
			t.Run(c.name, func(t *testing.T) {
				if c.name == "unvoiced text" && c.text.Persona() {
					t.Fatalf("premise: %s is voiced", c.text.ID())
				}
				box := testutil.Sandbox(t, testutil.Options{})
				writePersonaFile(t, box.Ctx, "tester", testerPersona)
				ctx := speaking(box, c.id, true)
				ctx.Plain = c.plain
				renderOf(ctx, c.text)
				plainOf(ctx, c.text)
				if got := box.Stderr.String(); got != "" {
					t.Errorf("stderr = %q, want none", got)
				}
			})
		}
	})

	t.Run("a broken neatpik.toml chosen by default never warns", func(t *testing.T) {
		box := testutil.Sandbox(t, testutil.Options{})
		writePersonaFile(t, box.Ctx, "neatpik", "schema = [\n")
		for _, id := range []string{"", "tester"} {
			writePersonaFile(t, box.Ctx, "tester", testerPersona)
			ctx := speaking(box, id, true)
			got := renderOf(ctx, nudge)
			// The default is unusable here, so the voice is the fallback: the
			// embedded Neatpik, lines included (the variant seed 1 picks).
			want := neatpikNudge
			if id == "tester" {
				want = testerNudge
			}
			if got != want {
				t.Errorf("HOMEBOT_PERSONA=%q: Render = %q, want %q", id, got, want)
			}
			ctx.Config = &config.Config{}
			renderOf(ctx, nudge)
			renderOf(ctx.Fork(nil), nudge)
			if got := box.Stderr.String(); got != "" {
				t.Errorf("HOMEBOT_PERSONA=%q: stderr = %q, want none: the user did not name it", id, got)
			}
		}
	})

	t.Run("the same broken neatpik.toml, named in the environment, warns once", func(t *testing.T) {
		box := testutil.Sandbox(t, testutil.Options{})
		writePersonaFile(t, box.Ctx, "neatpik", "schema = [\n")
		ctx := speaking(box, "neatpik", true)
		renderOf(ctx, nudge)
		ctx.Config = &config.Config{}
		renderOf(ctx, nudge)
		if got, want := box.Stderr.String(), warning(`"neatpik"`); got != want {
			t.Errorf("stderr = %q, want %q", got, want)
		}
	})

	t.Run("a persona named by homebot.yml that is unusable does not warn", func(t *testing.T) {
		box := testutil.Sandbox(t, testutil.Options{})
		ctx := speaking(box, "", true)
		ctx.Config = &config.Config{Persona: "nosuch"}
		renderOf(ctx, nudge)
		if got := box.Stderr.String(); got != "" {
			t.Errorf("stderr = %q: only HOMEBOT_PERSONA warns", got)
		}
	})
}

// Nudge on text that is not voiced says it neutral without resolving a
// persona. The persona is memoized at its first resolution, so a file that
// changes after the Nudge shows whether the Nudge resolved one: a later voiced
// text must read the new file.
func TestNudgeDoesNotResolveAVoiceForUnvoicedText(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	writePersonaFile(t, box.Ctx, "tester", testerPersona)
	ctx := speaking(box, "tester", true)

	unvoiced := msg.RemoteRepoDescription()
	if unvoiced.Persona() {
		t.Fatal("the fixture text is voiced; the test would prove nothing")
	}
	want := voice.Record(ctx, unvoiced)
	for _, key := range []string{"", "a|1", "b|2"} {
		if got := voice.Nudge(ctx, unvoiced, key); got != want {
			t.Errorf("Nudge(%q) = %q, want %q", key, got, want)
		}
	}
	writePersonaFile(t, box.Ctx, "tester", strings.ReplaceAll(testerPersona, "(Tester)", "(Second)"))
	if got, want := plainOf(ctx, msg.InboxHeaderCount(2)), "2 notes (Second)"; got != want {
		t.Errorf("voiced text after Nudge = %q, want %q: Nudge resolved the persona for text that is not voiced", got, want)
	}
	// Voiced text still resolves, and does so once.
	if got := voice.Nudge(ctx, msg.InboxNudge(2, ""), "k"); got != testerNudge {
		t.Errorf("voiced Nudge = %q, want %q", got, testerNudge)
	}
}
