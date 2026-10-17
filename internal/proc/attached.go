package proc

import (
	"errors"
	"io"
	"os/exec"
	"syscall"
	"time"

	"github.com/hyperlab-sh/homebot/internal/app"
)

// AttachedOptions adjust how RunAttached starts a program.
type AttachedOptions struct {
	// Dir is the working directory; empty means the current one. PWD is set
	// to match (see InDir).
	Dir string
	// Env is the environment the program starts with, before PWD and Vars:
	// usually the Context's (c.Env), or that with some variables dropped.
	// An empty Env is an empty environment.
	Env app.Env
	// Vars are set over Env, PWD included.
	Vars map[string]string
	// Stdin, Stdout and Stderr are the program's streams; a nil one is the
	// null device, as os/exec has it.
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// Grace, when positive, makes a stop (c.Context() being cancelled) send
	// SIGTERM first and SIGKILL only after this long, so a program such as
	// lazygit can restore the terminal. Zero kills at once.
	Grace time.Duration
}

// attachedPipeWait is how long RunAttached waits, once the program has
// exited, for the streams it was given that are not files. os/exec feeds
// those through pipes and waits until every holder of a pipe has closed it:
// a process the program left behind holding its stderr (an ssh
// ProxyCommand, a credential helper turned daemon) would otherwise hang
// homebot until that process exits. What the program wrote before it
// exited has been copied by then.
const attachedPipeWait = killGrace

// RunAttached runs argv to completion with the streams it is given instead
// of capturing them, so the program can prompt and sees a terminal where
// they are one, and returns its exit code. A program killed by a signal is
// code 1, as in Run. The program is found on the Context's PATH. The error
// is set, with code NotFound, only when the program could not run at all:
// not found, or not started. Unlike Run it writes no debug line and has no
// timeout, but like Run it does not wait long for a process the program left
// behind (attachedPipeWait).
//
// A program homebot runs by name goes into selfname.ToolNames, so no one
// can name their homebot after it (naming plan D3).
func RunAttached(c *app.Context, argv []string, opts AttachedOptions) (int, error) {
	if len(argv) == 0 {
		return NotFound, errors.New("proc: no program to run")
	}
	logProgram(c.Env, argv[0])
	env := InDir(opts.Env, opts.Dir)
	if len(opts.Vars) > 0 {
		env = env.With(opts.Vars)
	}
	bin, err := lookPath(c.Env, argv[0]) // the Context's PATH, as Run; logged above
	if err != nil {
		return NotFound, err
	}
	cmd := exec.CommandContext(c.Context(), bin, argv[1:]...)
	cmd.Args[0] = argv[0]
	cmd.Dir = opts.Dir
	cmd.Env = env.Environ()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = opts.Stdin, opts.Stdout, opts.Stderr
	cmd.WaitDelay = attachedPipeWait
	if opts.Grace > 0 {
		cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
		cmd.WaitDelay = opts.Grace
	}
	err = cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil, errors.Is(err, exec.ErrWaitDelay):
		// ErrWaitDelay: the program exited 0, and something it left
		// behind still held a pipe when the wait ran out.
		return 0, nil
	case errors.As(err, &exit):
		if code := exit.ExitCode(); code >= 0 {
			return code, nil
		}
		return 1, nil // killed by a signal; the TypeScript build reported 1 too
	}
	return NotFound, err
}
