package voice_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/actions"
	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/unpack"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func TestErrorText(t *testing.T) {
	voice.SpeakAs(t, tester{})
	said := fail.Say(msg.ConfigErrorProfiles("homebot.yml:2:1"))
	stepFailed := func(inner string) msg.Text { return msg.UnpackErrorStepFailed(1, inner) }
	sb := testutil.Sandbox(t, testutil.Options{})
	for _, c := range []struct {
		name string
		err  error
		want string
	}{
		{"said", said, "homebot.yml:2:1: profiles must be a mapping. Not that it matters."},
		{"said, then wrapped", fmt.Errorf("loading: %w", said), "loading: homebot.yml:2:1: profiles must be a mapping. Not that it matters."},
		{"said, then the tool's lines", fmt.Errorf("%w%s", said, "\n  git: boom"), "homebot.yml:2:1: profiles must be a mapping. Not that it matters.\n  git: boom"},
		{"said, no line in the persona", fail.Say(msg.ConfigErrorTriage("homebot.yml:5:1")), "homebot.yml:5:1: triage must be a mapping"},
		{"already said", fail.User("it was said"), "it was said"},
		{"a user error edited after it was said", func() error { e := fail.Say(msg.ConfigErrorProfiles("x")); e.Msg = "edited"; return e }(), "edited"},
		{"not a user error", errors.New("plain"), "plain"},
		{"nested, the inner error said", voice.Nest(fmt.Errorf("saidfail: %w", said), stepFailed, "\nhint"), "Step 1 failed: saidfail: homebot.yml:2:1: profiles must be a mapping. Not that it matters.\nhint"},
		{"nested, then wrapped", fmt.Errorf("unpack: %w", voice.Nest(said, stepFailed, "\nhint")), "unpack: Step 1 failed: homebot.yml:2:1: profiles must be a mapping. Not that it matters.\nhint"},
		{"nested, a plain inner error", voice.Nest(errors.New("plain"), stepFailed, ""), "Step 1 failed: plain"},
		{"nested, a nil inner error", voice.Nest(nil, stepFailed, "\nhint"), "Step 1 failed: \nhint"},
		{"attached, the inner error said", voice.Attach(said, msg.SecretsErrorEncrypt("a"), "\n  "), "Could not encrypt private/a.\n  homebot.yml:2:1: profiles must be a mapping. Not that it matters."},
		{"attached, nothing to attach", voice.Attach(nil, msg.SecretsErrorEncrypt("a"), "\n  "), "Could not encrypt private/a."},
		{"said with, texts of its own inside", sayWithReason("homebot.yml:2:1"), "Step 1 failed: homebot.yml:2:1: profiles must be a mapping. Not that it matters."},
		{"said with, then wrapped", fmt.Errorf("unpack: %w", sayWithReason("homebot.yml:2:1")), "unpack: Step 1 failed: homebot.yml:2:1: profiles must be a mapping. Not that it matters."},
		{"said with, no line in the persona", voice.SayWith(func(say func(msg.Text) string) msg.Text {
			return msg.UnpackErrorStepFailed(1, say(msg.ConfigErrorTriage("homebot.yml:5:1")))
		}), "Step 1 failed: homebot.yml:5:1: triage must be a mapping"},
		{"nested twice", voice.Nest(voice.Nest(said, stepFailed, "\ninner hint"), stepFailed, "\nouter hint"), "Step 1 failed: Step 1 failed: homebot.yml:2:1: profiles must be a mapping. Not that it matters.\ninner hint\nouter hint"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := voice.ErrorText(sb.Ctx, c.err); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// sayWithReason is an error whose message holds a text of its own, as the link
// action's layout error holds the planner's reasons.
func sayWithReason(file string) error {
	return voice.SayWith(func(say func(msg.Text) string) msg.Text {
		return msg.UnpackErrorStepFailed(1, say(msg.ConfigErrorProfiles(file)))
	})
}

// SayWith is stored neutral, is still a user error, and builds its message
// once for each form.
func TestSayWithStoresTheEnglishAndIsAUserError(t *testing.T) {
	voice.SpeakAs(t, tester{})
	err := sayWithReason("homebot.yml:2:1")
	if got, want := err.Error(), "Step 1 failed: homebot.yml:2:1: profiles must be a mapping"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	var ue *fail.UserError
	if !errors.As(err, &ue) {
		t.Fatal("no user error in the chain")
	}
	if ue.Msg != err.Error() {
		t.Errorf("user error message = %q, want %q", ue.Msg, err.Error())
	}

	builds := 0
	counted := voice.SayWith(func(say func(msg.Text) string) msg.Text {
		builds++
		return msg.UnpackErrorStepFailed(1, say(msg.ConfigErrorProfiles("f")))
	})
	if builds != 1 {
		t.Errorf("built %d times by SayWith itself, want once for the stored form", builds)
	}
	sb := testutil.Sandbox(t, testutil.Options{})
	_ = counted.Error()
	_ = voice.ErrorText(sb.Ctx, counted)
	if builds != 2 {
		t.Errorf("built %d times after Error() and ErrorText(), want 2", builds)
	}
}

// Whatever the voice, the stored form of a Nest or Attach error is the
// English, inner error included; only ErrorText says it in the voice.
func TestNestedErrorsStoreTheEnglish(t *testing.T) {
	voice.SpeakAs(t, tester{})
	said := fail.Say(msg.ConfigErrorProfiles("homebot.yml:2:1"))
	for _, c := range []struct {
		name        string
		err         error
		want, first string
	}{
		{"nest", voice.Nest(said, func(inner string) msg.Text { return msg.UnpackErrorStepFailed(1, inner) }, "\nhint"), "Step 1 failed: homebot.yml:2:1: profiles must be a mapping\nhint", "Step 1 failed: "},
		{"attach", voice.Attach(said, msg.SecretsErrorEncrypt("a"), "\n  "), "Could not encrypt private/a.\n  homebot.yml:2:1: profiles must be a mapping", "Could not encrypt private/a."},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := c.err.Error(); got != c.want {
				t.Errorf("Error() = %q, want %q", got, c.want)
			}
			var ue *fail.UserError
			if !errors.As(c.err, &ue) {
				t.Fatal("no user error in the chain")
			}
			// errors.As finds the wrapper's own message before the inner error's.
			if !strings.HasPrefix(ue.Msg, c.first) {
				t.Errorf("first user error = %q, want the wrapper's own message %q first", ue.Msg, c.first)
			}
		})
	}
}

