package cli

import (
	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/inbox"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func init() { register(Attention, "next", newNextCmd) }

// newNextCmd is homebot next: it shows the inbox's first item and runs its
// fix when that is a homebot command. Its page is verbatim: words after it
// are ignored.
func newNextCmd(c *app.Context) *cobra.Command {
	return &cobra.Command{
		Use:  "next",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return runNext(cmd, c) },
	}
}

func runNext(cmd *cobra.Command, c *app.Context) error {
	items, err := inbox.Collect(c, nameTaken)
	if err != nil {
		return err
	}
	st := ui.Styles(c)
	if len(items) == 0 {
		ui.Println(c, st.OK.Render(voice.Render(c, msg.NextSuccessNothing())))
		return nil
	}
	first := items[0]
	ui.Println(c, st.Accent.Render(voice.Render(c, msg.NextItemTitle(first.Title))))
	if first.Detail != "" {
		ui.Println(c, st.Muted.Render(voice.Render(c, msg.NextItemDetail(first.Detail))))
	}
	switch {
	case first.Fix == nil:
		ui.Println(c, voice.Render(c, msg.NextHintNoFix()))
		return nil
	case first.Fix.Shell != "":
		ui.Println(c, voice.Render(c, msg.NextHintRunYourself()))
		ui.Println(c, ui.Command(c, voice.Render(c, msg.FormatCommand(first.Fix.Shell))))
		return nil
	}
	ui.Println(c, ui.Command(c, voice.Render(c, msg.NextStatusRunning(inbox.FixText(c, first.Fix)))))
	return nextRun(cmd, c, first.Fix.Args)
}

// nextRun runs a fix's homebot command line as if it had been typed, so a
// fix with a mistake in it fails as the same line typed would.
func nextRun(cmd *cobra.Command, c *app.Context, args []string) error {
	root := NewRoot(c, Build{})
	argv, err := dispatch(c, root, args)
	if err != nil {
		return err
	}
	root.SetArgs(argv)
	return root.ExecuteContext(cmd.Context())
}
