package selfname

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/proc"
)

// errNoHome is what Link, Adopt and Unlink say for a Context without
// absolute home and state folders (app.ForReport): there is nowhere safe
// to put a link or a record.
var errNoHome = errors.New("selfname: no home or state folder")

// AfterStep is a test seam for the rename (D4): Link calls it after step 1
// for a preclaim, after step 2 (the new link made), and after step 3 (the
// old one removed). An error
// stops Link there, as a crash would, and Link returns it; the next Link
// finishes the rename. Tests that set it must not run in parallel.
var AfterStep = func(step int) error { return nil }

// beforeMake runs right before os.Symlink makes a new link, so a test can
// put a file there first (the race D4's EEXIST rule is for).
var beforeMake = func(path string) {}

// Test seams for a replacement between ownership inspection and mutation.
var beforeRemove = func(path string) {}
var beforeRepoint = func(path string) {}
var beforeTempRemove = func(path string) {}

// mode is what a Link may do.
type mode int

const (
	// mayMkdir may create ~/.local/bin: setup and homebot name (Link).
	mayMkdir mode = iota
	// inBin makes and re-points links, but only in a ~/.local/bin that
	// exists: the helper (LinkInBin).
	inBin
	// adoptOnly records the link that is there and changes nothing on disk
	// (Adopt).
	adoptOnly
)

// Link makes ~/.local/bin/<name> a link to homebot, re-points it when it is
// ours, or adopts the user's link to homebot, and records it; a recorded
// name that differs is renamed in D4's order. It may create ~/.local/bin:
// only setup and homebot name, which the user ran, call it.
//
// It refuses what Check refuses without the disk (the syntax, homebot's own
// names, ShellWords and ToolNames), a synced ~/.local/bin, and anything at
// the link's path that isn't ours or a link to homebot (Foreign). The
// caller runs Check first for the rest. A refusal is an *Error, and
// changes nothing; a failure of the file system comes back as it is.
func Link(c *app.Context, name string) error { return apply(c, name, mayMkdir) }

// LinkInBin is Link for the helper, which never creates ~/.local/bin (D4):
// when the folder is missing, it changes nothing and says NoBin.
func LinkInBin(c *app.Context, name string) error { return apply(c, name, inBin) }

// TempBlockedPath reports a foreign temporary path that would stop a repair
// of an owned, stale name link. It changes nothing, for the inbox check.
func TempBlockedPath(c *app.Context, name string) string {
	rec, st := readRecord(c)
	if st != goodRecord || !ours(c, rec, true, name) || sameFile(linkPath(c, name), proc.SelfPath(c)) {
		return ""
	}
	tmp := tempPath(c, name)
	if exists(tmp) {
		if _, home := pointsAtHomebot(c, tmp); !home {
			return tmp
		}
	}
	return ""
}

// Adopt records the user's link ~/.local/bin/<name> to homebot as the name's
// link (made=false). It is used as it is, and never re-pointed or removed.
// A link of ours keeps made=true. Nothing at the path is NoLink.
func Adopt(c *app.Context, name string) error { return apply(c, name, adoptOnly) }

func apply(c *app.Context, name string, m mode) (err error) {
	if v, refused := checkWords(name, nil); refused {
		return &Error{Reason: v.Reason}
	}
	if !usable(c) {
		return errNoHome
	}
	lock, err := transactionLock(c)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	return applyLocked(c, name, m)
}

