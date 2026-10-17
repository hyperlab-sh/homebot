package persona

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// The file rules (plan S3, I8) hold however a file is reached: Read and Load
// find it under personas/ from an id, ReadFile is given its path (persona
// validate <file>). A file that is not a regular file, or is over 256 KiB, is
// refused with V28 and never read to the end; a symlink to a regular file is
// followed. Each read runs in a goroutine with a 5 second limit, so a read
// that blocks fails the test instead of hanging it (no shell timeout: the
// macOS runner lacks it).

// within runs f, and fails the test when it does not return in five seconds.
func within[T any](t *testing.T, what string, f func() T) T {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan T, 1)
	go func() { done <- f() }()
	select {
	case out := <-done:
		return out
	case <-ctx.Done():
		t.Fatalf("%s blocked for 5 seconds", what)
		panic("unreachable")
	}
}

// readOut is what Read and ReadFile return.
type readOut struct {
	f   *File
	ds  []Diagnostic
	err error
}

// loadOut is what Load returns.
type loadOut struct {
	v   *Voice
	ds  []Diagnostic
	err error
}

// loadWithin is Load with the five second limit.
func loadWithin(t *testing.T, box *testutil.Box, id string) loadOut {
	t.Helper()
	return within(t, "Load", func() loadOut {
		v, ds, err := Load(box.Ctx, id)
		return loadOut{v, ds, err}
	})
}

// codes are the diagnostics' codes with their paths, "v28 /x/y.toml".
func codes(ds []Diagnostic) []string {
	var out []string
	for _, d := range ds {
		out = append(out, strings.TrimPrefix(d.Text.ID(), "persona.diag.")+" "+d.Path)
	}
	return out
}

// header is a valid persona file of exactly size bytes.
func header(size int) []byte {
	pad := size - len(testHeader) - len("#\n")
	return []byte(testHeader + "#" + strings.Repeat("x", pad) + "\n")
}

