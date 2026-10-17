package inbox

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/guard"
	"github.com/hyperlab-sh/homebot/internal/helper"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/proc"
)

// binaryPathItems reads installed helper and guard files; it never talks to
// systemd or launchd, and never repairs or executes a recorded binary.
func binaryPathItems(e *env) ([]Item, error) {
	var bad []string
	if helper.Installed(e.c) {
		unit := helper.Files(e.c)[0] // service on Linux, plist on macOS.
		path, err := helperBinaryPath(e.c, unit)
		if err != nil {
			if e.c.Debug != nil {
				e.c.Debug.Debug("inbox: cannot read helper binary path", "path", unit, "error", err)
			}
		} else if badBinaryPath(path) {
			bad = append(bad, path)
		}
	}
	if path, installed := guard.RecordedPath(e.c); installed && badBinaryPath(path) {
		bad = append(bad, path)
	}
	if len(bad) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool, len(bad))
	var lines []string
	for _, path := range bad {
		if seen[path] {
			continue
		}
		seen[path] = true
		lines = append(lines, "  "+strconv.Quote(path))
	}
	it := e.item("helper-path", Todo, msg.InboxHelperPathTitle(), &Fix{Args: []string{"setup"}})
	it.Detail = e.plain(msg.InboxHelperPathDetail(strings.Join(lines, "\n")))
	return []Item{it}, nil
}

func badBinaryPath(path string) bool {
	if !filepath.IsAbs(path) || proc.Versioned(path) {
		return true
	}
	info, err := os.Stat(path) // follow a stable symlink to its current target.
	return err != nil || !info.Mode().IsRegular() || unix.Faccessat(unix.AT_FDCWD, path, unix.X_OK, unix.AT_EACCESS) != nil
}

func helperBinaryPath(c *app.Context, file string) (string, error) {
	// O_NONBLOCK ensures an installed path replaced by a FIFO cannot hang
	// inbox collection. Stat the opened descriptor to avoid a path race and
	// permit the regular symlink target that an installer may leave there.
	f, err := os.OpenFile(file, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("helper file is not regular: %s", file)
	}
	const maxUnitBytes = 1 << 20
	data, err := io.ReadAll(io.LimitReader(f, maxUnitBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxUnitBytes {
		return "", fmt.Errorf("helper file exceeds %d bytes: %s", maxUnitBytes, file)
	}
	if c.OS == app.Linux {
		return systemdBinaryPath(string(data))
	}
	return launchdBinaryPath(data)
}

// systemdBinaryPath inverts helper.execQuote for the first ExecStart word.
func systemdBinaryPath(unit string) (string, error) {
	for line := range strings.SplitSeq(unit, "\n") {
		word, ok := strings.CutPrefix(line, "ExecStart=")
		if !ok {
			continue
		}
		if !strings.HasPrefix(word, `"`) {
			return "", strconv.ErrSyntax
		}
		escaped := false
		for i := 1; i < len(word); i++ {
			switch {
			case escaped:
				escaped = false
			case word[i] == '\\':
				escaped = true
			case word[i] == '"':
				path, err := strconv.Unquote(word[:i+1])
				if err != nil {
					return "", err
				}
				return strings.NewReplacer("%%", "%", "$$", "$").Replace(path), nil
			}
		}
		return "", strconv.ErrSyntax
	}
	return "", io.EOF
}

// launchdBinaryPath reads the first ProgramArguments string without invoking
// launchctl or accepting an executable named elsewhere in the plist.
func launchdBinaryPath(plist []byte) (string, error) {
	dec := xml.NewDecoder(strings.NewReader(string(plist)))
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", err
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "key" {
			continue
		}
		var key string
		if err := dec.DecodeElement(&key, &start); err != nil {
			return "", err
		}
		if key != "ProgramArguments" {
			continue
		}
		for {
			tok, err = dec.Token()
			if err != nil {
				return "", err
			}
			if next, ok := tok.(xml.StartElement); ok {
				if next.Name.Local != "array" {
					return "", strconv.ErrSyntax
				}
				break
			}
		}
		for {
			tok, err = dec.Token()
			if err != nil {
				return "", err
			}
			if next, ok := tok.(xml.StartElement); ok {
				if next.Name.Local != "string" {
					return "", strconv.ErrSyntax
				}
				var path string
				if err := dec.DecodeElement(&path, &next); err != nil {
					return "", err
				}
				return path, nil
			}
			if end, ok := tok.(xml.EndElement); ok && end.Name.Local == "array" {
				return "", strconv.ErrSyntax
			}
		}
	}
}
