package cli

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// tsCommands registers stand-ins for some of the TypeScript's commands, with
// the options src/cli.ts gave parseArgs, and returns what they ran.
func tsCommands(t *testing.T) *[]string {
	t.Helper()
	ran := &[]string{}
	saved := registry
	registry = nil
	t.Cleanup(func() { registry = saved })
	register(Attention, "inbox", fake("inbox", func(f *pflag.FlagSet) { f.Bool("all", false, "") }, ran))
	register(MovingIn, "link", fake("link", func(f *pflag.FlagSet) {
		f.Bool("dry-run", false, "")
		f.Bool("backup", false, "")
	}, ran))
	register(KeepingOrder, "save", fake("save", func(f *pflag.FlagSet) {
		f.StringP("message", "m", "", "")
		f.Bool("no-push", false, "")
	}, ran))
	register(KeepingOrder, "remote", fake("remote", nil, ran))
	register(KeepingOrder, "export", fake("export", nil, ran))
	register(KeepingOrder, "action", fake("action", nil, ran))
	register(KeepingOrder, "shell", fake("shell", nil, ran))
	return ran
}

// noRepo is a Context with no home repo anywhere it looks.
func noRepo(t *testing.T) (*app.Context, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	work := t.TempDir()
	var stdout, stderr bytes.Buffer
	c, err := app.New(app.Options{Env: app.NewEnv(testutil.SandboxEnv(work)), Cwd: work, OS: app.Linux, Stdout: &stdout, Stderr: &stderr})
	if err != nil {
		t.Fatal(err)
	}
	return c, &stdout, &stderr
}

func TestDispatch(t *testing.T) {
	tsCommands(t)
	cases := []struct {
		name string
		args []string
		want []string
		// err is the error's message, and hint its pointer, when there is one.
		err, hint string
	}{
		{"bare homebot runs the inbox", nil, []string{"inbox"}, "", ""},
		{"completion empty", []string{"__complete", ""}, []string{"__complete", ""}, "", ""},
		{"completion prefix", []string{"__complete", "sa"}, []string{"__complete", "sa"}, "", ""},
		{"completion keeps plain argument", []string{"__complete", "--plain", "sa"}, []string{"__complete", "--plain", "sa"}, "", ""},
		{"completion without descriptions", []string{"__completeNoDesc", "save", "-"}, []string{"__completeNoDesc", "save", "-"}, "", ""},
		{"an option first runs the inbox", []string{"--all"}, []string{"inbox", "--all"}, "", ""},
		{"a dash alone runs the inbox", []string{"-", "x"}, []string{"inbox", "-", "x"}, "", ""},
		{"-x runs the inbox, which refuses it", []string{"-x"}, nil, "Unknown option '-x'. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"-x\"", "See: homebot --help"},
		{"--help", []string{"--help"}, []string{"help"}, "", ""},
		{"-h", []string{"-h", "link"}, []string{"help"}, "", ""},
		{"help", []string{"help"}, []string{"help"}, "", ""},
		{"help with a command", []string{"help", "link", "--nope"}, []string{"help", "link", "--nope"}, "", ""},
		{"--version", []string{"--version", "x"}, []string{"version"}, "", ""},
		{"-v", []string{"-v"}, []string{"version"}, "", ""},
		{"a command", []string{"link", "--dry-run"}, []string{"link", "--dry-run"}, "", ""},
		{"a page", []string{"link", "--help"}, []string{"help", "link"}, "", ""},
		{"-h after other options", []string{"save", "-m", "msg", "-h"}, []string{"help", "save"}, "", ""},
		{"-h before a mistake", []string{"link", "--nope", "-h"}, []string{"help", "link"}, "", ""},
		{"the inbox's page", []string{"--all", "--help"}, []string{"help", "inbox"}, "", ""},
		{"nothing after -- is help", []string{"link", "--", "--help"}, []string{"link", "--", "--help"}, "", ""},
		{"the page of a verbatim command", []string{"export", "x", "-h"}, []string{"help", "export"}, "", ""},
		{"action's page", []string{"action", "--help"}, []string{"help", "action"}, "", ""},
		{"an action's --help is the action's", []string{"action", "greet", "--help"}, []string{"action", "greet", "--help"}, "", ""},
		{"an unknown option", []string{"link", "--nope"}, nil, "Unknown option '--nope'. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"--nope\"", "See: homebot link --help"},
		{"a missing value", []string{"save", "-m"}, nil, "Option '-m, --message <value>' argument missing", "See: homebot save --help"},
		{"no options at all", []string{"remote", "--x"}, nil, "Unknown option '--x'. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"--x\"", "See: homebot remote --help"},
		{"a verbatim command takes anything", []string{"export", "--nope", "--"}, []string{"export", "--nope", "--"}, "", ""},
		{"options after -- are arguments", []string{"link", "--", "--nope"}, []string{"link", "--", "--nope"}, "", ""},
		{"an unknown command", []string{"frobnicate"}, nil, `Unknown command "frobnicate". See: homebot --help`, ""},
		{"the page of a command the stand-ins lack", []string{"pack", "--help"}, []string{"help", "pack"}, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sb := testutil.Sandbox(t, testutil.Options{})
			got, err := dispatch(sb.Ctx, NewRoot(sb.Ctx, Build{}), tc.args)
			if tc.err == "" {
				if err != nil || !slices.Equal(got, tc.want) {
					t.Errorf("dispatch(%q) = %q, %v; want %q", tc.args, got, err, tc.want)
				}
				return
			}
			if err == nil || err.Error() != tc.err {
				t.Fatalf("dispatch(%q) = %q, %v; want the error %q", tc.args, got, err, tc.err)
			}
			var opt *optionError
			if errors.As(err, &opt) != (tc.hint != "") || tc.hint != "" && opt.hint != tc.hint {
				t.Errorf("hint = %+v, want %q", opt, tc.hint)
			}
		})
	}
}

