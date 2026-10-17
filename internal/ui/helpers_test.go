package ui_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// unset removes a variable of the sandbox environment in term's env.
const unset = "\x00unset"

// terminal is a Context over the sandbox environment with env applied, the
// given stdin, and buffers for stdout and stderr. The buffers are not
// terminals; TTY_FORCE=1 makes colorprofile treat them as ones.
type terminal struct {
	ctx            *app.Context
	stdout, stderr *bytes.Buffer
}

func newTerminal(t *testing.T, env map[string]string, stdin string) terminal {
	t.Helper()
	work := t.TempDir()
	vars := testutil.SandboxEnv(work)
	for k, v := range env {
		if v == unset {
			delete(vars, k)
		} else {
			vars[k] = v
		}
	}
	term := terminal{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	ctx, err := app.New(app.Options{
		Env:    app.NewEnv(vars),
		Cwd:    work,
		OS:     app.Linux,
		Stdin:  strings.NewReader(stdin),
		Stdout: term.stdout,
		Stderr: term.stderr,
	})
	if err != nil {
		t.Fatal(err)
	}
	term.ctx = ctx
	return term
}

// trueColor is a terminal environment that shows every colour.
var trueColor = map[string]string{"NO_COLOR": unset, "TTY_FORCE": "1", "TERM": "xterm-256color", "COLORTERM": "truecolor"}

func with(base map[string]string, more map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range more {
		out[k] = v
	}
	return out
}
