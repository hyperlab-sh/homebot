package cli

import (
	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/helper"
	"github.com/hyperlab-sh/homebot/internal/linker"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func init() { register(MovingIn, "unlink", newUnlinkCmd) }

func newUnlinkCmd(c *app.Context) *cobra.Command {
	return &cobra.Command{
		Use: "unlink",
		// The TypeScript unlink read no arguments, so a word is no error. An
		// option is: unlink --dry-run must not remove every link.
		Args: cobra.ArbitraryArgs,
		RunE: func(*cobra.Command, []string) error { return runUnlink(c) },
	}
}

func runUnlink(c *app.Context) error {
	if err := c.LocateRoot(); err != nil {
		return err
	}
	removed, err := linker.UnlinkAll(c)
	if err != nil {
		return err
	}
	ui.Println(c, voice.Render(c, msg.UnlinkStatusRemoved(len(removed))))
	ui.Println(c, voice.Command(c, msg.UnlinkHintCmd()))
	// The helper's tidy links everything again, so "unlink" lasts until
	// its next run. Say so, and how to keep a file unlinked for good.
	if len(removed) > 0 && helper.Installed(c) {
		ui.Println(c, ui.Styles(c).Muted.Render(voice.Render(c, msg.UnlinkHintHelper())))
	}
	refreshInbox(c)
	return nil
}
