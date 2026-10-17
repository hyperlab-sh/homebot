package proc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/app"
)

// LookPath finds name in the Context's PATH. A name with a slash in it is
// returned as it is.
//
// A program homebot looks up or runs by name goes into selfname.ToolNames,
// so no one can name their homebot after it (naming plan D3).
func LookPath(c *app.Context, name string) (string, error) {
	logProgram(c.Env, name)
	return lookPath(c.Env, name)
}

// HasCommand reports whether name is on the Context's PATH.
//
// A program homebot looks up or runs by name goes into selfname.ToolNames,
// so no one can name their homebot after it (naming plan D3).
func HasCommand(c *app.Context, name string) bool {
	logProgram(c.Env, name)
	_, err := lookPath(c.Env, name)
	return err == nil
}

// lookPath searches the absolute directories of env's PATH; relative entries
// are skipped, so the current directory never supplies a program.
func lookPath(env app.Env, name string) (string, error) {
	if strings.Contains(name, "/") {
		return name, nil
	}
	for _, dir := range filepath.SplitList(env.Get("PATH")) {
		if !filepath.IsAbs(dir) {
			continue
		}
		path := filepath.Join(dir, name)
		if isExecutable(path) {
			return path, nil
		}
	}
	// Bun's wording, which the TypeScript build passed on.
	return "", fmt.Errorf("Executable not found in $PATH: %q", name)
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// ProgramLogVar names the file a test binary logs every program homebot
// runs or looks up to, one base name a line (naming plan D3). cmd/homebot's
// TestMain sets it for its scripts and fails when a logged name is missing
// from selfname.ToolNames. It does nothing outside a test binary.
const ProgramLogVar = "HOMEBOT_TEST_PROC_LOG"

// logProgram appends name's base to the program log, when there is one.
// Run, RunAttached, LookPath and HasCommand call it; SelfPath and selfname's
// own lookups of a candidate name don't, so a name never reaches the log.
// A failure to write is ignored: the log is a test's backstop, never a
// reason for homebot to fail.
func logProgram(env app.Env, name string) {
	if !testing.Testing() || name == "" {
		return
	}
	path := env.Get(ProgramLogVar)
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return
	}
	// One write per line; O_APPEND keeps the lines of parallel scripts whole.
	_, _ = f.WriteString(filepath.Base(name) + "\n")
	_ = f.Close()
}
