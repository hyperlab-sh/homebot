package msg_test

import (
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/msg"
)

func TestSelfFillsTextAppendAndFixedValue(t *testing.T) {
	for _, tc := range []struct {
		name string
		text msg.Text
		want string
	}{
		{"text", msg.HelpNextUsage(), "zork next"},
		{"append", msg.InboxNextHint(), "zork next"},
		{"fixed value", msg.ActionLinkWarningInWay(3), "zork link"},
		{"inventory title", msg.InventoryTitle("host", "linux", "/tmp"), "zork inventory"},
	} {
		got := msg.SpeakWithSelf(msg.Neutral{}, tc.text, "zork", nil)
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q does not contain %q", tc.name, got, tc.want)
		}
		neutral := msg.Speak(msg.Neutral{}, tc.text, nil)
		if !strings.Contains(neutral, "homebot ") || strings.Contains(neutral, "zork") {
			t.Errorf("%s: stored text = %q", tc.name, neutral)
		}
	}
}

func TestSelfDoesNotExpandCallerValues(t *testing.T) {
	got := msg.SpeakWithSelf(msg.Neutral{}, msg.PackErrorMissing("{self}"), "zork", nil)
	if got != "{self} does not exist." {
		t.Errorf("caller string changed: %q", got)
	}
}

func TestBuildLeavesSelfForRenderer(t *testing.T) {
	line, err := msg.Build("help.next.usage", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := msg.SpeakWithSelf(msg.Neutral{}, line, "zork", nil); got != "zork next" {
		t.Errorf("rendered = %q", got)
	}
	if _, err := msg.Build("help.next.usage", map[string]any{"self": "fake"}); err == nil {
		t.Error("Build accepted a caller-supplied self")
	}
}
