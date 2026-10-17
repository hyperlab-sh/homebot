// Package app holds Context, the one way homebot code reaches the outside
// world: the environment, the clock, the standard streams, randomness and the
// directories homebot reads and writes. No other package reads os.Getenv,
// time.Now, os.Stdin, os.Stdout or os.Stderr; cmd/homebot builds the real
// Context from the process, and tests build one from a sandbox
// (internal/testutil).
//
// # Test hooks
//
// In a binary built by "go test", where testing.Testing reports true, New
// reads three variables from the Context's Env and lets them win over what
// the caller passed:
//
//   - HOMEBOT_TEST_NOW (milliseconds since the epoch) fixes Now;
//   - HOMEBOT_TEST_SEED seeds Rand;
//   - HOMEBOT_TEST_HOST fixes Host;
//   - HOMEBOT_TEST_TTY, 1 or 0, sets StdoutTerminal: whether stdout counts
//     as a terminal (StdoutIsTerminal).
//
// New also panics there when the state, cache, data or config directory lies
// outside the temp dir, so no test can write to a real home.
//
// A release binary is built by "go build", where testing.Testing is false at
// link time: these variables have no effect and nothing can turn them on.
// testscript runs the homebot command inside the test binary, so scripts get
// the hooks too.
package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"

	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
)

// Context is everything a command needs from the machine it runs on.
type Context struct {
	// Root is the home repo, the directory holding homebot.yml. It is empty
	// until LocateRoot finds it.
	Root string
	Home string
	OS   OS
	// Host is the short host name: the part before the first dot.
	Host string
	// Arch is the CPU architecture, named the way Node names it (x64, arm64).
	Arch string
	// BuildVersion and BuildChannel are set by cli.NewRoot for update checks.
	// They are copied with the Context and never read from homebot.yml.
	BuildVersion string
	BuildChannel string

	// StateDir holds per-machine state (links made, secret hashes,
	// timestamps). It is never committed.
	StateDir string
	// CacheDir holds disposable output such as the shell nudge line.
	CacheDir string
	// DataDir holds files homebot installs for itself, such as hooks.
	DataDir string
	// ConfigDir holds the user's own homebot files, such as personas.
	ConfigDir string

	// Cwd is the working directory: root location starts here, and relative
	// paths the user types resolve against it.
	Cwd string
	// Exe is the path of the running binary.
	Exe string

	Env    Env
	Now    func() time.Time
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// StdinIsTerminal overrides ui.Interactive's look at Stdin. Only tests
	// set it; nil means ask Stdin itself.
	StdinIsTerminal *bool
	// StdoutTerminal overrides StdoutIsTerminal's look at Stdout. Only tests
	// set it (HOMEBOT_TEST_TTY in a test binary); nil means ask Stdout
	// itself.
	StdoutTerminal *bool
	Rand           *rand.Rand
	// Debug receives debug lines; they are discarded unless HOMEBOT_DEBUG is set.
	Debug *slog.Logger
	// Plain asks for the neutral voice: HOMEBOT_PLAIN set to anything but
	// "0", or --plain on the command line.
	Plain bool
	// ForceVoice speaks in the active persona even when stdout is not a
	// terminal: persona show, persona preview and the persona use greeting
	// set it on a Fork (plan S1). Plain still wins over it.
	ForceVoice bool

	// Config is homebot.yml, loaded once Root is known (where context.ts
	// loads it). Nil before that, and for commands that run without a repo.
	Config *config.Config
	// ConfigWarningsQuiet sends configuration warnings to the debug log for
	// a quiet helper run. The CLI sets it before loading the repo.
	ConfigWarningsQuiet bool
	warningsReported    bool

	// ctx carries cancellation (Ctrl-C, SIGTERM) to everything the command
	// does; see Context and WithContext.
	ctx context.Context
	// memo backs Once.
	memo *memo
	// process backs OncePerProcess: unlike memo, a Fork shares it.
	process *memo
}

// StdoutIsTerminal reports whether stdout is a terminal: StdoutTerminal when
// a test set it, else whether Stdout is a file that is a terminal. Which
// voice is in effect depends on it (persona.Speaking, plan S1).
func (c *Context) StdoutIsTerminal() bool {
	if c.StdoutTerminal != nil {
		return *c.StdoutTerminal
	}
	f, ok := c.Stdout.(term.File) //nolint:forbidigo // only asks whether it is a terminal; nothing is written
	return ok && term.IsTerminal(f.Fd())
}

// Fork returns a copy of c with env laid over its environment and a fresh
// memo, so what Once worked out from the environment (the persona, the
// palette, the terminal's background) is worked out again for the copy.
// persona show and persona preview speak as another persona this way:
//
//	f := c.Fork(map[string]string{"HOMEBOT_PERSONA": id})
//	f.ForceVoice = true
//
// The copy shares OncePerProcess values with c, so what happens once per
// process (the unusable-persona warning) does not happen again on a Fork.
// What New derived from the environment (the directories, Plain, Debug and
// the test hooks) is copied as it is, not derived again; set those on the
// copy when they must change.
func (c *Context) Fork(env map[string]string) *Context {
	c2 := *c
	c2.Env = c.Env.With(env)
	c2.memo = &memo{}
	return &c2
}

// Context returns the context that cancels the running command, as
// http.Request.Context does. It is context.Background() until WithContext
// sets one; the root command sets it before any command runs.
func (c *Context) Context() context.Context {
	if c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}

