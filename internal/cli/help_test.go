package cli

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// tsPages is COMMAND_HELP's keys in src/help.ts: every command the
// TypeScript had.
var tsPages = []string{
	"inbox", "next", "unpack", "pack", "link", "unlink", "cleanup", "keys", "unlock", "lock",
	"triage", "inventory", "remote", "save", "export", "tidy", "setup", "actions", "action", "shell",
	"refresh", "hook",
}

// help runs args and returns the exit code, stdout and stderr.
func help(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	sb := testutil.Sandbox(t, testutil.Options{})
	code := execute(sb.Ctx, args...)
	return code, sb.Stdout.String(), sb.Stderr.String()
}

func usageOf(t *testing.T, name string) string {
	t.Helper()
	p, ok := pageOf(name)
	if !ok {
		t.Fatalf("no page for %q", name)
	}
	return voice.Plain(testutil.Sandbox(t, testutil.Options{}).Ctx, p.usage)
}

// help-remote.test.ts "--help": "every command has a page, and every page is
// a command". The Go port adds the version command's page.
func TestEveryTypeScriptCommandHasAPage(t *testing.T) {
	var names []string
	for _, p := range pages {
		names = append(names, p.name)
	}
	want := slices.Insert(slices.Clone(tsPages), len(tsPages)-2, "name", "persona")
	if want = append(want, "completion", "version"); !slices.Equal(names, want) {
		t.Errorf("pages %q, want %q", names, want)
	}
}

// A help page without a command in the root could not be run: dispatch would
// call its name an unknown command. The root is the real one, with every
// registered command, so a page added before its command fails here.
func TestEveryHelpPageHasACommand(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{})
	root := NewRoot(sb.Ctx, Build{})
	for _, p := range pages {
		if child(root, p.name) == nil {
			t.Errorf("the page of %q has no command in the root", p.name)
		}
	}
	for _, cmd := range root.Commands() {
		// help is the command that shows the pages and has none itself.
		if _, ok := pageOf(cmd.Name()); !ok && cmd.Name() != "help" {
			t.Errorf("the command %q has no page", cmd.Name())
		}
	}
}

// "each command answers --help and -h with its own page, and runs nothing".
func TestEachCommandAnswersHelpWithItsPage(t *testing.T) {
	ran := tsCommands(t)
	for _, p := range pages {
		for _, flag := range []string{"--help", "-h"} {
			code, out, errOut := help(t, p.name, flag)
			if code != 0 || errOut != "" {
				t.Errorf("%s %s: exit %d, stderr %q", p.name, flag, code, errOut)
			}
			if want := "Usage: " + usageOf(t, p.name); !strings.HasPrefix(out, want) {
				t.Errorf("%s %s: %q does not start with %q", p.name, flag, out, want)
			}
			if !strings.Contains(out, "-h, --help") {
				t.Errorf("%s %s: no -h, --help row", p.name, flag)
			}
		}
	}
	if len(*ran) > 0 {
		t.Errorf("ran %q", *ran)
	}
}

// "--help wins over the other options and never acts".
func TestHelpWinsOverTheOtherOptions(t *testing.T) {
	ran := tsCommands(t)
	if _, out, _ := help(t, "link", "--backup", "--help"); !strings.HasPrefix(out, "Usage: homebot link") {
		t.Errorf("link --backup --help = %q", out)
	}
	if _, out, _ := help(t, "save", "-m", "msg", "-h"); !strings.HasPrefix(out, "Usage: homebot save") {
		t.Errorf("save -m msg -h = %q", out)
	}
	if len(*ran) > 0 {
		t.Errorf("ran %q", *ran)
	}
}

// "unpack's page says it runs without asking and how to preview".
func TestUnpacksPageSaysItRunsWithoutAsking(t *testing.T) {
	_, out, _ := help(t, "unpack", "--help")
	for _, want := range []string{"without asking first", "--dry-run"} {
		if !strings.Contains(out, want) {
			t.Errorf("unpack --help lacks %q", want)
		}
	}
}

// "homebot help <command> is the same page".
func TestHelpCommandIsTheSamePage(t *testing.T) {
	for _, name := range tsPages {
		_, word, _ := help(t, "help", name)
		_, flag, _ := help(t, name, "--help")
		if word != flag {
			t.Errorf("help %s and %s --help differ:\n%s\n%s", name, name, word, flag)
		}
	}
}