func TestFileRulesHoldForReadLoadAndReadFile(t *testing.T) {
	outside := t.TempDir() // where link targets live, not in personas/
	good := filepath.Join(outside, "good.toml")
	if err := os.WriteFile(good, []byte(testHeader), 0o600); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(outside, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	write := func(body []byte) func(*testing.T, string) {
		return func(t *testing.T, path string) {
			t.Helper()
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	symlink := func(target string) func(*testing.T, string) {
		return func(t *testing.T, path string) {
			t.Helper()
			if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
		}
	}
	cases := []struct {
		id    string
		make  func(t *testing.T, path string)
		valid bool
	}{
		{"regular", write([]byte(testHeader)), true},
		{"symlink", symlink(good), true},
		{"symlink-chain", func(t *testing.T, path string) {
			t.Helper()
			hop := filepath.Join(outside, "hop")
			if err := os.Symlink(good, hop); err != nil && !os.IsExist(err) {
				t.Fatal(err)
			}
			symlink(hop)(t, path)
		}, true},
		{"limit", write(header(maxFileBytes)), true},
		{"one-over", write(header(maxFileBytes + 1)), false},
		{"kib-257", write(header(257 * 1024)), false},
		{"fifo", func(t *testing.T, path string) {
			t.Helper()
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"fifo-link", symlink(fifo), false},
		{"zero", symlink("/dev/zero"), false},
		{"null", symlink("/dev/null"), false},
		{"dangling", symlink(filepath.Join(outside, "missing")), false},
		{"loop", func(t *testing.T, path string) {
			t.Helper()
			symlink(path)(t, path)
		}, false},
		{"folder", func(t *testing.T, path string) {
			t.Helper()
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			box := testutil.Sandbox(t, testutil.Options{})
			dir := filepath.Join(box.Ctx.ConfigDir, "personas")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, tc.id+".toml")
			tc.make(t, path)

			read := within(t, "Read", func() readOut {
				f, ds, err := Read(box.Ctx, tc.id)
				return readOut{f, ds, err}
			})
			byPath := within(t, "ReadFile", func() readOut {
				f, ds, err := ReadFile(box.Ctx, path)
				return readOut{f, ds, err}
			})
			load := loadWithin(t, box, tc.id)
			active := within(t, "Active", func() *Voice { return Active(asking(box, tc.id)) })

			// Read and ReadFile give the same answer (m13).
			if (read.f == nil) != (byPath.f == nil) || (read.err == nil) != (byPath.err == nil) ||
				!slices.Equal(codes(read.ds), codes(byPath.ds)) {
				t.Errorf("Read %+v and ReadFile %+v differ", codes(read.ds), codes(byPath.ds))
			}

			if tc.valid {
				if read.err != nil || read.f == nil || len(read.ds) != 0 || read.f.ID != tc.id {
					t.Errorf("Read = %+v, %v, %v", read.f, codes(read.ds), read.err)
				}
				if load.err != nil || load.v == nil || load.v.ID != tc.id {
					t.Errorf("Load = %+v, %v, %v", load.v, codes(load.ds), load.err)
				}
				if active == nil || active.ID != tc.id {
					t.Errorf("Active = %+v, want %s", active, tc.id)
				}
				return
			}
			want := []string{"v28 " + path}
			if read.err == nil || read.f != nil || !slices.Equal(codes(read.ds), want) {
				t.Errorf("Read = %+v, %v, %v, want one V28 for %s", read.f, codes(read.ds), read.err, path)
			}
			if load.err == nil || load.v != nil || !slices.Contains(codes(load.ds), want[0]) {
				t.Errorf("Load = %+v, %v, %v, want V28 for %s", load.v, codes(load.ds), load.err, path)
			}
			if load.err != nil && !strings.Contains(load.err.Error(), path) && !strings.Contains(load.err.Error(), tc.id) {
				t.Errorf("Load's error does not say which file: %v", load.err)
			}
			// The persona was asked for, so the fallback is neatpik and
			// the problem is V28.
			if active == nil || active.ID != Default {
				t.Errorf("Active = %+v, want the fallback", active)
			}
			ctx := asking(box, tc.id)
			if p := ActiveProblem(ctx); p == nil || p.ID != tc.id || p.Origin != FromEnv || p.Diag.Text.ID() != "persona.diag.v28" || p.Diag.Path != path {
				t.Errorf("ActiveProblem = %+v", p)
			}
		})
	}
}

// A bad file never poisons the personas next to it, however it is bad.
func TestAnUnreadableFileDoesNotStopTheOthers(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	dir := filepath.Join(box.Ctx.ConfigDir, "personas")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "fifo.toml"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/zero", filepath.Join(dir, "zero.toml")); err != nil {
		t.Fatal(err)
	}
	writePersona(t, box.Ctx.ConfigDir, "fine", testHeader)
	type listOut struct {
		files []*File
		err   error
	}
	listed := within(t, "List", func() listOut {
		files, _, err := List(box.Ctx)
		return listOut{files, err}
	})
	if listed.err != nil {
		t.Errorf("List: %v", listed.err)
	}
	files := listed.files
	var ids []string
	for _, f := range files {
		ids = append(ids, f.ID)
	}
	if !slices.Contains(ids, "fine") || !slices.Contains(ids, Default) || slices.Contains(ids, "fifo") || slices.Contains(ids, "zero") {
		t.Errorf("List = %v", ids)
	}
	if v, _, err := Load(box.Ctx, "fine"); err != nil || v == nil {
		t.Errorf("Load(fine) = %v, %v", v, err)
	}
}

// HOMEBOT_PERSONA=../x is refused before any path is built from it: a file
// that path would have reached is never opened.
func TestAnIdThatIsNotAnIDNeverBuildsAPath(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	// <ConfigDir>/personas/../x.toml is <ConfigDir>/x.toml.
	decoy := filepath.Join(box.Ctx.ConfigDir, "x.toml")
	if err := os.MkdirAll(filepath.Dir(decoy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(decoy, []byte(testHeader), 0o600); err != nil {
		t.Fatal(err)
	}
	writePersona(t, box.Ctx.ConfigDir, "x", testHeader)
	for _, id := range []string{"../x", "..", ".", "a/b", "/etc/passwd", "x.toml", "X", "-x", "x y", "x\x00", "x\n", "~", "$HOME", "none", ""} {
		t.Run(id, func(t *testing.T) {
			if f, ds, err := Read(box.Ctx, id); f != nil || err == nil || len(ds) != 1 || ds[0].Text.ID() != "persona.diag.v4" {
				t.Errorf("Read = %+v, %v, %v", f, codes(ds), err)
			}
			if v, ds, err := Load(box.Ctx, id); v != nil || err == nil || !hasDiagnostic(ds, "v4") {
				t.Errorf("Load = %+v, %v, %v", v, codes(ds), err)
			}
			if id == "" || id == None {
				return
			}
			ctx := asking(box, id)
			if got := Active(ctx); got == nil || got.ID != Default {
				t.Errorf("Active = %+v, want the fallback", got)
			}
			p := ActiveProblem(ctx)
			if p == nil || p.ID != id || p.Origin != FromEnv || p.Diag.Text.ID() != "persona.diag.v4" {
				t.Errorf("ActiveProblem = %+v", p)
			}
			if _, ok := TakeWarning(ctx); !ok && id == "../x" {
				t.Error("no warning for an unusable HOMEBOT_PERSONA")
			}
		})
	}
	// The warning about an unusable HOMEBOT_PERSONA is once per process
	// (the flag is shared by every Fork): the first of these took it.
	if _, ok := TakeWarning(asking(box, "../x")); ok {
		t.Error("a second warning in one process")
	}
	// persona: in homebot.yml is held to the same rule.
	ctx := asking(box, "")
	ctx.Config = &config.Config{Persona: "../x"}
	if got := Active(ctx); got == nil || got.ID != Default {
		t.Errorf("Active with persona: ../x = %+v", got)
	}
	if p := ActiveProblem(ctx); p == nil || p.Origin != FromConfig {
		t.Errorf("ActiveProblem = %+v", p)
	}
}

// V26 (plan S5, I8): a variant longer than 4 × the English + 200 runes is
// dropped when the persona loads. One at the limit stays (W5 only warns).
func TestOverlongVariantIsDroppedAtLoad(t *testing.T) {
	// "Commit refused: {reason}" is 24 runes: the cap is 296.
	const id = "save.error.refused"
	atCap := strings.Repeat("x", 287) + " {reason}"
	over := strings.Repeat("x", 288) + " {reason}"
	atCapWide := strings.Repeat("é", 287) + " {reason}" // 296 runes, 574 bytes
	overWide := strings.Repeat("é", 288) + " {reason}"
	box := testutil.Sandbox(t, testutil.Options{})
	body := testHeader + "[lines]\n'" + id + "' = ['Fine, {reason}', '" + atCap + "', '" + over + "', '" + atCapWide + "', '" + overWide + "']\n"
	writePersona(t, box.Ctx.ConfigDir, "tester", body)

	got := loadWithin(t, box, "tester")
	v, ds, err := got.v, got.ds, got.err
	if err != nil || v == nil {
		t.Fatalf("Load = %v, %v, %v: a long variant must not make the persona unusable", v, codes(ds), err)
	}
	var kept []string
	for _, l := range v.Lines[id] {
		kept = append(kept, l.Text)
	}
	if want := []string{"Fine, {reason}", atCap, atCapWide}; !slices.Equal(kept, want) {
		t.Errorf("kept %d variants, want the three that fit: %q", len(kept), kept)
	}
	var capped []int
	for _, d := range ds {
		if d.Text.ID() == "persona.diag.v26" {
			if d.Severity != Error || d.LineID != id {
				t.Errorf("V26 = %+v", d)
			}
			capped = append(capped, d.Variant)
		}
	}
	if want := []int{3, 5}; !slices.Equal(capped, want) {
		t.Errorf("V26 for variants %v, want %v", capped, want)
	}
	if !hasDiagnostic(ds, "w5") {
		t.Errorf("no W5 for the variants at the cap: %v", codes(ds))
	}
}
