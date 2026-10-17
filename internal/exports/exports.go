// Package exports snapshots what is installed (Brewfile, apt and flatpak
// lists) into the home repo, so the repo always says what this machine has. A
// failed or empty export never overwrites a good file. A link at out is
// always replaced by the export, never followed. A command given {out} may
// also write a folder there (apt-repos does); the folder is compared and
// replaced as a whole. Export commands can call homebot itself, even when it
// isn't on PATH.
package exports

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/profile"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// IntervalMs is how long a successful export stays fresh for DueOnly runs.
const IntervalMs = 20 * 3600_000

// Result is what one export did.
type Result struct {
	Name string
	// Out is the output path, relative to the home repo.
	Out     string
	OK      bool
	Changed bool
	Skipped bool
	// Reason says why it was skipped, when it was not simply up to date.
	Reason string
	// Error is why it failed, as stored in state.
	Error string
	// ErrorText is the catalog message for a failure, so command output can
	// speak it while Error remains neutral for state.
	ErrorText msg.Text
	// Warning is something the export could not clean up, said whether the
	// export went through or failed. Tidy also records it in the journal.
	Warning string
	// WarningText is the catalog message for a warning, so tidy can record
	// its neutral form while keeping Warning for terminal output.
	WarningText msg.Text
}

// Options choose which exports run.
type Options struct {
	// Names limits the run to these exports; empty means all of them.
	Names []string
	// DueOnly skips exports that ran successfully within IntervalMs.
	DueOnly bool
	// Now is the clock in milliseconds; zero means ctx.Now.
	Now int64
}

func cfg(ctx *app.Context) *config.Config {
	if ctx.Config == nil {
		return config.Default()
	}
	return ctx.Config
}