func applyLocked(c *app.Context, name string, m mode) error {
	if p, ok := syncedBin(c); ok {
		return &Error{Reason: SyncedBin, Path: p}
	}
	if err := ensureBin(c, name, m); err != nil {
		return err
	}

	prev, st := readRecord(c)
	had := st == goodRecord
	if had && prev.Pending {
		// A preclaim is not proof that we made a link. Another actor may
		// have won the path while we were stopped. Explicit Adopt can take
		// such a link; Link leaves it untouched.
		if _, existsNow := onDisk(binDir(c), prev.Name); existsNow && m != adoptOnly {
			return foreign(binDir(c), prev.Name)
		}
		if _, existsNow := onDisk(binDir(c), prev.Name); !existsNow {
			if prev.Old == "" {
				if err := deleteRecord(c); err != nil {
					return err
				}
			} else {
				if err := settle(c, prev); err != nil {
					return err
				}
			}
			prev, st = readRecord(c)
			had = st == goodRecord
		}
	}
	if had && prev.Old != "" && prev.Name != name && prev.Old != name {
		// A rename to a third name, while the last one is half done:
		// finish or undo the last one first, then start this one.
		if err := settle(c, prev); err != nil {
			return err
		}
		return applyLocked(c, name, m)
	}

	// rec is the record during the change; old the link a rename leaves.
	rec := Rec{Name: name}
	known := false // the name's link is recorded as adopted already
	if had {
		switch name {
		case prev.Name:
			rec = prev
			known = !prev.Made
		case prev.Old:
			// Back to the name a half-done rename left: the rename's new
			// name becomes the old one.
			rec = Rec{Name: name, Made: prev.OldMade, Old: prev.Name, OldMade: prev.Made}
			rec.Target, _, _ = readTarget(linkPath(c, name))
			known = !prev.OldMade
		default:
			rec.Old, rec.OldMade = prev.Name, prev.Made
		}
	}
	if rec.Old != "" && rec.Target == "" {
		// A rename onto an existing, adoptable link still records the old
		// link before step 2. The transitional record needs a nonempty
		// target; ensureLink replaces this with the actual link target.
		tgt, err := target(c)
		if err != nil {
			return err
		}
		rec.Target = tgt
	}

	// Claim an absent new link in the record before making it. The claim is
	// explicitly pending: after a crash, it cannot prove who made the link.
	preclaimed := false
	if _, existsNow := onDisk(binDir(c), name); !existsNow && m != adoptOnly {
		tgt, err := target(c)
		if err != nil {
			return err
		}
		rec.Made, rec.Pending, rec.Target = false, true, tgt
		preclaimed = true
	}
	// Step 1: the record names both links before either changes. A first
	// link also needs its pending claim recorded before it appears on disk.
	if preclaimed || (rec.Old != "" && (!had || rec != prev)) {
		if err := writeRecord(c, rec); err != nil {
			return err
		}
	}
	if preclaimed {
		if err := AfterStep(1); err != nil {
			return err
		}
	}

	// Step 2: make, re-point or adopt the new link.
	ch, made, tgt, err := ensureLink(c, rec, m, known, preclaimed)
	if err != nil {
		if rec.Old != "" || preclaimed {
			// The record goes back to the old name.
			err = errors.Join(err, putRecord(c, prev, had))
		}
		return err
	}
	rec.Made, rec.Pending, rec.Target = made, false, tgt
	if preclaimed {
		raw, _, ok := readTarget(linkPath(c, name))
		spelled, found := onDisk(binDir(c), name)
		if !ok || raw != tgt || !found || spelled != name {
			return foreign(binDir(c), name)
		}
	}
	if preclaimed || !had || rec != prev {
		if err := writeRecord(c, rec); err != nil {
			return err
		}
	}
	if ch != nil {
		OnChange(c, *ch)
	}
	if rec.Old == "" {
		return nil
	}
	if err := AfterStep(2); err != nil {
		return err
	}

	// Step 3: remove the old link, if we made it and it is still ours.
	if err := removeOld(c, rec); err != nil {
		return err
	}
	if err := AfterStep(3); err != nil {
		return err
	}

	// Step 4: the record forgets the old name.
	rec.Old, rec.OldMade = "", false
	return writeRecord(c, rec)
}

// ensureBin makes sure ~/.local/bin is a folder, creating it (0755) only
// when m allows.
func ensureBin(c *app.Context, name string, m mode) error {
	bin := binDir(c)
	fi, err := os.Stat(bin)
	switch {
	case err == nil && fi.IsDir():
		return nil
	case err == nil, !errors.Is(err, fs.ErrNotExist):
		return &Error{Reason: NoBin, Path: bin}
	case m == adoptOnly:
		return &Error{Reason: NoLink, Path: linkPath(c, name)}
	case m == inBin:
		return &Error{Reason: NoBin, Path: bin}
	}
	return os.MkdirAll(bin, 0o755)
}

// settle finishes or undoes the half-done rename prev records (prev.Old is
// set). When the new name's link is in place, the old link is removed if
// ours, and old= is dropped (steps 3 and 4). When it is not, the rename
// never got past step 1, and the record goes back to the old name.
func settle(c *app.Context, prev Rec) error {
	spelled, ok := onDisk(binDir(c), prev.Name)
	if prev.Pending && ok {
		return foreign(binDir(c), prev.Name)
	}
	if _, home := pointsAtHomebot(c, linkPath(c, prev.Name)); ok && spelled == prev.Name && home {
		if err := removeOld(c, prev); err != nil {
			return err
		}
		prev.Old, prev.OldMade = "", false
		return writeRecord(c, prev)
	}
	back := Rec{Name: prev.Old, Made: prev.OldMade}
	back.Target, _, _ = readTarget(linkPath(c, prev.Old))
	if back.Target == "" {
		return &Error{Reason: NoLink, Path: linkPath(c, prev.Old)}
	}
	return writeRecord(c, back)
}

// removeOld removes rec.Old's link when the record says homebot made it
// and it is still ours, read with the old name (D4 step 3).
func removeOld(c *app.Context, rec Rec) error {
	if !rec.OldMade {
		return nil
	}
	path := linkPath(c, rec.Old)
	fi, raw, ok := linkSnapshot(path)
	if !ok {
		return nil
	}
	if !ours(c, rec, true, rec.Old) {
		return nil
	}
	beforeRemove(path)
	if !sameLink(path, fi, raw) {
		return &Error{Reason: Foreign, Path: path}
	}
	if err := removeIfMissing(path); err != nil {
		return err
	}
	OnChange(c, Change{Kind: Removed, Name: rec.Old, Path: path})
	return nil
}

