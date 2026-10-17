package cli

import (
	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/inbox"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/ui"
)

func init() { register(Attention, "inbox", newInboxCmd) }

// newInboxCmd is homebot inbox, also what a bare homebot runs. Words after
// it are ignored, as the TypeScript did.
func newInboxCmd(c *app.Context) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:  "inbox",
		Args: cobra.ArbitraryArgs,
		RunE: func(*cobra.Command, []string) error { return runInbox(c, all) },
	}
	cmd.Flags().BoolVar(&all, "all", false, "")
	return cmd
}

// runInbox rebuilds the inbox and its caches, prints it with what was done
// since the person last looked, and remembers that they looked now.
func runInbox(c *app.Context, all bool) error {
	if err := c.LocateRoot(); err != nil {
		return err
	}
	items, err := inbox.Refresh(c, inbox.Options{NameTaken: nameTaken})
	if err != nil {
		return err
	}
	done, err := inboxDoneSinceLastSeen(c)
	if err != nil {
		return err
	}
	ui.Println(c, inbox.Render(c, items, done, all))
	return state.Update(c, func(s *state.State) { s.LastSeen = c.Now().UnixMilli() })
}

// inboxDoneSinceLastSeen is the journal entries written after the person
// last saw the inbox.
func inboxDoneSinceLastSeen(c *app.Context) ([]state.Entry, error) {
	st, err := state.Load(c)
	if err != nil {
		return nil, err
	}
	journal, err := state.ReadJournal(c)
	if err != nil {
		return nil, err
	}
	var done []state.Entry
	for _, e := range journal {
		if e.At > st.LastSeen {
			done = append(done, e)
		}
	}
	return done, nil
}
