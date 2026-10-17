// Package proc runs other programs: git, gitleaks, gh and the like. Every
// call takes its environment, PATH and cancellation from the app.Context,
// never from the process, and logs one debug line.
package proc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/hyperlab-sh/homebot/internal/app"
)

// NotFound is the exit code Run reports when the program cannot be started,
// as a shell does.
const NotFound = 127

// Options adjust how Run starts a program.
type Options struct {
	// Dir is the working directory; empty means the current one.
	Dir string
	// Env is set over the Context's environment for this call.
	Env map[string]string
	// Stdin is fed to the program; nil gives it no input.
	Stdin io.Reader
	// Timeout stops the program after this long; zero means no limit.
	Timeout time.Duration
	// Group runs the program in a process group of its own, so a timeout or
	// cancellation stops everything it started, such as every stage of
	// sh -c 'a | b', not only the program. A program in its own group is
	// not in the terminal's foreground group and must never need the
	// terminal: reading from it, as sudo, ssh, gpg and git credential
	// prompts do, stops the program with SIGTTIN. Use it only for programs
	// that run unattended.
	Group bool
}

// Result is how a program ended and what it printed.
type Result struct {
	// Code is the exit code: NotFound when the program could not start, and
	// 1 when it was killed by a signal or stopped by the timeout or the
	// Context.
	Code   int
	Stdout string
	Stderr string
	// TimedOut says the program was killed because Options.Timeout passed.
	TimedOut bool
}

// Run runs argv to completion and captures its output. A non-zero exit is a
// Result, never an error. The program stays in homebot's process group, as
// it did in the TypeScript build, so it can prompt on the terminal and a
// Ctrl-C there reaches it directly. When Options.Timeout passes or
// c.Context() is cancelled, the program gets SIGTERM, then SIGKILL after a
// grace period; with Options.Group, its whole process group does.
//
// A program homebot runs by name goes into selfname.ToolNames, so no one
// can name their homebot after it (naming plan D3).
func Run(c *app.Context, argv []string, opts Options) Result {
	if len(argv) > 0 {
		logProgram(c.Env, argv[0])
	}
	start := c.Now()
	r := run(c, argv, opts)
	c.Debug.Debug("run",
		"argv", strings.Join(argv, " "),
		"dir", opts.Dir,
		"code", r.Code,
		"timedOut", r.TimedOut,
		"took", c.Now().Sub(start))
	return r
}

// killGrace is how long a stopped program has to exit after SIGTERM before
// it gets SIGKILL, and how long Run waits for the output pipes once the
// program has exited. Programs like git remove their lock files on SIGTERM
// well within it.
const killGrace = 500 * time.Millisecond

func run(c *app.Context, argv []string, opts Options) Result {
	if len(argv) == 0 {
		return Result{Code: NotFound, Stderr: "proc: no program to run"}
	}
	env := InDir(c.Env, opts.Dir)
	if len(opts.Env) > 0 {
		env = env.With(opts.Env)
	}
	path, err := lookPath(env, argv[0])
	if err != nil {
		return Result{Code: NotFound, Stderr: err.Error()}
	}

	ctx := c.Context()
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, path, argv[1:]...)
	cmd.Args[0] = argv[0]
	cmd.Dir = opts.Dir
	cmd.Env = env.Environ()
	cmd.Stdin = opts.Stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	stop := func(sig syscall.Signal) error { return cmd.Process.Signal(sig) }
	if opts.Group {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		stop = func(sig syscall.Signal) error { return killGroup(cmd, sig) }
	}
	cmd.Cancel = func() error { return stop(syscall.SIGTERM) }
	// After a cancel, wait this long before SIGKILL; once the program has
	// exited, wait this long for the pipes, so a grandchild that keeps them
	// open cannot hang Run.
	cmd.WaitDelay = killGrace

	if err := cmd.Start(); err != nil {
		if ctx.Err() != nil {
			return Result{Code: 1, Stderr: ctx.Err().Error(), TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded)}
		}
		return Result{Code: NotFound, Stderr: err.Error()}
	}
	waitErr := cmd.Wait()
	if opts.Group && ctx.Err() != nil {
		_ = killGroup(cmd, syscall.SIGKILL)
	}
	r := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	switch {
	case cmd.ProcessState == nil:
		r.Code = 1
		r.Stderr += fmt.Sprint(waitErr)
	case ctx.Err() != nil && !cmd.ProcessState.Success():
		r.Code = 1 // stopped by Options.Timeout or the Context
		r.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
	case cmd.ProcessState.ExitCode() < 0:
		r.Code = 1 // killed by a signal; the TypeScript build reported 1 too
	default:
		r.Code = cmd.ProcessState.ExitCode()
	}
	return r
}

// InDir is env for a program that runs in dir: PWD names dir as given, the
// way os/exec sets it when it builds the environment itself (homebot always
// passes one, so it must say so). Without it the program keeps homebot's
// PWD, which names another folder, and a shell's pwd falls back to the
// folder with every link resolved: a home behind a link, or macOS's temp
// dir under /var (a link to /private/var), reads as another path than the
// HOMEBOT_* variables give. An empty dir is homebot's own folder, and PWD
// stays as it is.
func InDir(env app.Env, dir string) app.Env {
	if dir == "" {
		return env
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return env.Without("PWD")
	}
	return env.With(map[string]string{"PWD": abs})
}

// killGroup sends sig to the program's process group. A group that is gone
// already is not an error.
func killGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	err := syscall.Kill(-cmd.Process.Pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
