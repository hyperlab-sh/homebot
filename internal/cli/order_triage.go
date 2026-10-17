package cli

import (
	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/triage"
)

func init() { register(KeepingOrder, "triage", newTriageCmd) }

func newTriageCmd(c *app.Context) *cobra.Command {
	return &cobra.Command{
		Use:  "triage",
		Args: cobra.ArbitraryArgs,
		RunE: func(*cobra.Command, []string) error { return runTriage(c) },
	}
}

// runTriage asks about everything new in the watched folders and the repo:
// track, ignore or snooze. Then it rebuilds the inbox.
func runTriage(c *app.Context) error {
	if err := c.LocateRoot(); err != nil {
		return err
	}
	if err := triage.Run(c); err != nil {
		return err
	}
	refreshInbox(c)
	return nil
}
