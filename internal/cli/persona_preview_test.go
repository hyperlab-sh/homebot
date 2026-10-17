package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func TestEveryVoicedEventHasAPreviewSample(t *testing.T) {
	b := personaBox(t)
	fork := personaPreviewFork(b.Ctx, "neatpik", "light")
	for _, meta := range msg.Entries() {
		if !meta.Persona {
			continue
		}
		t.Run(meta.ID, func(t *testing.T) {
			text, err := samplePersonaText(fork, meta)
			if err != nil {
				t.Fatal(err)
			}
			if got := voice.Render(fork, text); got == "" {
				t.Error("empty sample")
			}
		})
	}
}

// A slot the inbox fills with another message is shown as that message, not
// as the generic sample of its type: "3 things need you<urgent>" was what
// persona preview --event inbox.nudge printed.
func TestPreviewFillsTheSlotsTheInboxFills(t *testing.T) {
	b := personaBox(t)
	for _, tc := range []struct {
		event string
		want  string
	}{
		// Neatpik's first variant; the point is the filled slots, not the wording.
		{"inbox.nudge", "3 things are waiting (1 urgent), run: homebot"},
		{"inbox.notify.body", "A key needs attention (+2 more). Run: homebot"},
	} {
		fork := personaPreviewFork(b.Ctx, "neatpik", "light")
		meta, ok := msg.Lookup(tc.event)
		if !ok {
			t.Fatalf("no message %s", tc.event)
		}
		text, err := samplePersonaText(fork, meta)
		if err != nil {
			t.Fatal(err)
		}
		got := ansi.Strip(voice.Render(fork, text))
		if !strings.Contains(got, tc.want) || strings.Contains(got, "<") {
			t.Errorf("%s = %q, want it to contain %q and no placeholder", tc.event, got, tc.want)
		}
		code, out, stderr := runPersona(b, "preview", "neatpik", "--event", tc.event)
		if code != 0 || stderr != "" || strings.Contains(out, "<") || !strings.Contains(out, tc.want) {
			t.Errorf("preview --event %s: %d %q %q", tc.event, code, out, stderr)
		}
	}
}

// Every slot whose meaning is "another message id, or empty" is filled the
// way the inbox fills it: the preview of a message with such a slot never
// shows the slot raw (as in "was never unpacked here<last>").
func TestPreviewFillsEverySubMessageSlot(t *testing.T) {
	b := personaBox(t)
	checked := 0
	for _, meta := range msg.Entries() {
		if !meta.Persona {
			continue
		}
		for _, slot := range meta.Slots {
			if !namesAMessageOrEmpty(slot.Meaning) {
				continue
			}
			checked++
			code, out, stderr := runPersona(b, "preview", "neatpik", "--event", meta.ID)
			if code != 0 || stderr != "" {
				t.Errorf("preview --event %s: %d %q", meta.ID, code, stderr)
				continue
			}
			if raw := "<" + slot.Name + ">"; strings.Contains(out, raw) {
				t.Errorf("preview --event %s shows %s raw: %q", meta.ID, raw, out)
			}
		}
	}
	if checked < 4 {
		t.Fatalf("only %d slots name another message and are voiced; the sweep lost its subject", checked)
	}
}

// namesAMessageOrEmpty is the catalog's way of saying a slot is filled with
// another message or with nothing: a meaning that names a catalog id and
// ends in "empty". It is deliberately not the matcher the preview uses.
func namesAMessageOrEmpty(meaning string) bool {
	if !strings.HasSuffix(meaning, "empty") {
		return false
	}
	for _, word := range strings.FieldsFunc(meaning, func(r rune) bool { return r == ' ' || r == ',' }) {
		if _, ok := msg.Lookup(word); ok {
			return true
		}
	}
	return false
}
