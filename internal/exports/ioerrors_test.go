package exports_test

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/exports"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/ui"
)

var errInjected = errors.New("injected failure")

// isWork is true for a path inside the folder an export works in.
func isWork(p string) bool { return strings.Contains(p, ".homebot-export-") }

// inWork is what follows the work folder in p: new/<name> is the command's
// output and old/<name> the old export moved aside. It is "" outside it.
func inWork(p string) string {
	i := strings.Index(p, ".homebot-export-")
	if i < 0 {
		return ""
	}
	_, rest, _ := strings.Cut(p[i:], "/")
	return rest
}

// The renames of a swap, told apart by where they come from and go.
func intoOut(old, new string) bool {
	return strings.HasPrefix(inWork(old), "new/") && !isWork(new)
}
func asideOf(old, new string) bool { return !isWork(old) && isWork(new) }
func restore(old, new string) bool {
	return strings.HasPrefix(inWork(old), "old/") && !isWork(new)
}

// failRename fails the renames that match, until the test ends.
func failRename(t *testing.T, match ...func(old, new string) bool) {
	t.Helper()
	exports.Inject(t, func(s *exports.Sys) {
		real := s.Rename
		s.Rename = func(old, new string) error {
			for _, m := range match {
				if m(old, new) {
					return errInjected
				}
			}
			return real(old, new)
		}
	})
}

// workLeft lists the work folders an export left next to rel.
func workLeft(t *testing.T, sb *testutil.Box, rel string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(sb.Root, filepath.Dir(rel)))
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, e := range entries {
		if isWork(e.Name()) {
			left = append(left, e.Name())
		}
	}
	return left
}

func runErr(t *testing.T, sb *testutil.Box) ([]exports.Result, error) {
	t.Helper()
	return exports.RunExports(sb.Ctx, exports.Options{})
}

func wantInjected(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, errInjected) {
		t.Fatalf("RunExports error = %v, want the injected failure", err)
	}
}

// ---- every failed step of the swap leaves the old export and no work folder

