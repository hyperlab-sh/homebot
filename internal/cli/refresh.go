package cli

import (
	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/inbox"
)

// refreshInbox rebuilds the inbox caches after a command changes things.
// It never fails the command: what went wrong goes to the debug log
// (decision D8), and the next inbox or tidy run tries again.
func refreshInbox(c *app.Context) {
	if _, err := inbox.Refresh(c, inbox.Options{NameTaken: nameTaken}); err != nil {
		c.Debug.Debug("inbox refresh failed", "err", err)
	}
}
