package cli

import (
	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// ManEntry is one visible help page (or version) as neutral text for the
// generated man page. The page catalog remains the source of usage, options,
// and examples; root confirms that each entry is a runnable command.
type ManEntry struct {
	Name, Usage, Description string
	Options                  []ManRow
	Examples                 []ManRow
}

// manCommandName prefixes man page examples. Help adds selfname.Effective at
// render time, but a man page is static and ships to every machine, so it
// always uses the canonical name and never a locally chosen one.
const manCommandName = "homebot"

// ManRow is a label and its explanation in a man page table.
type ManRow struct{ Label, Meaning string }

func ManEntries(root *cobra.Command) []ManEntry {
	ctx := &app.Context{Plain: true}
	plain := func(t msg.Text) string { return voice.Plain(ctx, t) }
	var entries []ManEntry
	for _, p := range pages {
		if p.group == Hidden && p.name != "version" || child(root, p.name) == nil {
			continue
		}
		e := ManEntry{Name: p.name, Usage: plain(p.usage), Description: plain(p.about)}
		for _, o := range p.options {
			e.Options = append(e.Options, ManRow{o.flag, plain(o.meaning)})
		}
		for _, example := range p.examples {
			e.Examples = append(e.Examples, ManRow{manCommandName + " " + example.command, plain(example.meaning)})
		}
		entries = append(entries, e)
	}
	return entries
}
