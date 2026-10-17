package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// dispatch reads the command line as src/cli.ts's main() did and returns
// the arguments root runs, or the error to print instead:
//
//   - --help and -h first are the overview, --version and -v the version;
//   - nothing, or an option, first runs the inbox: "homebot --all";
//   - a --help or -h after a command's name, before any "--", is its page;
//     after "action", only the word right after it is;
//   - an unknown command, or a mistake among a command's options, is
//     reported after looking for the home repo, as the TypeScript did,
//     unless the command runs without one.
//
// --plain, the Go port's one global option, sets c.Plain. It is taken out
// where an option goes: before the command's name, and among the options of
// a command that parses them. A verbatim command (export, hook, shell, ...)
// gets it as typed, and so does an option that takes a value: in
// "save -m --plain" it is the message. Help takes it anywhere before "--":
// "help --plain link" is "help link" in the neutral voice.
func dispatch(c *app.Context, root *cobra.Command, args []string) ([]string, error) {
	if len(args) > 0 && (args[0] == cobra.ShellCompRequestCmd || args[0] == cobra.ShellCompNoDescRequestCmd) {
		return args, nil
	}
	args = leadingPlain(c, args)
	if len(args) > 0 {
		switch args[0] {
		case "--help", "-h":
			helpWords(c, args[1:])
			return []string{"help"}, nil
		case "help":
			return append([]string{"help"}, helpWords(c, args[1:])...), nil
		case "--version", "-v":
			return []string{"version"}, nil
		}
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		args = append([]string{"inbox"}, args...)
	}
	name, rest := args[0], args[1:]
	if name == "tidy" {
		for _, arg := range rest {
			if arg == "--" {
				break
			}
			if arg == "--quiet" || arg == "--quiet=true" {
				c.ConfigWarningsQuiet = true
			}
		}
	}
	p, paged := pageOf(name)
	cmd := child(root, name)
	// A command group may give its subcommands different options. Check the
	// chosen subcommand's flags while retaining the group's help page.
	optionCmd := cmd
	subWords := rest
	for len(subWords) > 0 && subWords[0] == plainFlag {
		subWords = subWords[1:]
	}
	if cmd != nil && len(subWords) > 0 {
		if sub := child(cmd, subWords[0]); sub != nil {
			optionCmd = sub
		}
	}
	if cmd != nil && paged && !p.verbatim && !cmd.DisableFlagParsing {
		var plain bool
		if rest, plain = takePlain(rest, optionsOf(optionCmd)); plain {
			c.Plain = true
		}
		args = append([]string{name}, rest...)
	}
	if paged && wantsHelp(name, rest) {
		// A subcommand with a page of its own: "persona use --help".
		if len(subWords) > 0 {
			if _, ok := subpageOf(name, subWords[0]); ok {
				return []string{"help", name, subWords[0]}, nil
			}
		}
		return []string{"help", name}, nil
	}
	switch {
	case cmd == nil:
		return nil, repoFirst(c, fail.User(voice.Plain(c, msg.CLIErrorUnknownCommand(name))))
	case paged && !p.verbatim && !cmd.DisableFlagParsing:
		if t := checkOptions(rest, optionsOf(optionCmd)); t.ID() != "" {
			err := newOptionError(c, name, voice.Plain(c, t))
			if p.noRepo {
				return nil, err
			}
			return nil, repoFirst(c, err)
		}
	}
	return args, nil
}

// repoFirst is the error of looking for the home repo, when there is one,
// and err otherwise: the TypeScript found the repo before it looked at the
// command line.
func repoFirst(c *app.Context, err error) error {
	if rootErr := c.LocateRoot(); rootErr != nil {
		return rootErr
	}
	return err
}

// wantsHelp reports whether rest, the words after a command's name, ask for
// its page: a --help or -h before any "--". The words after an action's
// name are the action's, so for "action" only the first word counts.
func wantsHelp(name string, rest []string) bool {
	isHelp := func(a string) bool { return a == "--help" || a == "-h" }
	if name == "action" {
		return len(rest) > 0 && isHelp(rest[0])
	}
	for _, a := range rest {
		if a == "--" {
			return false
		}
		if isHelp(a) {
			return true
		}
	}
	return false
}

// leadingPlain takes the --plain words before the command's name out of
// args, setting c.Plain when there is one.
func leadingPlain(c *app.Context, args []string) []string {
	for len(args) > 0 && args[0] == plainFlag {
		c.Plain = true
		args = args[1:]
	}
	return args
}

// helpWords is the words after "help", or after a first --help, with
// --plain taken out, setting c.Plain when it was there. Help reads only a
// command's name, so --plain is an option wherever it is before "--".
func helpWords(c *app.Context, words []string) []string {
	words, plain := takePlain(words, nil)
	if plain {
		c.Plain = true
	}
	return words
}

// plainFlag asks for the neutral voice (app.Context.Plain).
const plainFlag = "--plain"

// takePlain takes --plain out of args, a command's words after its name, and
// reports whether it was there. It looks only where an option goes, as
// parseArgs read args with specs: it stops at "--", and leaves alone the
// word an option takes as its value.
func takePlain(args []string, specs []optionSpec) ([]string, bool) {
	kept := make([]string, 0, len(args))
	plain := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--":
			return append(kept, args[i:]...), plain
		case plainFlag:
			plain = true
			continue
		}
		kept = append(kept, a)
		if takesNextWord(a, specs) && i+1 < len(args) {
			i++
			kept = append(kept, args[i])
		}
	}
	return kept, plain
}

// takesNextWord reports whether parseArgs gave the word after arg to arg as
// its value, by the rules tokenize follows: "-m", "--message", and a group
// of letters such as "-am" whose first letter that takes a value is its last.
func takesNextWord(arg string, specs []optionSpec) bool {
	letters := []rune(arg)
	short := len(letters) >= 2 && letters[0] == '-' && letters[1] != '-'
	switch {
	case short:
		for i := 1; i < len(letters); i++ {
			if takesValue(specs, longFor(specs, string(letters[i]))) {
				return i == len(letters)-1
			}
		}
		return false
	case len(letters) > 2 && strings.HasPrefix(arg, "--") && !strings.Contains(arg[3:], "="):
		return takesValue(specs, arg[2:])
	}
	return false
}

// child is root's command name, or nil.
func child(root *cobra.Command, name string) *cobra.Command {
	for _, cmd := range root.Commands() {
		if cmd.Name() == name {
			return cmd
		}
	}
	return nil
}
