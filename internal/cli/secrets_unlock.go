package cli

import (
	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/linker"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/secrets"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func init() { register(Secrets, "unlock", newUnlockCmd) }

func newUnlockCmd(c *app.Context) *cobra.Command {
	var theirs bool
	cmd := &cobra.Command{
		Use:  "unlock",
		Args: cobra.ArbitraryArgs,
		RunE: func(*cobra.Command, []string) error { return runUnlockCmd(c, theirs) },
	}
	cmd.Flags().BoolVar(&theirs, "theirs", false, "")
	return cmd
}

// runUnlockCmd unlocks the secrets and links them into $HOME. When a secret
// fails, what was done before it is printed first, then the error: the
// TypeScript printed nothing.
func runUnlockCmd(c *app.Context, theirs bool) error {
	if err := c.LocateRoot(); err != nil {
		return err
	}
	r, err := secrets.Unlock(c, secrets.UnlockOptions{Theirs: theirs})
	if err != nil {
		if len(r.Unlocked)+len(r.Verified)+len(r.Conflicts) > 0 {
			showUnlocked(c, r)
		}
		return err
	}
	showUnlocked(c, r)
	plan, err := linker.PlanLinks(c, linker.Private)
	if err != nil {
		return err
	}
	opts := linker.ApplyOptions{Safe: true}
	if err := linker.ApplyLinks(c, plan, opts); err != nil {
		return err
	}
	if done := plan.Changes(opts); len(done) > 0 {
		ui.Println(c, voice.Render(c, msg.UnlockStatusLinked(len(done))))
	}
	refreshInbox(c)
	return nil
}

func showUnlocked(c *app.Context, r secrets.UnlockResult) {
	st := ui.Styles(c)
	line := secretsStyleLead(c, st.OK, msg.UnlockSuccessUnlocked(len(r.Unlocked)), " ")
	if len(r.Verified) > 0 {
		line += voice.Render(c, msg.UnlockSuccessVerifiedSuffix(len(r.Verified)))
	}
	ui.Println(c, line)
	for _, rel := range r.Conflicts {
		ui.Println(c, secretsStyleLead(c, st.Error, msg.UnlockErrorConflict(rel), " "))
	}
	if len(r.Conflicts) > 0 {
		ui.Println(c, voice.Render(c, msg.UnlockHintOurs()))
		ui.Println(c, voice.Command(c, msg.UnlockHintOursCmd()))
		ui.Println(c, voice.Render(c, msg.UnlockHintTheirs()))
		ui.Println(c, voice.Command(c, msg.UnlockHintTheirsCmd()))
	}
}
