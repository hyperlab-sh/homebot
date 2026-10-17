package save

import (
	"fmt"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/gitx"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/state"
)

// SyncOptions says what Sync may do beyond pulling.
type SyncOptions struct{ Push bool }

// SyncResult is what Sync did. A remote that cannot be reached is not an
// error: Reachable is false and the rest is zero.
type SyncResult struct {
	Reachable bool
	Pulled    int
	Pushed    int
	Waiting   bool // behind with local changes; nothing pulled
}

// Sync brings the branch level with its remote: fetch, fast-forward or
// rebase (aborting cleanly on a conflict), then push when o.Push is set.
// state.LastSync is written once a fetch succeeds. Having no remote, or one
// that cannot be reached, is not an error.
func Sync(c *app.Context, o SyncOptions) (SyncResult, error) {
	var res SyncResult
	remotes := gitx.Run(c, c.Root, "remote")
	if remotes.Code != 0 {
		// Not "no remote": whether there is one is unknown.
		if err := c.Context().Err(); err != nil {
			return res, err
		}
		return res, fmt.Errorf("%w%s", fail.Say(msg.GitErrorFailed("remote")), fail.Failure("", remotes.Stderr, "git"))
	}
	if strings.TrimSpace(remotes.Stdout) == "" {
		return res, nil
	}
	if gitx.RunNet(c, c.Root, proc.Options{}, "fetch", "--quiet").Code != 0 {
		// A fetch cut short by Ctrl-C is not an unreachable remote.
		return res, c.Context().Err()
	}
	res.Reachable = true
	if err := state.Update(c, func(s *state.State) { s.LastSync = c.Now().UnixMilli() }); err != nil {
		return res, err
	}

	st, err := gitx.Status(c, c.Root)
	if err != nil {
		return res, err
	}
	if st.Upstream != "" && st.Behind > 0 {
		switch {
		case len(st.Changed) > 0:
			res.Waiting = true
		case st.Ahead == 0:
			if r := gitx.Run(c, c.Root, "merge", "--ff-only", "--quiet", "@{u}"); r.Code != 0 {
				return res, fmt.Errorf("%w%s", fail.Say(msg.SaveErrorFastForward()), fail.Failure("", r.Stderr, ""))
			}
			res.Pulled = st.Behind
		default:
			if r := gitx.Run(c, c.Root, "rebase", "--quiet", "@{u}"); r.Code != 0 {
				if a := gitx.Run(c, c.Root, "rebase", "--abort"); a.Code != 0 {
					return res, fmt.Errorf("%w%s", fail.Say(msg.SaveErrorRebaseStuck(c.Root)), fail.Failure("", a.Stderr, "git"))
				}
				return res, fail.Say(msg.SaveErrorDiverged(c.Root))
			}
			res.Pulled = st.Behind
		}
	}

	if !o.Push || res.Waiting || !gitx.HasCommits(c, c.Root) {
		return res, nil
	}
	if st, err = gitx.Status(c, c.Root); err != nil {
		return res, err
	}
	switch {
	case st.Upstream != "" && st.Ahead > 0:
		if r := gitx.RunNet(c, c.Root, proc.Options{}, "push", "--quiet"); r.Code != 0 {
			return res, pushFailed(r)
		}
		res.Pushed = st.Ahead
	case st.Upstream == "" && st.Branch != "" && gitx.Run(c, c.Root, "remote", "get-url", "origin").Code == 0:
		if r := gitx.RunNet(c, c.Root, proc.Options{}, "push", "--quiet", "-u", "origin", st.Branch); r.Code != 0 {
			return res, pushFailed(r)
		}
		res.Pushed = 1
	}
	return res, nil
}

func pushFailed(r proc.Result) error {
	return fmt.Errorf("%w%s", fail.Say(msg.SaveErrorPush()), fail.Failure("", r.Stderr, ""))
}
