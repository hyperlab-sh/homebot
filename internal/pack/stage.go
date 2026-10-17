package pack

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/gitx"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/ui"
)

// Stage runs git add on rel, a path relative to the repo root, when the home
// is a git repo. Pack stages what it moved into autolink/ through it, and
// triage what the person chose to track in the repo.
//
// A path the repo's .gitignore files exclude is left alone: naming it to git
// add would fail, and ignoring it was the person's choice. Pathspecs are
// literal, so a * ? or [ in a name stages that name only. When git add
// fails, failed turns git's stderr into the error returned.
//
// A nested repository is never left staged (D12): git add would stage it as
// a gitlink, a pointer whose files never reach the remote. A gitlink this
// git add newly staged at or below rel is unstaged again and
// pack.error.nested_repo returned; one HEAD already holds is left alone.
// One that replaced a committed file gets HEAD's entry back.
func Stage(c *app.Context, rel string, failed func(stderr string) error) error {
	if !gitx.IsRepo(c, c.Root) {
		return nil
	}
	// check-ignore takes paths, not pathspecs, and refuses the literal flag.
	if gitx.Run(c, c.Root, "check-ignore", "-q", "--", rel).Code == 0 {
		return nil
	}
	if r := literal(c, "add", "--", rel); r.Code != 0 {
		return failed(r.Stderr)
	}
	added, replaced, err := gitlinks(c, rel)
	if err != nil || len(added)+len(replaced) == 0 {
		return err
	}
	// -f: the gitlink was just staged, so it differs from HEAD; --cached
	// keeps the working tree as it is.
	if len(added) > 0 {
		args := append([]string{"rm", "--cached", "-f", "-q", "--"}, added...)
		if r := literal(c, args...); r.Code != 0 {
			return fmt.Errorf("%w%s", fail.Say(msg.GitErrorFailed("rm --cached "+strings.Join(added, " "))), fail.Failure("", r.Stderr, "git"))
		}
	}
	// A gitlink that replaced a committed file or link gets HEAD's entry
	// back, rather than a staged deletion.
	if len(replaced) > 0 {
		args := append([]string{"reset", "-q", "--"}, replaced...)
		if r := literal(c, args...); r.Code != 0 {
			return fmt.Errorf("%w%s", fail.Say(msg.GitErrorFailed("reset "+strings.Join(replaced, " "))), fail.Failure("", r.Stderr, "git"))
		}
	}
	links := append(added, replaced...)
	return fail.Say(msg.PackErrorNestedRepo(ui.Tildify(filepath.Join(c.Root, links[0]), c.Home)))
}

// gitlinks lists the paths at or below rel that git add just staged as
// gitlinks (mode 160000): nested repositories added as pointers. added are
// new to the index; replaced took the place of a file or link HEAD holds
// (a type change). A gitlink HEAD already holds is in neither: it was
// committed on purpose.
func gitlinks(c *app.Context, rel string) (added, replaced []string, err error) {
	r := literal(c, "ls-files", "-s", "-z", "--", rel)
	if r.Code != 0 {
		return nil, nil, fmt.Errorf("%w%s", fail.Say(msg.GitErrorFailed("ls-files "+rel)), fail.Failure("", r.Stderr, "git"))
	}
	var links []string
	for _, entry := range strings.Split(r.Stdout, "\x00") {
		// "<mode> <object> <stage>\t<path>"
		info, path, ok := strings.Cut(entry, "\t")
		if ok && strings.HasPrefix(info, "160000 ") {
			links = append(links, path)
		}
	}
	if len(links) == 0 {
		return nil, nil, nil
	}
	// Against HEAD (against nothing before the first commit). --relative
	// gives paths from the home, as ls-files does, also when the home sits
	// below the top of the git repository.
	r = literal(c, "diff", "--cached", "--relative", "--name-status", "--diff-filter=AT", "--no-renames", "-z", "--", rel)
	if r.Code != 0 {
		return nil, nil, fmt.Errorf("%w%s", fail.Say(msg.GitErrorFailed("diff --cached "+rel)), fail.Failure("", r.Stderr, "git"))
	}
	// "<status>\0<path>\0" per change.
	fields := strings.Split(r.Stdout, "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		status, path := fields[i], fields[i+1]
		if !slices.Contains(links, path) {
			continue
		}
		if status == "T" {
			replaced = append(replaced, path)
		} else {
			added = append(added, path)
		}
	}
	return added, replaced, nil
}

// literal runs git in the repo with pathspec magic off: every path names
// itself, never a glob.
func literal(c *app.Context, args ...string) proc.Result {
	return gitx.Run(c, c.Root, append([]string{"--literal-pathspecs"}, args...)...)
}
