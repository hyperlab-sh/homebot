package cli

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/secrets"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func init() { register(Secrets, "keys", newKeysCmd) }

func newKeysCmd(c *app.Context) *cobra.Command {
	keys := &cobra.Command{
		Use:  "keys",
		Args: cobra.ArbitraryArgs,
		RunE: func(*cobra.Command, []string) error {
			if err := c.LocateRoot(); err != nil {
				return err
			}
			return fail.User(voice.Plain(c, msg.CLIUsageKeys()))
		},
	}
	keys.AddCommand(&cobra.Command{
		Use:  "init",
		Args: cobra.ArbitraryArgs,
		RunE: func(*cobra.Command, []string) error { return runKeysInit(c) },
	})
	return keys
}

func runKeysInit(c *app.Context) error {
	if err := c.LocateRoot(); err != nil {
		return err
	}
	st := ui.Styles(c)
	ui.Println(c, voice.Render(c, msg.KeysStatusCreating()))
	ui.Println(c, st.Muted.Render(ui.WrapText(voice.Render(c, msg.KeysHintPassphrase()), "", ui.Columns(c))))
	ui.Println(c, "")
	public, err := secrets.InitKeys(c)
	if err != nil {
		return err
	}
	ui.Println(c, "")
	ui.Println(c, secretsStyleLead(c, st.OK, msg.KeysSuccessCreated(), ". "))
	ui.Println(c, voice.Render(c, msg.KeysStatusPublicKey(public)))
	ui.Println(c, voice.Render(c, msg.KeysHintCommit()))
	ui.Println(c, voice.Command(c, msg.KeysHintCommitCmd()))
	refreshInbox(c)
	return nil
}

// secretsStyleLead renders t and styles its start, up to where sep first
// appears: the first word with " ", the first sentence, its full stop
// included, with ". ". Text without sep is styled whole. The lead is found
// in the plain text and cut from the rendered text by width, so the styles
// of t's values never shift it.
func secretsStyleLead(c *app.Context, s ui.Style, t msg.Text, sep string) string {
	plain, rendered := voice.Plain(c, t), voice.Render(c, t)
	i := strings.Index(plain, sep)
	if i < 0 {
		return s.Render(rendered)
	}
	lead := plain[:i+len(strings.TrimRight(sep, " "))]
	return s.Render(lead) + ansi.TruncateLeft(rendered, ansi.StringWidth(lead), "")
}
