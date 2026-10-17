package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// execute runs args through Execute with c and returns the exit code.
func execute(c *app.Context, args ...string) int {
	return Execute(context.Background(), c, Build{Version: "test"}, args)
}

func TestExecute(t *testing.T) {
	const unknown = "Unknown option '%s'. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"%s\""
	cases := []struct {
		name           string
		args           []string
		code           int
		stdout, stderr string
	}{
		{"--version", []string{"--version"}, 0, "test\n", ""},
		{"-v", []string{"-v"}, 0, "test\n", ""},
		{"version", []string{"version"}, 0, "test\n", ""},
		{"the overview", []string{"--help"}, 0, "homebot: Attention-Deficit Home Discipline\n", ""},
		{"a page", []string{"link", "--help"}, 0, "Usage: homebot link\n", ""},
		{"a command", []string{"link", "--dry-run", "x"}, 0, "", ""},
		{"bare homebot", nil, 0, "", ""},
		// unknown-flag.txtar and flag-x-runs-inbox.txtar.
		{"an unknown option", []string{"link", "--nope"}, 1, "", "homebot: " + strings.ReplaceAll(unknown, "%s", "--nope") + "\nSee: homebot link --help\n"},
		{"an unknown option of bare homebot", []string{"--nope"}, 1, "", "homebot: " + strings.ReplaceAll(unknown, "%s", "--nope") + "\nSee: homebot --help\n"},
		{"-x", []string{"-x"}, 1, "", "homebot: " + strings.ReplaceAll(unknown, "%s", "-x") + "\nSee: homebot --help\n"},
		{"a missing value", []string{"save", "--message"}, 1, "", "homebot: Option '-m, --message <value>' argument missing\nSee: homebot save --help\n"},
		// help-unknown-command.txtar and unknown-command.txtar.
		{"help with an unknown command", []string{"help", "frobnicate"}, 1, "", "homebot: Unknown command \"frobnicate\". See: homebot --help\n"},
		{"an unknown command", []string{"frobnicate"}, 1, "", "homebot: Unknown command \"frobnicate\". See: homebot --help\n"},
		{"version takes no arguments", []string{"version", "extra", "more"}, 1, "", "homebot: version takes no arguments, not \"extra\". See: homebot version --help\n"},
		{"version takes no options", []string{"version", "--x"}, 1, "", "homebot: " + strings.ReplaceAll(unknown, "%s", "--x") + "\nSee: homebot version --help\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tsCommands(t)
			sb := testutil.Sandbox(t, testutil.Options{})
			code := execute(sb.Ctx, tc.args...)
			stdout, stderr := sb.Stdout.String(), sb.Stderr.String()
			if code != tc.code {
				t.Errorf("exit code %d, want %d", code, tc.code)
			}
			if !strings.HasPrefix(stdout, tc.stdout) || tc.stdout == "" && stdout != "" {
				t.Errorf("stdout = %q, want it to start with %q", stdout, tc.stdout)
			}
			if stderr != tc.stderr {
				t.Errorf("stderr = %q\n          want %q", stderr, tc.stderr)
			}
		})
	}
}

func TestCompletionArgumentErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"missing", []string{"completion"}, "homebot: Choose a completion shell: bash, zsh or fish. See: homebot completion --help\n"},
		{"unknown", []string{"completion", "powershell"}, "homebot: Unknown completion shell powershell. Choose bash, zsh or fish. See: homebot completion --help\n"},
		{"extra", []string{"completion", "zsh", "extra"}, "homebot: Completion takes one shell, not extra. See: homebot completion --help\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sb := testutil.Sandbox(t, testutil.Options{})
			if code := execute(sb.Ctx, tc.args...); code != 1 || sb.Stderr.String() != tc.want || sb.Stdout.Len() != 0 {
				t.Errorf("exit %d, stdout %q, stderr %q; want %q", code, sb.Stdout.String(), sb.Stderr.String(), tc.want)
			}
		})
	}
}

