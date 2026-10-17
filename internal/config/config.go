// Package config reads homebot.yml: the repo's one configuration file.
//
// It must not import internal/app: app.Context holds a *Config, so the
// dependency runs the other way. Errors are *fail.UserError.
package config

import (
	"github.com/hyperlab-sh/homebot/internal/layout"
	"github.com/hyperlab-sh/homebot/internal/msg"
)

// FileName is the configuration file at the repo root.
const FileName = layout.ConfigFile

// Cadence is how often the background helper commits: "off", "hourly" or "daily".
type Cadence string

// UpdateMode controls release checks by the background helper.
type UpdateMode string

const (
	UpdateOff    UpdateMode = "off"
	UpdateNotify UpdateMode = "notify"
	UpdateAuto   UpdateMode = "auto"
)

// The cadences autosave.commit accepts.
const (
	CadenceOff    Cadence = "off"
	CadenceHourly Cadence = "hourly"
	CadenceDaily  Cadence = "daily"
)

// Config is homebot.yml after validation, with defaults applied.
type Config struct {
	Profiles Profiles
	Triage   Triage
	Autosave Autosave
	Exports  []ExportSpec
	Limits   Limits
	Update   UpdateMode
	Warnings []msg.Text

	// Persona is persona: in homebot.yml, the persona this home speaks as:
	// "none", an id, or "" when the key is absent (neatpik then). Only an id
	// that passes ValidPersonaID, or "none", is ever stored here.
	Persona string
	// Name is name: in homebot.yml, the command the user gave their homebot
	// (plan D1). "" when the key is absent or says homebot, which both mean
	// no name. Only a value that passes ValidName is ever stored here.
	Name string
}

// maxPersonaID is the longest persona id or alias, in bytes (ASCII only).
const maxPersonaID = 32

// ValidPersonaID reports whether s has the syntax of a persona id or alias
// (plan S3): a lowercase ASCII letter, then lowercase letters, digits and
// hyphens, at most 32 characters. It is the one copy of that rule: persona
// calls it, and config needs it for persona: and name: (ValidName), and can't
// import persona (persona imports app, which imports config). A value is
// checked with it before it is ever part of a path. It says nothing about
// reserved names: "none" passes.
func ValidPersonaID(s string) bool {
	if s == "" || len(s) > maxPersonaID || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// Profiles picks the profile each machine unpacks.
type Profiles struct {
	// Default is the profile for hosts not listed in Hosts; empty when unset.
	Default string
	// Hosts maps a short host name to its profile. Never nil.
	Hosts map[string]string
}

// Triage configures which files in $HOME triage offers to pack.
type Triage struct {
	// Watch holds glob patterns, "~"-relative as written in the file.
	Watch []string
}

// Autosave configures what the background helper does with changes.
type Autosave struct {
	Commit Cadence
	Push   bool
}

// ExportSpec is one entry under exports:.
type ExportSpec struct {
	Name string
	// On lists the OSes ("linux", "macos") the export runs on.
	On []string
	// Out is the repo-relative output file; it may use {os}, {host} and {arch}.
	Out string
	// Run is a shell command. With {out} it writes the file itself; otherwise
	// its stdout becomes the file.
	Run string
	// Profiles, when non-nil, limits the export to machines whose default
	// profile is listed. Nil means every machine.
	Profiles []string
}

// Limits holds size limits the commit guard enforces.
type Limits struct {
	MaxFileMB float64
}

// Default returns the configuration used when homebot.yml leaves a key out.
func Default() *Config {
	return &Config{
		Profiles: Profiles{Hosts: map[string]string{}},
		Triage:   Triage{Watch: []string{"~/.config/*", "~/.*"}},
		Autosave: Autosave{Commit: CadenceOff, Push: false},
		Exports:  []ExportSpec{},
		Limits:   Limits{MaxFileMB: 50},
		Update:   UpdateOff,
	}
}

// minName is the shortest name: in homebot.yml. One-letter names are the
// commonest shell aliases (g, k), and Go can't see aliases.
const minName = 2

// ValidName reports whether s has the syntax of a name: value (plan D1): the
// syntax of a persona id, and at least two characters. It says nothing about
// reserved names, PATH or the machine; "homebot" passes (it means no name).
func ValidName(s string) bool { return len(s) >= minName && ValidPersonaID(s) }
