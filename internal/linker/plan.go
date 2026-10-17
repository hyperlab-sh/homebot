package linker

import (
	"bytes"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/layout"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// Tree is a top-level folder of the repo whose layers are linked into $HOME.
type Tree string

// The trees.
const (
	Autolink Tree = layout.Autolink
	Private  Tree = layout.Private
)

// Trees are the trees PlanLinks reads when given none, in the order it reads
// them.
var Trees = []Tree{Private, Autolink}

// TreeDir is where tree lives in the home repo.
func TreeDir(ctx *app.Context, tree Tree) string {
	if tree == Private {
		return ctx.Layout().Private
	}
	return ctx.Layout().Autolink
}

// Kind is what an Op does, or why it does nothing.
type Kind string

// The op kinds.
const (
	// OK is a link that is already right.
	OK Kind = "ok"
	// Create makes a new link.
	Create Kind = "create"
	// Replace repoints a link into the repo that points at the wrong file.
	Replace Kind = "replace"
	// Adopt replaces a file identical to the repo's copy with a link.
	Adopt Kind = "adopt"
	// Unfold turns a folder link into a real folder, so its contents can be
	// linked one by one.
	Unfold Kind = "unfold"
	// Prune removes a link homebot made that the repo no longer provides.
	Prune Kind = "prune"
	// Locked is a sealed secret with no plaintext yet: homebot unlock.
	Locked Kind = "locked"
	// Conflict is something in $HOME in the way. homebot link --backup can
	// move it aside.
	Conflict Kind = "conflict"
	// Invalid is a part of the repo that breaks a layout rule. Nothing under
	// Target is linked until it is fixed.
	Invalid Kind = "invalid"
)

// Op is one thing PlanLinks found for one path in $HOME.
type Op struct {
	Kind Kind
	// Target is the path in $HOME.
	Target string
	// Source is the repo path the link points to. Unfold and Prune have none;
	// a Conflict always has one.
	Source string
	// Was is where the link pointed before: Replace, Unfold and Prune.
	Was string
	// Reason says what is in the way (Conflict) or which rule the repo breaks
	// (Invalid), as neutral English, the same whatever persona speaks. Nothing
	// stores it today; it is set beside ReasonText so a store or log has the
	// neutral form ready without saying the text first.
	Reason string
	// ReasonText is Reason before it was said: the catalog message with its
	// values. A Conflict or an Invalid op always has one. Say it through the
	// voice (voice.Plain, voice.Render, or the say function of voice.SayWith)
	// where it is printed, so a persona may word it. Printing Reason instead
	// shows the English on a terminal too.
	ReasonText msg.Text
	// private is true when the folders ApplyLinks makes for the op hold
	// files of the private tree: they are made 0700, not 0777 less the umask.
	private bool
}

// Plan is what PlanLinks found, in the order ApplyLinks carries it out.
type Plan []Op

// Only returns the ops of the given kinds, in order.
func (p Plan) Only(kinds ...Kind) Plan {
	var out Plan
	for _, op := range p {
		if slices.Contains(kinds, op.Kind) {
			out = append(out, op)
		}
	}
	return out
}

// Pending returns the ops that change $HOME.
func (p Plan) Pending() Plan {
	return p.Only(Create, Replace, Adopt, Unfold, Prune)
}

// Changes returns the ops ApplyLinks carries out with opts, in order: what a
// successful ApplyLinks did.
func (p Plan) Changes(opts ApplyOptions) Plan {
	var out Plan
	for _, op := range p.Pending() {
		if op.Kind == Adopt && opts.Safe {
			continue
		}
		out = append(out, op)
	}
	return out
}

// Summary counts a plan's ops by kind.
type Summary struct {
	OK, Create, Replace, Adopt, Unfold, Prune, Locked, Conflict, Invalid int
}

// Summary counts p's ops by kind.
func (p Plan) Summary() Summary {
	var s Summary
	for _, op := range p {
		switch op.Kind {
		case OK:
			s.OK++
		case Create:
			s.Create++
		case Replace:
			s.Replace++
		case Adopt:
			s.Adopt++
		case Unfold:
			s.Unfold++
		case Prune:
			s.Prune++
		case Locked:
			s.Locked++
		case Conflict:
			s.Conflict++
		case Invalid:
			s.Invalid++
		}
	}
	return s
}

// PendingChanges plans every tree and returns the ops that would change
// $HOME.
func PendingChanges(ctx *app.Context) (Plan, error) {
	p, err := PlanLinks(ctx)
	if err != nil {
		return nil, err
	}
	return p.Pending(), nil
}

// provider is one repo entry that provides a name in $HOME.
type provider struct {
	abs string
	// raw is the name as spelled in the repo, such as (claude).
	raw    string
	tree   Tree
	isDir  bool
	marked bool
}

// PlanLinks works out what linking the given trees (every tree when none)
// into $HOME takes, and which links homebot made before the repo no longer
// provides. It changes nothing. The home repo must be located.
func PlanLinks(ctx *app.Context, trees ...Tree) (Plan, error) {
	if len(trees) == 0 {
		trees = Trees
	}
	var roots []provider
	for _, tree := range trees {
		for _, layer := range ctx.ActiveLayers() {
			abs := filepath.Join(TreeDir(ctx, tree), layer)
			if fsx.IsDir(abs) {
				roots = append(roots, provider{abs: abs, raw: layer, tree: tree, isDir: true, marked: true})
			}
		}
	}
	pl := &planner{ctx: ctx, repo: []string{ctx.Root}}
	if real, err := filepath.EvalSymlinks(ctx.Root); err == nil && real != ctx.Root {
		pl.repo = append(pl.repo, real)
	}
	if real, err := filepath.EvalSymlinks(ctx.Home); err == nil && real != ctx.Home {
		pl.realHome = real
	}
	if err := pl.walk("", roots, false); err != nil {
		return nil, err
	}
	if err := pl.prune(trees); err != nil {
		return nil, err
	}
	return pl.ops, nil
}

// planner collects the ops of one PlanLinks run.
type planner struct {
	ctx *app.Context
	ops Plan
	// repo is the home repo, as given and with symlinks resolved.
	repo []string
	// realHome is $HOME with symlinks resolved, when that differs.
	realHome string
}

// insideRepo reports whether target, a path in $HOME, is the repo or below
// it, as given or with $HOME's symlinks resolved. Linking there would write
// into the repo.
func (pl *planner) insideRepo(target string) bool {
	paths := []string{target}
	if pl.realHome != "" {
		if rest, ok := strings.CutPrefix(target, pl.ctx.Home); ok {
			paths = append(paths, pl.realHome+rest)
		}
	}
	for _, p := range paths {
		for _, r := range pl.repo {
			if app.IsInside(p, r) {
				return true
			}
		}
	}
	return false
}

func (pl *planner) add(op Op) { pl.ops = append(pl.ops, op) }

func (pl *planner) invalid(target, source string, reason msg.Text) {
	pl.add(Op{Kind: Invalid, Target: target, Source: source, Reason: voice.Record(pl.ctx, reason), ReasonText: reason})
}

func (pl *planner) conflict(target, source string, reason msg.Text) {
	pl.add(Op{Kind: Conflict, Target: target, Source: source, Reason: voice.Record(pl.ctx, reason), ReasonText: reason})
}

// shown is a repo path as messages show it: relative to the repo.
func (pl *planner) shown(abs string) string {
	rel, err := filepath.Rel(pl.ctx.Root, abs)
	if err != nil {
		return abs
	}
	return rel
}

func (pl *planner) shownAll(ps []provider, sep string) string {
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = pl.shown(p.abs)
	}
	return strings.Join(names, sep)
}

