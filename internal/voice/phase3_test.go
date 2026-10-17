package voice

import (
	"testing"

	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// named is the neutral voice under another name, so {bot} shows who speaks.
type named struct{ msg.Neutral }

func (named) Name() string { return "Tester" }

func TestRecordIsNeutralWhateverTheVoice(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	SpeakAs(t, named{})
	text := msg.InboxNudge(2, "")
	if got, want := Plain(ctx, text), "Tester: 2 things need you, run: homebot"; got != want {
		t.Fatalf("Plain = %q, want %q (the test speaker is not speaking)", got, want)
	}
	want := "homebot: 2 things need you, run: homebot"
	if got := Record(ctx, text); got != want {
		t.Errorf("Record = %q, want %q", got, want)
	}
	ctx.Plain = true
	if got := Record(ctx, text); got != want {
		t.Errorf("Record with --plain = %q, want %q", got, want)
	}
}

func TestRecordIsUnstyled(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	text := msg.InboxNudge(3, "")
	if got := Record(ctx, text); got != render(Neutral{}, text) {
		t.Errorf("Record = %q, want the neutral text without styles", got)
	}
}

// The stub: neutral until the voice engine speaks as personas.
func TestNudgeIsNeutralForNow(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	text := msg.InboxNudge(1, "")
	for _, key := range []string{"", "a,b|1"} {
		if got, want := Nudge(ctx, text, key), "homebot: 1 thing needs you, run: homebot"; got != want {
			t.Errorf("Nudge(%q) = %q, want %q", key, got, want)
		}
	}
}
