package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/actions"
	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func init() { register(KeepingOrder, "actions", newActionsCmd) }

// newActionsCmd lists the actions profile steps can use.
func newActionsCmd(c *app.Context) *cobra.Command {
	return &cobra.Command{
		Use:  "actions",
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, argv []string) error {
			if len(argv) > 0 {
				return fail.User(voice.Plain(c, msg.CLIUsageActions()))
			}
			if err := c.LocateRoot(); err != nil {
				return err
			}
			list, err := actions.List(c)
			if err != nil {
				return err
			}
			for _, a := range list {
				name := ui.Styles(c).Accent.Render(fmt.Sprintf("%-10s", a.Name))
				kind := ui.Styles(c).Muted.Render(fmt.Sprintf("%-8s", voice.Plain(c, kindLabel(a.Kind))))
				ui.Println(c, voice.Render(c, msg.ActionsLine(name, kind, a.Summary)))
			}
			return nil
		},
	}
}

// kindLabel is the word `homebot actions` shows for where an action comes from.
func kindLabel(k actions.Kind) msg.Text {
	switch k {
	case actions.Internal:
		return msg.ActionsKindInternal()
	case actions.Builtin:
		return msg.ActionsKindBuiltin()
	default:
		return msg.ActionsKindCustom()
	}
}
