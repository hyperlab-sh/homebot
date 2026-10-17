// Package cleanup finds the backups `homebot link --backup` left next to
// links, named <name>.homebot-backup-<time>, and goes through them one at a
// time: it shows how each differs from what is linked there now and asks
// whether to delete it, keep it, copy it into the repo file, or merge some of
// its lines into the repo file. fzf shows the diff beside the choices;
// without fzf the diff is printed and a select asks.
package cleanup

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/linker"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// Backup is a file or folder the linker set aside to make room for a link.
type Backup struct {
	Path   string // the backup on disk
	Target string // linker.BackupOf(Path)
	// IsDir is true for a folder, or a link to one: a link that was in the
	// way is moved aside as it is.
	IsDir bool
}

// FindBackups lists the backups under the linked trees, planning links itself.
func FindBackups(c *app.Context) ([]Backup, error) {
	plan, err := linker.PlanLinks(c)
	if err != nil {
		return nil, err
	}
	return FindBackupsIn(c, plan)
}

// FindBackupsIn lists the backups for an already computed plan, so the inbox
// can reuse the one it has. It looks in $HOME and in every folder under it
// that holds, or is above, something homebot links or would link.
func FindBackupsIn(c *app.Context, plan linker.Plan) ([]Backup, error) {
	recorded, err := linker.RecordedLinks(c)
	if err != nil {
		return nil, err
	}
	targets := make([]string, 0, len(plan)+len(recorded))
	for _, op := range plan {
		targets = append(targets, op.Target)
	}
	targets = append(targets, recorded...)
	dirs := map[string]bool{c.Home: true}
	for _, target := range targets {
		for d := filepath.Dir(target); app.IsInside(d, c.Home) && d != c.Home && !dirs[d]; d = filepath.Dir(d) {
			dirs[d] = true
		}
	}
	var found []Backup
	for _, dir := range slices.Sorted(maps.Keys(dirs)) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // a folder the links would create
		}
		for _, e := range entries {
			name, ok := linker.BackupOf(e.Name())
			if !ok {
				continue
			}
			path := filepath.Join(dir, e.Name())
			found = append(found, Backup{Path: path, Target: filepath.Join(dir, name), IsDir: fsx.IsDir(path)})
		}
	}
	return found, nil
}

// realPath resolves every link in path; ok is false when it cannot.
func realPath(path string) (string, bool) {
	real, err := filepath.EvalSymlinks(path)
	return real, err == nil
}

// realRoot is the home repo with its links resolved, or as it is.
func realRoot(c *app.Context) string {
	if real, ok := realPath(c.Root); ok {
		return real
	}
	return c.Root
}

// realHome is $HOME with its links resolved, or as it is.
func realHome(c *app.Context) string {
	if real, ok := realPath(c.Home); ok {
		return real
	}
	return c.Home
}

// tildify is path with $HOME as ~, whether path names $HOME as the
// Context does or with its links resolved.
func tildify(c *app.Context, path string) string {
	if short := ui.Tildify(path, c.Home); short != path {
		return short
	}
	return ui.Tildify(path, realHome(c))
}

// linkShown is where the link at path points, as the user should read it:
// the destination the link names (one hop, made absolute against the link's
// folder and cleaned), when that is the same file as real (the link fully
// resolved); real otherwise, as when "../x" climbs out of a folder that is
// itself a link. A link to a link shows the first hop, as the user wrote it;
// the TypeScript build showed the fully resolved path.
func linkShown(path, real string) string {
	dest, err := linker.ReadlinkAbs(path)
	if err != nil {
		return real
	}
	if sameFile(dest, real) {
		return dest
	}
	return real
}

// sameFile reports whether a and b reach the same file.
func sameFile(a, b string) bool {
	sa, err := os.Stat(a)
	if err != nil {
		return false
	}
	sb, err := os.Stat(b)
	return err == nil && os.SameFile(sa, sb)
}

// repoFileFor is the repo file the backup would be copied into: the target's
// link destination, when that is a file in the repo. Empty when there is none.
func repoFileFor(c *app.Context, b Backup) string {
	if b.IsDir {
		return ""
	}
	real, ok := realPath(b.Target)
	if !ok || !app.IsInside(real, realRoot(c)) {
		return ""
	}
	if st, err := os.Lstat(real); err != nil || !st.Mode().IsRegular() {
		return ""
	}
	return real
}

