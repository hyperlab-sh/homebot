package proc_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func TestRunCapturesOutputAndExitCode(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	r := proc.Run(b.Ctx, []string{"sh", "-c", "echo out; echo err >&2; exit 3"}, proc.Options{})
	if r.Code != 3 || r.Stdout != "out\n" || r.Stderr != "err\n" || r.TimedOut {
		t.Errorf("Run = %+v, want code 3 with out and err", r)
	}
	r = proc.Run(b.Ctx, []string{"true"}, proc.Options{})
	if r.Code != 0 || r.Stdout != "" || r.Stderr != "" {
		t.Errorf("true: Run = %+v", r)
	}
}

func TestRunCannotStart(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	plain := filepath.Join(b.Bin, "plain")
	if err := os.WriteFile(plain, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(b.Bin, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		argv   []string
		opts   proc.Options
		stderr string
	}{
		{"not on PATH", []string{"no-such-tool", "x"}, proc.Options{}, `Executable not found in $PATH: "no-such-tool"`},
		{"not executable", []string{"plain"}, proc.Options{}, `Executable not found in $PATH: "plain"`},
		{"a directory", []string{"adir"}, proc.Options{}, `Executable not found in $PATH: "adir"`},
		{"no argv", nil, proc.Options{}, "proc: no program to run"},
		{"path not executable", []string{plain}, proc.Options{}, "permission denied"},
		{"path missing", []string{filepath.Join(b.Bin, "gone")}, proc.Options{}, "no such file"},
		{"missing dir", []string{"true"}, proc.Options{Dir: filepath.Join(b.Dir, "gone")}, "no such file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := proc.Run(b.Ctx, tc.argv, tc.opts)
			if r.Code != proc.NotFound || !strings.Contains(r.Stderr, tc.stderr) {
				t.Errorf("Run = %+v, want code 127 and stderr containing %q", r, tc.stderr)
			}
		})
	}
}

func TestRunKilledBySignalIsCode1(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	r := proc.Run(b.Ctx, []string{"sh", "-c", "echo before; kill -9 $$"}, proc.Options{})
	if r.Code != 1 || r.TimedOut || r.Stdout != "before\n" {
		t.Errorf("Run = %+v, want code 1, not timed out, with the output so far", r)
	}
}

// within runs Run in the background and fails the test when it takes longer
// than limit.
func within(t *testing.T, limit time.Duration, c *app.Context, argv []string, opts proc.Options) proc.Result {
	t.Helper()
	done := make(chan proc.Result, 1)
	go func() { done <- proc.Run(c, argv, opts) }()
	select {
	case r := <-done:
		return r
	case <-time.After(limit):
		t.Fatalf("Run(%q) still running after %v", argv, limit)
		return proc.Result{}
	}
}

