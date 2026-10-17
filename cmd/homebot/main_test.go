package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/selfname"
)

func TestMain(m *testing.M) {
	testscript.Main(programLogChecked{m}, map[string]func(){
		"homebot": func() { os.Exit(run(context.Background(), os.Args[1:])) },
	})
}

// programLogChecked runs the tests with the program log on (naming plan
// D3): every homebot a script runs appends the name of each program it runs
// or looks up to the file proc.ProgramLogVar names (setupSandbox passes it
// on). After the tests, a logged name missing from selfname.ToolNames fails
// the run, so a program homebot starts to run can't be missed from the list
// a user's name is checked against. Run only runs in the top-level test
// process; a script's homebot never gets here.
type programLogChecked struct{ m *testing.M }

func (p programLogChecked) Run() int {
	dir, err := os.MkdirTemp("", "homebot-programs-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "program log:", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()
	log := filepath.Join(dir, "programs")
	if err := os.Setenv(proc.ProgramLogVar, log); err != nil {
		fmt.Fprintln(os.Stderr, "program log:", err)
		return 1
	}
	code := p.m.Run()
	missing, err := unlistedPrograms(log)
	if err != nil {
		fmt.Fprintln(os.Stderr, "program log:", err)
		return 1
	}
	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "FAIL: homebot ran or looked up programs that are not in selfname.ToolNames: %s\n"+
			"Add each to ToolNames (internal/selfname/words.go), so no one can name their homebot after it.\n",
			strings.Join(missing, ", "))
		return 1
	}
	return code
}

// unlistedPrograms reads the program log and returns the names in it that
// are not in selfname.ToolNames, sorted. A missing log (no script ran
// homebot) is no names.
func unlistedPrograms(log string) ([]string, error) {
	data, err := os.ReadFile(log)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, name := range strings.Fields(string(data)) {
		if !slices.Contains(selfname.ToolNames, name) && !slices.Contains(missing, name) {
			missing = append(missing, name)
		}
	}
	slices.Sort(missing)
	return missing, nil
}

// TestScripts runs the end-to-end scripts in testdata/script.
func TestScripts(t *testing.T) {
	files := scripts(t, "script")
	if len(files) == 0 {
		t.Fatal("no testdata/script/*.txtar scripts")
	}
	testscript.Run(t, scriptParams(t, files))
}

// TestParity runs the transcripts recorded from the TypeScript build in
// testdata/parity, help pages included. Every homebot they run must exit
// with the code its "# exit: N" comment recorded (exitcomments_test.go). A
// script that skips fails (noSkipT).
func TestParity(t *testing.T) {
	files := scripts(t, "parity")
	if len(files) == 0 {
		t.Fatal("no testdata/parity/*.txtar scripts")
	}
	testscript.RunT(noSkipT{stdT{t}, runtime.GOOS}, scriptParams(t, withExitCodes(t, files)))
}

// recordedOnLinux is the one reason a parity script may skip, and only off
// Linux: the goldens hold Linux paths, layers and tools.
const recordedOnLinux = "recorded on linux"

// stdT adapts a *testing.T to testscript.T, as testscript.Run does.
type stdT struct{ *testing.T }

func (t stdT) Run(name string, f func(testscript.T)) {
	t.T.Run(name, func(t *testing.T) { f(stdT{t}) })
}

func (stdT) Verbose() bool { return testing.Verbose() }

// noSkipT fails the scripts that skip. Every command is ported, so a skip in
// a parity script is a gap in the port, not a pending command. The one
// exception is the goldens' own platform: off Linux a script may skip with
// recordedOnLinux (CI runs the macOS suite too).
type noSkipT struct {
	testscript.T
	goos string
}

func (t noSkipT) Skip(args ...any) {
	reason := strings.TrimSpace(fmt.Sprint(args...))
	if t.goos != "linux" && reason == recordedOnLinux {
		t.T.Skip(args...)
		return
	}
	t.Fatal("a parity script must not skip, and this one did: " + reason)
}

func (t noSkipT) Run(name string, f func(testscript.T)) {
	t.T.Run(name, func(in testscript.T) { f(noSkipT{in, t.goos}) })
}

// scripts lists the .txtar files in the repo's testdata/<dir>.
func scripts(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", dir, "*.txtar"))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// The program log check catches a name missing from ToolNames, once, and
// passes the listed ones.
func TestUnlistedPrograms(t *testing.T) {
	log := filepath.Join(t.TempDir(), "programs")
	if got, err := unlistedPrograms(log); err != nil || got != nil {
		t.Errorf("no log: %q, %v; want nothing", got, err)
	}
	if err := os.WriteFile(log, []byte("git\nzz-unlisted\nsystemctl\nzz-unlisted\naa-unlisted\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := unlistedPrograms(log)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"aa-unlisted", "zz-unlisted"}; !slices.Equal(got, want) {
		t.Errorf("unlistedPrograms = %q, want %q", got, want)
	}
}