func TestCompletionCommandPrintsEachShellScriptOutsideARepo(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			c, stdout, stderr := noRepo(t)
			if code := execute(c, "completion", shell); code != 0 {
				t.Fatalf("exit %d, stderr %q", code, stderr.String())
			}
			if !strings.Contains(stdout.String(), "__complete") || stderr.Len() != 0 {
				t.Errorf("script missing Cobra callback or wrote stderr: %q", stderr.String())
			}
		})
	}
}

func TestExecuteRunsTheCommand(t *testing.T) {
	ran := tsCommands(t)
	for _, args := range [][]string{nil, {"--all"}, {"link", "--dry-run", "--", "--help"}, {"--plain", "save", "-m", "x"}, {"export", "--x"}, {"action", "greet", "-h"}} {
		sb := testutil.Sandbox(t, testutil.Options{})
		if code := execute(sb.Ctx, args...); code != 0 {
			t.Errorf("%q: exit code %d; stderr %q", args, code, sb.Stderr.String())
		}
	}
	want := []string{"inbox", "inbox", "link --help", "save", "export --x", "action greet -h"}
	if strings.Join(*ran, "|") != strings.Join(want, "|") {
		t.Errorf("ran %q, want %q", *ran, want)
	}
}

// --plain is an option of help too, before or after the command's name:
// "help --plain link" prints what "--plain help link" prints.
func TestHelpTakesPlain(t *testing.T) {
	tsCommands(t)
	for _, tc := range []struct{ args, same, starts string }{
		{"help --plain", "--plain help", "homebot: "},
		{"help --plain link", "--plain help link", "Usage: homebot link\n"},
		{"help link --plain", "--plain help link", "Usage: homebot link\n"},
		{"--help --plain", "--plain --help", "homebot: "},
	} {
		want := testutil.Sandbox(t, testutil.Options{})
		execute(want.Ctx, strings.Fields(tc.same)...)
		sb := testutil.Sandbox(t, testutil.Options{})
		code := execute(sb.Ctx, strings.Fields(tc.args)...)
		if code != 0 || sb.Stderr.Len() > 0 || !sb.Ctx.Plain {
			t.Errorf("%s: exit %d, Plain %v, stderr %q", tc.args, code, sb.Ctx.Plain, sb.Stderr.String())
		}
		if got := sb.Stdout.String(); got != want.Stdout.String() || !strings.HasPrefix(got, tc.starts) {
			t.Errorf("%s printed %q, want what %s prints, %q", tc.args, got, tc.same, want.Stdout.String())
		}
	}
}

// A command that has said what went wrong ends with its code, and nothing
// more is printed.
func TestACommandsExitCodeIsHomebots(t *testing.T) {
	withRegistry(t, func() {
		register(MovingIn, "link", func(*app.Context) *cobra.Command {
			return &cobra.Command{Use: "link", RunE: func(*cobra.Command, []string) error {
				return fmt.Errorf("link: %w", fail.Silently(3))
			}}
		})
		sb := testutil.Sandbox(t, testutil.Options{})
		if code := execute(sb.Ctx, "link"); code != 3 || sb.Stderr.Len() != 0 || sb.Stdout.Len() != 0 {
			t.Errorf("exit code %d, stdout %q, stderr %q; want 3 and nothing printed", code, sb.Stdout.String(), sb.Stderr.String())
		}
	})
}

