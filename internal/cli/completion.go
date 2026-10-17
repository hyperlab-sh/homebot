package cli

import (
	"io"
	"slices"

	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func init() { register(Hidden, "completion", newCompletionCmd) }

// completionShells are the shells Completion can write a script for, in the
// order the help page and the argument suggestions list them.
var completionShells = []string{"bash", "zsh", "fish"}

// Completion is shared by the CLI and the release documentation generator.
// The CLI checks shell first; the error here is for other callers and is
// the catalog's neutral wording (voice.Record), since there is no context.
func Completion(root *cobra.Command, shell string, w io.Writer) error {
	switch shell {
	case "bash":
		return root.GenBashCompletionV2(w, true)
	case "zsh":
		return root.GenZshCompletion(w)
	case "fish":
		return root.GenFishCompletion(w, true)
	default:
		return fail.User(voice.Record(nil, msg.CLIErrorCompletionUnknown(shell)))
	}
}

func newCompletionCmd(c *app.Context) *cobra.Command {
	return &cobra.Command{
		Use:       "completion <bash|zsh|fish>",
		Args:      cobra.ArbitraryArgs,
		ValidArgs: completionShells,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fail.User(voice.Plain(c, msg.CLIErrorCompletionMissing()))
			}
			if len(args) > 1 {
				return fail.User(voice.Plain(c, msg.CLIErrorCompletionExtra(args[1])))
			}
			if !slices.Contains(completionShells, args[0]) {
				return fail.User(voice.Plain(c, msg.CLIErrorCompletionUnknown(args[0])))
			}
			return Completion(cmd.Root(), args[0], ui.Out(c))
		},
	}
}
