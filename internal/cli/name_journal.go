package cli

import (
	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/selfname"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// Every completed link change is reported after selfname writes its record.
// The callback is installed once with the CLI; selfname has no catalog or
// journal dependency. A failed journal write cannot undo a completed link.
func init() { selfname.OnChange = journalNameChange }

func journalNameChange(c *app.Context, ch selfname.Change) {
	var line msg.Text
	switch ch.Kind {
	case selfname.Made:
		line = msg.NameJournalMade()
	case selfname.Adopted:
		line = msg.NameJournalAdopted()
	case selfname.Repointed:
		line = msg.NameJournalRepointed()
	case selfname.Removed:
		line = msg.NameJournalRemoved()
	default:
		return
	}
	if err := state.Journal(c, state.Entry{Msg: voice.Record(c, line)}); err != nil && c.Debug != nil {
		c.Debug.Debug("name journal write failed", "err", err)
	}
}
