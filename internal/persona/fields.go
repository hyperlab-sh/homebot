package persona

// Field describes a top-level persona TOML field for generated reference
// documentation and editor schemas. The parser and validator remain the
// authority for checks that cannot be expressed by JSON Schema.
type Field struct {
	Name, Type, Description string
	Required                bool
}

// Fields is the public shape accepted by ReadFile. Colour tokens and lines
// have their own generated sections because their children are constrained.
func Fields() []Field {
	return []Field{
		{"schema", "integer", "File format version; must be 1.", true},
		{"id", "string", "Optional ID; when present, it must match the filename.", false},
		{"name", "string", "Speaker name, 1 to 32 characters.", true},
		{"emoji", "string", "One emoji grapheme, without ASCII characters.", true},
		{"tagline", "string", "One line, at most 100 characters; no dollar sign or backtick.", false},
		{"extends", "string", "Explicit parent ID; none stops explicit parent traversal, then embedded Neatpik remains the fallback.", false},
		{"lang", "string", "BCP 47 language tag; English is the default.", false},
		{"colors", "object", "Accent, ok and muted light/dark colours; missing values inherit.", false},
		{"lines", "object", "Voice message IDs, each with an array of variants.", false},
	}
}
