package cli

import (
	"context"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/pack"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func init() { register(MovingIn, "pack", newPackCmd) }

func newPackCmd(c *app.Context) *cobra.Command {
	var opts pack.Options
	cmd := &cobra.Command{
		Use:  "pack",
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, paths []string) error { return packRun(c, paths, opts) },
	}
	cmd.Flags().StringVar(&opts.Layer, "layer", "", "")
	cmd.Flags().BoolVar(&opts.Private, "private", false, "")
	return cmd
}

// packRun packs each path in turn and prints a line for it. The first path
// that fails ends the command; the lines before it stay. After a Ctrl-C no
// later path is packed. When a path fails after it was moved, the line
// "moved X -> Y" goes to stderr before the error, so the file can be found.
func packRun(c *app.Context, paths []string, opts pack.Options) error {
	if err := c.LocateRoot(); err != nil {
		return err
	}
	if len(paths) == 0 {
		return fail.User(voice.Plain(c, msg.PackErrorNoArgs()))
	}
	st := ui.Styles(c)
	for _, path := range paths {
		if ctx := c.Context(); ctx.Err() != nil {
			return context.Cause(ctx)
		}
		r, err := pack.Pack(c, path, opts)
		to := r.To
		if rel, relErr := filepath.Rel(c.Root, r.To); relErr == nil {
			to = rel
		}
		if err != nil {
			if r.To != "" {
				ui.Eprintln(c, st.Muted.Render(voice.Render(c, msg.PackStatusMoved(ui.Tildify(r.From, c.Home), to))))
			}
			return err
		}
		line := styleWords(st.OK, voice.Render(c, msg.PackSuccessPacked(ui.Tildify(r.From, c.Home), to)), 1)
		if r.Sealed {
			line += st.Muted.Render(voice.Render(c, msg.PackSuccessSealedSuffix(r.SealedCount)))
		}
		ui.Println(c, line)
	}
	ui.Println(c, st.Muted.Render(voice.Render(c, msg.PackHintCommit())))
	ui.Println(c, voice.Command(c, msg.PackHintCommitCmd()))
	refreshInbox(c)
	return nil
}
