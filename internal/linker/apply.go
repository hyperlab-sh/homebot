package linker

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// ApplyOptions changes what ApplyLinks does.
type ApplyOptions struct {
	// Safe is unattended mode: real files are never replaced, even identical
	// ones, so Adopt ops are skipped.
	Safe bool
	// Done, when set, is called after each op is carried out, in order, so a
	// command can show what was done as it happens.
	Done func(Op)
}

// errChanged is apply's answer for an op whose target changed since the plan
// was made: it was left alone.
var errChanged = errors.New("linker: the target changed since the plan was made")

// ApplyLinks carries out the ops of plan that change $HOME, in order:
// plan.Changes(opts) is what it did when it returns nil. It stops at the
// first failure, and before the next op once ctx's command is told to stop
// (Ctrl-C), with the cause. An op whose target changed since the plan was
// made is skipped, with the ops below it, and the rest still run; the error
// then names each one. Either way it then records every link that points
// into the repo in links.json, so later runs can prune the links made before
// a failure too.
func ApplyLinks(ctx *app.Context, plan Plan, opts ApplyOptions) error {
	var err error
	var changed []string
	for _, op := range plan.Changes(opts) {
		if slices.ContainsFunc(changed, func(t string) bool { return app.IsInside(op.Target, t) }) {
			continue // below a link left alone: it is not the folder planned
		}
		if c := ctx.Context(); c.Err() != nil {
			err = context.Cause(c)
			break
		}
		e := apply(op)
		if errors.Is(e, errChanged) {
			changed = append(changed, op.Target)
			continue
		}
		if err = e; err != nil {
			break
		}
		if opts.Done != nil {
			opts.Done(op)
		}
	}
	return errors.Join(err, changedError(ctx, changed), saveLinkState(ctx, plan))
}

// changedError names the targets left alone because they changed since the
// plan was made; nil when there are none.
func changedError(ctx *app.Context, targets []string) error {
	if len(targets) == 0 {
		return nil
	}
	lines := make([]string, len(targets))
	for i, t := range targets {
		lines[i] = voice.Record(ctx, msg.LinkErrorChanged(ui.Tildify(t, ctx.Home)))
	}
	return fail.User(strings.Join(lines, "\n"))
}

// apply carries out one op. What it removes is a link or a file, never a
// folder, even one that appeared since the plan was made. Just before it
// removes anything it checks again that the target is what the plan saw: an
// identical file for Adopt, the same link for Replace, Unfold and Prune.
// When it is not, it returns errChanged and touches nothing. Folders it makes
// for private files are 0700.
func apply(op Op) error {
	perm := fs.FileMode(0o777)
	if op.private {
		perm = 0o700
	}
	switch op.Kind {
	case Create:
		if err := os.MkdirAll(filepath.Dir(op.Target), perm); err != nil {
			return err
		}
		return os.Symlink(op.Source, op.Target)
	case Adopt:
		if st, err := os.Lstat(op.Target); err != nil || !st.Mode().IsRegular() || !sameContent(op.Target, op.Source) {
			return errChanged
		}
		if err := unlink(op.Target); err != nil {
			return err
		}
		return os.Symlink(op.Source, op.Target)
	case Replace:
		if !stillLinkTo(op.Target, op.Was) {
			return errChanged
		}
		if err := unlink(op.Target); err != nil {
			return err
		}
		return os.Symlink(op.Source, op.Target)
	case Unfold:
		if !stillLinkTo(op.Target, op.Was) {
			return errChanged
		}
		if err := unlink(op.Target); err != nil {
			return err
		}
		return os.Mkdir(op.Target, perm)
	case Prune:
		if !stillLinkTo(op.Target, op.Was) {
			return errChanged
		}
		return unlink(op.Target)
	}
	return nil
}

// unlink removes the link or file at path, as unlinkSync did. Unlike
// os.Remove it never removes a folder.
func unlink(path string) error {
	if err := syscall.Unlink(path); err != nil {
		return &fs.PathError{Op: "unlink", Path: path, Err: err}
	}
	return nil
}

// saveLinkState records, sorted, every link among the recorded ones and
// plan's targets that points into the repo now.
func saveLinkState(ctx *app.Context, plan Plan) error {
	recorded, err := RecordedLinks(ctx)
	if err != nil {
		return err
	}
	candidates := slices.Clone(recorded)
	for _, op := range plan {
		candidates = append(candidates, op.Target)
	}
	slices.Sort(candidates)
	var owned []string
	for _, target := range slices.Compact(candidates) {
		_, ok, err := OwnsLink(ctx, target)
		if err != nil {
			return err
		}
		if ok {
			owned = append(owned, target)
		}
	}
	return (&state.Links{Targets: owned}).Save(ctx)
}

// RecordedLinks returns the link targets links.json records.
func RecordedLinks(ctx *app.Context) ([]string, error) {
	l, err := state.LoadLinks(ctx)
	if err != nil {
		return nil, err
	}
	return l.Targets, nil
}

// OwnsLink reports whether target is a symlink into the repo, and where it
// points. A link through another path to the repo (~/.home and the folder it
// links to) counts. Nothing there, or a file where a folder on the way should be, is
// not a link.
func OwnsLink(ctx *app.Context, target string) (was string, owned bool, err error) {
	st, err := fsx.LstatIfExists(target)
	if errors.Is(err, syscall.ENOTDIR) {
		return "", false, nil
	}
	if err != nil || st == nil || st.Mode()&fs.ModeSymlink == 0 {
		return "", false, err
	}
	dest, err := ReadlinkAbs(target)
	if err != nil {
		return "", false, err
	}
	if !pointsInside(dest, ctx.Root) {
		return "", false, nil
	}
	return dest, true, nil
}

// UnlinkAll removes every recorded link that still points into the repo and
// records none. Real files are never touched. It returns the links removed.
func UnlinkAll(ctx *app.Context) ([]string, error) {
	recorded, err := RecordedLinks(ctx)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, target := range recorded {
		_, ok, err := OwnsLink(ctx, target)
		if err != nil {
			return removed, err
		}
		if !ok {
			continue
		}
		if err := unlink(target); err != nil {
			return removed, err
		}
		removed = append(removed, target)
	}
	return removed, (&state.Links{}).Save(ctx)
}