func TestFailedStepsKeepTheOldExportAndLeaveNoWorkFolder(t *testing.T) {
	cases := []struct {
		name, run, out string
		old            map[string]string // files under home/.home, before
		inject         func(t *testing.T)
		stays          string // file that must still hold the old content
		want           string
	}{
		{name: "file: the rename into place fails", run: "echo fresh", out: "manifests/list.txt",
			old: map[string]string{"manifests/list.txt": "good\n"}, inject: func(t *testing.T) { failRename(t, intoOut) },
			stays: "manifests/list.txt", want: "good\n"},
		{name: "file: no old file, the rename fails", run: "echo fresh", out: "manifests/list.txt",
			inject: func(t *testing.T) { failRename(t, intoOut) }},
		{name: "file: the stdout cannot be written", run: "echo fresh", out: "manifests/list.txt",
			old: map[string]string{"manifests/list.txt": "good\n"},
			inject: func(t *testing.T) {
				exports.Inject(t, func(s *exports.Sys) {
					s.WriteFile = func(string, []byte, fs.FileMode) error { return errInjected }
				})
			},
			stays: "manifests/list.txt", want: "good\n"},
		{name: "file: the old file cannot be read to compare", run: "echo fresh", out: "manifests/list.txt",
			old: map[string]string{"manifests/list.txt": "good\n"},
			inject: func(t *testing.T) {
				exports.Inject(t, func(s *exports.Sys) {
					real := s.ReadFile
					s.ReadFile = func(p string) ([]byte, error) {
						if !isWork(p) {
							return nil, errInjected
						}
						return real(p)
					}
				})
			},
			stays: "manifests/list.txt", want: "good\n"},
		{name: "what is at out cannot be looked at", run: "echo fresh", out: "manifests/list.txt",
			old: map[string]string{"manifests/list.txt": "good\n"},
			inject: func(t *testing.T) {
				exports.Inject(t, func(s *exports.Sys) {
					real := s.Lstat
					s.Lstat = func(p string) (fs.FileInfo, error) {
						if strings.HasSuffix(p, "manifests/list.txt") {
							return nil, &fs.PathError{Op: "lstat", Path: p, Err: errInjected}
						}
						return real(p)
					}
				})
			},
			stays: "manifests/list.txt", want: "good\n"},
		{name: "the work folder's own folders cannot be made", run: "echo fresh", out: "manifests/list.txt",
			old: map[string]string{"manifests/list.txt": "good\n"},
			inject: func(t *testing.T) {
				exports.Inject(t, func(s *exports.Sys) {
					s.Mkdir = func(string, fs.FileMode) error { return errInjected }
				})
			},
			stays: "manifests/list.txt", want: "good\n"},
		{name: "folder: moving the old folder aside fails", run: folderCmd, out: "manifests/repos",
			old: map[string]string{"manifests/repos/old": "kept\n"}, inject: func(t *testing.T) { failRename(t, asideOf) },
			stays: "manifests/repos/old", want: "kept\n"},
		{name: "folder: the rename into place fails and the old folder comes back", run: folderCmd, out: "manifests/repos",
			old: map[string]string{"manifests/repos/old": "kept\n"}, inject: func(t *testing.T) { failRename(t, intoOut) },
			stays: "manifests/repos/old", want: "kept\n"},
		{name: "folder: no old folder, the rename fails", run: folderCmd, out: "manifests/repos",
			inject: func(t *testing.T) { failRename(t, intoOut) }},
		{name: "folder: the old folder cannot be walked", run: folderCmd, out: "manifests/repos",
			old: map[string]string{"manifests/repos/sub/old": "kept\n"},
			inject: func(t *testing.T) {
				exports.Inject(t, func(s *exports.Sys) {
					real := s.WalkFiles
					s.WalkFiles = func(d string) ([]string, error) {
						if !isWork(d) {
							return nil, errInjected
						}
						return real(d)
					}
				})
			},
			stays: "manifests/repos/sub/old", want: "kept\n"},
		{name: "folder: a file of the old folder cannot be read to compare", run: folderCmd, out: "manifests/repos",
			old: map[string]string{"manifests/repos/y": "2", "manifests/repos/a/x": "1"},
			inject: func(t *testing.T) {
				exports.Inject(t, func(s *exports.Sys) {
					real := s.ReadFile
					s.ReadFile = func(p string) ([]byte, error) {
						if !isWork(p) {
							return nil, errInjected
						}
						return real(p)
					}
				})
			},
			stays: "manifests/repos/y", want: "2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sb := setup(t, c.run, c.out)
			files := map[string]string{}
			for k, v := range c.old {
				files["home/.home/"+k] = v
			}
			sb.Files(files)
			c.inject(t)
			_, err := runErr(t, sb)
			wantInjected(t, err)
			if c.stays != "" {
				eq(t, read(t, sb, c.stays), c.want)
			} else {
				eq(t, exists(sb, c.out), false)
			}
			eq(t, workLeft(t, sb, c.out), []string(nil))
		})
	}
}

func TestFailureToMakeTheWorkFolderIsAnError(t *testing.T) {
	sb := setup(t, "echo fresh", "manifests/list.txt")
	sb.Files(map[string]string{"home/.home/manifests/list.txt": "good\n"})
	exports.Inject(t, func(s *exports.Sys) {
		s.MkdirTemp = func(string, string) (string, error) { return "", errInjected }
	})
	_, err := runErr(t, sb)
	wantInjected(t, err)
	eq(t, read(t, sb, "manifests/list.txt"), "good\n")
}

