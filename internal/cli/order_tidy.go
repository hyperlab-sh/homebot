package cli

import (
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/inbox"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/tidy"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/update"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func init() { register(KeepingOrder, "tidy", newTidyCmd) }

type tidyFlags struct{ notify, quiet, helper bool }

func newTidyCmd(c *app.Context) *cobra.Command {
	var f tidyFlags
	cmd := &cobra.Command{
		Use:  "tidy",
		Args: cobra.ArbitraryArgs,
		RunE: func(*cobra.Command, []string) error { return tidyRun(c, f) },
	}
	cmd.Flags().BoolVar(&f.notify, "notify", false, "")
	cmd.Flags().BoolVar(&f.quiet, "quiet", false, "")
	cmd.Flags().BoolVar(&f.helper, "helper", false, "")
	_ = cmd.Flags().MarkHidden("helper")
	return cmd
}

// tidyRun does the hourly chores by hand and says what it did. --quiet is
// what the background helper passes: it prints nothing, errors apart.
func tidyRun(c *app.Context, f tidyFlags) error {
	r, err := tidy.Run(c, tidy.Options{Notify: f.notify, NameTaken: nameTaken})
	if err != nil {
		return err
	}
	if r.Ran && f.helper && c.Config != nil && c.Config.Update != config.UpdateOff {
		info, _ := debug.ReadBuildInfo()
		outcome, stepErr := update.Step(c, update.Build{Version: c.BuildVersion, Channel: c.BuildChannel}, info, c.Config.Update, update.DefaultDeadline)
		if stepErr != nil {
			c.Debug.Debug("update: helper step failed", "error", stepErr)
		}
		if outcome.UpdatedVersion != "" {
			if journalErr := state.Journal(c, state.Entry{Msg: voice.Record(c, msg.UpdateJournalUpdated(outcome.UpdatedVersion))}); journalErr != nil {
				c.Debug.Debug("update: journal failed", "error", journalErr)
			}
			inbox.NotifyUpdate(c, outcome.UpdatedVersion, "")
		}
		if outcome.BrokenCommand != "" {
			inbox.NotifyUpdate(c, "", outcome.BrokenCommand)
		}
		if _, refreshErr := inbox.Refresh(c, inbox.Options{Notify: f.notify, NameTaken: nameTaken}); refreshErr != nil {
			c.Debug.Debug("update: inbox refresh failed", "error", refreshErr)
		}
	}
	if f.quiet {
		return nil
	}
	printTidyResult(c, r)
	return nil
}

// printTidyResult says what a tidy did and what failed, on stdout; its
// warnings go to stderr in the warning style, as homebot export prints them.
func printTidyResult(c *app.Context, r tidy.Result) {
	if !r.Ran {
		ui.Println(c, voice.Render(c, msg.TidyStatusLocked()))
		return
	}
	st := ui.Styles(c)
	for _, d := range r.Did {
		ui.Println(c, secretsStyleLead(c, st.OK, msg.TidyLineDone(d), " "))
	}
	for _, e := range r.Errors {
		ui.Println(c, secretsStyleLead(c, st.Error, msg.TidyLineFailed(e.Step, e.Said()), " "))
	}
	for _, w := range r.Warnings {
		printWarning(c, w)
	}
	if len(r.Did) == 0 && len(r.Errors) == 0 {
		ui.Println(c, voice.Render(c, msg.TidyStatusNothing()))
	}
	ui.Println(c, st.Muted.Render(voice.Render(c, msg.TidyStatusInboxCount(len(r.Items)))))
}
