package guard

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/gitx"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// gitlinkMode is the mode of a submodule commit, which has no blob to size.
const gitlinkMode = "160000"

// stagedSizes returns the size of each staged blob, by path, in the order
// of changes. Gitlinks are skipped.
func stagedSizes(c *app.Context, changes []gitx.RawChange) ([]string, map[string]int64, error) {
	var paths, ids []string
	for _, ch := range changes {
		if ch.NewMode == gitlinkMode || ch.NewID == "" {
			continue
		}
		paths = append(paths, ch.Path)
		ids = append(ids, ch.NewID)
	}
	objs, err := gitx.CatFileCheck(c, c.Root, ids)
	if err != nil {
		return nil, nil, err
	}
	sizes := make(map[string]int64, len(objs))
	var sized []string
	for i, o := range objs {
		if o.Missing {
			continue
		}
		sized = append(sized, paths[i])
		sizes[paths[i]] = o.Size
	}
	return sized, sizes, nil
}

// oversized lists a problem for every staged file over limits.maxFileMB.
func oversized(c *app.Context, changes []gitx.RawChange) ([]problem, error) {
	paths, sizes, err := stagedSizes(c, changes)
	if err != nil {
		return nil, err
	}
	limitMB := c.Config.Limits.MaxFileMB
	limit := strconv.FormatFloat(limitMB, 'f', -1, 64)
	var problems []problem
	for _, p := range paths {
		size := sizes[p]
		if float64(size) <= limitMB*1024*1024 {
			continue
		}
		problems = append(problems, problem{text: voice.Record(c, msg.GuardTooBig(p, overSize(c, size, limitMB), limit))})
	}
	return problems, nil
}

// overSize writes size, a size over limitMB, as format.size does, unless
// that would round to the limit or below it ("1.0 MB (limit 1 MB)"): then
// in MB with two decimals, rounded half up, or rounded up when even that
// is not above the limit, so the size never reads as the limit itself.
func overSize(c *app.Context, size int64, limitMB float64) string {
	human := voice.Record(c, ui.HumanSize(size))
	value, unit, _ := strings.Cut(human, " ")
	if shown, err := strconv.ParseFloat(value, 64); err != nil || unit != "MB" || shown > limitMB {
		return human
	}
	const mb = 1 << 20
	hundredths := (size*100 + mb/2) / mb
	if float64(hundredths) <= limitMB*100 {
		hundredths = (size*100 + mb - 1) / mb
	}
	whole := strconv.FormatInt(hundredths/100, 10)
	frac := strconv.FormatInt(hundredths%100+100, 10)[1:]
	return voice.Record(c, msg.FormatSize(whole+"."+frac, unit))
}

// gitFailed is the error "git <what> failed." with what git said.
func gitFailed(r proc.Result, what string) error {
	said := strings.TrimSpace(r.Stderr)
	if said == "" {
		said = r.Stdout
	}
	return fmt.Errorf("%w%s", fail.Say(msg.GitErrorFailed(what)), fail.Failure("", said, "git"))
}
