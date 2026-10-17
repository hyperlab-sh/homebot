package msg

import (
	"slices"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/msg/catalog"
)

// NeutralName is the speaker's name in the neutral voice: what {bot} becomes.
const NeutralName = "homebot"

// Text is one catalog message with the values of its placeholders, built by
// a generated accessor such as InboxHeaderCount. The zero Text is empty. Turn
// a Text into words with voice.Render, for the terminal, or voice.Plain, for
// text that is kept. Printed as it is, a Text shows only !msg(<id>), so a
// missed Render or Plain is plain to see and never reads as homebot's words.
type Text struct {
	m    *message
	args []Arg
}

// Arg is the value of one placeholder of a Text.
type Arg struct {
	Name string
	Type catalog.Type
	// Value is an int for an int placeholder, a Text (or, from Build, a
	// string) for a duration and a string otherwise. A fixed placeholder's
	// value is the catalog's.
	Value any
}

// message is one catalog entry, as the generated table holds it.
type message struct {
	id, kind, class, text, append string
	persona                       bool
	slots                         []Slot
}

// ID is the catalog id, such as inbox.header.count; "" for the zero Text.
func (t Text) ID() string {
	if t.m == nil {
		return ""
	}
	return t.m.id
}

// String is "!msg(<id>)", what fmt prints for a Text that skipped the
// voice: the id only, never the catalog's words. The zero Text is "!msg()".
func (t Text) String() string {
	return "!msg(" + t.ID() + ")"
}

// Template is the catalog's English text, placeholders unfilled.
func (t Text) Template() string {
	if t.m == nil {
		return ""
	}
	return t.m.text
}

// Append is the literal text layout code adds after the message, such as
// " {self} next"; it is never voiced.
func (t Text) Append() string {
	if t.m == nil {
		return ""
	}
	return t.m.append
}

// Persona reports whether a persona may reword t.
func (t Text) Persona() bool {
	return t.m != nil && t.m.persona
}

// Args are the placeholder values, in the catalog's declaration order, fixed
// placeholders included. {bot} is not among them: the voice fills it.
func (t Text) Args() []Arg {
	return slices.Clone(t.args)
}

// Values maps each placeholder to the value a template is formatted with in
// stored, neutral text.
func (t Text) Values(bot string, render func(Text) string) map[string]any {
	return t.ValuesWithSelf(bot, NeutralName, render)
}

// ValuesWithSelf maps each placeholder to the value a template is formatted with.
// Fixed facts may refer to {self}, expanded once so a user-chosen name is
// never parsed as catalog syntax.
func (t Text) ValuesWithSelf(bot, self string, render func(Text) string) map[string]any {
	values := make(map[string]any, len(t.args)+2)
	values[catalog.Bot] = bot
	values[catalog.Self] = self
	fixed := make(map[string]bool, len(t.m.slots))
	for _, slot := range t.m.slots {
		fixed[slot.Name] = slot.Value != ""
	}
	for _, a := range t.args {
		if nested, ok := a.Value.(Text); ok {
			values[a.Name] = render(nested)
		} else if value, ok := a.Value.(string); ok && fixed[a.Name] {
			values[a.Name] = strings.ReplaceAll(value, catalog.SelfRef, self)
		} else {
			values[a.Name] = a.Value
		}
	}
	return values
}