// WithContext returns a shallow copy of c whose Context is ctx. It panics on
// a nil ctx.
func (c *Context) WithContext(ctx context.Context) *Context {
	if ctx == nil {
		panic("app: WithContext with a nil context.Context")
	}
	c2 := *c
	c2.ctx = ctx
	return &c2
}

// Options are what New builds a Context from. cmd/homebot fills them from the
// running process; nothing in New reads the process itself.
type Options struct {
	Env Env
	Cwd string
	Exe string
	// Root skips root location when set.
	Root string
	// Home defaults to $HOME.
	Home string
	// OS defaults to the OS homebot was built for.
	OS OS
	// Host is the host name; New keeps the part before the first dot.
	Host string
	// Arch defaults to the architecture homebot was built for.
	Arch string
	// Now is the clock. Only a test binary may leave it nil, and only with
	// HOMEBOT_TEST_NOW set.
	Now func() time.Time
	// Stdin defaults to an empty reader; Stdout and Stderr discard by default.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// New builds a Context. It does not locate the home repo; commands that need
// it call LocateRoot. When o.Root is set, New loads its homebot.yml.
func New(o Options) (*Context, error) {
	env := o.Env
	home := o.Home
	if home == "" {
		home = env.Get("HOME")
	}
	if home == "" {
		return nil, fail.Say(msg.ContextErrorNoHome())
	}

	osName := o.OS
	if osName == "" {
		detected, err := DetectOS(runtime.GOOS)
		if err != nil {
			return nil, err
		}
		osName = detected
	} else if osName != Linux && osName != MacOS {
		return nil, unsupportedOS(string(osName))
	}

	arch := o.Arch
	if arch == "" {
		arch = nodeArch(runtime.GOARCH)
	}

	host := o.Host
	if h, ok := testHost(env); ok {
		host = h
	}
	host, _, _ = strings.Cut(host, ".")

	now := o.Now
	if clock, err := testClock(env); err != nil {
		return nil, err
	} else if clock != nil {
		now = clock
	}
	if now == nil {
		return nil, errors.New("app: Options.Now is nil")
	}

	rnd, err := testRand(env)
	if err != nil {
		return nil, err
	}
	if rnd == nil {
		rnd = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	}

	tty, err := testTTY(env)
	if err != nil {
		return nil, err
	}

	stdin, stdout, stderr := o.streams()

	root := o.Root
	if root != "" {
		root = filepath.Clean(root)
	}

	c := &Context{
		Root:           root,
		Home:           home,
		OS:             osName,
		Host:           host,
		Arch:           arch,
		StateDir:       filepath.Join(xdgDir(env, "XDG_STATE_HOME", home, ".local/state"), "homebot"),
		CacheDir:       filepath.Join(xdgDir(env, "XDG_CACHE_HOME", home, ".cache"), "homebot"),
		DataDir:        filepath.Join(xdgDir(env, "XDG_DATA_HOME", home, ".local/share"), "homebot"),
		ConfigDir:      filepath.Join(xdgDir(env, "XDG_CONFIG_HOME", home, ".config"), "homebot"),
		Cwd:            o.Cwd,
		Exe:            o.Exe,
		Env:            env,
		Now:            now,
		Stdin:          stdin,
		Stdout:         stdout,
		Stderr:         stderr,
		StdoutTerminal: tty,
		Rand:           rnd,
		Debug:          newDebugLogger(env, stderr),
		Plain:          on(env.Get("HOMEBOT_PLAIN")),
		memo:           &memo{},
		process:        &memo{},
	}
	guardDirs(c)
	if c.Root != "" {
		cfg, err := config.LoadDebug(c.Root, c.Debug)
		if err != nil {
			return nil, err
		}
		c.Config = cfg
	}
	return c, nil
}

// streams is the standard streams, each with its default when nil.
func (o Options) streams() (stdin io.Reader, stdout, stderr io.Writer) {
	stdin, stdout, stderr = o.Stdin, o.Stdout, o.Stderr
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	return stdin, stdout, stderr
}

// ForReport is a Context with only what printing an error needs: the
// environment and the streams. It is for the error New returned, which
// leaves no Context to print through: it never fails, so such an error is
// printed as any other is (cli.Report). It is no Context to run a command
// with.
//
// It is always Plain, whatever HOMEBOT_PLAIN says: a startup error is said
// in the neutral voice and never loads a persona. It has no Root, no Config
// and no ConfigDir, so there is nowhere safe to read a persona from: the
// error may be homebot.yml's own, and a persona looked up under an empty
// ConfigDir would be read relative to the working directory, where a cloned
// repo could ship one.
func ForReport(o Options) *Context {
	stdin, stdout, stderr := o.streams()
	now := o.Now
	if now == nil {
		now = func() time.Time { return time.Time{} }
	}
	return &Context{
		Home:    o.Home,
		Cwd:     o.Cwd,
		Exe:     o.Exe,
		Env:     o.Env,
		Now:     now,
		Stdin:   stdin,
		Stdout:  stdout,
		Stderr:  stderr,
		Debug:   newDebugLogger(o.Env, stderr),
		Plain:   true,
		memo:    &memo{},
		process: &memo{},
	}
}

// xdgDir returns the XDG base directory in name, or home/fallback when it is
// unset, empty or relative, as the XDG spec says.
func xdgDir(env Env, name, home, fallback string) string {
	if dir := env.Get(name); filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(home, fallback)
}
