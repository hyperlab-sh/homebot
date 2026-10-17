package app

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"
)

// inTest reports whether this binary was built by "go test". It is a
// variable only so tests can play a release build.
var inTest = testing.Testing

// testClock returns the fixed clock HOMEBOT_TEST_NOW asks for, if any.
func testClock(env Env) (func() time.Time, error) {
	v, ok := env.Lookup("HOMEBOT_TEST_NOW")
	if !ok || !inTest() {
		return nil, nil
	}
	ms, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("HOMEBOT_TEST_NOW is not a number of milliseconds: %q", v)
	}
	now := time.UnixMilli(ms).UTC()
	return func() time.Time { return now }, nil
}

// testHost returns the host name HOMEBOT_TEST_HOST asks for, if any.
func testHost(env Env) (string, bool) {
	v, ok := env.Lookup("HOMEBOT_TEST_HOST")
	if !ok || !inTest() {
		return "", false
	}
	return v, true
}

// testRand returns the seeded source HOMEBOT_TEST_SEED asks for, if any.
func testRand(env Env) (*rand.Rand, error) {
	v, ok := env.Lookup("HOMEBOT_TEST_SEED")
	if !ok || !inTest() {
		return nil, nil
	}
	seed, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("HOMEBOT_TEST_SEED is not a whole number: %q", v)
	}
	return rand.New(rand.NewPCG(seed, 0)), nil
}

// guardDirs panics, in a test binary, when a directory homebot writes to lies
// outside the temp dir. testscript runs the homebot command with
// TMPDIR=$WORK/.tmp, so there the script's $WORK counts as temporary too.
func guardDirs(c *Context) {
	if !inTest() {
		return
	}
	temp := sameDir(os.TempDir())
	allowed := temp
	if work := c.Env.Get("WORK"); work != "" {
		for _, w := range sameDir(work) {
			if slices.Contains(temp, filepath.Join(w, ".tmp")) {
				allowed = append(allowed, sameDir(work)...)
				break
			}
		}
	}
	for _, d := range []struct{ name, dir string }{
		{"StateDir", c.StateDir},
		{"CacheDir", c.CacheDir},
		{"DataDir", c.DataDir},
		{"ConfigDir", c.ConfigDir},
	} {
		if !insideAny(d.dir, allowed) {
			panic(fmt.Sprintf("app: a test resolved %s to %s, outside the temp dir %s; build test Contexts with testutil.Sandbox", d.name, d.dir, temp[0]))
		}
	}
}

// sameDir returns dir cleaned and, when it differs, with its symlinks
// resolved: macOS's $TMPDIR ends in a slash and lives under /var, a link to
// /private/var.
func sameDir(dir string) []string {
	dir = filepath.Clean(dir)
	if real, err := filepath.EvalSymlinks(dir); err == nil && real != dir {
		return []string{dir, real}
	}
	return []string{dir}
}

func insideAny(path string, dirs []string) bool {
	for _, dir := range dirs {
		if IsInside(path, dir) {
			return true
		}
	}
	return false
}

// testTTY returns what HOMEBOT_TEST_TTY says StdoutIsTerminal answers, if
// anything: 1 for a terminal, 0 for none. It lets a test, a testscript
// among them, see the output a person at a terminal sees (the persona's
// voice) while stdout is a buffer.
func testTTY(env Env) (*bool, error) {
	v, ok := env.Lookup("HOMEBOT_TEST_TTY")
	if !ok || !inTest() {
		return nil, nil
	}
	var tty bool
	switch v {
	case "1":
		tty = true
	case "0":
	default:
		return nil, fmt.Errorf("HOMEBOT_TEST_TTY is not 1 or 0: %q", v)
	}
	return &tty, nil
}
