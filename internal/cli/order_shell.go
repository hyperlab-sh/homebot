package cli

import (
	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/shell"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func init() { register(KeepingOrder, "shell", newShellCmd) }

func newShellCmd(c *app.Context) *cobra.Command {
	return &cobra.Command{
		Use:  "shell [shell]",
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 || args[0] != "zsh" {
				return fail.User(voice.Plain(c, msg.CLIUsageShell()))
			}
			_, err := ui.Out(c).Write([]byte(shell.ZshFor(c)))
			return err
		},
	}
}
