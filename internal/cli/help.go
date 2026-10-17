package cli

import (
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/selfname"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// helpFlag is the left column of the row every page ends its options with.
const helpFlag = "-h, --help"

// overview is "homebot --help", as help.ts's overview() built it: every
// command that is not hidden, by group, with its usage and summary.
func overview(c *app.Context) string {
	st := ui.Styles(c)
	say := func(t msg.Text) string { return voice.Render(c, t) }
	width := 0
	for _, p := range pages {
		if p.group != Hidden {
			width = max(width, utf8.RuneCountInString(say(p.usage)))
		}
	}
	lines := []string{title(c)}
	for _, g := range []struct {
		group Group
		label msg.Text
	}{
		{Attention, msg.Text{}},
		{MovingIn, msg.HelpGroupMovingIn()},
		{Secrets, msg.HelpGroupSecrets()},
		{KeepingOrder, msg.HelpGroupKeepingOrder()},
	} {
		lines = append(lines, "")
		if g.label.ID() != "" {
			lines = append(lines, st.Accent.Render(say(g.label)))
		}
		for _, p := range pages {
			if p.group == g.group {
				lines = append(lines, say(msg.HelpOverviewRow(pad(say(p.usage), width+3), say(p.summary))))
			}
		}
	}
	lines = append(lines, "", st.Muted.Render(say(msg.HelpOverviewFooter())))
	return strings.Join(lines, "\n")
}

// title is the overview's first line, the program's name in bold.
func title(c *app.Context) string {
	line := voice.Render(c, msg.HelpOverviewTitle())
	if rest, ok := strings.CutPrefix(line, "homebot"); ok {
		return ui.Styles(c).Accent.Render(selfname.Effective(c)) + rest
	}
	return line
}

// pageText is "homebot <command> --help", as help.ts's commandHelp() built
// it: the usage, what the command does, its options and its examples.
func pageText(c *app.Context, p page) string {
	st := ui.Styles(c)
	say := func(t msg.Text) string { return voice.Render(c, t) }
	lines := []string{st.Accent.Render(say(msg.HelpPageUsageLabel())) + " " + say(p.usage), "", say(p.about)}
	table := func(label msg.Text, rows [][2]string) {
		width := 0
		for _, r := range rows {
			width = max(width, utf8.RuneCountInString(r[0]))
		}
		lines = append(lines, "", st.Accent.Render(say(label)))
		for _, r := range rows {
			lines = append(lines, say(msg.HelpPageRow(pad(r[0], width+3), r[1])))
		}
	}
	if subs := subpagesOf(p.name); len(subs) > 0 {
		var commands [][2]string
		for _, sub := range subs {
			commands = append(commands, [2]string{say(sub.usage), say(sub.summary)})
		}
		table(msg.HelpPageCommandsLabel(), commands)
	}
	var options [][2]string
	for _, o := range p.options {
		options = append(options, [2]string{o.flag, say(o.meaning)})
	}
	table(msg.HelpPageOptionsLabel(), append(options, [2]string{helpFlag, say(msg.HelpPageHelpOption())}))
	if len(p.examples) > 0 {
		var examples [][2]string
		for _, e := range p.examples {
			examples = append(examples, [2]string{selfname.Effective(c) + " " + e.command, say(e.meaning)})
		}
		table(msg.HelpPageExamplesLabel(), examples)
	}
	return strings.Join(lines, "\n")
}

// pad fills s with spaces to width characters, as padEnd did.
func pad(s string, width int) string {
	return s + strings.Repeat(" ", max(0, width-utf8.RuneCountInString(s)))
}

// newHelpCmd is "homebot help [command]": the overview, or a command's page.
// The dispatcher sends --help and -h here too. Anything after the command's
// name is ignored, except a subcommand with a page of its own ("help persona
// use"), and "help --help" asks for a command named --help.
func newHelpCmd(c *app.Context) *cobra.Command {
	return &cobra.Command{
		Use:                "help [command]",
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 || args[0] == "" {
				ui.Println(c, overview(c))
				return nil
			}
			p, ok := pageOf(args[0])
			if !ok {
				return fail.User(voice.Plain(c, msg.CLIErrorUnknownCommand(args[0])))
			}
			if len(args) > 1 {
				if sub, ok := subpageOf(args[0], args[1]); ok {
					p = sub
				}
			}
			ui.Println(c, pageText(c, p))
			return nil
		},
	}
}

// describe gives every command under root its page: cobra's Short, Long,
// Example and flag usages say what the page says, and cobra's own help
// prints the page. A verbatim command's flags are not parsed, nor are its
// subcommands'. It reads nothing from c but the voice.
func describe(root *cobra.Command, c *app.Context) {
	root.Short = voice.Plain(c, msg.BrandPackageDescription())
	for _, cmd := range root.Commands() {
		p, ok := pageOf(cmd.Name())
		if !ok {
			continue
		}
		cmd.Short = voice.Plain(c, p.summary)
		cmd.Long = voice.Plain(c, p.about)
		cmd.Hidden = p.group == Hidden
		var examples []string
		for _, e := range p.examples {
			examples = append(examples, "  "+e.command)
		}
		cmd.Example = strings.Join(examples, "\n")
		for _, o := range p.options {
			if f := cmd.Flags().Lookup(o.name()); f != nil {
				f.Usage = voice.Plain(c, o.meaning)
				continue
			}
			for _, sub := range cmd.Commands() {
				if f := sub.Flags().Lookup(o.name()); f != nil {
					f.Usage = voice.Plain(c, o.meaning)
				}
			}
		}
		if p.verbatim {
			takeVerbatim(cmd)
		}
		cmd.SetHelpFunc(func(*cobra.Command, []string) { ui.Println(c, pageText(c, p)) })
		for _, sub := range cmd.Commands() {
			sp, ok := subpageOf(cmd.Name(), sub.Name())
			if !ok {
				continue
			}
			sub.Short = voice.Plain(c, sp.summary)
			sub.Long = voice.Plain(c, sp.about)
			for _, o := range sp.options {
				if f := sub.Flags().Lookup(o.name()); f != nil {
					f.Usage = voice.Plain(c, o.meaning)
				}
			}
			sub.SetHelpFunc(func(*cobra.Command, []string) { ui.Println(c, pageText(c, sp)) })
		}
	}
}

// takeVerbatim turns off flag parsing for cmd and every command under it:
// the TypeScript handed a verbatim command's words over unparsed, so
// "keys init --x" ignored the --x.
func takeVerbatim(cmd *cobra.Command) {
	cmd.DisableFlagParsing = true
	for _, sub := range cmd.Commands() {
		takeVerbatim(sub)
	}
}
