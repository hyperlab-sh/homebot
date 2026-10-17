package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/secrets"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func init() { register(Secrets, "lock", newLockCmd) }

func newLockCmd(c *app.Context) *cobra.Command {
	var opts secrets.LockOptions
	cmd := &cobra.Command{
		Use:  "lock",
		Args: cobra.ArbitraryArgs,
		RunE: func(*cobra.Command, []string) error { return runLockCmd(c, opts) },
	}
	cmd.Flags().BoolVar(&opts.Ours, "ours", false, "")
	cmd.Flags().BoolVar(&opts.Wipe, "wipe", false, "")
	return cmd
}

// runLockCmd seals the secrets and, with --wipe, deletes the safe plaintext.
// When a secret fails, what was done before it is printed first, then the
// error: the TypeScript printed nothing.
func runLockCmd(c *app.Context, opts secrets.LockOptions) error {
	if err := c.LocateRoot(); err != nil {
		return err
	}
	r, err := secrets.Lock(c, opts)
	if err != nil {
		if len(r.Sealed)+len(r.Wiped) > 0 {
			showLocked(c, opts, r)
		}
		return err
	}
	showLocked(c, opts, r)
	refreshInbox(c)
	return nil
}

func showLocked(c *app.Context, opts secrets.LockOptions, r secrets.LockResult) {
	st := ui.Styles(c)
	switch {
	case opts.Wipe:
		ui.Println(c, voice.Render(c, msg.LockStatusWiped(len(r.Sealed), len(r.Wiped))))
		for _, rel := range r.Kept {
			ui.Println(c, secretsStyleLead(c, st.Todo, msg.LockWarningKept(rel), " "))
		}
	case len(r.Sealed) > 0:
		paths := make([]string, len(r.Sealed))
		for i, rel := range r.Sealed {
			paths[i] = "private/" + rel
		}
		ui.Println(c, secretsStyleLead(c, st.OK, msg.LockSuccessSealed(strings.Join(paths, ", ")), " "))
	default:
		ui.Println(c, voice.Render(c, msg.LockStatusNothing()))
	}
}
