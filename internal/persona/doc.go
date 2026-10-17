// Package persona reads persona files and decides which voice is in effect
// (plan S1, S3 and S4).
//
// A persona is a TOML file: a header (name, emoji, colours) and the
// lines it says instead of the catalog's English. The built-ins are embedded;
// a user's live under <ConfigDir>/personas.
//
// persona imports only app, config, msg, msg/catalog and proc. It never
// imports ui, voice, fsx, state, inbox or cli: voice and ui ask it which
// voice is in effect (Speaking, NudgeVoice), so it can't ask them anything.
// It never prints and never renders a msg.Text: it returns texts
// (diagnostics, the unusable-persona warning) for voice or cli to print, and
// its debug lines go to ctx.Debug. It reads files with os, because fsx
// imports voice.
package persona

import (
	// The persona file format (plan S3); the loader decodes with it.
	_ "github.com/BurntSushi/toml"
)