// src/cli.ts located the home repo before it looked at the command line, so
// outside a repo that is the error, whatever is wrong after it.
func TestTheRepoIsLookedUpBeforeTheCommandLine(t *testing.T) {
	tsCommands(t)
	c, _, _ := noRepo(t)
	noConfig := c.LocateRoot()
	if noConfig == nil {
		t.Fatal("premise: a home repo was found")
	}
	for _, args := range [][]string{{"frobnicate"}, {"link", "--nope"}, {"-x"}} {
		c, _, _ := noRepo(t)
		if _, err := dispatch(c, NewRoot(c, Build{}), args); err == nil || err.Error() != noConfig.Error() {
			t.Errorf("dispatch(%q) = %v, want %v", args, err, noConfig)
		}
	}
	// The commands that run outside a repo report their own.
	for args, want := range map[string]string{
		"version --x": "Unknown option '--x'. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"--x\"",
	} {
		c, _, _ := noRepo(t)
		if _, err := dispatch(c, NewRoot(c, Build{}), strings.Fields(args)); err == nil || err.Error() != want {
			t.Errorf("dispatch(%q) = %v, want %q", args, err, want)
		}
	}
	// Help, the version and verbatim commands do not look at all.
	for _, args := range []string{"help", "--help", "link --help", "--version", "shell --nope", "export"} {
		c, _, _ := noRepo(t)
		if _, err := dispatch(c, NewRoot(c, Build{}), strings.Fields(args)); err != nil {
			t.Errorf("dispatch(%q) = %v, want no error", args, err)
		}
	}
}