// When even the way back fails, the old folder is not lost: it stays where
// the error says, in the voice's words from the catalog, at <work>/old/<name>
// whatever out is named.
func TestSwapThatCannotGoBackSaysWhereTheOldExportIs(t *testing.T) {
	for _, name := range []string{"repos", "previous", "new", "old"} {
		t.Run(name, func(t *testing.T) {
			out := "manifests/" + name
			sb := setup(t, folderCmd, out)
			sb.Files(map[string]string{"home/.home/" + out + "/old": "kept\n"})
			failRename(t, intoOut, restore)
			_, err := runErr(t, sb)
			var ue *fail.UserError
			if !errors.As(err, &ue) {
				t.Fatalf("RunExports error = %v, want a *fail.UserError", err)
			}
			if !strings.Contains(err.Error(), "The previous export is kept at ") {
				t.Fatalf("error = %q, want it to say where the old export is", err)
			}
			left := workLeft(t, sb, out)
			if len(left) != 1 {
				t.Fatalf("work folders = %v, want the one holding the old export", left)
			}
			keptAt := filepath.Join(sb.Ctx.Root, "manifests", left[0], "old", name)
			if b, rerr := os.ReadFile(filepath.Join(keptAt, "old")); rerr != nil || string(b) != "kept\n" {
				t.Errorf("%s/old = %q, %v; want the old export", keptAt, b, rerr)
			}
			if !strings.HasSuffix(err.Error(), "The previous export is kept at "+keptAt+".") {
				t.Errorf("error = %q, want it to end with the path of the kept export, %s", err, keptAt)
			}
		})
	}
}

// ---- out may have any name, the names used inside the work folder too

// The command writes into <work>/new/<name> and the old folder goes aside
// into <work>/old/<name>, so no name of out can make the two collide. Each
// export runs twice with a change between the runs, so the second run swaps
// the old export out.
func TestOutMayHaveTheNameOfAnythingInTheWorkFolder(t *testing.T) {
	for _, name := range []string{"previous", "new", "old"} {
		for _, kind := range []string{"file", "folder", "folder over a link"} {
			t.Run(kind+" named "+name, func(t *testing.T) {
				seen := filepath.Join(t.TempDir(), "seen")
				out := "manifests/" + name
				// Where the command was told to write, then the export itself.
				run := "printf '%s' {out} > " + ui.ShellQuote(seen) + " && cat marker > {out}"
				content := out
				if kind != "file" {
					run = "printf '%s' {out} > " + ui.ShellQuote(seen) + " && mkdir -p {out} && cat marker > {out}/v"
					content = out + "/v"
				}
				sb := setup(t, run, out)
				if kind == "folder over a link" {
					dest := filepath.Join(t.TempDir(), "target")
					if err := os.MkdirAll(dest, 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dest, "theirs"), []byte("theirs\n"), 0o644); err != nil {
						t.Fatal(err)
					}
					p := filepath.Join(sb.Ctx.Root, out)
					if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(dest, p); err != nil {
						t.Fatal(err)
					}
				}
				for _, v := range []string{"one\n", "two\n"} {
					if err := os.WriteFile(filepath.Join(sb.Ctx.Root, "marker"), []byte(v), 0o644); err != nil {
						t.Fatal(err)
					}
					rs, err := runErr(t, sb)
					if err != nil {
						t.Fatalf("run with %q: RunExports: %v", v, err)
					}
					if len(rs) != 1 || !rs[0].OK || !rs[0].Changed || rs[0].Error != "" {
						t.Fatalf("run with %q: results = %+v, want the export changed", v, rs)
					}
					eq(t, read(t, sb, content), v)
					eq(t, workLeft(t, sb, out), []string(nil))
					told, err := os.ReadFile(seen)
					if err != nil {
						t.Fatal(err)
					}
					p := string(told)
					work := filepath.Dir(filepath.Dir(p))
					if filepath.Base(p) != name || filepath.Base(filepath.Dir(p)) != "new" ||
						!strings.HasPrefix(filepath.Base(work), ".homebot-export-") ||
						filepath.Dir(work) != filepath.Join(sb.Ctx.Root, "manifests") {
						t.Errorf("{out} = %q, want <repo>/manifests/.homebot-export-*/new/%s", p, name)
					}
				}
			})
		}
	}
}

// ---- a clean-up that fails is a warning, not a failure

// failCleanup fails the first removal of a work folder and returns the
// folder it was asked to remove.
func failCleanup(t *testing.T) *string {
	t.Helper()
	var work string
	exports.Inject(t, func(s *exports.Sys) {
		real := s.RemoveAll
		s.RemoveAll = func(p string) error {
			if isWork(p) && work == "" {
				work = p
				return errInjected
			}
			return real(p)
		}
	})
	return &work
}

