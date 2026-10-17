package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fsx"
)

// Entry is one line of journal.jsonl: something homebot did on its own, so
// the inbox can report it later.
type Entry struct {
	At    int64  `json:"at"`
	Msg   string `json:"msg"`
	Error bool   `json:"error,omitempty"`
}

func journalPath(ctx *app.Context) string { return filepath.Join(ctx.StateDir, "journal.jsonl") }

// Journal appends e as one JSON line. A zero At is filled from the clock.
func Journal(ctx *app.Context, e Entry) error {
	if e.At == 0 {
		e.At = ctx.Now().UnixMilli()
	}
	line, err := marshalNoEscape(e)
	if err != nil {
		return err
	}
	line = append(fsx.RawLineSeparators(line), '\n')
	if err := os.MkdirAll(ctx.StateDir, 0o777); err != nil {
		return err
	}
	f, err := os.OpenFile(journalPath(ctx), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o666)
	if err != nil {
		return err
	}
	_, err = f.Write(line)
	return errors.Join(err, f.Close())
}

// ReadJournal returns every entry, oldest first. A missing file is no entries;
// a line that is not an entry (a torn write from a crash) is skipped.
func ReadJournal(ctx *app.Context) ([]Entry, error) {
	data, err := os.ReadFile(journalPath(ctx))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var e Entry
		if json.Unmarshal(line, &e) != nil {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}
