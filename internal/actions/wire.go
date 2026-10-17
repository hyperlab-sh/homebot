package actions

import (
	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/linker"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/secrets"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func init() {
	RegisterInternal("link", msg.ActionLinkSummary(), linkAction)
	RegisterInternal("unlock", msg.ActionUnlockSummary(), unlockAction)
}

// linkAction links the named trees (all when none are named) into $HOME,
// as the link step of a profile does.
func linkAction(c *app.Context, args []string) error {
	trees := make([]linker.Tree, 0, len(args))
	for _, a := range args {
		t := linker.Tree(a)
		if t != linker.Autolink && t != linker.Private {
			return fail.Say(msg.ActionLinkErrorUnknownTree(a))
		}
		trees = append(trees, t)
	}
	plan, err := linker.PlanLinks(c, trees...)
	if err != nil {
		return err
	}
	opts := linker.ApplyOptions{}
	if err := linker.ApplyLinks(c, plan, opts); err != nil {
		return err
	}
	line := voice.Render(c, msg.ActionLinkStatusChanged(len(plan.Changes(opts))))
	if n := len(plan.Only(linker.Conflict)); n > 0 {
		line += ui.Styles(c).Todo.Render(voice.Render(c, msg.ActionLinkWarningInWay(n)))
	}
	ui.Println(c, line)
	if invalid := plan.Only(linker.Invalid); len(invalid) > 0 {
		// The error is stored with the neutral reasons (unpack keeps it) and
		// printed with the persona's: SayWith builds it once for each.
		return voice.SayWith(func(say func(msg.Text) string) msg.Text {
			reasons := ""
			for i, op := range invalid {
				if i > 0 {
					reasons += "\n"
				}
				reasons += "  " + say(op.ReasonText)
			}
			return msg.ActionLinkErrorLayout(len(invalid), reasons)
		})
	}
	return nil
}

// unlockAction decrypts private/ unless there is nothing to decrypt, so a
// repo without secrets never asks for a passphrase.
func unlockAction(c *app.Context, _ []string) error {
	all, err := secrets.List(c)
	if err != nil {
		return err
	}
	pending := 0
	for _, s := range all {
		switch s.State {
		case secrets.Sealed, secrets.New, secrets.Modified:
		default:
			pending++
		}
	}
	if pending == 0 {
		ui.Println(c, voice.Render(c, msg.ActionUnlockStatusNothing()))
		return nil
	}
	r, err := secrets.Unlock(c, secrets.UnlockOptions{})
	if err != nil {
		return err
	}
	line := voice.Render(c, msg.ActionUnlockStatusDecrypted(len(r.Unlocked)))
	if n := len(r.Conflicts); n > 0 {
		line += ui.Styles(c).Todo.Render(voice.Render(c, msg.ActionUnlockWarningConflicts(n)))
	}
	ui.Println(c, line)
	return nil
}