func leftBehind(work string) string {
	return "The temporary folder " + work + " could not be removed. It is safe to delete."
}

func TestFailedCleanupAfterAGoodSwapIsAWarning(t *testing.T) {
	sb := setup(t, "echo one", "manifests/a.txt")
	configure(t, sb, "exports:\n  - name: a\n    out: manifests/a.txt\n    run: \"echo one\"\n  - name: b\n    out: manifests/b.txt\n    run: \"echo two\"\n")
	work := failCleanup(t)
	rs, err := runErr(t, sb)
	if err != nil {
		t.Fatalf("RunExports: %v, want the run to go on", err)
	}
	if len(rs) != 2 || !rs[0].OK || !rs[0].Changed || !rs[1].OK || !rs[1].Changed {
		t.Fatalf("results = %+v, want both exports done", rs)
	}
	eq(t, rs[0].Warning, leftBehind(*work))
	eq(t, rs[1].Warning, "")
	eq(t, read(t, sb, "manifests/a.txt"), "one\n")
	eq(t, read(t, sb, "manifests/b.txt"), "two\n")
}

// The warning makes no claim about the export: here the export failed, and
// the result says so on its own.
func TestFailedCleanupAfterAFailedExportIsAWarningThatClaimsNothing(t *testing.T) {
	for _, c := range []struct{ name, run, want string }{
		{"the command fails", "exit 1", "exited with 1"},
		{"the output is empty", "true", "produced an empty file; kept the previous manifests/list.txt"},
	} {
		t.Run(c.name, func(t *testing.T) {
			sb := setup(t, c.run, "manifests/list.txt")
			sb.Files(map[string]string{"home/.home/manifests/list.txt": "good\n"})
			work := failCleanup(t)
			r := one(t, sb)
			eq(t, r.OK, false)
			if !strings.HasPrefix(r.Error, c.want) {
				t.Errorf("Error = %q, want it to start with %q", r.Error, c.want)
			}
			eq(t, r.Warning, leftBehind(*work))
			eq(t, read(t, sb, "manifests/list.txt"), "good\n")
		})
	}
}

func TestFailedCleanupAlongsideAnErrorKeepsTheError(t *testing.T) {
	sb := setup(t, "echo fresh", "manifests/list.txt")
	failRename(t, intoOut)
	exports.Inject(t, func(s *exports.Sys) {
		s.RemoveAll = func(p string) error { return errors.New("cannot remove") }
	})
	_, err := runErr(t, sb)
	if !errors.Is(err, errInjected) || !strings.Contains(err.Error(), "cannot remove") {
		t.Fatalf("RunExports error = %v, want both failures", err)
	}
}

// ---- a read-only folder in the output does not keep the work folder

// A command may write a folder without write permission (0o555). Removing
// the work folder then needs the folders made writable first, or every run
// leaves one behind. Run as root, where permissions stop no delete, this
// cannot fail; fsx's tests cover the retry with a remover that checks the
// permissions whoever runs them.
func TestReadOnlyFolderInTheOutputDoesNotKeepTheWorkFolder(t *testing.T) {
	run := "mkdir -p {out}/ro && cat marker > {out}/ro/f && chmod 555 {out}/ro"
	sb := setup(t, run, "manifests/repos")
	t.Cleanup(func() { fsx.MakeWritable(sb.Root) })
	for _, step := range []struct {
		marker  string
		changed bool
	}{{"one\n", true}, {"one\n", false}, {"two\n", true}} {
		if err := os.WriteFile(filepath.Join(sb.Root, "marker"), []byte(step.marker), 0o644); err != nil {
			t.Fatal(err)
		}
		r := one(t, sb)
		if !r.OK || r.Changed != step.changed || r.Warning != "" {
			t.Fatalf("with %q: result = %+v, want OK, changed %v, no warning", step.marker, r, step.changed)
		}
		eq(t, workLeft(t, sb, "manifests/repos"), []string(nil))
		eq(t, read(t, sb, "manifests/repos/ro/f"), step.marker)
	}
}