// repoRel is dest relative to the home repo, as the success lines show it.
func repoRel(c *app.Context, dest string) string {
	if rel, err := filepath.Rel(realRoot(c), dest); err == nil {
		return rel
	}
	return dest
}

// backupDiff is how the backup differs from what is at its place now; ""
// when they are the same. "-" lines are only in the backup. With color, git
// colors it.
func backupDiff(c *app.Context, b Backup, color bool) string {
	now, ok := realPath(b.Target)
	shown := ui.Tildify(b.Target, c.Home)
	if !ok {
		return voice.Plain(c, msg.CleanupDiffOnlyCopy(shown)) + "\n"
	}
	was, wasOK := realPath(b.Path)
	link := ""
	if st, err := os.Lstat(b.Path); err == nil && st.Mode()&fs.ModeSymlink != 0 {
		dest := voice.Plain(c, msg.CleanupDiffGone())
		if wasOK {
			dest = tildify(c, linkShown(b.Path, was))
		}
		link = voice.Plain(c, msg.CleanupDiffBackupIsLink(dest)) + "\n"
	}
	if !wasOK {
		return link
	}
	if nowIsDir := fsx.IsDir(now); nowIsDir != b.IsDir {
		return link + voice.Plain(c, msg.CleanupDiffKindMismatch(kindWord(c, b.IsDir), shown, kindWord(c, nowIsDir))) + "\n"
	}
	// Paths under $HOME read as ~/... in the diff's header. Each side is
	// named the way its link names it (a repo behind a link of its own, ~/.home
	// on another volume, still reads as ~/.home/...; so does a backup that is a
	// link into a folder on another volume), and when that is not under $HOME,
	// by its links resolved, which may be (HOMEBOT_ROOT=/srv/dotfiles where
	// /srv/dotfiles links to ~/dotfiles). Both are paths git reads through,
	// never links themselves, or the diff would compare link texts.
	names := []string{was, now}
	var prefixes []string
	wasRel, wasIn := headerName(c, b.Path, was)
	nowRel, nowIn := headerName(c, b.Target, now)
	if wasIn && nowIn {
		names = []string{wasRel, nowRel}
		prefixes = []string{"--src-prefix=~/", "--dst-prefix=~/"}
	}
	colorFlag := "--no-color"
	if color {
		colorFlag = "--color=always"
	}
	argv := []string{"git", "--no-pager", "diff", "--no-index", "--no-ext-diff", colorFlag}
	argv = append(append(append(argv, prefixes...), "--"), names...)
	r := proc.Run(c, argv, proc.Options{Dir: c.Home})
	if r.Code == 0 {
		return link
	}
	if r.Stdout != "" {
		return link + r.Stdout
	}
	return link + r.Stderr
}

// headerName is the file at path, which resolves to real, relative to $HOME
// for the diff's header: as the link at path names it when that is under
// $HOME, else as real is. ok is false when neither is.
func headerName(c *app.Context, path, real string) (rel string, ok bool) {
	if rel, ok := homeRel(c, linkNamed(path, real)); ok {
		return rel, true
	}
	return homeRel(c, real)
}

// linkNamed is the file at path as the link at path names it, when that
// path is a plain file or folder reaching the same file as real (path
// fully resolved); real otherwise, also when path is not a link.
func linkNamed(path, real string) string {
	named := linkShown(path, real)
	if named == real {
		return real
	}
	if st, err := os.Lstat(named); err != nil || st.Mode()&fs.ModeSymlink != 0 {
		return real
	}
	return named
}

// homeRel is path relative to $HOME, whether path is under $HOME as the
// Context names it or under its links resolved; ok is false when it is
// under neither.
func homeRel(c *app.Context, path string) (rel string, ok bool) {
	for _, home := range []string{c.Home, realHome(c)} {
		if app.IsInside(path, home) {
			return relTo(home, path), true
		}
	}
	return path, false
}

func kindWord(c *app.Context, isDir bool) string {
	if isDir {
		return voice.Plain(c, msg.CleanupWordFolder())
	}
	return voice.Plain(c, msg.CleanupWordFile())
}

// relTo is path relative to dir; path is inside dir.
func relTo(dir, path string) string {
	if rel, err := filepath.Rel(dir, path); err == nil {
		return rel
	}
	return path
}
