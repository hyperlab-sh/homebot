package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

// fakeT records what a script run reports instead of failing the test, so a
// test can check that the harness fails when it should.
type fakeT struct {
	log    strings.Builder
	failed bool
}

var errAbort = errors.New("script aborted")

func (t *fakeT) Skip(args ...any)  { t.Log(args...); panic(errAbort) }
func (t *fakeT) Fatal(args ...any) { t.Log(args...); t.FailNow() }
func (t *fakeT) Parallel()         {}
func (t *fakeT) Log(args ...any)   { fmt.Fprint(&t.log, args...) }
func (t *fakeT) FailNow()          { t.failed = true; panic(errAbort) }
func (t *fakeT) Verbose() bool     { return false }
func (t *fakeT) Run(_ string, f func(testscript.T)) {
	defer catchAbort()
	f(t)
}

func catchAbort() {
	if r := recover(); r != nil {
		if err, ok := r.(error); !ok || !errors.Is(err, errAbort) {
			panic(r)
		}
	}
}

// runScript runs one script through the real harness and reports whether it
// failed, with its log.
func runScript(t *testing.T, script string) (bool, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.txtar")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	ft := &fakeT{}
	func() {
		defer catchAbort()
		testscript.RunT(ft, scriptParams(t, []string{path}))
	}()
	return ft.failed, ft.log.String()
}

func TestHarnessFailures(t *testing.T) {
	cases := []struct {
		name   string
		script string
		want   []string
	}{
		{
			name:   "cmpwork mismatch shows a diff",
			script: "exec sh -c 'echo \"$WORK/x\"'\ncmpwork stdout want\n-- want --\n$WORK/y\n",
			want:   []string{"stdout and want differ", "-$WORK/x", "+$WORK/y"},
		},
		{
			name:   "cmpwork does not expand the golden",
			script: "exec sh -c 'echo \"$WORK\" > want; echo \"$WORK\"'\ncmpwork stdout want\n",
			want:   []string{"stdout and want differ"},
		},
		{
			name:   "cmpwork compares bytes",
			script: "exec sh -c 'printf x'\ncmpwork stdout want\n-- want --\nx\n",
			want:   []string{"stdout and want differ"},
		},
		{
			name:   "negated cmpwork",
			script: "exec true\n! cmpwork stdout want\n-- want --\n",
			want:   []string{"unsupported: ! cmpwork"},
		},
		{
			name:   "cmpwork with one argument",
			script: "exec true\ncmpwork stdout\n",
			want:   []string{"usage: cmpwork <file|stdout|stderr> <golden>"},
		},
		{
			name:   "cmpwork without its golden",
			script: "exec true\ncmpwork stdout missing\n",
			want:   []string{"missing"},
		},
		{
			name:   "unknown condition",
			script: "[bogus] exec true\n",
			want:   []string{`unknown condition "bogus"`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			failed, log := runScript(t, tc.script)
			if !failed {
				t.Fatalf("the script passed; log:\n%s", log)
			}
			for _, want := range tc.want {
				if !strings.Contains(log, want) {
					t.Errorf("log lacks %q:\n%s", want, log)
				}
			}
		})
	}
}

func TestHarnessPasses(t *testing.T) {
	script := "exec sh -c 'echo \"$WORK $WORK\"'\ncmpwork stdout want\n-- want --\n$WORK $WORK\n"
	if failed, log := runScript(t, script); failed {
		t.Errorf("a matching cmpwork failed:\n%s", log)
	}
}

// A parity script that skips fails the run, except off Linux for the one
// reason the goldens' platform gives.
func TestParityScriptsCannotSkip(t *testing.T) {
	cases := []struct {
		name, goos, reason string
		wantFailed         bool
	}{
		{"any skip on linux", "linux", "needs something", true},
		{"the platform skip on linux", "linux", recordedOnLinux, true},
		{"the platform skip on macOS", "darwin", recordedOnLinux, false},
		{"another skip on macOS", "darwin", "not ported yet", true},
		{"a skip with no reason on macOS", "darwin", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "s.txtar")
			script := "skip '" + tc.reason + "'\n"
			if tc.reason == "" {
				script = "skip\n"
			}
			if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
				t.Fatal(err)
			}
			ft := &fakeT{}
			func() {
				defer catchAbort()
				testscript.RunT(noSkipT{ft, tc.goos}, scriptParams(t, []string{path}))
			}()
			if ft.failed != tc.wantFailed {
				t.Errorf("failed = %v, want %v; log:\n%s", ft.failed, tc.wantFailed, ft.log.String())
			}
			if tc.wantFailed && !strings.Contains(ft.log.String(), "must not skip") {
				t.Errorf("log lacks the reason for failing:\n%s", ft.log.String())
			}
		})
	}
}