// registerTestAction stubs the internal action name until the test ends.
func registerTestAction(t *testing.T, name string, run actions.InternalFunc) {
	t.Helper()
	actions.StubInternal(t, name, msg.ActionRunSummary(), run)
}

// An internal action's error is said in the voice of the Context, inside
// "Step N failed:".
func TestUnpackSaysAStepsSayErrorInThePersonasVoice(t *testing.T) {
	registerTestAction(t, "saidfail", func(*app.Context, []string) error {
		return fmt.Errorf("saidfail: %w", fail.Say(msg.ConfigErrorProfiles("homebot.yml:2:1")))
	})
	resume := "\nFix it, then resume:\n  homebot unpack workstation --from 1"
	for _, c := range []struct {
		name  string
		voice voice.Persona
		want  string
	}{
		{"neutral", voice.Neutral{}, "Step 1 failed: saidfail: homebot.yml:2:1: profiles must be a mapping" + resume},
		{"persona", tester{}, "Step 1 failed: saidfail: homebot.yml:2:1: profiles must be a mapping. Not that it matters." + resume},
	} {
		t.Run(c.name, func(t *testing.T) {
			voice.SpeakAs(t, c.voice)
			sb := testutil.Sandbox(t, testutil.Options{})
			sb.Files(map[string]string{"home/.home/manifests/profiles/workstation.yml": "steps:\n  - saidfail\n"})
			err := unpack.Run(sb.Ctx, "", unpack.Options{})
			var ue *fail.UserError
			if !errors.As(err, &ue) {
				t.Fatalf("err = %v", err)
			}
			// What is stored is neutral, whoever the voice is: the hint and
			// the step's own words are the English.
			neutral := "Step 1 failed: saidfail: homebot.yml:2:1: profiles must be a mapping" + resume
			if got := err.Error(); got != neutral {
				t.Errorf("stored error = %q, want %q", got, neutral)
			}
			// What is printed says the nested error in the voice too.
			if got := voice.ErrorText(sb.Ctx, err); got != c.want {
				t.Errorf("printed error = %q, want %q", got, c.want)
			}
		})
	}
}