// prune adds the links made before that the repo no longer provides. Links
// under a broken part of the repo stay until it is fixed, and a tree that is
// not planned is not pruned.
func (pl *planner) prune(trees []Tree) error {
	wanted := map[string]bool{}
	var broken []string
	for _, op := range pl.ops {
		wanted[op.Target] = true
		if op.Kind == Invalid {
			broken = append(broken, op.Target)
		}
	}
	treeDirs := make([]string, len(trees))
	for i, t := range trees {
		treeDirs[i] = TreeDir(pl.ctx, t)
	}
	recorded, err := RecordedLinks(pl.ctx)
	if err != nil {
		return err
	}
	for _, target := range recorded {
		if wanted[target] || slices.ContainsFunc(broken, func(b string) bool { return app.IsInside(target, b) }) {
			continue
		}
		was, owned, err := OwnsLink(pl.ctx, target)
		if err != nil {
			return err
		}
		if owned && slices.ContainsFunc(treeDirs, func(d string) bool { return pointsInside(was, d) }) {
			pl.add(Op{Kind: Prune, Target: target, Was: was})
		}
	}
	return nil
}

// walk plans the names that dirs provide at rel, a slash-separated path below
// $HOME ("" at the top). virtual is true below a folder that does not exist
// yet, where nothing in $HOME is looked at.
func (pl *planner) walk(rel string, dirs []provider, virtual bool) error {
	ctx := pl.ctx
	top := rel == ""
	kids := map[string][]provider{}
	locked := map[string]provider{}
	var lockedOrder []string

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir.abs)
		if err != nil {
			return err
		}
		for _, ent := range entries {
			if skip[ent.Name()] {
				continue
			}
			raw := ent.Name()
			sealed := false
			if dir.tree == Private && strings.HasSuffix(raw, ".age") {
				raw = strings.TrimSuffix(raw, ".age")
				plain, err := fsx.LstatIfExists(filepath.Join(dir.abs, raw))
				if err != nil {
					return err
				}
				if plain != nil {
					continue // the plaintext is there; link that
				}
				sealed = true
			}
			name, ok := ParseName(raw, top)
			if !ok {
				continue
			}
			source := filepath.Join(dir.abs, ent.Name())
			target := filepath.Join(ctx.Home, rel, raw)
			if name.Problem != nil {
				pl.invalid(target, source, *name.Problem)
				continue
			}
			// Every name lands below the folder it is in, so never outside
			// $HOME. ParseName refuses the names that would not; this is the
			// backstop.
			if home := filepath.Join(ctx.Home, rel, name.Home); !landsBelow(home, filepath.Join(ctx.Home, rel)) {
				pl.invalid(target, source, msg.LayoutOutsideFolder(ui.Tildify(home, ctx.Home)))
				continue
			}
			// A name that would land on the repo or in it would link the
			// repo into itself.
			if home := filepath.Join(ctx.Home, rel, name.Home); pl.insideRepo(home) {
				pl.invalid(target, source, msg.LayoutInsideRepo(ui.Tildify(home, ctx.Home)))
				continue
			}
			isDir := !sealed && ent.IsDir()
			if name.Marked && !isDir {
				pl.invalid(target, source, msg.LayoutFileParens(raw, name.Home))
				continue
			}
			p := provider{abs: filepath.Join(dir.abs, raw), raw: raw, tree: dir.tree, isDir: isDir, marked: name.Marked}
			if sealed {
				if _, seen := locked[name.Home]; !seen {
					lockedOrder = append(lockedOrder, name.Home)
				}
				locked[name.Home] = p
			} else {
				kids[name.Home] = append(kids[name.Home], p)
			}
		}
	}

	// Folders made below rel hold private files when the private tree
	// provides rel.
	private := !top && slices.ContainsFunc(dirs, func(p provider) bool { return p.tree == Private })
	for _, name := range slices.Sorted(maps.Keys(kids)) {
		relPath := name
		if !top {
			relPath = rel + "/" + name
		}
		if err := pl.plan(relPath, kids[name], virtual, private); err != nil {
			return err
		}
	}

	for _, name := range lockedOrder {
		if _, provided := kids[name]; provided {
			continue
		}
		relPath := name
		if !top {
			relPath = rel + "/" + name
		}
		pl.add(Op{Kind: Locked, Target: filepath.Join(ctx.Home, relPath), Source: locked[name].abs})
	}
	return nil
}

