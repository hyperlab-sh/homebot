package cli

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func TestReport(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"one line", fail.User("No homebot.yml here."), "homebot: No homebot.yml here.\n"},
		{"detail keeps its layout", fail.User(fail.Failure("Push failed.", "fatal: no remote\n", "git")), "homebot: Push failed.\n  git: no remote\n"},
		{"plain error", errors.New("unknown flag: --nope"), "homebot: unknown flag: --nope\n"},
		// src/cli.ts prints every line after the first, an empty last one too.
		{"trailing newline", errors.New("Push failed.\n"), "homebot: Push failed.\n\n"},
		{"said", fail.Say(msg.ConfigErrorProfiles("homebot.yml:2:1")), "homebot: homebot.yml:2:1: profiles must be a mapping\n"},
		{"said, lines after the first keep their layout", fail.Say(msg.ActionLinkErrorLayout(2, "a\n  b")), "homebot: link: the repo breaks 2 layout rules:\n  a\n  b\n"},
		// The message is found in the chain and said in the voice; what
		// wrapped it stays around it.
		{"said, then wrapped", fmt.Errorf("loading: %w", fail.Say(msg.ConfigErrorProfiles("homebot.yml:2:1"))), "homebot: loading: homebot.yml:2:1: profiles must be a mapping\n"},
		// A message edited after Say is the message.
		{"said, then edited", edited(fail.Say(msg.ConfigErrorProfiles("homebot.yml:2:1")), "Something else."), "homebot: Something else.\n"},
		// No home repo: the error, then the pointer to setup, wherever the
		// error is in the chain.
		{"no repo", fail.Say(msg.ContextErrorNoConfig()), "homebot: No homebot.yml found. Run homebot inside your home repo, or set HOMEBOT_ROOT.\nNew here? This clones your home repo or starts one: homebot setup\n"},
		{"no repo, wrapped", fmt.Errorf("link: %w", fail.Say(msg.ContextErrorNoConfig())), "homebot: link: No homebot.yml found. Run homebot inside your home repo, or set HOMEBOT_ROOT.\nNew here? This clones your home repo or starts one: homebot setup\n"},
		{"none", nil, ""},
		{"exit", fail.Silently(3), ""},
		{"exit, wrapped", fmt.Errorf("link: %w", fail.Silently(1)), ""},
		// The whole message is the error, as src/cli.ts printed parseArgs'.
		{"an option", &optionError{"Option '--message' argument is ambiguous.\nDid you forget?", "See: homebot save --help"}, "homebot: Option '--message' argument is ambiguous.\nDid you forget?\nSee: homebot save --help\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sb := testutil.Sandbox(t, testutil.Options{})
			Report(sb.Ctx, tc.err)
			if got := sb.Stderr.String(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			if sb.Stdout.Len() != 0 {
				t.Errorf("wrote to stdout: %q", sb.Stdout.String())
			}
		})
	}
}

func edited(e *fail.UserError, message string) *fail.UserError {
	e.Msg = message
	return e
}

func TestOptionErrorsPointAtThePage(t *testing.T) {
	c := testutil.Sandbox(t, testutil.Options{}).Ctx
	for command, want := range map[string]string{
		"save":  "See: homebot save --help",
		"inbox": "See: homebot --help",
		"":      "See: homebot --help",
	} {
		if got := newOptionError(c, command, "x").hint; got != want {
			t.Errorf("newOptionError(%q).hint = %q, want %q", command, got, want)
		}
	}
}

func TestExitCode(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"none":          {nil, 0},
		"an error":      {errors.New("x"), 1},
		"a user error":  {fail.User("x"), 1},
		"exit":          {fail.Silently(3), 3},
		"exit, wrapped": {fmt.Errorf("x: %w", fail.Silently(2)), 2},
		"an option":     {&optionError{message: "x"}, 1},
	} {
		if got := ExitCode(tc.err); got != tc.want {
			t.Errorf("%s: ExitCode = %d, want %d", name, got, tc.want)
		}
	}
}

// layout.test.ts "errors on the command line": src/pack.ts's error, whose
// second line is a command to run.
func TestTheFirstLineIsTheErrorWhatFollowsKeepsItsLayout(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{})
	err := fail.Say(msg.PackErrorNoKeys())
	Report(sb.Ctx, err)
	if want := "homebot: Create keys before packing secrets:\n  homebot keys init\n"; sb.Stderr.String() != want {
		t.Errorf("stderr = %q, want %q", sb.Stderr.String(), want)
	}
	if ExitCode(err) != 1 {
		t.Errorf("ExitCode = %d, want 1", ExitCode(err))
	}
}

func TestReportColoursTheFirstLineOnATerminal(t *testing.T) {
	work := t.TempDir()
	vars := testutil.SandboxEnv(work)
	delete(vars, "NO_COLOR")
	vars["TTY_FORCE"], vars["TERM"], vars["COLORTERM"] = "1", "xterm-256color", "truecolor"
	var stderr bytes.Buffer
	c, err := app.New(app.Options{Env: app.NewEnv(vars), Cwd: work, OS: app.Linux, Stdout: &bytes.Buffer{}, Stderr: &stderr})
	if err != nil {
		t.Fatal(err)
	}
	Report(c, errors.New("Push failed.\n  git: no remote"))
	first, rest, _ := strings.Cut(stderr.String(), "\n")
	const red = "38;2;255;107;107"
	if !strings.Contains(first, red) || !strings.Contains(first, "homebot: Push failed.") {
		t.Errorf("first line %q, want it red", first)
	}
	if rest != "  git: no remote\n" {
		t.Errorf("the lines after it %q, want them as they are", rest)
	}
}

// Every command that needs the repo, outside one, points at setup.
func TestNoRepoErrorPointsAtSetup(t *testing.T) {
	c, stdout, stderr := noRepo(t)
	if code := execute(c, "link"); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	want := "homebot: No homebot.yml found. Run homebot inside your home repo, or set HOMEBOT_ROOT.\nNew here? This clones your home repo or starts one: homebot setup\n"
	if stderr.String() != want {
		t.Errorf("stderr:\n%s\nwant:\n%s", stderr, want)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout: %q", stdout)
	}
	// Other errors get no pointer.
	sb := testutil.Sandbox(t, testutil.Options{})
	Report(sb.Ctx, fail.Say(msg.ContextErrorNoHome()))
	if strings.Contains(sb.Stderr.String(), "homebot setup") {
		t.Errorf("stderr: %q", sb.Stderr)
	}
}
