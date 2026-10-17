package cli

import (
	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/guard"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func init() { register(Hidden, "hook", newHookCmd) }

// newHookCmd is homebot hook pre-commit, what the commit guard's shim runs.
// Its page is verbatim, so it checks its own arguments.
func newHookCmd(c *app.Context) *cobra.Command {
	return &cobra.Command{
		Use:  "hook [hook]",
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 || args[0] != "pre-commit" {
				// As recorded: no home is an error before the usage line.
				if err := c.LocateRoot(); err != nil {
					return err
				}
				return fail.User(voice.Plain(c, msg.CLIUsageHook()))
			}
			// The guard finds the repo git is committing in itself; the home
			// LocateRoot would pick (HOMEBOT_ROOT) may be another one.
			if code := guard.PreCommit(c); code != 0 {
				return fail.Silently(code)
			}
			return nil
		},
	}
}