// plan plans one name in $HOME, at relPath, that providers provide. private
// is true when the folder relPath is in holds private files.
func (pl *planner) plan(relPath string, providers []provider, virtual, private bool) error {
	ctx := pl.ctx
	target := filepath.Join(ctx.Home, relPath)
	source := providers[0].abs
	var files []provider
	for _, p := range providers {
		if !p.isDir {
			files = append(files, p)
		}
	}

	if len(providers) > 1 && len(files) > 0 {
		pl.invalid(target, source, msg.LayoutBothLayers(pl.shownAll(providers, " and ")))
		return nil
	}
	if len(files) == 1 {
		return pl.leaf(target, files[0], virtual, private || files[0].tree == Private)
	}

	// Folders: () or a built-in name keeps it real; mixing spellings is an error.
	builtIn := slices.Contains(AlwaysReal, relPath)
	var marked, plain []provider
	for _, p := range providers {
		if p.marked {
			marked = append(marked, p)
		} else {
			plain = append(plain, p)
		}
	}
	if !builtIn && len(marked) > 0 && len(plain) > 0 {
		pl.invalid(target, source, msg.LayoutSpelling(marked[0].raw, pl.shownAll(marked, ", "), plain[0].raw, pl.shownAll(plain, ", ")))
		return nil
	}
	keepReal := builtIn || len(marked) > 0
	if !keepReal {
		// A plain folder may become a link, so nothing inside may need to stay real.
		abs := make([]string, len(providers))
		for i, p := range providers {
			abs[i] = p.abs
		}
		inner, found, err := findMarked(abs)
		if err != nil {
			return err
		}
		if found {
			pl.invalid(target, source, pl.mustStayReal(providers, inner))
			return nil
		}
	}
	fold := !keepReal && len(providers) == 1 && providers[0].tree == Autolink
	holdsPrivate := slices.ContainsFunc(providers, func(p provider) bool { return p.tree == Private })

	var st fs.FileInfo
	if !virtual {
		var err error
		if st, err = fsx.LstatIfExists(target); err != nil {
			return err
		}
	}
	switch {
	case st == nil:
		if fold {
			pl.add(Op{Kind: Create, Target: target, Source: source, private: private})
			return nil
		}
		return pl.walk(relPath, providers, true)
	case st.Mode()&fs.ModeSymlink != 0:
		dest, err := ReadlinkAbs(target)
		if err != nil {
			return err
		}
		switch {
		case fold && sameTarget(dest, source):
			pl.add(Op{Kind: OK, Target: target, Source: source})
		case pointsInside(dest, ctx.Root) && fold:
			pl.add(Op{Kind: Replace, Target: target, Source: source, Was: dest, private: private})
		case pointsInside(dest, ctx.Root):
			pl.add(Op{Kind: Unfold, Target: target, Was: dest, private: holdsPrivate})
			return pl.walk(relPath, providers, true)
		default:
			pl.conflict(target, source, msg.ConflictSymlinkOutside(dest))
		}
	case st.IsDir():
		return pl.walk(relPath, providers, false)
	default:
		pl.conflict(target, source, msg.ConflictFileVsFolder())
	}
	return nil
}

