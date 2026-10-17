// Package pack moves a file or folder from $HOME into the home repo and
// links it back: homebot pack ~/.config/foo.
package pack

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/linker"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/secrets"
	"github.com/hyperlab-sh/homebot/internal/ui"
)

// Options says where Pack puts the path.
type Options struct {
	Layer   string // "", "common", "linux", "macos"; "" = common
	Private bool
}

// Result is where a packed path came from and where it went.
type Result struct {
	From   string // absolute source path
	To     string // absolute path in the repo
	Sealed bool
	// SealedCount is how many secrets the seal encrypted: every new or
	// changed one under private/, not only those of the packed path.
	SealedCount int
}

// Pack moves path into the repo, links it back to where it was, and seals
// it when o.Private is set. A relative path is taken from c.Cwd. The repo
// must be located (c.LocateRoot) first.
//
// A layer this machine does not link is refused before anything moves, since
// homebot link could never link the path back. With o.Private the plaintext
// lives only under private/<layer>/, which the repo ignores but for .age
// files and a few names (README.md, .gitignore, .gitkeep, recipients.txt):
// a path holding such a name, or a .git folder, is refused before anything
// moves, because git would stage it. Without o.Private a path holding a .git
// at any depth is refused too (D12): git would stage a gitlink, and the
// files would never reach the remote. With o.Private, private/.gitignore is
// written first when it is missing. After the move a failure leaves the
// plaintext where it is, under private/, and the Result holds From and To;
// the seal comes after the link (and runs even when the link fails), so a
// failed seal leaves the path working from $HOME.
//
// When the home is a git repo, what was moved into autolink/ is staged, so
// homebot save commits it. A private path is not: only its .age file is
// committed, and save stages private/ itself.
func Pack(c *app.Context, path string, o Options) (Result, error) {
	return packWith(c, path, o, os.Rename)
}

// packWith is Pack with the rename a move starts with.
func packWith(c *app.Context, path string, o Options, rename renameFunc) (Result, error) {
	layer, err := layerOf(c, o)
	if err != nil {
		return Result{}, err
	}
	if !slices.Contains(c.ActiveLayers(), layer) {
		return Result{}, user(c, msg.PackErrorLayerNotLinked(layer, string(c.OS), strings.Join(c.ActiveLayers(), ", ")))
	}
	abs := absolute(c, path)
	shown := ui.Tildify(abs, c.Home)

	if app.IsInside(abs, c.Root) || app.IsInside(c.Root, abs) {
		return Result{}, user(c, msg.PackErrorIsRepo(shown))
	}
	st, err := fsx.LstatIfExists(abs)
	if err != nil {
		return Result{}, err
	}
	if st == nil {
		return Result{}, user(c, msg.PackErrorMissing(shown))
	}
	if _, owned, err := linker.OwnsLink(c, abs); err != nil {
		return Result{}, err
	} else if owned {
		return Result{}, user(c, msg.PackErrorManaged(shown))
	}
	if st.Mode()&fs.ModeSymlink != 0 {
		return Result{}, user(c, msg.PackErrorSymlink(shown))
	}
	for dir := filepath.Dir(abs); app.IsInside(dir, c.Home) && dir != c.Home; dir = filepath.Dir(dir) {
		if _, owned, err := linker.OwnsLink(c, dir); err != nil {
			return Result{}, err
		} else if owned {
			return Result{}, user(c, msg.PackErrorInsideManaged(shown, ui.Tildify(dir, c.Home)))
		}
	}

	tree := linker.Autolink
	if o.Private {
		tree = linker.Private
		if _, err := os.Stat(c.Layout().Recipients); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				return Result{}, err
			}
			return Result{}, user(c, msg.PackErrorNoKeys())
		}
	}
	to, err := repoPath(c, abs, tree, layer)
	if err != nil {
		return Result{}, err
	}

	if o.Private {
		if bad, err := privateReserved(abs, filepath.Base(to)); err != nil {
			return Result{}, unreadable(c, err)
		} else if bad != "" {
			return Result{}, user(c, msg.PackErrorPrivateReserved(ui.Tildify(bad, c.Home)))
		}
	} else if git, err := nestedGit(abs, filepath.Base(to)); err != nil {
		return Result{}, unreadable(c, err)
	} else if git != "" {
		return Result{}, user(c, msg.PackErrorNestedRepo(ui.Tildify(filepath.Dir(git), c.Home)))
	}

	if o.Private {
		// Before the plaintext lands in private/: without its .gitignore,
		// save would stage it (D14).
		if err := secrets.EnsurePrivateGitignore(c); err != nil {
			return Result{}, err
		}
	}

	if err := move(abs, to, rename); err != nil {
		var kept *sourceKeptError
		if errors.As(err, &kept) {
			shownTo, relErr := filepath.Rel(c.Root, to)
			if relErr != nil {
				shownTo = to
			}
			return Result{From: abs, To: to}, user(c, msg.PackErrorSourceNotRemoved(shown, shownTo, kept.err.Error()))
		}
		return Result{}, err
	}
	res := Result{From: abs, To: to}
	// Link first, then seal: a seal that fails (a key age cannot encrypt to
	// in recipients.txt, another secret it cannot read) must not leave the
	// path missing from $HOME. A link that fails still lets the seal run, so
	// the secret is sealed for the next save all the same.
	linkErr := linkBack(c, tree, abs, shown)
	if o.Private {
		sealed, err := secrets.Seal(c)
		res.SealedCount = len(sealed.Sealed)
		res.Sealed = res.SealedCount > 0
		switch {
		case linkErr != nil && err != nil:
			// The missing link is the more urgent news; the seal's error
			// comes with it.
			return res, errors.Join(linkErr, err)
		case err != nil:
			return res, err
		}
	}
	if linkErr != nil {
		return res, linkErr
	}
	if !o.Private {
		rel, err := filepath.Rel(c.Root, to)
		if err != nil {
			return res, err
		}
		failed := func(stderr string) error {
			return fmt.Errorf("%w%s", fail.Say(msg.GitErrorFailed("add "+rel)), fail.Failure("", stderr, "git"))
		}
		if err := Stage(c, rel, failed); err != nil {
			return res, err
		}
	}
	return res, nil
}

