package msg

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/hyperlab-sh/homebot/internal/msg/catalog"
)

// Slot is one placeholder of a catalog entry, as catalog.yaml declares it.
type Slot struct {
	Name     string
	Type     catalog.Type
	Optional bool
	Value    string // fixed value; "" for a normal placeholder
	Meaning  string
	Sample   string // the generator's sample value, for previews
}

// Meta is what the binary knows of a catalog entry without embedding
// catalog.yaml: the generated table behind the accessors.
type Meta struct {
	ID, Kind, Class, Text, Append string
	Persona                       bool
	Slots                         []Slot
}

// Entries are every catalog entry, in catalog order. The slice is the
// caller's own.
func Entries() []Meta {
	out := make([]Meta, len(messages))
	for i := range messages {
		out[i] = messages[i].meta()
	}
	return out
}

// Lookup is the entry with id, if there is one.
func Lookup(id string) (Meta, bool) {
	i, ok := index()[id]
	if !ok {
		return Meta{}, false
	}
	return messages[i].meta(), true
}

// index maps each id to its place in messages.
var index = sync.OnceValue(func() map[string]int {
	m := make(map[string]int, len(messages))
	for i := range messages {
		m[messages[i].id] = i
	}
	return m
})

func (m *message) meta() Meta {
	return Meta{
		ID: m.id, Kind: m.kind, Class: m.class, Text: m.text, Append: m.append,
		Persona: m.persona, Slots: slices.Clone(m.slots),
	}
}

// Build is the Text of the entry id with values, for code that cannot call
// the entry's typed accessor because the id is data: the persona preview
// and the tests that render every entry. Code that knows the message calls
// its accessor; a forbidigo rule (.golangci.yml) allows Build only in tests
// and internal/cli/order_persona_preview.go.
//
// values holds what the accessor's arguments would: an int for an int
// placeholder, a string for a string or path, and a Text or a string for a
// duration. A placeholder left out is the zero value of its type, as an
// accessor's empty argument would be, unless it is required. Build fills in
// fixed placeholders and refuses a value for one, a value for bot (the voice
// fills it), an unknown name and a value of the wrong type.
func Build(id string, values map[string]any) (Text, error) {
	i, ok := index()[id]
	if !ok {
		return Text{}, fmt.Errorf("msg: no catalog entry %q", id)
	}
	m := &messages[i]
	slots := map[string]Slot{}
	for _, sl := range m.slots {
		slots[sl.Name] = sl
	}
	var problems []string
	for _, name := range slices.Sorted(maps.Keys(values)) {
		sl, declared := slots[name]
		switch {
		case !declared:
			problems = append(problems, fmt.Sprintf("%q is not a placeholder of %s", name, id))
		case name == catalog.Bot:
			problems = append(problems, "bot is the speaker's name; the voice fills it")
		case name == catalog.Self:
			problems = append(problems, "self is the command name; the renderer fills it")
		case sl.Value != "":
			problems = append(problems, fmt.Sprintf("%q is fixed to %q; it takes no value", name, sl.Value))
		}
	}
	var args []Arg
	for _, sl := range m.slots {
		if sl.Name == catalog.Bot || sl.Name == catalog.Self {
			continue
		}
		if sl.Value != "" {
			args = append(args, Arg{Name: sl.Name, Type: sl.Type, Value: sl.Value})
			continue
		}
		v, given := values[sl.Name]
		if !given {
			if !sl.Optional {
				problems = append(problems, fmt.Sprintf("%q is required", sl.Name))
				continue
			}
			v = zero(sl.Type)
		}
		if !fits(sl.Type, v) {
			problems = append(problems, fmt.Sprintf("%q is a %s placeholder; %T does not fit it", sl.Name, sl.Type, v))
			continue
		}
		args = append(args, Arg{Name: sl.Name, Type: sl.Type, Value: v})
	}
	if len(problems) > 0 {
		return Text{}, fmt.Errorf("msg: building %s: %s", id, strings.Join(problems, "; "))
	}
	return Text{m: m, args: args}, nil
}

// fits reports whether v may be the value of a placeholder of type t.
func fits(t catalog.Type, v any) bool {
	switch v.(type) {
	case int:
		return t == catalog.Int
	case string:
		return t == catalog.String || t == catalog.Path || t == catalog.Duration
	case Text:
		return t == catalog.Duration
	}
	return false
}

// zero is what an accessor's empty argument of type t is.
func zero(t catalog.Type) any {
	switch t {
	case catalog.Int:
		return 0
	case catalog.Duration:
		return Text{}
	}
	return ""
}
