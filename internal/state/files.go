package state

import (
	"path/filepath"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fsx"
)

// Links is links.json: the link targets homebot made, so a later run can prune
// the ones the repo no longer provides.
type Links struct {
	Targets []string
}

// LoadLinks reads links.json; a missing file is no links.
func LoadLinks(ctx *app.Context) (*Links, error) {
	var t []string
	if err := fsx.ReadJSON(ctx, filepath.Join(ctx.StateDir, "links.json"), &t); err != nil {
		return nil, err
	}
	return &Links{Targets: t}, nil
}

// Save writes links.json as a JSON array, [] when empty.
func (l *Links) Save(ctx *app.Context) error {
	t := l.Targets
	if t == nil {
		t = []string{}
	}
	return fsx.WriteJSON(filepath.Join(ctx.StateDir, "links.json"), t)
}

// SecretRecord holds the hashes of a secret's plaintext and .age file as of
// the last seal or unlock on this machine.
type SecretRecord struct {
	Plain string `json:"plain"`
	Age   string `json:"age"`
}

// Secrets is secrets.json, keyed by path relative to private/, without .age.
type Secrets struct {
	Records OrderedMap[SecretRecord]
}

// LoadSecrets reads secrets.json; a missing file is no records.
func LoadSecrets(ctx *app.Context) (*Secrets, error) {
	s := &Secrets{}
	if err := fsx.ReadJSON(ctx, filepath.Join(ctx.StateDir, "secrets.json"), &s.Records); err != nil {
		return nil, err
	}
	return s, nil
}

// Save writes secrets.json.
func (s *Secrets) Save(ctx *app.Context) error {
	return fsx.WriteJSON(filepath.Join(ctx.StateDir, "secrets.json"), s.Records)
}

// SnoozePath is where snooze.json lives.
func SnoozePath(ctx *app.Context) string { return filepath.Join(ctx.StateDir, "snooze.json") }

// Snoozes is snooze.json: item key to the millisecond time the snooze ends.
type Snoozes struct {
	Until OrderedMap[int64]
}

// LoadSnoozes reads snooze.json; a missing file is no snoozes.
func LoadSnoozes(ctx *app.Context) (*Snoozes, error) {
	s := &Snoozes{}
	if err := fsx.ReadJSON(ctx, SnoozePath(ctx), &s.Until); err != nil {
		return nil, err
	}
	return s, nil
}

// Save writes snooze.json.
func (s *Snoozes) Save(ctx *app.Context) error {
	return fsx.WriteJSON(SnoozePath(ctx), s.Until)
}

// Snooze hides key for days days from now, keeping every other snooze.
func Snooze(ctx *app.Context, key string, days int) error {
	s, err := LoadSnoozes(ctx)
	if err != nil {
		return err
	}
	s.Until.Set(key, ctx.Now().UnixMilli()+int64(days)*86_400_000)
	return s.Save(ctx)
}