// linkBack links tree back into $HOME, and checks that abs, shown as
// shown, is there again.
func linkBack(c *app.Context, tree linker.Tree, abs, shown string) error {
	plan, err := linker.PlanLinks(c, tree)
	if err != nil {
		return err
	}
	if err := linker.ApplyLinks(c, plan, linker.ApplyOptions{Safe: true}); err != nil {
		return err
	}
	if st, err := fsx.LstatIfExists(abs); err != nil {
		return err
	} else if st == nil {
		return user(c, msg.PackErrorLinkFailed(shown))
	}
	return nil
}

// RepoPathFor is where Pack would put path in the repo. It refuses what Pack
// refuses about the name and the repo's layout, but does not look at the
// path itself.
func RepoPathFor(c *app.Context, path string, o Options) (string, error) {
	layer, err := layerOf(c, o)
	if err != nil {
		return "", err
	}
	tree := linker.Autolink
	if o.Private {
		tree = linker.Private
	}
	return repoPath(c, absolute(c, path), tree, layer)
}

func user(_ *app.Context, t msg.Text) error { return fail.Say(t) }

// absolute resolves a path the user typed: ~ is the home, a relative path
// starts at the working directory of c, never the process's.
func absolute(c *app.Context, path string) string {
	path = ui.ExpandHome(path, c.Home)
	if !filepath.IsAbs(path) {
		path = filepath.Join(c.Cwd, path)
	}
	return filepath.Clean(path)
}

// layerOf is o.Layer, common when empty, if it names a layer.
func layerOf(c *app.Context, o Options) (string, error) {
	layer := o.Layer
	if layer == "" {
		layer = "common"
	}
	if !slices.Contains(app.Layers, layer) {
		return "", user(c, msg.PackErrorUnknownLayer(layer, strings.Join(app.Layers, ", ")))
	}
	return layer, nil
}

