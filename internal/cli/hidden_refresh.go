package cli

import (
	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/inbox"
)

func init() { register(Hidden, "refresh", newRefreshCmd) }

// newRefreshCmd is homebot refresh: it rebuilds the inbox's cache files and
// prints nothing. Its page is verbatim: words after it are ignored.
func newRefreshCmd(c *app.Context) *cobra.Command {
	return &cobra.Command{
		Use:  "refresh",
		Args: cobra.ArbitraryArgs,
		RunE: func(*cobra.Command, []string) error {
			_, err := inbox.Refresh(c, inbox.Options{NameTaken: nameTaken})
			return err
		},
	}
}
