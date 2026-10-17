// Package state reads and writes the per-machine files under the state and
// cache directories: state.json, journal.jsonl, links.json, secrets.json,
// snooze.json and the shell cache files nudge and count. The formats are the
// TypeScript build's, byte for byte, with timestamps in JavaScript
// milliseconds.
package state

import (
	"encoding/json"
	"path/filepath"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fsx"
)

// ExportResult is the outcome of the last run of one export.
type ExportResult struct {
	At    int64  `json:"at"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Unpack records the last profile unpacked here.
type Unpack struct {
	Profile string `json:"profile"`
	Hash    string `json:"hash"`
	At      int64  `json:"at"`
}

// Notify records the last desktop notification sent.
type Notify struct {
	Key string `json:"key"`
	At  int64  `json:"at"`
}

// Blocked records the last commit attempt that a guard refused.
type Blocked struct {
	Reason string `json:"reason"`
	At     int64  `json:"at"`
}

// TidyError is an automatic chore that failed on the last tidy run.
type TidyError struct {
	Step    string `json:"step"`
	Message string `json:"message"`
	At      int64  `json:"at"`
}

// UpdateCheck records the last release query and the first sighting of its
// current answer. Failed marks an offline query for the shorter retry cadence.
type UpdateCheck struct {
	At           int64  `json:"at"`
	Latest       string `json:"latest,omitempty"`
	FirstSeen    int64  `json:"firstSeen,omitempty"`
	FirstAttempt int64  `json:"firstAttempt,omitempty"`
	LastAttempt  int64  `json:"lastAttempt,omitempty"`
	Failed       bool   `json:"failed,omitempty"`
}

// UpdateFailed records an unattended upgrade failure for the inbox.
type UpdateFailed struct {
	Version string `json:"version"`
	At      int64  `json:"at"`
	Reason  string `json:"reason"`
	Error   string `json:"error,omitempty"`
}

// State is state.json. A zero timestamp and a nil pointer mean "not set" and
// are left out of the file. Fields this build does not know are kept and
// written back, so a newer or older build's data survives.
type State struct {
	LastTidy     int64
	LastAutosave int64
	// LastSync is the last time the remote answered a fetch.
	LastSync int64
	// LastSeen is when the inbox was last shown to a person.
	LastSeen     int64
	Exports      OrderedMap[ExportResult]
	Unpack       *Unpack
	Notify       *Notify
	Blocked      *Blocked
	TidyErrors   []TidyError
	UpdateCheck  *UpdateCheck
	UpdateFailed *UpdateFailed

	order []string
	extra map[string]json.RawMessage
}

// stateKeys are the keys State types, in declaration order.
var stateKeys = []string{"lastTidy", "lastAutosave", "lastSync", "lastSeen", "exports", "unpack", "notify", "blocked", "tidyErrors", "updateCheck", "updateFailed"}

func statePath(ctx *app.Context) string { return filepath.Join(ctx.StateDir, "state.json") }

// present reports whether key would be written.
func (s *State) present(key string) bool {
	switch key {
	case "lastTidy":
		return s.LastTidy != 0
	case "lastAutosave":
		return s.LastAutosave != 0
	case "lastSync":
		return s.LastSync != 0
	case "lastSeen":
		return s.LastSeen != 0
	case "unpack":
		return s.Unpack != nil
	case "notify":
		return s.Notify != nil
	case "blocked":
		return s.Blocked != nil
	case "updateCheck":
		return s.UpdateCheck != nil
	case "updateFailed":
		return s.UpdateFailed != nil
	case "exports", "tidyErrors":
		return true
	}
	_, ok := s.extra[key]
	return ok
}

func (s *State) value(key string) any {
	switch key {
	case "lastTidy":
		return s.LastTidy
	case "lastAutosave":
		return s.LastAutosave
	case "lastSync":
		return s.LastSync
	case "lastSeen":
		return s.LastSeen
	case "exports":
		return s.Exports
	case "unpack":
		return s.Unpack
	case "notify":
		return s.Notify
	case "blocked":
		return s.Blocked
	case "updateCheck":
		return s.UpdateCheck
	case "updateFailed":
		return s.UpdateFailed
	case "tidyErrors":
		if s.TidyErrors == nil {
			return []TidyError{}
		}
		return s.TidyErrors
	}
	return s.extra[key]
}

// keyOrder is the order keys are written in: those loaded from the file, in
// file order but with exports and tidyErrors first (the TypeScript spreads a
// default object under the file's), then keys set since, in declaration order.
func (s *State) keyOrder() []string {
	var out []string
	seen := map[string]bool{}
	add := func(k string) {
		if !seen[k] && s.present(k) {
			seen[k] = true
			out = append(out, k)
		}
	}
	for _, k := range s.order {
		add(k)
	}
	for _, k := range stateKeys {
		add(k)
	}
	return out
}

// MarshalJSON writes the keys in keyOrder.
func (s State) MarshalJSON() ([]byte, error) {
	var out []byte
	out = append(out, '{')
	for i, k := range s.keyOrder() {
		if i > 0 {
			out = append(out, ',')
		}
		kb, err := marshalNoEscape(k)
		if err != nil {
			return nil, err
		}
		vb, err := marshalNoEscape(s.value(k))
		if err != nil {
			return nil, err
		}
		out = append(out, kb...)
		out = append(out, ':')
		out = append(out, vb...)
	}
	return append(out, '}'), nil
}

// UnmarshalJSON reads the typed fields and keeps the others.
func (s *State) UnmarshalJSON(data []byte) error {
	keys, raws, err := decodeObject(data)
	if err != nil {
		return err
	}
	*s = State{order: []string{"exports", "tidyErrors"}}
	typed := map[string]bool{}
	for _, k := range stateKeys {
		typed[k] = true
	}
	for _, k := range keys {
		s.order = append(s.order, k)
		raw := raws[k]
		var dst any
		switch k {
		case "lastTidy":
			dst = &s.LastTidy
		case "lastAutosave":
			dst = &s.LastAutosave
		case "lastSync":
			dst = &s.LastSync
		case "lastSeen":
			dst = &s.LastSeen
		case "exports":
			dst = &s.Exports
		case "unpack":
			dst = &s.Unpack
		case "notify":
			dst = &s.Notify
		case "blocked":
			dst = &s.Blocked
		case "updateCheck":
			dst = &s.UpdateCheck
		case "updateFailed":
			dst = &s.UpdateFailed
		case "tidyErrors":
			dst = &s.TidyErrors
		}
		if dst == nil {
			if s.extra == nil {
				s.extra = map[string]json.RawMessage{}
			}
			s.extra[k] = raw
			continue
		}
		if err := json.Unmarshal(raw, dst); err != nil {
			return err
		}
	}
	return nil
}

// Load reads state.json; a missing file is an empty State. A file that is not
// valid JSON is an error that says the file is safe to delete.
func Load(ctx *app.Context) (*State, error) {
	s := &State{order: []string{"exports", "tidyErrors"}}
	if err := fsx.ReadJSON(ctx, statePath(ctx), s); err != nil {
		return nil, err
	}
	return s, nil
}

// Update loads state.json, applies change and writes it back atomically. A
// failed write leaves the old file in place.
func Update(ctx *app.Context, change func(*State)) error {
	s, err := Load(ctx)
	if err != nil {
		return err
	}
	change(s)
	// A key cleared and set again goes last, as deleting and re-adding a
	// property does in JavaScript.
	kept := s.order[:0:0]
	for _, k := range s.order {
		if s.present(k) {
			kept = append(kept, k)
		}
	}
	s.order = kept
	return fsx.WriteJSON(statePath(ctx), s)
}