// coloured is a Context with no home repo and buffers for streams, in a
// colour terminal's environment with env applied ("" unsets a variable).
func coloured(t *testing.T, env map[string]string) (*app.Context, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	work := t.TempDir()
	vars := testutil.SandboxEnv(work)
	delete(vars, "NO_COLOR")
	vars["TERM"], vars["COLORTERM"] = "xterm-256color", "truecolor"
	for k, v := range env {
		if v == "" {
			delete(vars, k)
		} else {
			vars[k] = v
		}
	}
	var stdout, stderr bytes.Buffer
	c, err := app.New(app.Options{Env: app.NewEnv(vars), Cwd: work, OS: app.Linux, Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		t.Fatal(err)
	}
	return c, &stdout, &stderr
}

// The error line follows the Context's colour settings, not the process's:
// CLICOLOR_FORCE colours it on a pipe, and NO_COLOR wins over that.
func TestErrorsFollowTheContextsColours(t *testing.T) {
	const red = "\x1b[38;2;255;107;107m"
	for _, tc := range []struct {
		name string
		env  map[string]string
		red  bool
	}{
		{"a pipe", nil, false},
		{"forced", map[string]string{"CLICOLOR_FORCE": "1"}, true},
		{"forced, NO_COLOR", map[string]string{"CLICOLOR_FORCE": "1", "NO_COLOR": "1"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tsCommands(t)
			// One mistake the dispatcher finds, one cobra's command returns.
			for _, args := range [][]string{{"pack"}, {"version", "extra"}} {
				c, _, stderr := coloured(t, tc.env)
				if code := execute(c, args...); code != 1 {
					t.Errorf("%q: exit code %d, want 1", args, code)
				}
				got := stderr.String()
				if strings.HasPrefix(got, red+"homebot: ") != tc.red {
					t.Errorf("%q: stderr %q, want it red: %v", args, got, tc.red)
				}
				if !tc.red && strings.Contains(got, "\x1b[38") {
					t.Errorf("%q: stderr %q, want it plain", args, got)
				}
			}
		})
	}
}

// fang asked the process's terminal for its background on every error.
// Report asks only when it prints in colour to a terminal and stdin and
// stdout are terminals too, so the error path asks nothing on pipes, with
// stderr alone on the terminal (there is no stdout terminal to ask through),
// or with stderr redirected from one (ui.ErrStyles). No test before these
// asks the terminal, so the process has not yet had its one query.
func TestAnErrorAsksTheTerminalNothing(t *testing.T) {
	queries := []string{"\x1b]11;", "\x1b[c"}
	failing := [][]string{{"pack"}, {"frobnicate"}, {"version", "extra"}, {"link", "--nope"}, {"save", "-m"}}
	t.Run("colour forced on pipes", func(t *testing.T) {
		tsCommands(t)
		for _, args := range failing {
			c, stdout, stderr := coloured(t, map[string]string{"CLICOLOR_FORCE": "1", "TTY_FORCE": "1"})
			execute(c, args...)
			for _, q := range queries {
				if strings.Contains(stdout.String()+stderr.String(), q) {
					t.Errorf("%q: the output holds the query %q: stdout %q, stderr %q", args, q, stdout.String(), stderr.String())
				}
			}
		}
	})
	t.Run("errors on a terminal", func(t *testing.T) {
		tsCommands(t)
		for _, args := range failing {
			master, tty := testutil.OpenPTY(t)
			work := t.TempDir()
			vars := testutil.SandboxEnv(work)
			delete(vars, "NO_COLOR")
			vars["TERM"], vars["COLORTERM"] = "xterm-256color", "truecolor"
			c, err := app.New(app.Options{Env: app.NewEnv(vars), Cwd: work, OS: app.Linux, Stdin: tty, Stdout: &bytes.Buffer{}, Stderr: tty})
			if err != nil {
				t.Fatal(err)
			}
			execute(c, args...)
			if _, err := tty.WriteString("end\n"); err != nil {
				t.Fatal(err)
			}
			got := readPTY(t, master, "end")
			if !strings.Contains(got, "\x1b[38;2;255;107;107mhomebot: ") {
				t.Errorf("%q: the terminal got %q, want a red error", args, got)
			}
			for _, q := range queries {
				if strings.Contains(got, q) {
					t.Errorf("%q: the terminal was asked %q: %q", args, q, got)
				}
			}
		}
	})
	t.Run("stderr redirected from a terminal", func(t *testing.T) {
		tsCommands(t)
		for _, args := range failing {
			master, tty := testutil.OpenPTY(t)
			work := t.TempDir()
			vars := testutil.SandboxEnv(work)
			delete(vars, "NO_COLOR")
			vars["TERM"], vars["COLORTERM"] = "xterm-256color", "truecolor"
			var stderr bytes.Buffer
			c, err := app.New(app.Options{Env: app.NewEnv(vars), Cwd: work, OS: app.Linux, Stdin: tty, Stdout: tty, Stderr: &stderr})
			if err != nil {
				t.Fatal(err)
			}
			execute(c, args...)
			if !strings.HasPrefix(stderr.String(), "homebot: ") {
				t.Errorf("%q: stderr %q, want the error, plain", args, stderr.String())
			}
			if _, err := tty.WriteString("end\n"); err != nil {
				t.Fatal(err)
			}
			got := readPTY(t, master, "end")
			for _, q := range queries {
				if strings.Contains(got, q) {
					t.Errorf("%q: the terminal was asked %q: %q", args, q, got)
				}
			}
		}
	})
}

// A run that ends with nothing to say, a *fail.Exit included, leaves the
// terminal alone even when every stream is the terminal: only printing
// styled text asks it for its background.
func TestASilentEndAsksTheTerminalNothing(t *testing.T) {
	withRegistry(t, func() {
		register(MovingIn, "link", func(*app.Context) *cobra.Command {
			return &cobra.Command{Use: "link", RunE: func(*cobra.Command, []string) error { return fail.Silently(3) }}
		})
		register(MovingIn, "unlink", leaf("unlink"))
		for _, args := range [][]string{{"link"}, {"unlink"}} {
			master, tty := testutil.OpenPTY(t)
			work := t.TempDir()
			vars := testutil.SandboxEnv(work)
			delete(vars, "NO_COLOR")
			vars["TERM"], vars["COLORTERM"] = "xterm-256color", "truecolor"
			c, err := app.New(app.Options{Env: app.NewEnv(vars), Cwd: work, OS: app.Linux, Stdin: tty, Stdout: tty, Stderr: tty})
			if err != nil {
				t.Fatal(err)
			}
			execute(c, args...)
			if _, err := tty.WriteString("end\n"); err != nil {
				t.Fatal(err)
			}
			// readPTY returns as soon as "end" has arrived. The line discipline
			// writes "end" and the "\r\n" it makes of the test's own "\n" to the
			// pty separately, so a read can return between them: the line
			// ending is not part of what this test asks.
			if got := strings.TrimSuffix(readPTY(t, master, "end"), "\r\n"); got != "end" {
				t.Errorf("%q: the terminal got %q, want nothing before end", args, got)
			}
		}
	})
}

// Ctrl-C stops the program a command runs, and the command then fails;
// the run still ends as the TypeScript's did, with the signal's code and no
// message. A cancel without a *fail.Exit cause is an ordinary failure.
func TestAStoppedRunEndsSilentlyWithItsCode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cause  error
		code   int
		stderr string
	}{
		{"SIGINT", fail.Silently(130), 130, ""},
		{"SIGTERM", fail.Silently(143), 143, ""},
		{"no signal", nil, 1, "homebot: sleep: exit 1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withRegistry(t, func() {
				started := make(chan struct{})
				register(MovingIn, "link", func(c *app.Context) *cobra.Command {
					return &cobra.Command{Use: "link", RunE: func(*cobra.Command, []string) error {
						close(started)
						if r := proc.Run(c, []string{"sleep", "5"}, proc.Options{}); r.Code != 0 {
							return fmt.Errorf("sleep: exit %d", r.Code)
						}
						return nil
					}}
				})
				sb := testutil.Sandbox(t, testutil.Options{})
				ctx, cancel := context.WithCancelCause(context.Background())
				go func() {
					<-started
					time.Sleep(50 * time.Millisecond)
					cancel(tc.cause)
				}()
				begin := time.Now()
				code := Execute(ctx, sb.Ctx, Build{Version: "test"}, []string{"link"})
				if took := time.Since(begin); took > 3*time.Second {
					t.Errorf("the run took %v; the child was not stopped", took)
				}
				if code != tc.code || sb.Stderr.String() != tc.stderr {
					t.Errorf("exit %d, stderr %q; want %d, %q", code, sb.Stderr.String(), tc.code, tc.stderr)
				}
			})
		})
	}
}

// readPTY reads what was written to the terminal master is the other side
// of, until it holds want.
func readPTY(t *testing.T, master *os.File, want string) string {
	t.Helper()
	if err := master.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var got []byte
	buf := make([]byte, 256)
	for !bytes.Contains(got, []byte(want)) {
		n, err := master.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil {
			t.Fatalf("the terminal got %q, then: %v", got, err)
		}
	}
	return string(got)
}
