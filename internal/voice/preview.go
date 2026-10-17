package voice

import (
	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/selfname"
)

// PreviewVariant renders one validated variant with the same message formatter
// as Render. Plain still wins, and a rejected line falls back to catalog text.
func PreviewVariant(ctx *app.Context, v *persona.Voice, t msg.Text, line persona.Line) string {
	if ctx.Plain || v == nil {
		return Render(ctx, t)
	}
	return msg.SpeakWithSelf(variantSpeaker{name: v.Name, text: line.Text, lang: line.Lang}, t, selfname.Effective(ctx), nil)
}

type variantSpeaker struct {
	name, text string
	lang       language.Tag
}

func (s variantSpeaker) Name() string                             { return s.name }
func (s variantSpeaker) Line(string) (string, language.Tag, bool) { return s.text, s.lang, true }
