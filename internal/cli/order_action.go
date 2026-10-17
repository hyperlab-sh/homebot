package cli

import (
	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/actions"
	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func init() { register(KeepingOrder, "action", newActionCmd) }

// newActionCmd runs one action by hand. Flag parsing is off: everything after
// the name belongs to the action. A --help or -h right after "action" never
// gets here: dispatch routes it to "help action".
func newActionCmd(c *app.Context) *cobra.Command {
	return &cobra.Command{
		Use:                "action <name> [args...]",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, argv []string) error {
			if len(argv) == 0 {
				return fail.User(voice.Plain(c, msg.CLIUsageAction()))
			}
			if err := c.LocateRoot(); err != nil {
				return err
			}
			return actions.Run(c, argv[0], argv[1:])
		},
	}
}