// ensureLink is D4 step 2 for rec.Name: it makes the link when nothing is
// there, re-points it when it is ours and no longer resolves to homebot,
// and adopts the user's link to homebot. It returns the change, whether
// homebot made the link, and the link's target.
func ensureLink(c *app.Context, rec Rec, m mode, known, expectedAbsent bool) (*Change, bool, string, error) {
	name := rec.Name
	bin := binDir(c)
	path := linkPath(c, name)
	spelled, ok := onDisk(bin, name)
	if !ok {
		if m == adoptOnly {
			return nil, false, "", &Error{Reason: NoLink, Path: path}
		}
		tgt, err := target(c)
		if err != nil {
			return nil, false, "", err
		}
		beforeMake(path)
		if err := os.Symlink(tgt, path); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return nil, false, "", foreign(bin, name)
			}
			return nil, false, "", err
		}
		return &Change{Kind: Made, Name: name, Path: path, Target: tgt}, true, tgt, nil
	}
	// Another process created a link after we checked that the path was
	// absent. Do not treat it as ours just because the record now says made.
	if expectedAbsent {
		return nil, false, "", foreign(bin, name)
	}
	if spelled != name {
		return nil, false, "", foreign(bin, name)
	}
	raw, home := pointsAtHomebot(c, path)
	if !home {
		return nil, false, "", &Error{Reason: Foreign, Path: path}
	}
	if !ours(c, rec, true, name) {
		if known {
			return nil, false, raw, nil
		}
		return &Change{Kind: Adopted, Name: name, Path: path, Target: raw}, false, raw, nil
	}
	if m == adoptOnly || sameFile(path, proc.SelfPath(c)) {
		return nil, true, raw, nil
	}
	tgt, err := target(c)
	if err != nil {
		return nil, false, "", err
	}
	fi, observedRaw, ok := linkSnapshot(path)
	if !ok || observedRaw != raw {
		return nil, false, "", foreign(bin, name)
	}
	if err := repoint(c, name, tgt, fi, observedRaw); err != nil {
		return nil, false, "", err
	}
	return &Change{Kind: Repointed, Name: name, Path: path, Target: tgt}, true, tgt, nil
}

// foreign is the Foreign error for name, with the spelling on disk.
func foreign(bin, name string) *Error {
	spelled, _ := onDisk(bin, name)
	if spelled == "" {
		spelled = name
	}
	return &Error{Reason: Foreign, Path: filepath.Join(bin, spelled)}
}

// repoint replaces our link at name with one to tgt: a new link at the
// temp path, renamed over ours (D4). A stale temp is removed only when it
// is a link to homebot; anything else there stops it with TempBlocked, and
// our link stays as it is.
func repoint(c *app.Context, name, tgt string, original os.FileInfo, originalRaw string) error {
	tmp := tempPath(c, name)
	if exists(tmp) {
		if _, home := pointsAtHomebot(c, tmp); !home {
			return &Error{Reason: TempBlocked, Path: tmp}
		}
		fi, raw, ok := linkSnapshot(tmp)
		if !ok {
			return &Error{Reason: TempBlocked, Path: tmp}
		}
		beforeTempRemove(tmp)
		if !sameLink(tmp, fi, raw) {
			return &Error{Reason: TempBlocked, Path: tmp}
		}
		if err := removeIfMissing(tmp); err != nil {
			return err
		}
	}
	if err := os.Symlink(tgt, tmp); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return &Error{Reason: TempBlocked, Path: tmp}
		}
		return err
	}
	path := linkPath(c, name)
	beforeRepoint(path)
	if !sameLink(path, original, originalRaw) {
		return &Error{Reason: Foreign, Path: path}
	}
	if err := os.Rename(tmp, path); err != nil {
		return errors.Join(err, removeIfMissing(tmp))
	}
	return nil
}

// Unlink removes every link the record says homebot made and that is still
// ours (R12), and deletes the record. An adopted link is left alone. A
// record that can't be read owns nothing: it is deleted, and no link is
// touched. A synced ~/.local/bin is SyncedBin, and the record is kept.
func Unlink(c *app.Context) (err error) {
	if !usable(c) {
		return errNoHome
	}
	lock, err := transactionLock(c)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	rec, st := readRecord(c)
	switch st {
	case noRecord:
		return nil
	case badRecord:
		return deleteRecord(c)
	}
	if p, ok := syncedBin(c); ok {
		return &Error{Reason: SyncedBin, Path: p}
	}
	var removed []Change
	for _, l := range []struct {
		name string
		made bool
	}{{rec.Name, rec.Made}, {rec.Old, rec.OldMade}} {
		if l.name == "" || !l.made {
			continue
		}
		path := linkPath(c, l.name)
		fi, raw, ok := linkSnapshot(path)
		if !ok {
			continue
		}
		if !ours(c, rec, true, l.name) {
			continue
		}
		beforeRemove(path)
		if !sameLink(path, fi, raw) {
			return &Error{Reason: Foreign, Path: path}
		}
		if err := removeIfMissing(path); err != nil {
			return err
		}
		removed = append(removed, Change{Kind: Removed, Name: l.name, Path: path})
	}
	if err := deleteRecord(c); err != nil {
		return err
	}
	for _, ch := range removed {
		OnChange(c, ch)
	}
	return nil
}
