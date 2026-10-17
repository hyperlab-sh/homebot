package cli

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/inbox"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/msg/catalog"
	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// personaSaveEvents are the sample lines whose only slots are counts, so they
// read as a real run does ("committed 3 files") and not as a template
// ("See: homebot <command_space>--help"). preview and show both use them.
var personaSaveEvents = []string{"save.success.committed", "save.success.pulled", "save.success.pushed"}

// personaShowEvents are the four lines persona show says in the persona's
// voice (plan Task 7, persona show): the save results preview shows, and the
// empty inbox.
var personaShowEvents = append(slices.Clone(personaSaveEvents), "next.success.nothing")

func personaShowSamples(c *app.Context, id string) error {
	fork := c.Fork(map[string]string{"HOMEBOT_PERSONA": id})
	fork.ForceVoice = true
	for _, eventID := range personaShowEvents {
		meta, ok := msg.Lookup(eventID)
		if !ok {
			continue
		}
		t, err := samplePersonaText(fork, meta)
		if err != nil {
			return err
		}
		ui.Println(c, voice.Render(fork, t))
	}
	return nil
}

// previewUrgentTitle is the title of the urgent item the preview inbox shows.
const previewUrgentTitle = "A key needs attention"

func personaPreview(c *app.Context, id, event string, all bool) error {
	if !persona.ValidID(id) || id == persona.None {
		return personaInvalidID(c, id)
	}
	if err := personaMissing(c, id); err != nil {
		return err
	}
	v, ds, err := persona.Load(c, id)
	personaDiagnostics(c, ds)
	if err != nil {
		return fail.User(voice.Plain(c, msg.PersonaErrorUnusable(msg.StripUnsafe(id))))
	}
	if event != "" {
		meta, ok := msg.Lookup(event)
		if !ok || !meta.Persona {
			return fail.User(voice.Plain(c, msg.PersonaErrorEvent(msg.StripUnsafe(event))))
		}
		for _, theme := range []string{"light", "dark"} {
			fork := personaPreviewFork(c, id, theme)
			ui.Println(c, voice.Render(c, msg.PersonaLabelTheme(theme)))
			if err := previewEvent(c, fork, v, meta, all); err != nil {
				return err
			}
		}
		return nil
	}
	for _, theme := range []string{"light", "dark"} {
		fork := personaPreviewFork(c, id, theme)
		ui.Println(c, voice.Render(c, msg.PersonaLabelTheme(theme)))
		items := []inbox.Item{
			{ID: "preview.urgent", Level: inbox.Urgent, Title: previewUrgentTitle, Fix: &inbox.Fix{Args: []string{"keys", "init"}}},
			{ID: "preview.todo.1", Level: inbox.Todo, Title: "A file needs a link"},
			{ID: "preview.todo.2", Level: inbox.Todo, Title: "A save is waiting"},
		}
		ui.Println(c, inbox.Render(fork, items, nil, true))
		mark, nudge := inbox.NudgeLine(fork, items)
		ui.Println(c, mark+" "+nudge)
		title, body := inbox.NotificationText(fork, items)
		ui.Println(c, title)
		ui.Println(c, body)
		for _, mid := range personaSaveEvents {
			meta, ok := msg.Lookup(mid)
			if !ok {
				continue
			}
			t, buildErr := samplePersonaText(fork, meta)
			if buildErr != nil {
				return buildErr
			}
			ui.Println(c, voice.Render(fork, t))
		}
		if all {
			for _, meta := range msg.Entries() {
				if !meta.Persona || len(v.Lines[meta.ID]) < 2 {
					continue
				}
				if err := previewEvent(c, fork, v, meta, true); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func personaPreviewFork(c *app.Context, id, theme string) *app.Context {
	fork := c.Fork(map[string]string{"HOMEBOT_PERSONA": id, "HOMEBOT_THEME": theme})
	fork.Host = "laptop"
	fork.ForceVoice = true
	return fork
}

func previewEvent(c, fork *app.Context, v *persona.Voice, meta msg.Meta, all bool) error {
	t, err := samplePersonaText(fork, meta)
	if err != nil {
		return err
	}
	if !all || len(v.Lines[meta.ID]) == 0 {
		ui.Println(c, voice.Render(c, msg.PersonaLabelVariant(meta.ID, 1, voice.Render(fork, t))))
		return nil
	}
	for i, line := range v.Lines[meta.ID] {
		ui.Println(c, voice.Render(c, msg.PersonaLabelVariant(meta.ID, i+1, voice.PreviewVariant(fork, v, t, line))))
	}
	return nil
}

// samplePersonaText is the message meta says, with the sample value of every
// slot the catalog gives one. c is the voice the sample will be said in: a
// slot that the inbox fills with another message is filled with that message
// as it would say it, and not with the slot's meaning.
func samplePersonaText(c *app.Context, meta msg.Meta) (msg.Text, error) {
	values := map[string]any{}
	for _, slot := range meta.Slots {
		if slot.Name == catalog.Bot || slot.Name == catalog.Self || slot.Value != "" {
			continue
		}
		if v, ok := inboxSlotSample(c, meta.ID, slot.Name, slot.Meaning); ok {
			values[slot.Name] = v
			continue
		}
		sample := slot.Sample
		if sample == "" {
			sample = slot.Meaning
		}
		switch slot.Type {
		case catalog.Int:
			n, err := strconv.Atoi(sample)
			if err != nil {
				n = 2
			}
			values[slot.Name] = n
		case catalog.Duration:
			values[slot.Name] = sample
		default:
			values[slot.Name] = strings.ReplaceAll(sample, "\n", " ")
		}
	}
	t, err := msg.Build(meta.ID, values)
	if err != nil {
		return msg.Text{}, fmt.Errorf("persona preview %s: %w", meta.ID, err)
	}
	return t, nil
}

// inboxSlotSample is the value the inbox gives slot of the message id, for
// the slots it fills with another message. Left to the generic sample they
// read as "3 things need you<urgent>".
//
// The nudge's urgency suffix and the notification's item title and "+N more"
// are filled for the preview items (one urgent of three): one urgent, a
// title, and the two items after the first. Any other slot whose meaning is
// "another message id, or empty" (see subMessageOf) gets that message, said
// in the voice c speaks and filled with its own samples, so the next such slot
// needs no case here. ok is false for every other slot.
func inboxSlotSample(c *app.Context, id, slot, meaning string) (value string, ok bool) {
	switch {
	case id == "inbox.nudge" && slot == "urgent":
		return voice.Nudge(c, msg.InboxNudgeUrgent(1), ""), true
	case id == "inbox.notify.body" && slot == "title":
		return previewUrgentTitle, true
	case id == "inbox.notify.body" && slot == "more":
		return voice.Nudge(c, msg.InboxNotifyMore(2), ""), true
	}
	sub, ok := subMessageOf(meaning)
	if !ok {
		return "", false
	}
	t, err := samplePersonaText(c, sub)
	if err != nil {
		return "", false
	}
	return voice.Nudge(c, t, ""), true
}

// subMessageOf is the message a slot is filled with when its meaning starts
// with a catalog id and ends in "or empty" or "else empty": for example
// "inbox.nudge.urgent or empty" or "inbox.never_unpacked.last when another
// profile was unpacked, else empty". The inbox fills such a slot with that
// message, or with nothing.
func subMessageOf(meaning string) (msg.Meta, bool) {
	first, _, _ := strings.Cut(meaning, " ")
	if !strings.HasSuffix(meaning, " or empty") && !strings.HasSuffix(meaning, " else empty") {
		return msg.Meta{}, false
	}
	return msg.Lookup(first)
}
