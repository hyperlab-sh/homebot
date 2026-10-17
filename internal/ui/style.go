package ui

import (
	"image/color"
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/persona"
)

// Style styles text. Unlike lipgloss.Style.Render it keeps the text's
// layout: each line is styled on its own, without padding lines to one width,
// empty lines stay empty and tabs stay tabs. A span styled inside the text
// does not end the style around it.
type Style struct {
	s lipgloss.Style
	// open is the sequence that starts s's span.
	open string
}

func newStyle() Style {
	return style(lipgloss.NewStyle().TabWidth(lipgloss.NoTabConversion))
}

func style(s lipgloss.Style) Style {
	r := s.Render("x")
	return Style{s: s, open: r[:strings.Index(r, "x")]}
}

func (s Style) bold() Style                    { return style(s.s.Bold(true)) }
func (s Style) faint() Style                   { return style(s.s.Faint(true)) }
func (s Style) foreground(c color.Color) Style { return style(s.s.Foreground(c)) }

// Render returns text with the style applied. Styles render in full colour;
// the writers Out and Err bring them down to what the terminal shows.
func (s Style) Render(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = s.s.Render(s.reopen(line))
		}
	}
	return strings.Join(lines, "\n")
}

// reset ends a span: lipgloss writes the short form.
var reset = regexp.MustCompile(`\x1b\[0?m`)

// reopen starts s again after each reset in line that has text after it,
// so an inner span closing does not end s.
func (s Style) reopen(line string) string {
	if s.open == "" {
		return line
	}
	var b strings.Builder
	last := 0
	for _, loc := range reset.FindAllStringIndex(line, -1) {
		b.WriteString(line[last:loc[1]])
		if loc[1] < len(line) {
			b.WriteString(s.open)
		}
		last = loc[1]
	}
	b.WriteString(line[last:])
	return b.String()
}

// Palette is the styles homebot writes with, named by the tokens of plan
// 6.11. Accent, OK and Muted are a persona's; the neutral voice keeps
// src/ui.ts's bold, green and dim. The others are fixed.
type Palette struct {
	// Accent is for headings and labels (ui.ts bold).
	Accent Style
	// OK is for something done (ui.ts green).
	OK Style
	// Muted is for secondary text (ui.ts dim).
	Muted Style
	// Urgent marks urgent inbox items and the urgent count.
	Urgent Style
	// Todo is for what needs attention but is not broken (ui.ts yellow).
	Todo Style
	// Command is a command to run (ui.ts cyan).
	Command Style
	// Error is for failures; it looks like Urgent.
	Error Style
	// Notice is the unfold label, the one use of blue.
	Notice Style
	// Path is for paths in messages: bold, without colour.
	Path Style
	// Count is for counts in messages: bold.
	Count Style
	// Alarm is the commit guard's headline: Urgent, bold.
	Alarm Style
}

// The fixed colours of plan 6.11, light and dark.
var (
	urgentLight, urgentDark   = lipgloss.Color("#C62828"), lipgloss.Color("#FF6B6B")
	todoLight, todoDark       = lipgloss.Color("#A16207"), lipgloss.Color("#FACC15")
	commandLight, commandDark = lipgloss.Color("#0E7490"), lipgloss.Color("#67E8F9")
)

// paletteKey is Styles' memo key: one palette per voice in effect.
type paletteKey struct{ voice *persona.Voice }

// Styles returns the palette for ctx's terminal, light or dark, in the voice
// in effect (persona.Speaking).
//
// It is memoized per persona.Speaking's answer, in the load order of plan
// S1: a palette worked out before LocateRoot, when only HOMEBOT_PERSONA or
// neatpik can be speaking, is worked out again once homebot.yml is loaded
// and names another persona, as persona.Active is. Help pages call it too,
// so on a terminal a help page resolves the persona for its palette; help
// never locates the root, so that palette comes from HOMEBOT_PERSONA or
// neatpik.
func Styles(ctx *app.Context) Palette {
	key := paletteKeyOf(ctx)
	return app.Once(ctx, key, func() Palette { return palette(isDark(ctx), key.voice, ctx.Env.Get("NO_COLOR") != "") })
}

// NeutralStyles provides the fixed palette for a catalog entry that cannot
// use a persona or its name. It does not resolve the active persona.
func NeutralStyles(ctx *app.Context) Palette {
	return app.Once(ctx, paletteKey{}, func() Palette {
		return palette(isDark(ctx), nil, ctx.Env.Get("NO_COLOR") != "")
	})
}

func paletteKeyOf(ctx *app.Context) paletteKey { return paletteKey{persona.Speaking(ctx)} }

// ErrStyles is the palette for text printed to Err. When Err shows no
// colour, the terminal is not asked for its background: the colours would
// not show, so the dark palette stands in, its bold and faint intact.
func ErrStyles(ctx *app.Context) Palette {
	if writer(ctx, stderrStream).Profile <= colorprofile.ASCII {
		return palette(true, persona.Speaking(ctx), ctx.Env.Get("NO_COLOR") != "")
	}
	return Styles(ctx)
}

func palette(dark bool, voice *persona.Voice, noColor bool) Palette {
	pick := lipgloss.LightDark(dark)
	urgent := newStyle().foreground(pick(urgentLight, urgentDark))
	p := Palette{
		Accent:  newStyle().bold(),
		OK:      newStyle().foreground(lipgloss.Green),
		Muted:   newStyle().faint(),
		Urgent:  urgent,
		Todo:    newStyle().foreground(pick(todoLight, todoDark)),
		Command: newStyle().foreground(pick(commandLight, commandDark)),
		Error:   urgent,
		Notice:  newStyle().foreground(lipgloss.Blue),
		Path:    newStyle().bold(),
		Count:   newStyle().bold(),
		Alarm:   urgent.bold(),
	}
	if voice != nil {
		p.Accent = personaStyle(p.Accent, voice.Colors.Accent, dark, noColor)
		p.OK = personaStyle(p.OK, voice.Colors.OK, dark, noColor)
		p.Muted = personaStyle(p.Muted, voice.Colors.Muted, dark, noColor)
	}
	return p
}

// personaStyle is base with a persona's colour for it, or base when the
// persona sets none. Under NO_COLOR a persona colour leaves no escape at all,
// bold included (plan M2: "the persona's text appears with no escape
// sequence"), while the neutral palette keeps its bold and faint, as NO_COLOR
// allows (terminal.go). The difference is the spec's: a persona's accent is
// its colour, and with the colour off nothing is left of it.
func personaStyle(base Style, pair persona.Pair, dark, noColor bool) Style {
	value := pair.Light
	if dark {
		value = pair.Dark
	}
	if value == "" {
		return base
	}
	if noColor {
		return newStyle()
	}
	return newStyle().foreground(lipgloss.Color(value))
}
