package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// realTree is the command tree with every registered command.
func realTree(t *testing.T) *cobra.Command {
	t.Helper()
	return NewRoot(testutil.Sandbox(t, testutil.Options{}).Ctx, Build{Version: "test"})
}

// below calls fn for every command under root, at any depth, with the page
// of the top-level command it is under.
func below(root *cobra.Command, fn func(cmd *cobra.Command, top page, paged bool)) {
	var walk func(*cobra.Command, page, bool)
	walk = func(cmd *cobra.Command, top page, paged bool) {
		fn(cmd, top, paged)
		for _, sub := range cmd.Commands() {
			walk(sub, top, paged)
		}
	}
	for _, cmd := range root.Commands() {
		if cmd.Name() == "help" {
			continue
		}
		p, ok := pageOf(cmd.Name())
		walk(cmd, p, ok)
	}
}

// flagsOf is cmd's flags but help, as "-m, --message" or "--all".
func flagsOf(cmd *cobra.Command) []string {
	var flags []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "help" || f.Hidden {
			return
		}
		if f.Shorthand != "" {
			flags = append(flags, "-"+f.Shorthand+", --"+f.Name)
		} else {
			flags = append(flags, "--"+f.Name)
		}
	})
	slices.Sort(flags)
	return flags
}

// pageFlags includes flags owned by subcommands: one help page documents the
// whole top-level command, including persona new and persona preview options.
func pageFlags(cmd *cobra.Command) []string {
	seen := map[string]bool{}
	var visit func(*cobra.Command)
	visit = func(c *cobra.Command) {
		for _, flag := range flagsOf(c) {
			seen[flag] = true
		}
		for _, sub := range c.Commands() {
			visit(sub)
		}
	}
	visit(cmd)
	flags := make([]string, 0, len(seen))
	for flag := range seen {
		flags = append(flags, flag)
	}
	slices.Sort(flags)
	return flags
}

// optionFlags is the page's options as flagsOf names flags: the left
// column without the value's name, "-m, --message" for "-m, --message TEXT".
func optionFlags(p page) []string {
	var flags []string
	for _, o := range p.options {
		var names []string
		for _, f := range strings.Fields(o.flag) {
			if strings.HasPrefix(f, "-") {
				names = append(names, strings.TrimSuffix(f, ","))
			}
		}
		flags = append(flags, strings.Join(names, ", "))
	}
	slices.Sort(flags)
	return flags
}

// Every top-level command but help has a page, listed under the group it
// registered with, whose options are its flags, letters included; a
// verbatim command has none. cobra's fields say what the page says.
func TestEveryCommandHasItsPage(t *testing.T) {
	root := realTree(t)
	for _, cmd := range root.Commands() {
		name := cmd.Name()
		if name == "help" {
			continue
		}
		p, ok := pageOf(name)
		if !ok {
			t.Errorf("%s has no page in help_pages.go", name)
			continue
		}
		if g, registered := cmd.Annotations[groupAnnotation]; registered && Group(g) != p.group {
			t.Errorf("%s registered in group %s, its page is in %s", name, g, p.group)
		}
		if flags, options := pageFlags(cmd), optionFlags(p); !slices.Equal(flags, options) {
			t.Errorf("%s has flags %q, its page options %q", name, flags, options)
		}
		if cmd.Short == "" || cmd.Long == "" {
			t.Errorf("%s: Short %q, Long %q; want the page's summary and about", name, cmd.Short, cmd.Long)
		}
		if (cmd.Example != "") != (len(p.examples) > 0) {
			t.Errorf("%s: Example %q, and its page has %d examples", name, cmd.Example, len(p.examples))
		}
	}
}

// Under a verbatim command nothing parses flags, at any depth, and every
// flag anywhere says what it does.
func TestEveryCommandBelowFollowsItsPage(t *testing.T) {
	below(realTree(t), func(cmd *cobra.Command, top page, paged bool) {
		if paged && top.verbatim && !cmd.DisableFlagParsing {
			t.Errorf("%s is under the verbatim %s but parses flags", cmd.CommandPath(), top.name)
		}
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			if f.Name != "help" && !f.Hidden && f.Usage == "" {
				t.Errorf("%s --%s has no usage", cmd.CommandPath(), f.Name)
			}
		})
	})
}