// ---- nothing homebot did not create in this run is ever deleted

// squat puts files and folders where the old pid-named temp names were,
// beside out, and an earlier run's work folder, and returns a check that
// they are all still there, byte for byte.
func squat(t *testing.T, sb *testutil.Box, rel string, folder bool) func() {
	t.Helper()
	pid := os.Getpid()
	var made []string
	for _, suffix := range []string{fmt.Sprintf(".tmp-%d", pid), fmt.Sprintf(".old-%d", pid), ".old-2025", ".tmp-1"} {
		p := filepath.Join(sb.Root, rel+suffix)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if folder {
			if err := os.MkdirAll(filepath.Join(p, "in"), 0o755); err != nil {
				t.Fatal(err)
			}
			p = filepath.Join(p, "in", "mine")
		}
		if err := os.WriteFile(p, []byte("mine\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		made = append(made, p)
	}
	// An earlier run's work folder: it may hold the only copy of an old
	// export that could not be put back, so no later run may touch it.
	earlier := filepath.Join(sb.Root, filepath.Dir(rel), ".homebot-export-123456")
	base := filepath.Base(rel)
	for name, body := range map[string]string{
		"old/" + base + "/sub/kept": "kept\n",
		"old/" + base + "/top":      "top\n",
		"new/" + base:               "half\n",
		"previous/old":              "round two\n",
	} {
		p := filepath.Join(earlier, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before := tree(t, earlier)
	return func() {
		t.Helper()
		for _, p := range made {
			if b, err := os.ReadFile(p); err != nil || string(b) != "mine\n" {
				t.Errorf("%s = %q, %v; want it untouched", p, b, err)
			}
		}
		if _, err := os.Lstat(earlier); err != nil {
			t.Fatalf("the earlier work folder is gone: %v", err)
		}
		if after := tree(t, earlier); !maps.Equal(after, before) {
			t.Errorf("earlier work folder = %v, want it untouched: %v", after, before)
		}
	}
}

func TestPathsHomebotDidNotMakeSurviveEveryPath(t *testing.T) {
	cases := []struct {
		name, run, out, old string
		inject              func(t *testing.T)
		wantErr             bool
	}{
		{name: "a file, changed", run: "echo fresh", out: "manifests/list.txt", old: "good\n"},
		{name: "a file, unchanged", run: "echo good", out: "manifests/list.txt", old: "good\n"},
		{name: "a file, new", run: "echo fresh", out: "manifests/list.txt"},
		{name: "a file, command fails", run: "exit 1", out: "manifests/list.txt", old: "good\n"},
		{name: "a file, the swap fails", run: "echo fresh", out: "manifests/list.txt", old: "good\n", inject: func(t *testing.T) { failRename(t, intoOut) }, wantErr: true},
		{name: "a folder, changed", run: folderCmd, out: "manifests/repos", old: "x"},
		{name: "a folder, new", run: folderCmd, out: "manifests/repos"},
		{name: "a folder, the swap fails", run: folderCmd, out: "manifests/repos", old: "x", inject: func(t *testing.T) { failRename(t, intoOut) }, wantErr: true},
		{name: "a folder, the swap fails and cannot go back", run: folderCmd, out: "manifests/repos", old: "x", inject: func(t *testing.T) { failRename(t, intoOut, restore) }, wantErr: true},
	}
	for _, c := range cases {
		for _, folder := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, squatters folders=%v", c.name, folder), func(t *testing.T) {
				sb := setup(t, c.run, c.out)
				if c.old != "" {
					rel := c.out + "/old"
					if strings.HasSuffix(c.out, ".txt") {
						rel = c.out
					}
					sb.Files(map[string]string{"home/.home/" + rel: c.old})
				}
				check := squat(t, sb, c.out, folder)
				if c.inject != nil {
					c.inject(t)
				}
				_, err := runErr(t, sb)
				if (err != nil) != c.wantErr {
					t.Fatalf("RunExports error = %v, want error: %v", err, c.wantErr)
				}
				check()
			})
		}
	}
}

// ---- links at out are replaced, never followed

func TestLinkAtOutIsReplacedAndItsTargetNeverTouched(t *testing.T) {
	for _, kind := range []string{"file", "folder"} {
		for _, target := range []string{"dangling", "valid, other content", "valid, same content"} {
			t.Run(kind+" export, "+target+" link", func(t *testing.T) {
				run, out := "echo fresh", "manifests/list.txt"
				content := "fresh\n"
				if kind == "folder" {
					run, out = folderCmd, "manifests/repos"
				}
				sb := setup(t, run, out)
				outside := t.TempDir()
				dest := filepath.Join(outside, "target")
				switch {
				case target == "dangling":
				case kind == "file":
					body := "theirs\n"
					if strings.HasSuffix(target, "same content") {
						body = content
					}
					if err := os.WriteFile(dest, []byte(body), 0o644); err != nil {
						t.Fatal(err)
					}
				default:
					if err := os.MkdirAll(dest, 0o755); err != nil {
						t.Fatal(err)
					}
					body := map[string]string{"theirs": "theirs\n"}
					if strings.HasSuffix(target, "same content") {
						body = map[string]string{"y": "2", "a/x": "1"}
					}
					for name, b := range body {
						if err := os.MkdirAll(filepath.Dir(filepath.Join(dest, name)), 0o755); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(dest, name), []byte(b), 0o644); err != nil {
							t.Fatal(err)
						}
					}
				}
				before := tree(t, outside)
				p := filepath.Join(sb.Root, out)
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(dest, p); err != nil {
					t.Fatal(err)
				}
				r := one(t, sb)
				eq(t, r.OK && r.Changed, true)
				fi, err := os.Lstat(p)
				if err != nil || fi.Mode()&os.ModeSymlink != 0 {
					t.Fatalf("out = %v, %v; want the link replaced by the export", fi, err)
				}
				if kind == "file" {
					eq(t, read(t, sb, out), content)
				} else {
					eq(t, files(t, sb, out), []string{"a/x", "y"})
				}
				if after := tree(t, outside); len(after) != len(before) {
					t.Errorf("outside = %v, want %v: the link was followed", after, before)
				} else {
					for k, v := range before {
						if after[k] != v {
							t.Errorf("outside %q = %q, want %q", k, after[k], v)
						}
					}
				}
				eq(t, workLeft(t, sb, out), []string(nil))
			})
		}
	}
}