// readPid returns the pid a script wrote to file.
func readPid(t *testing.T, file string) int {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

// gone waits until no process answers kill(target, 0): a pid, or a negative
// process group id.
func gone(t *testing.T, target int) {
	t.Helper()
	for range 100 {
		if syscall.Kill(target, 0) != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("process %d outlived Run", target)
}

// killAtCleanup kills the process whose pid a script writes to file, so a
// grandchild Run leaves behind does not outlive the test.
func killAtCleanup(t *testing.T, file string) {
	t.Cleanup(func() {
		if data, err := os.ReadFile(file); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
}

// cancelOnFile returns a Context that is cancelled once file exists.
func cancelOnFile(t *testing.T, c *app.Context, file string) *app.Context {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		defer cancel()
		for range 250 {
			if _, err := os.Stat(file); err == nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	return c.WithContext(ctx)
}

func TestRunTimeout(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	pidFile := filepath.Join(b.Dir, "child.pid")
	script := `echo $$ > "$1"; exec sleep 30`
	r := within(t, 3*time.Second, b.Ctx, []string{"sh", "-c", script, "sh", pidFile}, proc.Options{Timeout: 300 * time.Millisecond})
	if r.Code != 1 || !r.TimedOut {
		t.Errorf("Run = %+v, want code 1 and TimedOut", r)
	}
	gone(t, readPid(t, pidFile))

	r = proc.Run(b.Ctx, []string{"sh", "-c", "exit 4"}, proc.Options{Timeout: 10 * time.Second})
	if r.Code != 4 || r.TimedOut {
		t.Errorf("fast program under a timeout: Run = %+v", r)
	}
}

func TestRunTimeoutKillsAProgramThatIgnoresSIGTERM(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	pidFile := filepath.Join(b.Dir, "child.pid")
	script := `trap "" TERM; echo $$ > "$1"; exec sleep 30`
	r := within(t, 3*time.Second, b.Ctx, []string{"sh", "-c", script, "sh", pidFile}, proc.Options{Timeout: 200 * time.Millisecond})
	if r.Code != 1 || !r.TimedOut {
		t.Errorf("Run = %+v, want code 1 and TimedOut", r)
	}
	gone(t, readPid(t, pidFile))
}

func TestRunStopsWhenTheContextIsCancelled(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	pidFile := filepath.Join(b.Dir, "child.pid")
	c := cancelOnFile(t, b.Ctx, pidFile)
	script := `echo $$ > "$1"; exec sleep 30`
	r := within(t, 2*time.Second, c, []string{"sh", "-c", script, "sh", pidFile}, proc.Options{})
	if r.Code != 1 || r.TimedOut {
		t.Errorf("Run = %+v, want code 1, not TimedOut", r)
	}
	gone(t, readPid(t, pidFile))

	r = proc.Run(c, []string{"true"}, proc.Options{})
	if r.Code != 1 || !strings.Contains(r.Stderr, "context canceled") {
		t.Errorf("Run under a cancelled Context = %+v, want code 1 rather than 127", r)
	}
}

func TestRunSendsSIGTERMBeforeSIGKILL(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	pidFile := filepath.Join(b.Dir, "child.pid")
	c := cancelOnFile(t, b.Ctx, pidFile)
	script := `trap 'echo got TERM; exit 3' TERM; echo $$ > "$1"; while :; do sleep 0.05; done`
	r := within(t, 2*time.Second, c, []string{"sh", "-c", script, "sh", pidFile}, proc.Options{})
	if r.Code != 1 || r.Stdout != "got TERM\n" {
		t.Errorf("Run = %+v, want code 1 after the program handled SIGTERM", r)
	}
}

func TestRunStopsOnlyTheProgramByDefault(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	pidFile := filepath.Join(b.Dir, "grandchild.pid")
	killAtCleanup(t, pidFile)
	c := cancelOnFile(t, b.Ctx, pidFile)
	script := `sleep 30 & echo $! > "$1"; wait`
	r := within(t, 3*time.Second, c, []string{"sh", "-c", script, "sh", pidFile}, proc.Options{})
	if r.Code != 1 || r.TimedOut {
		t.Errorf("Run = %+v, want code 1, not TimedOut", r)
	}
	if pid := readPid(t, pidFile); syscall.Kill(pid, 0) != nil {
		t.Errorf("grandchild %d was stopped without Options.Group", pid)
	}
}

func TestRunGroupStopsTheWholePipeline(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	// $1 gets the shell's pid, which is the group id, and $2 the pid of the
	// pipeline's first stage, a grandchild. The patient stage records the
	// SIGTERM it gets in $2.term.
	stage := `sh -c 'echo $$ > "$0"; exec sleep 30' "$2" | cat`
	patient := `sh -c 'trap "echo > \"\$0.term\"; exit 0" TERM; echo $$ > "$0"; while :; do sleep 0.05; done' "$2" | cat`
	cases := []struct {
		name   string
		script string
		cancel bool
	}{
		{"timeout", `echo $$ > "$1"; ` + stage, false},
		{"timeout, SIGTERM ignored", `trap "" TERM; echo $$ > "$1"; ` + stage, false},
		{"cancel", `echo $$ > "$1"; ` + patient, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			leaderFile, stageFile := filepath.Join(dir, "leader.pid"), filepath.Join(dir, "stage.pid")
			killAtCleanup(t, stageFile)
			c, opts := b.Ctx, proc.Options{Group: true}
			if tc.cancel {
				c = cancelOnFile(t, c, stageFile)
			} else {
				opts.Timeout = 500 * time.Millisecond
			}
			r := within(t, 3*time.Second, c, []string{"sh", "-c", tc.script, "sh", leaderFile, stageFile}, opts)
			if r.Code != 1 || r.TimedOut == tc.cancel {
				t.Errorf("Run = %+v, want code 1 and TimedOut %v", r, !tc.cancel)
			}
			gone(t, readPid(t, stageFile))
			gone(t, -readPid(t, leaderFile))
			if _, err := os.Stat(stageFile + ".term"); tc.cancel && err != nil {
				t.Errorf("the pipeline's first stage got no SIGTERM: %v", err)
			}
		})
	}
}

func TestRunProcessGroup(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	script := `echo $$; ps -o pgid= -p $$`
	pids := func(opts proc.Options) (pid, pgid string) {
		t.Helper()
		r := proc.Run(b.Ctx, []string{"sh", "-c", script}, opts)
		f := strings.Fields(r.Stdout)
		if r.Code != 0 || len(f) != 2 {
			t.Fatalf("Run = %+v, want a pid and a process group id", r)
		}
		return f[0], f[1]
	}

	ours := strconv.Itoa(syscall.Getpgrp())
	if _, pgid := pids(proc.Options{}); pgid != ours {
		t.Errorf("default: child process group = %s, want ours, %s", pgid, ours)
	}
	if pid, pgid := pids(proc.Options{Group: true}); pgid != pid || pgid == ours {
		t.Errorf("Group: child process group = %s, want its own pid %s", pgid, pid)
	}
}

func TestRunDoesNotWaitForAGrandchildHoldingThePipes(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	pidFile := filepath.Join(b.Dir, "grandchild.pid")
	killAtCleanup(t, pidFile)
	script := `sleep 30 & echo $! > "$1"; echo done`
	r := within(t, 3*time.Second, b.Ctx, []string{"sh", "-c", script, "sh", pidFile}, proc.Options{})
	if r.Code != 0 || r.Stdout != "done\n" {
		t.Errorf("Run = %+v, want code 0 and done", r)
	}
}

func TestRunUsesTheContextPath(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	elsewhere := t.TempDir()
	if err := os.WriteFile(filepath.Join(elsewhere, "only-in-process"), []byte("#!/bin/sh\necho leaked\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", elsewhere+string(filepath.ListSeparator)+os.Getenv("PATH"))
	if r := proc.Run(b.Ctx, []string{"only-in-process"}, proc.Options{}); r.Code != proc.NotFound {
		t.Errorf("a program on the process PATH ran: %+v", r)
	}

	b.WithStubs(map[string]string{"git": "echo stub git \"$@\""})
	if r := proc.Run(b.Ctx, []string{"git", "status"}, proc.Options{}); r.Stdout != "stub git status\n" {
		t.Errorf("a stub in $WORK/bin must shadow the real git: %+v", r)
	}
	if r := proc.Run(b.Ctx, []string{"sh", "-c", "echo $PATH"}, proc.Options{}); r.Stdout != b.Ctx.Env.Get("PATH")+"\n" {
		t.Errorf("child PATH = %q, want the Context's %q", r.Stdout, b.Ctx.Env.Get("PATH"))
	}

	r := proc.Run(b.Ctx, []string{"only-in-process"}, proc.Options{Env: map[string]string{"PATH": elsewhere}})
	if r.Stdout != "leaked\n" {
		t.Errorf("Options.Env PATH must drive the lookup: %+v", r)
	}
}

func TestRunEnvironment(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	t.Setenv("HOMEBOT_LEAK", "from the process")
	script := `printf '%s|%s|%s|%s' "$EXTRA" "$HOME" "$HOMEBOT_LEAK" "$NO_COLOR"`
	r := proc.Run(b.Ctx, []string{"sh", "-c", script}, proc.Options{Env: map[string]string{"EXTRA": "x", "HOME": "/elsewhere"}})
	if want := "x|/elsewhere||1"; r.Stdout != want {
		t.Errorf("child env = %q, want %q", r.Stdout, want)
	}
	if b.Ctx.Env.Get("HOME") != b.Home {
		t.Error("Options.Env changed the Context's Env")
	}
}

func TestRunStdinAndDir(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	if r := proc.Run(b.Ctx, []string{"cat"}, proc.Options{Stdin: strings.NewReader("fed\n")}); r.Stdout != "fed\n" {
		t.Errorf("cat with stdin: %+v", r)
	}
	if r := proc.Run(b.Ctx, []string{"cat"}, proc.Options{}); r.Code != 0 || r.Stdout != "" {
		t.Errorf("cat without stdin must see end of input at once: %+v", r)
	}
	want, err := filepath.EvalSymlinks(b.Root)
	if err != nil {
		t.Fatal(err)
	}
	if r := proc.Run(b.Ctx, []string{"pwd", "-P"}, proc.Options{Dir: b.Root}); r.Stdout != want+"\n" {
		t.Errorf("pwd in Dir = %q, want %q", r.Stdout, want)
	}
}

// A program run in Options.Dir sees that folder as Dir names it: $PWD, and
// a shell's pwd with it, name the link a home sits behind (or macOS's /var),
// not the folder it resolves to, and not homebot's own PWD.
func TestRunNamesDirAsGiven(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{BehindLink: true})
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PWD": b.Dir}) // homebot's own
	shown := `printf '%s|%s' "$PWD" "$(pwd)"`
	if r := proc.Run(b.Ctx, []string{"sh", "-c", shown}, proc.Options{Dir: b.Root}); r.Stdout != b.Root+"|"+b.Root {
		t.Errorf("in Dir: %q, want %s twice", r.Stdout, b.Root)
	}
	// A relative Dir is taken from homebot's working folder, as exec takes it.
	if r := proc.Run(b.Ctx, []string{"printenv", "PWD"}, proc.Options{Dir: "."}); !filepath.IsAbs(strings.TrimSpace(r.Stdout)) {
		t.Errorf("relative Dir: PWD = %q, want an absolute path", r.Stdout)
	}
	// The kernel still has the real folder.
	real, err := filepath.EvalSymlinks(b.Root)
	if err != nil {
		t.Fatal(err)
	}
	if r := proc.Run(b.Ctx, []string{"pwd", "-P"}, proc.Options{Dir: b.Root}); r.Stdout != real+"\n" {
		t.Errorf("pwd -P = %q, want %q", r.Stdout, real)
	}
	// Options.Env has the last word.
	if r := proc.Run(b.Ctx, []string{"printenv", "PWD"}, proc.Options{Dir: b.Root, Env: map[string]string{"PWD": "/given"}}); r.Stdout != "/given\n" {
		t.Errorf("Options.Env PWD: %q", r.Stdout)
	}
	// Without Dir the program runs where homebot does, and PWD stays homebot's.
	if r := proc.Run(b.Ctx, []string{"printenv", "PWD"}, proc.Options{}); r.Stdout != b.Dir+"\n" {
		t.Errorf("without Dir: PWD = %q, want %q", r.Stdout, b.Dir)
	}
	if b.Ctx.Env.Get("PWD") != b.Dir {
		t.Error("Run changed the Context's Env")
	}
}

func TestRunLogsOneDebugLinePerCall(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	var log bytes.Buffer
	b.Ctx.Debug = slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug}))

	proc.Run(b.Ctx, []string{"sh", "-c", "exit 2"}, proc.Options{Dir: b.Root})
	proc.Run(b.Ctx, []string{"no-such-tool"}, proc.Options{})

	lines := strings.Split(strings.TrimSuffix(log.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d debug lines, want one per call:\n%s", len(lines), log.String())
	}
	for _, want := range []string{`msg=run`, `argv="sh -c exit 2"`, "dir=" + b.Root, "code=2", "timedOut=false", "took=0s"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("line %q lacks %q", lines[0], want)
		}
	}
	if !strings.Contains(lines[1], "code=127") {
		t.Errorf("line %q lacks code=127", lines[1])
	}
}

func TestLookPath(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	b.WithStubs(map[string]string{"tool": "true"})

	if got, err := proc.LookPath(b.Ctx, "tool"); err != nil || got != filepath.Join(b.Bin, "tool") {
		t.Errorf("LookPath(tool) = %q, %v", got, err)
	}
	if got, err := proc.LookPath(b.Ctx, "./anything/at/all"); err != nil || got != "./anything/at/all" {
		t.Errorf("a name with a slash must come back as it is: %q, %v", got, err)
	}
	if !proc.HasCommand(b.Ctx, "tool") || !proc.HasCommand(b.Ctx, "sh") || proc.HasCommand(b.Ctx, "no-such-tool") {
		t.Error("HasCommand disagrees with the Context's PATH")
	}

	t.Chdir(b.Dir)
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": "bin"})
	if proc.HasCommand(b.Ctx, "tool") {
		t.Error("a relative PATH entry supplied a program")
	}
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": ""})
	if proc.HasCommand(b.Ctx, "sh") {
		t.Error("an empty PATH found sh")
	}
}
