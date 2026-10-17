package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/gitx"
	"github.com/hyperlab-sh/homebot/internal/linker"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/save"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func init() { register(KeepingOrder, "save", newSaveCmd) }

type saveFlags struct {
	message string
	noPush  bool
}

func newSaveCmd(c *app.Context) *cobra.Command {
	var f saveFlags
	cmd := &cobra.Command{
		Use:  "save",
		Args: cobra.ArbitraryArgs,
		RunE: func(*cobra.Command, []string) error { return runSave(c, f) },
	}
	cmd.Flags().StringVarP(&f.message, "message", "m", "", "")
	cmd.Flags().BoolVar(&f.noPush, "no-push", false, "")
	return cmd
}

// runSave seals, commits, then pulls and pushes. After a pull it relinks, so
// files another machine added show up in $HOME.
func runSave(c *app.Context, f saveFlags) error {
	if err := c.LocateRoot(); err != nil {
		return err
	}
	r, err := save.Save(c, save.Options{Message: f.message})
	if err != nil {
		return err
	}
	st := ui.Styles(c)
	if r.Sealed > 0 {
		ui.Println(c, secretsStyleLead(c, st.OK, msg.SaveSuccessSealed(r.Sealed), " "))
	}
	if r.Committed {
		ui.Println(c, secretsStyleLead(c, st.OK, msg.SaveSuccessCommitted(len(r.Files)), " "))
	} else {
		ui.Println(c, voice.Render(c, msg.SaveStatusNothing()))
	}

	s, err := save.Sync(c, save.SyncOptions{Push: !f.noPush})
	if err != nil {
		return err
	}
	if s.Pulled > 0 {
		ui.Println(c, secretsStyleLead(c, st.OK, msg.SaveSuccessPulled(s.Pulled), " "))
	}
	if s.Pushed > 0 {
		ui.Println(c, secretsStyleLead(c, st.OK, msg.SaveSuccessPushed(s.Pushed), " "))
	}
	if s.Waiting {
		ui.Println(c, st.Todo.Render(voice.Render(c, msg.SaveWarningWaiting())))
	}
	if !s.Reachable && strings.TrimSpace(gitx.Run(c, c.Root, "remote").Stdout) != "" {
		ui.Println(c, st.Todo.Render(voice.Render(c, msg.SaveWarningUnreachable())))
		ui.Println(c, voice.Command(c, msg.SaveWarningUnreachableCmd()))
	}
	if s.Pulled > 0 {
		plan, err := linker.PlanLinks(c)
		if err != nil {
			return err
		}
		if err := linker.ApplyLinks(c, plan, linker.ApplyOptions{Safe: true}); err != nil {
			return err
		}
	}
	refreshInbox(c)
	return nil
}
