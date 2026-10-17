package gitx

import (
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
)

// RawChange is one record of git diff --raw -z.
type RawChange struct {
	// Status is the change letter: A, C, D, M, R, T, U or X.
	Status string
	// Score is the similarity of a rename or copy, such as "100"; else "".
	Score string
	// Path is the new path for a rename or copy.
	Path     string
	OrigPath string
	// OldMode and NewMode are the octal file modes before and after, such
	// as "100644"; "000000" where there is no file, and "160000" for a
	// gitlink (a submodule commit, which has no blob).
	OldMode string
	NewMode string
	// OldID and NewID are the object ids before and after; all zeros where
	// there is no object.
	OldID string
	NewID string
}

// StagedChanges lists what is staged: added, copied, modified and renamed
// files, not deletions or type changes.
func StagedChanges(c *app.Context, dir string) ([]RawChange, error) {
	r := Run(c, dir, "diff", "--cached", "--raw", "-z", "--diff-filter=ACMR")
	if r.Code != 0 {
		return nil, failed(r, "diff --cached")
	}
	return ParseRaw(r.Stdout), nil
}

// StagedFiles lists the paths of the staged additions, copies, changes and
// renames; a rename lists the new path.
func StagedFiles(c *app.Context, dir string) ([]string, error) {
	changes, err := StagedChanges(c, dir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, ch := range changes {
		paths = append(paths, ch.Path)
	}
	return paths, nil
}

// ParseRaw parses the output of git diff --raw -z.
func ParseRaw(out string) []RawChange {
	f := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	var changes []RawChange
	for i := 0; i < len(f); i++ {
		// :modeA modeB shaA shaB status
		fields := strings.Fields(f[i])
		if !strings.HasPrefix(f[i], ":") || len(fields) < 5 || i+1 >= len(f) {
			continue
		}
		st := fields[4]
		ch := RawChange{
			Status: st[:1], Score: st[1:],
			OldMode: fields[0][1:], NewMode: fields[1],
			OldID: fields[2], NewID: fields[3],
		}
		i++
		ch.Path = f[i]
		if ch.Status == "R" || ch.Status == "C" {
			if i+1 >= len(f) {
				continue
			}
			ch.OrigPath = ch.Path
			i++
			ch.Path = f[i]
		}
		changes = append(changes, ch)
	}
	return changes
}