// "the overview lists every visible command and points at the pages".
func TestTheOverviewListsEveryVisibleCommand(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}, {"help", ""}} {
		code, out, _ := help(t, args...)
		if code != 0 {
			t.Errorf("%q: exit %d", args, code)
		}
		for _, p := range pages {
			usage := usageOf(t, p.name)
			if listed := strings.Contains(out, "  "+usage+" "); listed != (p.group != Hidden) {
				t.Errorf("%q: %s listed %v, hidden %v", args, p.name, listed, p.group == Hidden)
			}
		}
		if !strings.Contains(out, "homebot <command> --help") {
			t.Errorf("%q: no pointer at the pages", args)
		}
		columns := map[int]bool{}
		for line := range strings.SplitSeq(out, "\n") {
			if strings.HasPrefix(line, "  homebot") {
				columns[strings.LastIndex(line, "   ")+3] = true
			}
		}
		if len(columns) != 1 {
			t.Errorf("%q: summaries start in columns %v", args, slices.Collect(maps.Keys(columns)))
		}
	}
}

// "the inbox's page answers homebot --all --help".
func TestTheInboxsPageAnswersAllHelp(t *testing.T) {
	if _, out, _ := help(t, "--all", "--help"); !strings.HasPrefix(out, "Usage: homebot [--all]") {
		t.Errorf("--all --help = %q", out)
	}
}

// "help works without a repo".
func TestHelpWorksWithoutARepo(t *testing.T) {
	tsCommands(t)
	for _, args := range [][]string{{"save", "--help"}, {"help", "save"}, {"--help"}, {"--version"}} {
		c, stdout, stderr := noRepo(t)
		if code := execute(c, args...); code != 0 || stderr.Len() > 0 {
			t.Errorf("%q: exit %d, stderr %q", args, code, stderr)
		}
		if args[0] == "save" && !strings.HasPrefix(stdout.String(), "Usage: homebot save") {
			t.Errorf("%q = %q", args, stdout)
		}
	}
}

// "after `action <name>`, --help belongs to the action" is TestWantsHelp and
// TestExecuteRunsTheCommand; that "action nope --help" says there is no such
// action is the action command's, in action-help-quirk.txtar.

// "an unknown command or option points at the right help".
func TestAnUnknownCommandOrOptionPointsAtTheRightHelp(t *testing.T) {
	tsCommands(t)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"help", "nope"}, `Unknown command "nope". See: homebot --help`},
		{[]string{"link", "--nope"}, "See: homebot link --help"},
		{[]string{"nope"}, "See: homebot --help"},
	} {
		code, _, errOut := help(t, tc.args...)
		if code != 1 || !strings.Contains(errOut, tc.want) {
			t.Errorf("%q: exit %d, stderr %q; want 1 and %q", tc.args, code, errOut, tc.want)
		}
	}
}

// "pages are plain text when not on a terminal", and styled when on one.
func TestPagesAreStyledOnlyOnATerminal(t *testing.T) {
	for _, args := range [][]string{{"link", "--help"}, {"--help"}} {
		if _, out, _ := help(t, args...); strings.Contains(out, "\x1b[") {
			t.Errorf("%q: escape sequences without a terminal: %q", args, out)
		}
	}
	if _, ok := pageOf("nope"); ok {
		t.Error("a page for nope")
	}

	c, stdout := terminal(t)
	if code := execute(c, "link", "--help"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	first, _, _ := strings.Cut(stdout.String(), "\n")
	if !strings.Contains(first, "\x1b[") || !strings.Contains(first, "Usage:") {
		t.Errorf("the page's first line %q is not styled", first)
	}
	stdout.Reset()
	execute(c, "--help")
	if title, _, _ := strings.Cut(stdout.String(), "\n"); !strings.Contains(title, "\x1b[1mhomebot\x1b[") {
		t.Errorf("the overview's title %q does not start with a bold homebot", title)
	}
}

// terminal is a Context whose stdout is a colour terminal, for the styles.
func terminal(t *testing.T) (*app.Context, *bytes.Buffer) {
	t.Helper()
	work := t.TempDir()
	vars := testutil.SandboxEnv(work)
	delete(vars, "NO_COLOR")
	vars["TTY_FORCE"], vars["TERM"], vars["COLORTERM"] = "1", "xterm-256color", "truecolor"
	var stdout bytes.Buffer
	c, err := app.New(app.Options{Env: app.NewEnv(vars), Cwd: work, OS: app.Linux, Stdout: &stdout, Stderr: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	return c, &stdout
}

// The help parity goldens, read here so that a change to a page fails this
// package's tests too, not only the parity scripts.
func TestTheOverviewIsTheRecordedOne(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "parity", "help-overview.txtar"))
	if err != nil {
		t.Fatal(err)
	}
	_, golden, ok := strings.Cut(string(data), "-- help-overview.stdout --\n")
	golden, _, _ = strings.Cut(golden, "-- help-overview.stderr --\n")
	if !ok {
		t.Fatal("no help-overview.stdout in help-overview.txtar")
	}
	if _, out, _ := help(t, "--help"); out != golden {
		t.Errorf("the overview differs from the recording:\n%s\nwant\n%s", out, golden)
	}
}
