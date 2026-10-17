package selfname

import (
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/config"
)

// self is the base name of every homebot binary a link may point at.
const self = "homebot"

// validName is the name syntax, config.ValidName (D1): a persona id of at
// least two characters, since one-letter names are the commonest rc aliases.
//
// It is written out here because config.ValidName lands with naming task
// N1; once both are on main, this becomes config.ValidName.
func validName(s string) bool {
	return len(s) >= 2 && config.ValidPersonaID(s)
}

// recordState is what readRecord found.
type recordState int

const (
	noRecord recordState = iota
	badRecord
	goodRecord
)

// usable reports whether c names absolute home and state folders. A
// Context without them (app.ForReport) has no record and no link: a
// relative path would be read against the working directory.
func usable(c *app.Context) bool {
	return filepath.IsAbs(c.Home) && filepath.IsAbs(c.StateDir)
}

func recordPath(c *app.Context) string { return filepath.Join(c.StateDir, "name") }

// Record reads the naming record. ok is false when it is missing or
// unreadable, or holds a name that fails the syntax.
func Record(c *app.Context) (rec Rec, ok bool) {
	rec, st := readRecord(c)
	return rec, st == goodRecord
}

func readRecord(c *app.Context) (Rec, recordState) {
	if !usable(c) {
		return Rec{}, noRecord
	}
	data, err := os.ReadFile(recordPath(c))
	if errors.Is(err, fs.ErrNotExist) {
		return Rec{}, noRecord
	}
	if err != nil {
		return Rec{}, badRecord
	}
	rec, ok := parseRecord(string(data))
	if !ok {
		return Rec{}, badRecord
	}
	return rec, goodRecord
}

// parseRecord reads the record's key=value lines strictly: every key known
// and given once, name and made present, old_made exactly when old is, and
// every name valid. Anything else is a bad record, which owns no link.
func parseRecord(text string) (Rec, bool) {
	seen := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSuffix(text, "\n"), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return Rec{}, false
		}
		switch k {
		case "name", "target", "made", "pending", "old", "old_made":
		default:
			return Rec{}, false
		}
		if _, dup := seen[k]; dup {
			return Rec{}, false
		}
		seen[k] = v
	}
	rec := Rec{Name: seen["name"], Target: seen["target"], Old: seen["old"]}
	var ok bool
	if rec.Made, ok = parseBool(seen, "made"); !ok || !validName(rec.Name) {
		return Rec{}, false
	}
	if rec.Target == "" || strings.Contains(rec.Target, "\n") {
		return Rec{}, false
	}
	if _, hasPending := seen["pending"]; hasPending {
		if rec.Pending, ok = parseBool(seen, "pending"); !ok || !rec.Pending || rec.Made {
			return Rec{}, false
		}
	}
	_, hasOld := seen["old"]
	_, hasOldMade := seen["old_made"]
	if hasOld != hasOldMade {
		return Rec{}, false
	}
	if hasOld {
		if rec.OldMade, ok = parseBool(seen, "old_made"); !ok || !validName(rec.Old) || rec.Old == rec.Name {
			return Rec{}, false
		}
	}
	return rec, true
}

func parseBool(seen map[string]string, k string) (bool, bool) {
	switch seen[k] {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

func formatRecord(rec Rec) string {
	s := fmt.Sprintf("name=%s\ntarget=%s\nmade=%t\n", rec.Name, rec.Target, rec.Made)
	if rec.Pending {
		s += "pending=true\n"
	}
	if rec.Old != "" {
		s += fmt.Sprintf("old=%s\nold_made=%t\n", rec.Old, rec.OldMade)
	}
	return s
}

// writeRecord replaces the record through a temp file and a rename, so a
// crash leaves the old record or the new one, never half of one (D2).
func writeRecord(c *app.Context, rec Rec) error {
	if !validName(rec.Name) || rec.Target == "" || strings.Contains(rec.Target, "\n") || (rec.Pending && rec.Made) {
		return fmt.Errorf("selfname: refusing to write a bad record for %q", rec.Name)
	}
	path := recordPath(c)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return err
	}
	var f *os.File
	var err error
	for range 100 {
		f, err = os.OpenFile(path+".tmp-"+strconv.FormatUint(uint64(rand.Uint32()), 36), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
		if !errors.Is(err, fs.ErrExist) {
			break
		}
	}
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.WriteString(formatRecord(rec))
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return syncDir(dir)
}

// deleteRecord removes the record; a missing one is not an error.
func deleteRecord(c *app.Context) error {
	if err := os.Remove(recordPath(c)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// putRecord writes rec, or deletes the record when had is false: how a
// failed step goes back to what was there before.
func putRecord(c *app.Context, rec Rec, had bool) error {
	if !had {
		return deleteRecord(c)
	}
	return writeRecord(c, rec)
}

// syncDir flushes dir's entries, so a rename survives a crash. A file
// system that can't sync a folder is not an error.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if closeErr := d.Close(); err == nil {
		err = closeErr
	}
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTTY) || errors.Is(err, errors.ErrUnsupported) {
		return nil
	}
	return err
}
