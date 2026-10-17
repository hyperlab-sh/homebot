package selfname

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/proc"
)

// ownNames are homebot's names that need no registry: Check refuses them
// even when the caller's Taken.Reserved leaves them out.
var ownNames = []string{"homebot", "help", "version", "completion", "man"}

// systemDirs are checked whatever PATH says, because the helper's PATH and
// the shell's can differ (D3).
var systemDirs = []string{"/usr/local/bin", "/opt/homebrew/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"}

// Check says whether name may be the command on this machine (D3), with
// the lists only the caller can build in taken. The checks run in D3's
// order, and the first that refuses gives the reason. It changes nothing.
//
// Its lookups of name are never logged to the test program log: the name
// is no program homebot runs.
func Check(c *app.Context, name string, taken Taken) Verdict {
	if v, refused := checkWords(name, taken.Reserved); refused {
		return v
	}
	if !filepath.IsAbs(c.Home) {
		return Verdict{Reason: NoBin, Path: binDir(c)}
	}
	bin := binDir(c)
	for _, dir := range searchDirs(c.Env) {
		if dir == bin || sameFile(dir, bin) {
			// What ~/.local/bin holds is the foreign-file check's.
			continue
		}
		if p := filepath.Join(dir, name); exists(p) {
			return Verdict{Reason: OnPath, Path: p}
		}
	}
	if file, ok := taken.Planned[name]; ok {
		return Verdict{Reason: InRepo, Path: file}
	}
	v := Verdict{Existing: Absent}
	if spelled, ok := onDisk(bin, name); ok {
		rec, st := readRecord(c)
		switch {
		case spelled != name:
			if spelled == "" {
				spelled = name
			}
			return Verdict{Reason: Foreign, Path: filepath.Join(bin, spelled)}
		case ours(c, rec, st == goodRecord, name):
			v.Existing = Ours
		default:
			if _, ok := pointsAtHomebot(c, linkPath(c, name)); !ok {
				return Verdict{Reason: Foreign, Path: linkPath(c, name)}
			}
			v.Existing = Adoptable
		}
	}
	if p, ok := syncedBin(c); ok {
		return Verdict{Reason: SyncedBin, Path: p}
	}
	v.InWorkTree = inWorkTree(c, bin)
	return v
}

// checkWords runs the checks that need no disk: syntax, homebot's names,
// the shells' words and the programs homebot runs.
func checkWords(name string, reserved []string) (Verdict, bool) {
	switch {
	case !validName(name):
		return Verdict{Reason: Syntax}, true
	case slices.Contains(ownNames, name) || slices.Contains(reserved, name):
		return Verdict{Reason: Reserved}, true
	case slices.Contains(ShellWords, name):
		return Verdict{Reason: Shell}, true
	case slices.Contains(ToolNames, name):
		return Verdict{Reason: Tool}, true
	}
	return Verdict{}, false
}

// searchDirs are the absolute folders of PATH, then the system folders,
// each once. Relative entries are skipped, as proc's lookup skips them.
func searchDirs(env app.Env) []string {
	var dirs []string
	for _, d := range append(filepath.SplitList(env.Get("PATH")), systemDirs...) {
		if !filepath.IsAbs(d) {
			continue
		}
		if d = filepath.Clean(d); !slices.Contains(dirs, d) {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// firstOnPath is what a shell runs for name: the first executable regular
// file by that name in PATH's absolute folders, as proc.LookPath finds it,
// without the program log.
func firstOnPath(env app.Env, name string) (string, bool) {
	for _, dir := range filepath.SplitList(env.Get("PATH")) {
		if !filepath.IsAbs(dir) {
			continue
		}
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0 {
			return p, true
		}
	}
	return "", false
}

// gitRepoVars point git at a repository other than the one around the
// folder it runs in; Check's work-tree question drops them, so a hook or a
// script that exported them can't change the answer.
var gitRepoVars = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR",
	"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_PREFIX",
	"GIT_NAMESPACE",
}

// inWorkTree reports whether bin, or the nearest folder above it that
// exists, is inside a git work tree (D3's warning). No git, or a git that
// fails, is no warning.
func inWorkTree(c *app.Context, bin string) bool {
	dir := bin
	for {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
	gc := *c
	gc.Env = c.Env.Without(gitRepoVars...)
	r := proc.Run(&gc, []string{"git", "rev-parse", "--is-inside-work-tree"}, proc.Options{Dir: dir})
	return r.Code == 0 && strings.TrimSpace(r.Stdout) == "true"
}
