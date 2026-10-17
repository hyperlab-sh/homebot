package cli

import (
	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/exports"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func init() { register(KeepingOrder, "export", newExportCmd) }

func newExportCmd(c *app.Context) *cobra.Command {
	return &cobra.Command{
		Use:  "export [name...]",
		RunE: func(_ *cobra.Command, args []string) error { return runExportCmd(c, args) },
	}
}

func runExportCmd(c *app.Context, names []string) error {
	if err := c.LocateRoot(); err != nil {
		return err
	}
	results, err := exports.RunExports(c, exports.Options{Names: names})
	if err := printExportOutcome(c, results, err); err != nil {
		return err
	}
	refreshInbox(c)
	return nil
}

// printExportOutcome prints what RunExports returned. When it failed, only
// the warnings of the exports that ran before the failure are printed, as
// tidy journals them: each names a folder that stays behind. The error is
// returned for the caller to report.
func printExportOutcome(c *app.Context, results []exports.Result, err error) error {
	if err != nil {
		for _, r := range results {
			printWarning(c, r.Warning)
		}
		return err
	}
	printExportResults(c, results)
	return nil
}

// printExportResults says what each export did, one line each on stdout. A
// warning goes under its export's line, to stderr in the warning style.
func printExportResults(c *app.Context, results []exports.Result) {
	if len(results) == 0 {
		ui.Println(c, voice.Render(c, msg.ExportStatusNone()))
	}
	st := ui.Styles(c)
	label := func(s ui.Style, t msg.Text) string { return s.Render(voice.Plain(c, t)) }
	for _, r := range results {
		switch {
		case r.Reason != "":
			ui.Println(c, voice.Render(c, msg.ExportLineSkipped(label(st.Muted, msg.LabelExportSkipped()), r.Name, r.Reason)))
		case r.OK && r.Changed:
			ui.Println(c, voice.Render(c, msg.ExportLineOk(label(st.OK, msg.LabelExportUpdated()), r.Out)))
		case r.OK:
			ui.Println(c, voice.Render(c, msg.ExportLineOk(label(st.OK, msg.LabelExportUnchanged()), r.Out)))
		default:
			errorText := r.Error
			if r.ErrorText.ID() != "" {
				errorText = voice.Plain(c, r.ErrorText)
			}
			ui.Println(c, voice.Render(c, msg.ExportLineFailed(label(st.Error, msg.LabelExportFailed()), r.Name, errorText)))
		}
		printWarning(c, r.Warning)
	}
}

// printWarning prints a non-fatal problem to stderr in the warning style;
// nothing when w is empty.
func printWarning(c *app.Context, w string) {
	if w != "" {
		ui.Eprintln(c, ui.ErrStyles(c).Todo.Render(w))
	}
}
