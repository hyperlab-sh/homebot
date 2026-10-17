// Package catalog reads homebot's message catalog (internal/msg/catalog.yaml)
// and its text syntax. It imports nothing from homebot, so the code
// generator, internal/msg and the voice all read the catalog the same way.
package catalog

import (
	"fmt"

	"github.com/goccy/go-yaml"
)

// File is catalog.yaml. Any field it does not declare is an error, so a
// misspelt key fails go generate instead of being ignored.
type File struct {
	Version int `yaml:"version"`
	// Conventions, Palette and Glyphs are documentation for people, kept as
	// decoded YAML.
	Conventions any     `yaml:"conventions"`
	Palette     any     `yaml:"palette"`
	Glyphs      any     `yaml:"glyphs"`
	Messages    []Entry `yaml:"messages"`
}

// Entry is one user-facing message.
type Entry struct {
	ID   string `yaml:"id"`
	Kind string `yaml:"kind"`
	// Text is the message in the catalog's English, in the syntax of Template.
	Text string `yaml:"text"`
	// Append is literal text layout code adds after Text, so persona wording
	// never holds a command. Empty when there is none.
	Append       string        `yaml:"append"`
	Placeholders []Placeholder `yaml:"placeholders"`
	// Color is the palette style the message is shown in, as prose.
	Color string `yaml:"color"`
	// Class says how the message is built: voice, mixed or fact.
	Class string `yaml:"class"`
	// Persona reports whether a persona may reword the message.
	Persona bool   `yaml:"persona"`
	Notes   string `yaml:"notes"`
	// Source is where dothaul's TypeScript printed the message, as file:line.
	Source string `yaml:"source"`
	// Level and Fix are set on inbox titles: urgent or todo, and the command
	// shown after the arrow.
	Level string `yaml:"level"`
	Fix   string `yaml:"fix"`
	// SpeakerName marks entries where the literal "homebot" is the speaker.
	SpeakerName bool `yaml:"speaker_name"`
}

// Placeholder declares one {name} of an entry.
type Placeholder struct {
	Name string `yaml:"name"`
	Type Type   `yaml:"type"`
	// Optional placeholders may be left empty by code and dropped by personas.
	Optional bool `yaml:"optional"`
	// Value makes the placeholder fixed: a fact lifted out of the sentence
	// (a command, a file name, a key), always this string. Code never passes
	// it, so the accessor's signature and the neutral text stay as they were,
	// while a persona's wording cannot change or drop the fact. Only a
	// string placeholder that is not optional may have one. nil when the
	// catalog writes no value, so an empty one ("") can be refused.
	Value   *string `yaml:"value"`
	Meaning string  `yaml:"meaning"`
	// Forms repeats a plural selector's forms, as written in Text.
	Forms string `yaml:"forms"`
}

// Fixed is the placeholder's value and whether it has one.
func (p Placeholder) Fixed() (string, bool) {
	if p.Value == nil {
		return "", false
	}
	return *p.Value, true
}

// Type is the kind of value a placeholder holds.
type Type string

// The placeholder types.
const (
	// Int is a count or number; plural selectors need one.
	Int Type = "int"
	// String is any text.
	String Type = "string"
	// Path is a file system path.
	Path Type = "path"
	// Duration is a relative time, one of the format.ago.* messages.
	Duration Type = "duration"
)

// Valid reports whether t is one of the placeholder types.
func (t Type) Valid() bool {
	switch t {
	case Int, String, Path, Duration:
		return true
	}
	return false
}

// Bot is the placeholder the voice fills with the persona's name ("homebot"
// in the neutral voice). Code never passes it.
const Bot = "bot"

// Self is the placeholder the renderer fills with the command word: the
// name the user gave their homebot when it works on this machine, else
// homebot (naming plan D7). Code never passes it. It may also stand inside
// an append or a fixed value, the only placeholder either may hold; the
// renderer expands it there one level deep.
const Self = "self"

// SelfRef is {self} as it is written in a text, an append or a fixed value.
const SelfRef = "{" + Self + "}"

// Load decodes catalog.yaml, refusing fields File does not declare.
func Load(data []byte) (*File, error) {
	var f File
	if err := yaml.UnmarshalWithOptions(data, &f, yaml.DisallowUnknownField()); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	if len(f.Messages) == 0 {
		return nil, fmt.Errorf("catalog: no messages")
	}
	return &f, nil
}
