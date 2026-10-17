// Package triage finds the things in $HOME and in the home repo that nobody
// has decided about yet, asks about each one (track, ignore or snooze), and
// remembers the decisions in decisions.yml and snooze.json. The inbox and
// inventory read Candidates. It ports src/triage.ts.
package triage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/gitx"
	"github.com/hyperlab-sh/homebot/internal/helper"
	"github.com/hyperlab-sh/homebot/internal/linker"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/ui"
)

// Kind says where a candidate lives.
type Kind string

const (
	Home Kind = "home" // in $HOME; Key is "~/…"
	Repo Kind = "repo" // untracked in the repo; Key is repo-relative
)

// Candidate is one thing triage asks about.
type Candidate struct {
	Key  string // shown and remembered
	Abs  string
	Kind Kind
}

// machineLocal matches *.local names: they belong to this machine only.
// ~/.local itself is not one.
var machineLocal = regexp.MustCompile(`.\.local$`)

// Candidates lists what triage would ask about now (c.Now decides snoozes).
//
// In $HOME it lists the entries of each watched pattern's folder, in name
// order. Patterns are not globs: "~/.config/*" lists the folder's names that
// do not start with a dot and "~/.*" the ones that do, as the TypeScript
// did. Left out are the repo and anything holding it, the watched folders
// themselves, backups, *.local files, what decisions.yml ignores, what is
// snoozed, what homebot links or holds links under, and a folder that holds
// only homebot's own helper. In the repo it lists untracked files that are
// not snoozed, except under private/, which save stages itself.
func Candidates(c *app.Context) ([]Candidate, error) {
	ignores, err := LoadIgnores(c)
	if err != nil {
		return nil, err
	}
	snoozes, err := state.LoadSnoozes(c)
	if err != nil {
		return nil, err
	}
	now := c.Now().UnixMilli()
	snoozed := func(key string) bool {
		until, ok := snoozes.Until.Get(key)
		return ok && until > now
	}
	links, err := linker.RecordedLinks(c)
	if err != nil {
		return nil, err
	}
	cfg := c.Config
	if cfg == nil {
		cfg = config.Default()
	}
	var watched []string
	bases := map[string]bool{}
	for _, w := range cfg.Triage.Watch {
		p := ui.ExpandHome(w, c.Home)
		watched = append(watched, p)
		bases[filepath.Dir(p)] = true
	}
	helperFiles := helper.Files(c)

	var out []Candidate
	seen := map[string]bool{}
	for _, pattern := range watched {
		base := filepath.Dir(pattern)
		dotted := filepath.Base(pattern) == ".*"
		names, err := readNames(base)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			if strings.HasPrefix(name, ".") != dotted {
				continue
			}
			if linker.IsBackup(name) || machineLocal.MatchString(name) {
				continue
			}
			abs := filepath.Join(base, name)
			key := ui.Tildify(abs, c.Home)
			if seen[abs] || bases[abs] {
				continue
			}
			seen[abs] = true
			if app.IsInside(abs, c.Root) || app.IsInside(c.Root, abs) {
				continue
			}
			if isIgnored(key, ignores) || snoozed(key) {
				continue
			}
			managed, err := isManaged(c, abs, links, helperFiles)
			if err != nil {
				return nil, err
			}
			if !managed {
				out = append(out, Candidate{Key: key, Abs: abs, Kind: Home})
			}
		}
	}

	if !gitx.IsRepo(c, c.Root) {
		return out, nil
	}
	untracked, err := gitx.UntrackedFiles(c, c.Root, ".")
	if err != nil {
		return nil, err
	}
	for _, rel := range untracked {
		if rel == "private" || strings.HasPrefix(rel, "private/") || snoozed(rel) {
			continue
		}
		out = append(out, Candidate{Key: rel, Abs: filepath.Join(c.Root, rel), Kind: Repo})
	}
	return out, nil
}

// readNames lists dir's entry names in byte order. A folder that is not
// there, or a file where the folder would be, has none.
func readNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names, nil
}

// isManaged says whether abs is homebot's already: a link into the repo, a
// folder some recorded link lies under, or a folder holding only the
// helper's files.
func isManaged(c *app.Context, abs string, links, helperFiles []string) (bool, error) {
	if _, owned, err := linker.OwnsLink(c, abs); err != nil || owned {
		return owned, err
	}
	if slices.ContainsFunc(links, func(l string) bool { return app.IsInside(l, abs) }) {
		return true, nil
	}
	st, err := os.Lstat(abs)
	if err != nil || !st.IsDir() {
		return false, nil
	}
	return holdsOnly(abs, helperFiles), nil
}

// holdsOnly says whether dir holds something and every file in it, at any
// depth, is in allowed. It stops at the first stranger. A folder it cannot
// read counts as holding a stranger.
func holdsOnly(dir string, allowed []string) bool {
	found := false
	var walk func(d string) bool
	walk = func(d string) bool {
		entries, err := os.ReadDir(d)
		if err != nil {
			return false
		}
		for _, e := range entries {
			abs := filepath.Join(d, e.Name())
			switch {
			case e.IsDir():
				if !walk(abs) {
					return false
				}
			case slices.Contains(allowed, abs):
				found = true
			default:
				return false
			}
		}
		return true
	}
	return walk(dir) && found
}
