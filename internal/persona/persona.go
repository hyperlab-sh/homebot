package persona

import (
	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/msg"
)

// Default is the persona homebot speaks as when nothing picks one, and the
// one it falls back to when the picked one is unusable (plan S1, S4).
const Default = "neatpik"

// None is the id that turns the persona off: the neutral voice everywhere.
const None = "none"

// ValidID reports whether s has the syntax of a persona id (plan
// S3; config.ValidPersonaID). HOMEBOT_PERSONA, persona: and every command
// argument are checked with it before any path is built from them.
func ValidID(s string) bool { return config.ValidPersonaID(s) }

// Pair is a colour token's value on a light and on a dark background:
// "#RRGGBB" or an ANSI index "0" to "255". "" inherits it down the chain.
type Pair struct{ Light, Dark string }

// Colors are the persona's three colour tokens (plan S8).
type Colors struct{ Accent, OK, Muted Pair }

// Source is where a persona file comes from.
type Source int

const (
	// Builtin is embedded in homebot.
	Builtin Source = iota
	// User is under <ConfigDir>/personas.
	User
	// UserOverridesBuiltin is a user file with a built-in's id, which
	// shadows it.
	UserOverridesBuiltin
)

// Severity is how bad a Diagnostic is.
type Severity int

const (
	// Warning is reported by persona validate and changes nothing.
	Warning Severity = iota
	// Error drops a variant, or makes the persona unusable (plan S4).
	Error
)

// Diagnostic is one finding of the validator (plan S5).
type Diagnostic struct {
	Severity Severity
	// Path is the file the finding is about: the absolute path of a user
	// file, "builtin/<id>/persona.toml" for a built-in. Load fills it for
	// every file of the chain, so a failure in a parent names the parent.
	Path string
	// Line and Col are the position in the file; 0 when unknown.
	Line, Col int
	// LineID is the message id, for line diagnostics.
	LineID string
	// Variant is 1-based; 0 when the diagnostic is not about a variant.
	Variant int
	Text    msg.Text
	// inLines marks a finding about a [lines] entry. LineID says the same
	// except for the entry whose key is empty, which has none, and must not
	// pass for a header error: a bad line never makes a persona unusable.
	inLines bool
}

// File is one parsed persona file, without its chain.
type File struct {
	ID, Name, Emoji, Tagline, Extends, Lang string

	Colors Colors
	Lines  map[string][]string
	Source Source
	// Path is the file's path, "builtin/<id>/persona.toml" for a built-in.
	Path       string
	schema     int
	schemaSet  bool
	declaredID string
}

// Voice is a usable persona with its chain merged (plan S4).
type Voice struct {
	ID, Name, Emoji string
	Lang            language.Tag
	// Colors are merged down the chain: every token is set.
	Colors Colors
	// Lines are merged per id: the first persona in the chain that has an
	// id supplies all of its variants.
	Lines map[string][]Line
}

// Line is one variant of a persona's line.
type Line struct {
	Text string
	// Lang is the language of the persona that supplied it: a line is
	// formatted with its supplier's plural rules (plan S4, M5).
	Lang language.Tag
}

// Origin is where the active persona was picked.
type Origin int

const (
	// FromEnv is HOMEBOT_PERSONA.
	FromEnv Origin = iota
	// FromConfig is persona: in homebot.yml.
	FromConfig
	// FromDefault is no choice at all: nothing set HOMEBOT_PERSONA or
	// persona:, so the default neatpik was resolved and failed (a broken
	// user personas/neatpik.toml). It is not a config problem, and nothing
	// should turn it into an inbox item.
	FromDefault
)

// Problem is why the active persona fell back to neatpik (plan S4).
type Problem struct {
	ID     string
	Origin Origin
	Diag   Diagnostic
}

// ValidateOptions are the checks Validate adds where it knows enough. The
// alias checks (V8, W3) left with the alias: the name a user gives homebot
// is selfname's (naming plan D3).
type ValidateOptions struct{}
