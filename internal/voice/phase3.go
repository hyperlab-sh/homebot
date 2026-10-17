package voice

import (
	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/selfname"
)

// Record is t in the neutral voice, unstyled, whatever ctx's persona: for
// text that is stored or sent away (the journal, state, files written into
// the home repo, commit messages, anything sent to a forge; plan S10). {bot}
// is homebot.
func Record(_ *app.Context, t msg.Text) string {
	return render(Neutral{}, t)
}

// Nudge is t for the nudge cache and notifications: in the persona whatever
// stdout is (persona.NudgeVoice), with a stable pick among its variants
// keyed by key, so the line changes only when the inbox does (plan S6).
// Text that is not voiced is said neutral without resolving a persona.
// Printed commands take the effective name, as in Render.
func Nudge(ctx *app.Context, t msg.Text, key string) string {
	self := selfname.Effective(ctx)
	if !voiced(t) {
		// Not voiced: the same words in every voice. Do not resolve one.
		return renderWithSelf(Neutral{}, t, self, nil)
	}
	v := persona.NudgeVoice(ctx)
	if v == nil {
		return renderWithSelf(Neutral{}, t, self, nil)
	}
	return renderWithSelf(stableSpeaker{voice: v, key: key}, t, self, nil)
}
