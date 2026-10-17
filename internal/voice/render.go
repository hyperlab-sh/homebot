package voice

import (
	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/msg/catalog"
	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/selfname"
	"github.com/hyperlab-sh/homebot/internal/ui"
)

// Render is t as ctx's voice says it, for the terminal: counts are bold and
// paths in the path style (plan 6.9). Print it with ui.Println or
// ui.Eprintln, which fit the styles to the terminal or remove them.
func Render(ctx *app.Context, t msg.Text) string {
	p := voiceOf(ctx, t)
	var pal ui.Palette
	if voiced(t) {
		pal = ui.Styles(ctx)
	} else {
		pal = ui.NeutralStyles(ctx)
	}
	return renderWithSelf(p, t, selfname.Effective(ctx), &pal)
}

// Plain is t as ctx's voice says it without styles, for terminal text that
// does not need styling. Use Record for stored text and forge arguments;
// notifications and the nudge cache use Nudge.
func Plain(ctx *app.Context, t msg.Text) string {
	return render(voiceOf(ctx, t), t)
}

// Command prints a catalog command with the effective name and command style.
// The inner rendering is unstyled so formatting the whole command applies one
// consistent style instead of nesting terminal escapes.
func Command(ctx *app.Context, t msg.Text) string {
	line := renderWithSelf(voiceOf(ctx, t), t, selfname.Effective(ctx), nil)
	return ui.Command(ctx, Render(ctx, msg.FormatCommand(line)))
}

// voiceOf is the persona ctx speaks with: the neutral voice when ctx is
// Plain (--plain or HOMEBOT_PLAIN), its speaker otherwise.
func voiceOf(ctx *app.Context, t msg.Text) Persona {
	if ctx.Plain {
		return Neutral{}
	}
	if testSpeaker != nil {
		return testSpeaker(ctx)
	}
	if !voiced(t) {
		return Neutral{}
	}
	v := persona.Speaking(ctx)
	if v == nil {
		return Neutral{}
	}
	if ctx.StdoutIsTerminal() {
		if warning, ok := persona.TakeWarning(ctx); ok {
			// The warning is not voiced, so Plain says it neutral without
			// resolving a voice again.
			ui.Eprintln(ctx, Plain(ctx, warning))
		}
	}
	if !t.Persona() {
		return namedOnly{v.Name}
	}
	return rotatingSpeaker{voice: v, choices: choices(ctx)}
}

// voiced is whether t can be spoken in a persona's voice at all: it is a
// persona entry, or it carries {bot}. Text that is not voiced is the
// catalog's words in every voice, so nothing needs to resolve one for it.
func voiced(t msg.Text) bool { return t.Persona() || hasBot(t) }

// testSpeaker is set only by SpeakAs in tests of the renderer seam.
var testSpeaker func(*app.Context) Persona

func hasBot(t msg.Text) bool {
	m, ok := msg.Lookup(t.ID())
	if !ok {
		return false
	}
	for _, slot := range m.Slots {
		if slot.Name == catalog.Bot {
			return true
		}
	}
	return false
}

// render is t as p says it (msg.Speak). With a palette, int and path values
// are styled; nested messages, such as a duration, are not.
func render(p Persona, t msg.Text) string {
	return renderWithSelf(p, t, msg.NeutralName, nil)
}

func renderWithSelf(p Persona, t msg.Text, self string, pal *ui.Palette) string {
	return msg.SpeakWithSelf(p, t, self, styler(t, pal))
}

// styler styles t's values by their placeholder type: counts in the count
// style, paths in the path style, everything else as it is.
func styler(t msg.Text, pal *ui.Palette) func(name, value string) string {
	if pal == nil {
		return nil
	}
	types := map[string]catalog.Type{}
	for _, a := range t.Args() {
		types[a.Name] = a.Type
	}
	return func(name, value string) string {
		switch types[name] {
		case catalog.Int:
			return pal.Count.Render(value)
		case catalog.Path:
			return pal.Path.Render(value)
		}
		return value
	}
}
