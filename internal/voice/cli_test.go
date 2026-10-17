package voice_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/cli"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// tester is a persona with its own line for one config error.
type tester struct{}

func (tester) Name() string { return "Tester" }
func (tester) Line(id string) (string, language.Tag, bool) {
	if id == "config.error.profiles" {
		return "{file}: {profiles} must be a mapping. Not that it matters.", language.English, true
	}
	return "", language.Und, false
}

// A fail.Say error is built where there is no Context; the CLI says it in the
// Context's voice.
func TestTheCLISaysASayErrorInThePersonasVoice(t *testing.T) {
	voice.SpeakAs(t, tester{})
	said := fail.Say(msg.ConfigErrorProfiles("homebot.yml:2:1"))
	for _, c := range []struct {
		name string
		err  error
		want string
	}{
		{"said", said, "homebot: homebot.yml:2:1: profiles must be a mapping. Not that it matters.\n"},
		{"said, then wrapped", fmt.Errorf("loading: %w", said), "homebot: loading: homebot.yml:2:1: profiles must be a mapping. Not that it matters.\n"},
		// The persona has no line for it: the catalog's words.
		{"said, no line", fail.Say(msg.ConfigErrorTriage("homebot.yml:5:1")), "homebot: homebot.yml:5:1: triage must be a mapping\n"},
		// A User error was said when it was made.
		{"already said", fail.User("homebot.yml:2:1: profiles must be a mapping"), "homebot: homebot.yml:2:1: profiles must be a mapping\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			sb := testutil.Sandbox(t, testutil.Options{})
			cli.Report(sb.Ctx, c.err)
			if got := sb.Stderr.String(); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
	if want := "homebot.yml:2:1: profiles must be a mapping"; said.Error() != want {
		t.Errorf("Error() = %q, want the neutral %q", said.Error(), want)
	}
}

func TestAPlainContextSpeaksNeutrally(t *testing.T) {
	voice.SpeakAs(t, tester{})
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	text := msg.ConfigErrorProfiles("homebot.yml:2:1")
	if got := voice.Plain(ctx, text); got != "homebot.yml:2:1: profiles must be a mapping. Not that it matters." {
		t.Fatalf("premise: the persona does not speak: %q", got)
	}
	ctx.Plain = true
	for name, got := range map[string]string{"Plain": voice.Plain(ctx, text), "Render": ansi.Strip(voice.Render(ctx, text))} {
		if got != "homebot.yml:2:1: profiles must be a mapping" {
			t.Errorf("%s with Plain set = %q, want the neutral voice", name, got)
		}
	}
}

// SpeakAs swaps the voice of the whole process, so a parallel test must not
// use it: it panics, before or after t.Parallel, instead of racing.
func TestSpeakAsRefusesAParallelTest(t *testing.T) {
	panicked := func(f func()) (msg string) {
		defer func() {
			if r := recover(); r != nil {
				msg = fmt.Sprint(r)
			}
		}()
		f()
		return ""
	}
	t.Run("called from a parallel test", func(t *testing.T) {
		t.Parallel()
		got := panicked(func() { voice.SpeakAs(t, tester{}) })
		if !strings.Contains(got, "cannot be used in a parallel test") {
			t.Errorf("SpeakAs in a parallel test: panic %q, want the parallel refusal", got)
		}
		// The refused call changed nothing.
		ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
		if text := voice.Plain(ctx, msg.ConfigErrorProfiles("f")); strings.Contains(text, "Not that it matters") {
			t.Errorf("the refused SpeakAs still changed the voice: %q", text)
		}
	})
	t.Run("t.Parallel after SpeakAs", func(t *testing.T) {
		voice.SpeakAs(t, tester{})
		if got := panicked(t.Parallel); got == "" {
			t.Error("t.Parallel after SpeakAs did not panic")
		}
	})
	t.Run("serial use", func(t *testing.T) {
		if got := panicked(func() { voice.SpeakAs(t, tester{}) }); got != "" {
			t.Errorf("SpeakAs in a serial test panicked: %q", got)
		}
	})
}