// ExportsFor lists the exports that apply to this machine: its OS, and its
// default profile when the export names profiles.
func ExportsFor(ctx *app.Context) []config.ExportSpec {
	def, haveDef := profile.DefaultProfile(ctx)
	var out []config.ExportSpec
	for _, e := range cfg(ctx).Exports {
		if !slices.Contains(e.On, string(ctx.OS)) {
			continue
		}
		if e.Profiles != nil && (!haveDef || !slices.Contains(e.Profiles, def)) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func outPath(ctx *app.Context, spec config.ExportSpec) (string, error) {
	vars := []profile.Var{{Name: "os", Value: string(ctx.OS)}, {Name: "host", Value: ctx.Host}, {Name: "arch", Value: ctx.Arch}}
	where := voice.Record(ctx, msg.ExportWhere(spec.Name))
	rel, err := profile.Interpolate(spec.Out, vars, where)
	if err != nil {
		return "", err
	}
	// config.Load checked out as written; {host} and the like, and links
	// made since, could still take it somewhere RemoveAll must never reach.
	if err := config.ExportOutError(ctx.Root, where, spec.Name, rel); err != nil {
		return "", &badOutError{err}
	}
	return filepath.Join(ctx.Root, rel), nil
}

// badOutError is an out that config.ExportOutError refuses.
type badOutError struct{ error }

func (e *badOutError) Unwrap() error { return e.error }

// ExportOutPath is where an export writes, in the home repo. An output path
// with an unknown {variable} comes back as written, joined to the repo;
// RunExports reports that error. One that config.ExportOutError refuses
// comes back empty.
func ExportOutPath(ctx *app.Context, spec config.ExportSpec) string {
	p, err := outPath(ctx, spec)
	var bad *badOutError
	if errors.As(err, &bad) {
		return ""
	}
	if err != nil {
		return filepath.Join(ctx.Root, spec.Out)
	}
	return p
}

// SelfCommand is how to invoke this same homebot from a shell: the running
// binary's path, quoted.
func SelfCommand(ctx *app.Context) string {
	return ui.ShellQuote(ctx.Exe)
}

// shellKnows is true for programs on PATH and for shell builtins and keywords.
func shellKnows(ctx *app.Context, word string) bool {
	r := proc.Run(ctx, []string{"bash", "-c", `command -v -- "$1" >/dev/null || type -t -- "$1" >/dev/null`, "_", word}, proc.Options{})
	return r.Code == 0
}

// sysOps is how the export touches the file system, a variable so tests can
// fail any step (export_test.go).
type sysOps struct {
	Rename    func(oldpath, newpath string) error
	RemoveAll func(path string) error
	MkdirTemp func(dir, pattern string) (string, error)
	Mkdir     func(path string, perm fs.FileMode) error
	Lstat     func(path string) (fs.FileInfo, error)
	Stat      func(path string) (fs.FileInfo, error)
	WalkFiles func(dir string) ([]string, error)
	ReadFile  func(path string) ([]byte, error)
	WriteFile func(path string, data []byte, perm fs.FileMode) error
}

var sys = sysOps{
	Rename:    os.Rename,
	RemoveAll: fsx.RemoveAll,
	MkdirTemp: os.MkdirTemp,
	Mkdir:     os.Mkdir,
	Lstat:     os.Lstat,
	Stat:      os.Stat,
	WalkFiles: fsx.WalkFiles,
	ReadFile:  os.ReadFile,
	WriteFile: os.WriteFile,
}

// WorkPattern names the folder an export works in, next to its out. Every
// path an export creates or removes lies inside a folder made with it, so
// homebot deletes only what it made in this run, never a name that happens
// to be free or taken. Inside it, the command writes to new/<out's name>
// and a folder export's old copy goes aside to old/<out's name>: two
// folders, so no name of out can make the two paths one.
const WorkPattern = ".homebot-export-*"

// sameTree is true when both folders hold the same files with the same contents.
func sameTree(a, b string) (bool, error) {
	fa, err := sys.WalkFiles(a)
	if err != nil {
		return false, err
	}
	fb, err := sys.WalkFiles(b)
	if err != nil {
		return false, err
	}
	if !slices.Equal(fa, fb) {
		return false, nil
	}
	for _, f := range fa {
		x, err := sys.ReadFile(filepath.Join(a, f))
		if err != nil {
			return false, err
		}
		y, err := sys.ReadFile(filepath.Join(b, f))
		if err != nil {
			return false, err
		}
		if !bytes.Equal(x, y) {
			return false, nil
		}
	}
	return true, nil
}

func fileSize(path string) (int64, bool) {
	fi, err := sys.Stat(path)
	if err != nil {
		return 0, false
	}
	return fi.Size(), true
}

func stderrTail(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.Join(lines, " ")
}

// keptAside is a swap that failed after it moved the previous export out of
// the way and could not move it back: it is at path, inside the work folder.
type keptAside struct {
	path  string
	cause error
}

func (e *keptAside) Error() string { return e.cause.Error() }
func (e *keptAside) Unwrap() error { return e.cause }

func runOne(ctx *app.Context, spec config.ExportSpec) (Result, error) {
	out, err := outPath(ctx, spec)
	if err != nil {
		return Result{}, err
	}
	rel := strings.TrimPrefix(out, ctx.Root+string(filepath.Separator))
	res := Result{Name: spec.Name, Out: rel}
	words := strings.Fields(spec.Run)
	if len(words) > 0 && words[0] != "homebot" && !shellKnows(ctx, words[0]) {
		res.OK, res.Skipped = true, true
		res.Reason = voice.Plain(ctx, msg.ExportReasonNotInstalled(words[0]))
		return res, nil
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return res, err
	}
	// The command writes into a folder of its own, made for this run, so
	// that its output, the old export moved aside and any leftover all live
	// where only this run put them.
	work, err := sys.MkdirTemp(filepath.Dir(out), WorkPattern)
	if err != nil {
		return res, err
	}
	res, err = runIn(ctx, spec, res, out, work)
	var kept *keptAside
	if errors.As(err, &kept) {
		// The previous export is in work: leave it all there.
		return res, fail.Say(msg.ExportErrorKeptAside(kept.cause.Error(), kept.path))
	}
	// Only this run's own work folder is removed. Work folders that earlier
	// runs left beside out are never cleaned up, however stale: one may hold
	// the only kept copy of an old export, the one a keptAside error told the
	// person about.
	if rmErr := sys.RemoveAll(work); rmErr != nil {
		ctx.Debug.Debug("export: could not remove its work folder", "path", work, "error", rmErr)
		if err != nil {
			return res, errors.Join(err, rmErr)
		}
		// Say what is left, whether the export went through or failed: the
		// warning claims neither, res does.
		res.WarningText = msg.ExportWarningLeftBehind(work)
		res.Warning = voice.Plain(ctx, res.WarningText)
	}
	return res, err
}

// runIn runs the export's command with its output inside work and puts the
// output in out's place.
func runIn(ctx *app.Context, spec config.ExportSpec, res Result, out, work string) (Result, error) {
	rel := res.Out
	fresh, aside := filepath.Join(work, "new"), filepath.Join(work, "old")
	for _, d := range []string{fresh, aside} {
		if err := sys.Mkdir(d, 0o700); err != nil {
			return res, err
		}
	}
	tmp := filepath.Join(fresh, filepath.Base(out))
	writesItself := strings.Contains(spec.Run, "{out}")
	command := spec.Run
	if writesItself {
		command = strings.ReplaceAll(command, "{out}", ui.ShellQuote(tmp))
	}
	self := fmt.Sprintf("homebot() { %s \"$@\"; }", SelfCommand(ctx))
	r := proc.Run(ctx, []string{"bash", "-c", self + "\n" + command}, proc.Options{Dir: ctx.Root, Env: map[string]string{"HOMEBOT_ROOT": ctx.Root}})
	failed := func(text msg.Text) Result {
		res.ErrorText = text
		res.Error = voice.Record(ctx, text)
		return res
	}
	if r.Code != 0 {
		tail := stderrTail(r.Stderr)
		if tail != "" {
			tail = ": " + tail
		}
		return failed(msg.ExportErrorExit(r.Code, tail)), nil
	}
	if !writesItself {
		if err := sys.WriteFile(tmp, []byte(r.Stdout), 0o644); err != nil {
			return res, err
		}
	}
	// Stat, not Lstat: the TypeScript's existsSync follows links, and a
	// dangling link is no output. Lstat would let a command that links {out}
	// to nothing put that dangling link in the repo in place of the last good
	// export.
	if _, err := sys.Stat(tmp); err != nil {
		return failed(msg.ExportErrorNoOutput(rel)), nil
	}
	wroteFolder := fsx.IsDir(tmp)
	// What is at out now. A link there is homebot's to replace, whatever it
	// points at or holds: the checks below read through links, so they run
	// only for a real file or folder.
	var old fs.FileInfo
	switch fi, err := sys.Lstat(out); {
	case err == nil:
		old = fi
	case !errors.Is(err, fs.ErrNotExist):
		// Not knowing what is there, do not replace it.
		return res, err
	}
	isLink := old != nil && old.Mode()&fs.ModeSymlink != 0
	// Never delete something of a different kind: a folder to put a file in
	// its place, or a file to put a folder.
	if old != nil && !isLink {
		switch {
		case !wroteFolder && old.IsDir():
			return failed(msg.ExportErrorOutIsFolder(rel)), nil
		case wroteFolder && !old.IsDir():
			return failed(msg.ExportErrorOutIsFile(rel)), nil
		}
	}
	if wroteFolder {
		return replaceFolder(res, out, tmp, filepath.Join(aside, filepath.Base(out)), old != nil, isLink, failed)
	}
	return replaceFile(res, out, tmp, isLink, func() Result { return failed(msg.ExportErrorEmptyFile(rel)) })
}

// replaceFolder puts the folder tmp in out's place. The old out is moved
// aside, not deleted first, so a failed rename of tmp brings it back and a
// good export is never lost to a half-done swap; a folder cannot be renamed
// over another. The aside is inside the work folder, which the caller
// removes once the swap is done.
func replaceFolder(res Result, out, tmp, aside string, exists, isLink bool, failed func(msg.Text) Result) (Result, error) {
	if !isLink {
		fresh, err := sys.WalkFiles(tmp)
		if err != nil {
			return res, err
		}
		old, err := sys.WalkFiles(out)
		if err != nil {
			return res, err
		}
		if len(fresh) == 0 && len(old) > 0 {
			return failed(msg.ExportErrorEmptyFolder(res.Out)), nil
		}
		if exists {
			same, err := sameTree(out, tmp)
			if err != nil {
				return res, err
			}
			if same {
				res.OK = true
				return res, nil
			}
		}
	}
	if !exists {
		if err := sys.Rename(tmp, out); err != nil {
			return res, err
		}
	} else {
		if err := sys.Rename(out, aside); err != nil {
			return res, err
		}
		if err := sys.Rename(tmp, out); err != nil {
			if back := sys.Rename(aside, out); back != nil {
				return res, &keptAside{path: aside, cause: errors.Join(err, back)}
			}
			return res, err
		}
	}
	res.OK, res.Changed = true, true
	return res, nil
}

// replaceFile puts the file tmp in out's place with one rename, which
// replaces a file or a link there atomically and never follows the link.
func replaceFile(res Result, out, tmp string, isLink bool, empty func() Result) (Result, error) {
	if !isLink {
		if size, ok := fileSize(out); ok && size > 0 {
			if n, _ := fileSize(tmp); n == 0 {
				return empty(), nil
			}
		}
		if fi, err := sys.Stat(out); err == nil && !fi.IsDir() {
			a, err := sys.ReadFile(out)
			if err != nil {
				return res, err
			}
			b, err := sys.ReadFile(tmp)
			if err != nil {
				return res, err
			}
			if bytes.Equal(a, b) {
				res.OK = true
				return res, nil
			}
		}
	}
	if err := sys.Rename(tmp, out); err != nil {
		return res, err
	}
	res.OK, res.Changed = true, true
	return res, nil
}

// RunExports runs the exports that apply to this machine, or the ones named,
// and records each outcome in state. An unknown name, one that is not an
// export for this OS and profile, is an error and nothing runs.
func RunExports(ctx *app.Context, opts Options) ([]Result, error) {
	now := opts.Now
	if now == 0 {
		now = ctx.Now().UnixMilli()
	}
	st, err := state.Load(ctx)
	if err != nil {
		return nil, err
	}
	specs := ExportsFor(ctx)
	var unknown []string
	for _, n := range opts.Names {
		if !slices.ContainsFunc(specs, func(s config.ExportSpec) bool { return s.Name == n }) {
			unknown = append(unknown, n)
		}
	}
	if len(unknown) > 0 {
		names := make([]string, len(specs))
		for i, s := range specs {
			names[i] = s.Name
		}
		known := strings.Join(names, ", ")
		if known == "" {
			known = voice.Record(ctx, msg.ExportWordNone())
		}
		// Said at print (fail.Say), not now: a caller that passes names and
		// stores the error, as tidy could, keeps the neutral English.
		return nil, fail.Say(msg.ExportErrorUnknown(strings.Join(unknown, ", "), known))
	}
	var results []Result
	for _, spec := range specs {
		if len(opts.Names) > 0 && !slices.Contains(opts.Names, spec.Name) {
			continue
		}
		if last, ok := st.Exports.Get(spec.Name); opts.DueOnly && ok && last.OK && now-last.At < IntervalMs {
			results = append(results, Result{Name: spec.Name, Out: spec.Out, OK: true, Skipped: true})
			continue
		}
		r, err := runOne(ctx, spec)
		if err != nil {
			return results, err
		}
		results = append(results, r)
	}
	err = state.Update(ctx, func(s *state.State) {
		for _, r := range results {
			if r.Reason != "" {
				s.Exports.Delete(r.Name)
			}
			if r.Skipped {
				continue
			}
			s.Exports.Set(r.Name, state.ExportResult{At: now, OK: r.OK, Error: r.Error})
		}
	})
	return results, err
}