// mustStayReal says why a plain folder cannot be one: inner, below it, must
// stay real or is a bad name.
func (pl *planner) mustStayReal(providers []provider, inner markedEntry) msg.Text {
	name, _ := ParseName(inner.raw, false)
	switch {
	case name.Problem != nil:
		return msg.LayoutInnerError(pl.shown(inner.abs), voice.Record(pl.ctx, *name.Problem))
	case !inner.isDir:
		return msg.LayoutInnerFileParens(pl.shown(inner.abs), inner.raw, name.Home)
	}
	everyLayer := ""
	if len(providers) > 1 {
		everyLayer = voice.Record(pl.ctx, msg.LayoutMustMarkEveryLayer())
	}
	return msg.LayoutMustMark(providers[0].raw, everyLayer, pl.shown(inner.abs))
}

// leaf plans a file the repo provides at target. private is true when the
// folders made for it hold private files.
func (pl *planner) leaf(target string, p provider, virtual, private bool) error {
	source := p.abs
	var st fs.FileInfo
	if !virtual {
		var err error
		if st, err = fsx.LstatIfExists(target); err != nil {
			return err
		}
	}
	switch {
	case st == nil:
		pl.add(Op{Kind: Create, Target: target, Source: source, private: private})
	case st.Mode()&fs.ModeSymlink != 0:
		dest, err := ReadlinkAbs(target)
		if err != nil {
			return err
		}
		switch {
		case sameTarget(dest, source):
			pl.add(Op{Kind: OK, Target: target, Source: source})
		case pointsInside(dest, pl.ctx.Root):
			pl.add(Op{Kind: Replace, Target: target, Source: source, Was: dest, private: private})
		default:
			pl.conflict(target, source, msg.ConflictSymlinkOutside(dest))
		}
	case st.Mode().IsRegular() && sameContent(target, source):
		pl.add(Op{Kind: Adopt, Target: target, Source: source})
	case st.IsDir():
		pl.conflict(target, source, msg.ConflictFolderVsFile())
	default:
		pl.conflict(target, source, msg.ConflictDifferent())
	}
	return nil
}

// markedEntry is a ()-name found below a folder.
type markedEntry struct {
	abs   string
	raw   string
	isDir bool
}

// findMarked returns the first ()-name anywhere below dirs, depth first.
func findMarked(dirs []string) (markedEntry, bool, error) {
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return markedEntry{}, false, err
		}
		for _, ent := range entries {
			if skip[ent.Name()] {
				continue
			}
			abs := filepath.Join(dir, ent.Name())
			if LooksMarked(ent.Name()) {
				return markedEntry{abs: abs, raw: ent.Name(), isDir: ent.IsDir()}, true, nil
			}
			if ent.IsDir() {
				if m, found, err := findMarked([]string{abs}); err != nil || found {
					return m, found, err
				}
			}
		}
	}
	return markedEntry{}, false, nil
}

// landsBelow reports whether target, a clean path, is strictly below dir.
func landsBelow(target, dir string) bool {
	return target != dir && app.IsInside(target, dir)
}

// ReadlinkAbs is where the symlink at path points, as a clean absolute path:
// a relative destination is taken from the link's own folder, one hop only.
func ReadlinkAbs(path string) (string, error) {
	dest, err := os.Readlink(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(filepath.Dir(path), dest)
	}
	return filepath.Clean(dest), nil
}

// sameContent reports whether the files at a and b hold the same bytes. A
// file that cannot be read is never the same.
func sameContent(a, b string) bool {
	x, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	y, err := os.ReadFile(b)
	if err != nil {
		return false
	}
	return bytes.Equal(x, y)
}