// help-remote.test.ts "after `action <name>`, --help belongs to the action"
// and "nothing after -- counts".
func TestWantsHelp(t *testing.T) {
	for _, tc := range []struct {
		name string
		rest []string
		want bool
	}{
		{"action", []string{"--help"}, true},
		{"action", []string{"-h"}, true},
		{"action", []string{"brew", "--help"}, false},
		{"action", nil, false},
		{"pack", []string{"--", "--help"}, false},
		{"pack", []string{"-h", "--", "x"}, true},
		{"pack", []string{"x", "--help"}, true},
		{"pack", []string{"--helpx"}, false},
		{"pack", nil, false},
	} {
		if got := wantsHelp(tc.name, tc.rest); got != tc.want {
			t.Errorf("wantsHelp(%q, %q) = %v, want %v", tc.name, tc.rest, got, tc.want)
		}
	}
}

func TestPlainIsTakenOutWhereAnOptionGoes(t *testing.T) {
	save := []optionSpec{{name: "message", short: "m", value: true}, {name: "no-push"}}
	for _, tc := range []struct {
		args, want string
		plain      bool
	}{
		{"", "", false},
		{"--plain", "", true},
		{"--plain --no-push", "--no-push", true},
		{"x --plain y", "x y", true},
		{"--no-push --plain --plain", "--no-push", true},
		{"-- --plain", "-- --plain", false},
		{"--plain -- --plain", "-- --plain", true},
		// The word after an option that takes a value is its value.
		{"-m --plain", "-m --plain", false},
		{"--message --plain", "--message --plain", false},
		{"-m x --plain", "-m x", true},
		{"--message=x --plain", "--message=x", true},
		{"-mx --plain", "-mx", true},
		{"--no-push -m --plain --plain", "--no-push -m --plain", true},
		{"--plainly --plain=1", "--plainly --plain=1", false},
	} {
		got, plain := takePlain(strings.Fields(tc.args), save)
		if !slices.Equal(got, strings.Fields(tc.want)) || plain != tc.plain {
			t.Errorf("takePlain(%q) = %q, %v; want %q, %v", tc.args, got, plain, tc.want, tc.plain)
		}
	}
}

func TestPlainIsTheGlobalOption(t *testing.T) {
	tsCommands(t)
	for _, tc := range []struct {
		args, want string
		plain      bool
		err        string
	}{
		{"--plain", "inbox", true, ""},
		{"--plain --plain link", "link", true, ""},
		{"--plain --version", "version", true, ""},
		{"link --plain --dry-run", "link --dry-run", true, ""},
		{"--all --plain", "inbox --all", true, ""},
		{"link -- --plain", "link -- --plain", false, ""},
		{"save -m --plain", "", false, "Option '-m' argument is ambiguous."},
		// A verbatim command gets its words as typed.
		{"export --plain", "export --plain", false, ""},
		{"shell --plain zsh", "shell --plain zsh", false, ""},
		{"action --plain greet", "action --plain greet", false, ""},
		{"--plain action greet --plain", "action greet --plain", true, ""},
		// Help reads only a command's name, so --plain goes anywhere
		// before "--".
		{"--help --plain", "help", true, ""},
		{"-h link --plain", "help", true, ""},
		{"help --plain", "help", true, ""},
		{"help --plain link", "help link", true, ""},
		{"help link --plain", "help link", true, ""},
		{"--plain help link", "help link", true, ""},
		{"help -- --plain", "help -- --plain", false, ""},
	} {
		sb := testutil.Sandbox(t, testutil.Options{})
		got, err := dispatch(sb.Ctx, NewRoot(sb.Ctx, Build{}), strings.Fields(tc.args))
		switch {
		case tc.err != "" && (err == nil || !strings.HasPrefix(err.Error(), tc.err)):
			t.Errorf("dispatch(%q) = %q, %v; want the error %q", tc.args, got, err, tc.err)
		case tc.err == "" && (err != nil || !slices.Equal(got, strings.Fields(tc.want))):
			t.Errorf("dispatch(%q) = %q, %v; want %q", tc.args, got, err, tc.want)
		}
		if sb.Ctx.Plain != tc.plain {
			t.Errorf("dispatch(%q): Plain = %v, want %v", tc.args, sb.Ctx.Plain, tc.plain)
		}
	}
}