// Every command answers help with its page, through the dispatcher and
// through cobra's own help; a command below a top-level one answers with
// the top-level command's page, as the TypeScript did, unless it has a page
// of its own (a subpage).
func TestEveryCommandAnswersHelp(t *testing.T) {
	below(realTree(t), func(cmd *cobra.Command, top page, paged bool) {
		if !paged {
			t.Errorf("%s is under a command without a page", cmd.CommandPath())
			return
		}
		path := strings.Fields(pathBelowRoot(cmd))
		sb := testutil.Sandbox(t, testutil.Options{})
		root := NewRoot(sb.Ctx, Build{Version: "test"})
		usage := usageOf(t, top.name)
		if len(path) > 1 {
			if sp, ok := subpageOf(path[0], path[1]); ok {
				usage = voice.Plain(sb.Ctx, sp.usage)
			}
		}
		want := "Usage: " + usage + "\n"
		if code := execute(sb.Ctx, append(path, "--help")...); code != 0 || !strings.HasPrefix(sb.Stdout.String(), want) {
			t.Errorf("%s --help: exit %d, %q", cmd.CommandPath(), code, sb.Stdout.String())
		}
		sb.Stdout.Reset()
		sub, _, err := root.Find(path)
		if err != nil {
			t.Fatal(err)
		}
		sub.HelpFunc()(sub, nil)
		if !strings.HasPrefix(sb.Stdout.String(), want) {
			t.Errorf("cobra's help of %s = %q", cmd.CommandPath(), sb.Stdout.String())
		}
	})
}

// A command's own PersistentPreRun would replace the root's, which hands
// the command the cancellable context (app.Context.Context).
func TestNoCommandReplacesTheRootsPreRun(t *testing.T) {
	root := realTree(t)
	if root.PersistentPreRunE == nil {
		t.Fatal("premise: the root has no PersistentPreRunE")
	}
	below(root, func(cmd *cobra.Command, _ page, _ bool) {
		if cmd.PersistentPreRunE != nil || cmd.PersistentPreRun != nil {
			t.Errorf("%s sets its own PersistentPreRun", cmd.CommandPath())
		}
	})
}

// withSub builds a command name with one subcommand, sub, which takes a
// --x flag and appends its arguments to ran.
func withSub(name, sub string, ran *[]string) func(*app.Context) *cobra.Command {
	return func(*app.Context) *cobra.Command {
		cmd := &cobra.Command{Use: name}
		s := &cobra.Command{Use: sub, RunE: func(_ *cobra.Command, args []string) error {
			*ran = append(*ran, strings.Join(append([]string{sub}, args...), " "))
			return nil
		}}
		s.Flags().Bool("x", false, "x")
		cmd.AddCommand(s)
		return cmd
	}
}

// src/cli.ts handed "keys init" the words after it unparsed: a verbatim
// command's subcommands parse no flags either.
func TestVerbatimReachesSubcommands(t *testing.T) {
	ran := &[]string{}
	withRegistry(t, func() {
		register(Secrets, "keys", withSub("keys", "init", ran))
		sb := testutil.Sandbox(t, testutil.Options{})
		if code := execute(sb.Ctx, "keys", "init", "--x", "--nope"); code != 0 {
			t.Errorf("keys init --x --nope: exit %d, stderr %q", code, sb.Stderr.String())
		}
	})
	if want := []string{"init --x --nope"}; !slices.Equal(*ran, want) {
		t.Errorf("ran %q, want %q", *ran, want)
	}
}

// A flag mistake cobra finds points at the page of the command it was
// given to, named by its whole path.
func TestAFlagMistakePointsAtItsCommand(t *testing.T) {
	ran := &[]string{}
	withRegistry(t, func() {
		register(KeepingOrder, "remote", withSub("remote", "add", ran))
		sb := testutil.Sandbox(t, testutil.Options{})
		root := NewRoot(sb.Ctx, Build{Version: "test"})
		root.SetArgs([]string{"remote", "add", "--x=maybe"})
		err := root.Execute()
		Report(sb.Ctx, err)
		if want := "See: homebot remote add --help\n"; !strings.HasSuffix(sb.Stderr.String(), want) {
			t.Errorf("stderr %q, want it to end with %q", sb.Stderr.String(), want)
		}
	})
}

func TestPathBelowRoot(t *testing.T) {
	root := &cobra.Command{Use: "homebot"}
	keys := &cobra.Command{Use: "keys"}
	initCmd := &cobra.Command{Use: "init [name]"}
	root.AddCommand(keys)
	keys.AddCommand(initCmd)
	for cmd, want := range map[*cobra.Command]string{root: "", keys: "keys", initCmd: "keys init"} {
		if got := pathBelowRoot(cmd); got != want {
			t.Errorf("pathBelowRoot(%s) = %q, want %q", cmd.Name(), got, want)
		}
	}
}
