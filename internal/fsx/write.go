package fsx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"unicode/utf8"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/ui"
)

// WriteFileAtomic writes data to path through a temp file and a rename, so a
// reader never sees a half-written file, then syncs the directory so the
// rename survives a crash. It creates the parent directories: 0700 for a
// file only its owner may read or write (perm has no group or other bits,
// as for a secret's plaintext), 0777 less the umask otherwise. Directories
// that exist keep their mode.
//
// The file gets mode perm &^ umask on every write, even when it existed with
// another mode; os.WriteFile keeps an existing file's mode. A symlink at path
// is replaced, not followed.
func WriteFileAtomic(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	dirPerm := fs.FileMode(0o777)
	if perm&0o077 == 0 {
		dirPerm = 0o700
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return err
	}
	f, err := createTemp(path, perm)
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
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
		return errors.Join(err, removeIfExists(tmp))
	}
	return syncDir(dir)
}

// createTemp creates a new file next to path, named path.tmp-<random>, with
// mode perm filtered by the umask. It is os.CreateTemp with a mode: a temp
// file left by a crashed process only costs another name. (os.CreateTemp and
// a Chmod would skip the umask and leave WriteJSON's 0666 files writable by
// everyone.)
func createTemp(path string, perm fs.FileMode) (*os.File, error) {
	for range 10000 {
		name := path + ".tmp-" + strconv.FormatUint(uint64(rand.Uint32()), 36)
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		if !errors.Is(err, fs.ErrExist) {
			return f, err
		}
	}
	return nil, fmt.Errorf("fsx: no free temp file name next to %s", path)
}

// syncDir flushes dir's entries to disk. A filesystem that cannot sync a
// directory is not an error: the write has happened, only its durability is
// weaker.
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

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// WriteJSON writes v to path atomically as JSON indented by two spaces with a
// final newline, the layout JSON.stringify(v, null, 2) gave the state files.
// <, >, & and the line separators U+2028 and U+2029 are written as they are,
// as JavaScript writes them. The file gets mode 0666 &^ umask.
//
// Two differences from JSON.stringify remain: a map's keys come out sorted
// (JavaScript keeps insertion order), and a nil slice is null (JavaScript
// writes []). State that must match the TypeScript build byte for byte uses
// structs, whose fields keep their declared order, and non-nil slices.
func WriteJSON(path string, v any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return err
	}
	return WriteFileAtomic(path, RawLineSeparators(buf.Bytes()), 0o666)
}

// RawLineSeparators turns encoding/json's \u2028 and \u2029 escapes back into
// the characters. Every backslash in JSON text starts an escape, so a
// backslash and the character after it are copied as a pair.
func RawLineSeparators(b []byte) []byte {
	if !bytes.Contains(b, []byte(`\u202`)) {
		return b
	}
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] != '\\' || i+1 == len(b):
			out = append(out, b[i])
		case i+5 < len(b) && string(b[i+1:i+5]) == "u202" && (b[i+5] == '8' || b[i+5] == '9'):
			out = utf8.AppendRune(out, rune(0x2020+int(b[i+5]-'0')))
			i += 5
		default:
			out = append(out, b[i], b[i+1])
			i++
		}
	}
	return out
}

// ReadJSON decodes the JSON file at path into v. A missing file leaves v as it
// is and is not an error, so v holds the fallback. A file that is not valid
// JSON gives a user error (fsutil.error.bad_json) that says it is safe to
// delete, with path shortened to ~ when it is under the home.
func ReadJSON(c *app.Context, path string, v any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fail.Say(msg.FsutilErrorBadJSON(ui.Tildify(path, c.Home)))
	}
	return nil
}
