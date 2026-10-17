package cli

import (
	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/remote"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func init() { register(KeepingOrder, "remote", newRemoteCmd) }

func newRemoteCmd(c *app.Context) *cobra.Command {
	return &cobra.Command{
		Use:  "remote [url]",
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error { return remoteRun(c, args) },
	}
}

// remoteRun sets up, checks and pushes to the backup remote. It asks
// questions only on a terminal.
func remoteRun(c *app.Context, args []string) error {
	if err := c.LocateRoot(); err != nil {
		return err
	}
	if len(args) > 1 {
		return fail.User(voice.Plain(c, msg.CLIUsageRemote()))
	}
	url := ""
	if len(args) == 1 {
		url = args[0]
	}
	err := remote.Run(c, url, remote.IO{
		Ask:         func(q string) string { return ui.Ask(c, q) },
		Log:         func(line string) { ui.Println(c, line) },
		Interactive: ui.Interactive(c),
	}, remote.Options{})
	if err != nil {
		return err
	}
	refreshInbox(c)
	return nil
}
