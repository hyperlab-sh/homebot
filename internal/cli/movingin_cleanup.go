package cli

import (
	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/cleanup"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func init() { register(MovingIn, "cleanup", newCleanupCmd) }

func newCleanupCmd(c *app.Context) *cobra.Command {
	var list bool
	cmd := &cobra.Command{
		Use:  "cleanup",
		Args: cobra.ArbitraryArgs,
		RunE: func(*cobra.Command, []string) error { return runCleanup(c, list) },
	}
	cmd.Flags().BoolVar(&list, "list", false, "")
	return cmd
}

// runCleanup reviews the backups link --backup left, one at a time. With
// --list, or when no one can answer (stdin is not a terminal), it only lists
// them.
func runCleanup(c *app.Context, list bool) error {
	if err := c.LocateRoot(); err != nil {
		return err
	}
	backups, err := cleanup.FindBackups(c)
	if err != nil {
		return err
	}
	st := ui.Styles(c)
	if len(backups) == 0 {
		ui.Println(c, st.OK.Render(voice.Render(c, msg.CleanupSuccessNone())))
		return nil
	}
	if list || !ui.Interactive(c) {
		for _, b := range backups {
			ui.Println(c, voice.Render(c, msg.CleanupListItem(ui.Tildify(b.Path, c.Home))))
		}
		if !list {
			ui.Println(c, st.Muted.Render(voice.Render(c, msg.CleanupHintTerminal())))
			ui.Println(c, voice.Command(c, msg.CleanupHintTerminalCmd()))
		}
		return nil
	}
	r, err := cleanup.Review(c, backups, cleanup.DefaultChooser(c), cleanup.TerminalPicker(c))
	if err != nil {
		return err
	}
	if r.Merged > 0 {
		ui.Println(c, voice.Render(c, msg.CleanupStatusSummaryMerged(r.Deleted, r.Used, r.Kept, r.Merged)))
	} else {
		ui.Println(c, voice.Render(c, msg.CleanupStatusSummary(r.Deleted, r.Used, r.Kept)))
	}
	refreshInbox(c)
	return nil
}
