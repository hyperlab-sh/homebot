package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hyperlab-sh/homebot/internal/app"
)

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

// startupFailure makes the options that app.New fails on, and what it says
// in the neutral voice.
func startupFailure(t *testing.T, kind string, env map[string]string) (app.Options, string, *bytes.Buffer) {
	t.Helper()
	work := t.TempDir()
	stderr := &bytes.Buffer{}
	o := app.Options{Now: time.Now, OS: app.Linux, Stderr: stderr, Stdout: &bytes.Buffer{}}
	vars := map[string]string{"HOME": filepath.Join(work, "home")}
	for k, v := range env {
		vars[k] = v
	}
	var want string
	switch kind {
	case "no home":
		delete(vars, "HOME")
		want = "HOME is not set."
	case "config":
		// main never sets Root, so a config error cannot reach reportStartup
		// through it; this is a Said error of the kind New can return, to test
		// that reportStartup prints one like any other.
		root := filepath.Join(work, "repo")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "homebot.yml"), []byte("profiles: 3\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		o.Root = root
		want = filepath.Join(root, "homebot.yml") + ":1:11: profiles must be a mapping"
	}
	o.Env = app.NewEnv(vars)
	return o, want, stderr
}

// The error app.New returns is printed as every other error is: the same
// "homebot: " line, coloured as the others are when stderr shows colour,
// exit code 1.
func TestStartupErrorsArePrintedLikeOtherErrors(t *testing.T) {
	for _, kind := range []string{"no home", "config"} {
		t.Run(kind, func(t *testing.T) {
			t.Run("plain", func(t *testing.T) {
				o, want, stderr := startupFailure(t, kind, map[string]string{"NO_COLOR": "1"})
				_, err := app.New(o)
				if err == nil {
					t.Fatal("app.New succeeded")
				}
				if code := reportStartup(o, err); code != 1 {
					t.Errorf("exit code = %d, want 1", code)
				}
				if got, wantOut := stderr.String(), "homebot: "+want+"\n"; got != wantOut {
					t.Errorf("stderr = %q, want %q", got, wantOut)
				}
			})
			t.Run("coloured", func(t *testing.T) {
				o, want, stderr := startupFailure(t, kind, map[string]string{"CLICOLOR_FORCE": "1", "TERM": "xterm-256color"})
				_, err := app.New(o)
				if err == nil {
					t.Fatal("app.New succeeded")
				}
				reportStartup(o, err)
				got := stderr.String()
				if !strings.Contains(got, "\x1b[") {
					t.Errorf("stderr = %q, want the error style the other errors have", got)
				}
				if plain := ansi.ReplaceAllString(got, ""); plain != "homebot: "+want+"\n" {
					t.Errorf("stderr without styles = %q, want %q", plain, "homebot: "+want+"\n")
				}
			})
		})
	}
}

// Options with no streams and no environment still print, to nowhere.
func TestStartupErrorWithNoOptionsDoesNotPanic(t *testing.T) {
	// No streams at all, and an empty environment: nothing to print through,
	// and nothing may panic.
	if code := reportStartup(app.Options{}, os.ErrNotExist); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}