// repoPath is where a $HOME path goes in the repo. Parts the repo already
// has keep their spelling. New folders above the item get () so they stay
// real folders (the app keeps its other files out of the repo), except
// built-ins like .config. The item itself is plain, so a packed folder can
// become one link.
//
//	~/.config/nvim                  -> autolink/common/config/nvim
//	~/.claude/statusline-command.sh -> autolink/common/(claude)/statusline-command.sh
func repoPath(c *app.Context, absHome string, tree linker.Tree, layer string) (string, error) {
	rel, err := filepath.Rel(c.Home, absHome)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", user(c, msg.PackErrorOutsideHome(absHome))
	}
	parts := strings.Split(rel, "/")
	if i := slices.IndexFunc(parts, linker.LooksMarked); i >= 0 {
		return "", user(c, msg.PackErrorParens(parts[i]))
	}
	top := parts[0]
	switch {
	case slices.Contains(linker.NoDot, top):
		// Library stays Library.
	case len(top) >= 2 && top[0] == '.' && top[1] != '.' && !strings.HasPrefix(top, ".dot-"):
		parts[0] = top[1:]
	default:
		return "", user(c, msg.PackErrorTopLevel(strings.Join(linker.NoDot, ", "), top))
	}

	// Every tree and layer, so the spelling matches wherever the folder
	// already exists.
	var roots []string
	for _, t := range linker.Trees {
		for _, l := range app.Layers {
			roots = append(roots, filepath.Join(linker.TreeDir(c, t), l))
		}
	}
	exists := func(spelled []string, name string) (string, bool) {
		for _, r := range roots {
			p := filepath.Join(r, filepath.Join(spelled...), name)
			if _, err := os.Stat(p); err == nil {
				return p, true
			}
		}
		return "", false
	}

	var spelled []string
	plainParent := ""
	for i, name := range parts {
		homeRel := strings.Join(strings.Split(rel, "/")[:i+1], "/")
		isItem := i == len(parts)-1
		existing, where := "", ""
		for _, n := range []string{"(" + name + ")", name} {
			if p, ok := exists(spelled, n); ok {
				existing, where = n, p
				break
			}
		}
		if existing != "" && isItem {
			shown, err := filepath.Rel(c.Root, where)
			if err != nil {
				shown = where
			}
			return "", user(c, msg.PackErrorAlreadyInRepo(shown))
		}
		builtIn := slices.Contains(linker.AlwaysReal, homeRel)
		switch {
		case existing != "":
			spelled = append(spelled, existing)
			if !builtIn && !linker.LooksMarked(existing) && plainParent == "" {
				plainParent = filepath.Join(spelled...)
			}
		case isItem || builtIn:
			spelled = append(spelled, name)
		case plainParent != "":
			return "", user(c, msg.PackErrorPlainParent(plainParent, filepath.Base(plainParent)))
		default:
			spelled = append(spelled, "("+name+")")
		}
	}
	return filepath.Join(append([]string{linker.TreeDir(c, tree), layer}, spelled...)...), nil
}

// privateReserved returns the first path at or below root whose name private/
// does not ignore: README.md, .gitignore, .gitkeep and recipients.txt files,
// *.age files, and .git entries. Git would stage them, and a .gitignore among
// them can re-include anything. root is judged by repoName, the name it has
// in the repo (a leading dot is dropped there: ~/.README.md lands as
// README.md, ~/.gitignore as gitignore); everything below it keeps its own
// name. Names are compared without regard to case on every OS, since git
// does so on a case-insensitive filesystem (macOS). It looks at names only,
// never into links.
func privateReserved(root, repoName string) (string, error) {
	return firstNamed(root, repoName, privateReservedName)
}

// nestedGit returns the first .git entry, folder or file, below root: a
// repository of its own, which git add would stage as a gitlink, a pointer
// whose files never reach the remote (D12). root is judged by repoName, and
// names are compared as privateReserved compares them.
func nestedGit(root, repoName string) (string, error) {
	return firstNamed(root, repoName, func(name string) bool { return strings.EqualFold(name, ".git") })
}

// firstNamed returns the first path at or below root whose name is bad,
// root itself judged by repoName. It looks at names only, never into links.
// A folder it cannot read is an *unreadableError: what it holds is unknown.
func firstNamed(root, repoName string, bad func(name string) bool) (string, error) {
	var found string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				return &unreadableError{path: p, err: err}
			}
			return err
		}
		name := d.Name()
		if p == root {
			name = repoName
		}
		if bad(name) {
			found = p
			return filepath.SkipAll
		}
		return nil
	})
	return found, err
}

// unreadableError is a folder firstNamed could not look into.
type unreadableError struct {
	path string
	err  error
}

func (e *unreadableError) Error() string { return e.err.Error() }
func (e *unreadableError) Unwrap() error { return e.err }

// unreadable says an *unreadableError from the walks before a move as
// pack.error.unreadable; other errors pass through.
func unreadable(c *app.Context, err error) error {
	var u *unreadableError
	if errors.As(err, &u) {
		return user(c, msg.PackErrorUnreadable(ui.Tildify(u.path, c.Home)))
	}
	return err
}

// privateReservedName says whether git would stage a file of this name under
// private/ (see privateReserved).
func privateReservedName(name string) bool {
	for _, reserved := range append([]string{".git"}, secrets.PrivateDocs...) {
		if strings.EqualFold(name, reserved) {
			return true
		}
	}
	return strings.HasSuffix(strings.ToLower(name), ".age")
}