// A link to something non-empty is replaced even by an empty export: the
// guard that keeps a good file is for a file homebot wrote, not a link.
func TestLinkToANonEmptyFileIsReplacedByAnEmptyExport(t *testing.T) {
	sb := setup(t, "true", "manifests/list.txt")
	dest := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(dest, []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(sb.Root, "manifests/list.txt")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dest, p); err != nil {
		t.Fatal(err)
	}
	r := one(t, sb)
	eq(t, r.OK && r.Changed, true)
	eq(t, read(t, sb, "manifests/list.txt"), "")
	if b, _ := os.ReadFile(dest); string(b) != "theirs\n" {
		t.Errorf("target = %q, want it untouched", b)
	}
}

// The TypeScript tested the command's output with existsSync, which follows
// links: a command that links {out} to nothing wrote no output. Homebot does
// the same, so a dangling link never replaces the last good export.
func TestDanglingLinkAsTheCommandsOutputIsNoOutput(t *testing.T) {
	sb := setup(t, "ln -s /nonexistent/nowhere {out}", "manifests/list.txt")
	sb.Files(map[string]string{"home/.home/manifests/list.txt": "good\n"})
	r := one(t, sb)
	eq(t, r.OK, false)
	eq(t, r.Error, "the command did not write manifests/list.txt, the file given to it as {out}")
	eq(t, read(t, sb, "manifests/list.txt"), "good\n")
	eq(t, workLeft(t, sb, "manifests/list.txt"), []string(nil))
}
