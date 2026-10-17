package cli

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/linker"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func init() { register(MovingIn, "link", newLinkCmd) }

type linkFlags struct {
	dryRun bool
	backup bool
}

func newLinkCmd(c *app.Context) *cobra.Command {
	var f linkFlags
	cmd := &cobra.Command{
		Use:  "link",
		Args: cobra.ArbitraryArgs,
		RunE: func(*cobra.Command, []string) error { return runLink(c, f) },
	}
	cmd.Flags().BoolVar(&f.dryRun, "dry-run", false, "")
	cmd.Flags().BoolVar(&f.backup, "backup", false, "")
	return cmd
}

func runLink(c *app.Context, f linkFlags) error {
	if err := c.LocateRoot(); err != nil {
		return err
	}
	plan, err := linker.PlanLinks(c)
	if err != nil {
		return err
	}
	st := ui.Styles(c)
	if f.dryRun {
		showOps(c, plan)
		ui.Println(c, st.Muted.Render(voice.Render(c, msg.LinkStatusDryRunOk(plan.Summary().OK))))
		return nil
	}
	if f.backup {
		moved, err := linker.BackupConflicts(c, plan)
		for _, m := range moved {
			line := voice.Render(c, msg.LinkStatusMovedAside(ui.Tildify(m, c.Home)))
			ui.Println(c, styleWords(st.Todo, line, 2))
		}
		if len(moved) > 0 {
			ui.Println(c, st.Muted.Render(voice.Render(c, msg.LinkHintCompare())))
			ui.Println(c, voice.Command(c, msg.LinkHintCompareCmd()))
		}
		if err := linkStopped(c); err != nil {
			return err
		}
		if err != nil {
			return err
		}
		if plan, err = linker.PlanLinks(c); err != nil {
			return err
		}
	}
	if err := linkStopped(c); err != nil {
		return err
	}
	// Each change is shown as it is made, so a Ctrl-C leaves the list of
	// what was done on the screen.
	var done linker.Plan
	opts := linker.ApplyOptions{Done: func(op linker.Op) {
		done = append(done, op)
		showOps(c, linker.Plan{op})
	}}
	err = linker.ApplyLinks(c, plan, opts)
	if stop := linkStopped(c, msg.LinkSummaryBase(len(done), plan.Summary().OK)); stop != nil {
		return stop
	}
	if err != nil {
		return err
	}
	conflicts := plan.Only(linker.Conflict)
	invalid := plan.Only(linker.Invalid)
	showOps(c, conflicts)
	showOps(c, invalid)

	s := plan.Summary()
	line := voice.Render(c, msg.LinkSummaryBase(len(done), s.OK))
	if s.Locked > 0 {
		line += voice.Render(c, msg.LinkSummaryLocked(s.Locked))
	}
	if len(conflicts) > 0 {
		line += st.Todo.Render(voice.Render(c, msg.LinkSummaryBlocked(len(conflicts))))
	}
	if len(invalid) > 0 {
		line += st.Error.Render(voice.Render(c, msg.LinkSummaryInvalid(len(invalid))))
	}
	ui.Println(c, line)
	if len(conflicts) > 0 {
		ui.Println(c, st.Muted.Render(voice.Render(c, msg.LinkHintBackup())))
		ui.Println(c, voice.Command(c, msg.LinkHintBackupCmd()))
	}
	refreshInbox(c)
	if len(invalid) > 0 {
		return fail.Silently(1)
	}
	return nil
}

// linkStopped is nil until the command is told to stop (Ctrl-C). Then it
// prints the summary lines given, what was done so far, and that the rest
// was left, and returns the cause: no further backup or link is made.
func linkStopped(c *app.Context, summary ...msg.Text) error {
	ctx := c.Context()
	if ctx.Err() == nil {
		return nil
	}
	for _, t := range summary {
		ui.Println(c, voice.Render(c, t))
	}
	ui.Println(c, ui.Styles(c).Todo.Render(voice.Render(c, msg.LinkStatusInterrupted())))
	return context.Cause(ctx)
}

// showOps prints one line per op that is not already right: its label, the
// path in $HOME (or, for a layout error, in the repo) and why, when blocked.
func showOps(c *app.Context, plan linker.Plan) {
	st := ui.Styles(c)
	for _, op := range plan {
		var label string
		switch op.Kind {
		case linker.OK:
			continue
		case linker.Create:
			label = st.OK.Render(voice.Plain(c, msg.LabelLinkOpCreate()))
		case linker.Replace:
			label = st.OK.Render(voice.Plain(c, msg.LabelLinkOpReplace()))
		case linker.Adopt:
			label = st.OK.Render(voice.Plain(c, msg.LabelLinkOpAdopt()))
		case linker.Unfold:
			label = st.Notice.Render(voice.Plain(c, msg.LabelLinkOpUnfold()))
		case linker.Prune:
			label = st.Todo.Render(voice.Plain(c, msg.LabelLinkOpPrune()))
		case linker.Locked:
			label = st.Muted.Render(voice.Plain(c, msg.LabelLinkOpLocked()))
		case linker.Conflict:
			label = st.Error.Render(voice.Plain(c, msg.LabelLinkOpConflict()))
		case linker.Invalid:
			label = st.Error.Render(voice.Plain(c, msg.LabelLinkOpInvalid()))
		}
		target := ui.Tildify(op.Target, c.Home)
		if op.Kind == linker.Invalid {
			if rel, err := filepath.Rel(c.Root, op.Source); err == nil {
				target = rel
			}
		}
		reason := ""
		if op.Kind == linker.Conflict || op.Kind == linker.Invalid {
			reason = st.Muted.Render("  " + voice.Plain(c, op.ReasonText))
		}
		ui.Println(c, voice.Render(c, msg.LinkOpLine(label, target, reason)))
	}
}

// styleWords styles the first n words of line, as the TypeScript colored a
// status word and left the path after it plain.
func styleWords(s ui.Style, line string, n int) string {
	if n <= 0 {
		return line
	}
	end := 0
	for range n {
		i := strings.IndexByte(line[end:], ' ')
		if i < 0 {
			return s.Render(line)
		}
		end += i + 1
	}
	return s.Render(line[:end-1]) + line[end-1:]
}
