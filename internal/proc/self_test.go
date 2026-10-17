package proc_test

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// selfExeVar makes the test binary, run as a helper, print the os.Executable
// it sees and the SelfPath it computes with an empty PATH, then exit
// (TestSelfPathOfTheRunningBinary).
const selfExeVar = "HOMEBOT_TEST_SELF_EXE"

func TestMain(m *testing.M) {
	if os.Getenv(selfExeVar) == "1" {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		c := &app.Context{Exe: exe, Env: app.NewEnv(map[string]string{"PATH": ""})}
		fmt.Printf("%s\n%s\n", exe, proc.SelfPath(c))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestSelfPath(t *testing.T) {
	t.Run("homebot on PATH wins over the running binary", func(t *testing.T) {
		b := testutil.Sandbox(t, testutil.Options{})
		b.Ctx.Exe = "/opt/elsewhere/homebot"
		onPath := filepath.Join(b.Bin, "homebot")
		if err := os.WriteFile(onPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": b.Bin})
		if got := proc.SelfPath(b.Ctx); got != onPath {
			t.Errorf("SelfPath = %q, want %q", got, onPath)
		}
	})
	t.Run("a symlink on PATH is not resolved", func(t *testing.T) {
		b := testutil.Sandbox(t, testutil.Options{})
		real := filepath.Join(b.Dir, "Cellar", "1.0", "homebot")
		if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(b.Bin, "homebot")
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}
		b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": b.Bin})
		if got := proc.SelfPath(b.Ctx); got != link {
			t.Errorf("SelfPath = %q, want the link %q", got, link)
		}
	})
	t.Run("without homebot on PATH it is the running binary", func(t *testing.T) {
		b := testutil.Sandbox(t, testutil.Options{})
		b.Ctx.Exe = "/opt/elsewhere/homebot"
		b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": b.Bin})
		if got := proc.SelfPath(b.Ctx); got != "/opt/elsewhere/homebot" {
			t.Errorf("SelfPath = %q, want c.Exe", got)
		}
	})
}

// layout is a sandbox where homebot is not on PATH and c.Exe is the name's
// link, as os.Executable gives it on macOS when the user types the name.
type layout struct {
	b *testutil.Box
	// bin is ~/.local/bin.
	bin string
}

func newLayout(t *testing.T) layout {
	t.Helper()
	b := testutil.Sandbox(t, testutil.Options{})
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": b.Bin})
	bin := filepath.Join(b.Home, ".local", "bin")
	mkdirs(t, bin)
	return layout{b: b, bin: bin}
}

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func executable(t *testing.T, path string) {
	t.Helper()
	mkdirs(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	mkdirs(t, filepath.Dir(link))
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// The name's link run from the front door's layout: ~/.local/bin/zork ->
// homebot (relative) -> the real ~/.local/bin/homebot.
func TestSelfPathFollowsTheNamesLinkOneHopFrontDoor(t *testing.T) {
	l := newLayout(t)
	stable := filepath.Join(l.bin, "homebot")
	executable(t, stable)
	zork := filepath.Join(l.bin, "zork")
	symlink(t, "homebot", zork)
	l.b.Ctx.Exe = zork
	if got := proc.SelfPath(l.b.Ctx); got != stable {
		t.Errorf("SelfPath = %q, want %q", got, stable)
	}
}

// Homebrew's cask: ~/.local/bin/zork -> <p>/bin/homebot ->
// <p>/Caskroom/homebot/<v>/homebot. One hop reaches the stable link and
// stops there: the versioned path would break at the next upgrade.
func TestSelfPathFollowsTheNamesLinkOneHopCaskroom(t *testing.T) {
	for _, version := range []string{"0.1.0", "0.1.0-rc.1", "0.1.0_1"} {
		t.Run(version, func(t *testing.T) {
			l := newLayout(t)
			prefix := filepath.Join(l.b.Dir, "opt", "home brew")
			versioned := filepath.Join(prefix, "Caskroom", "homebot", version, "homebot")
			executable(t, versioned)
			stable := filepath.Join(prefix, "bin", "homebot")
			symlink(t, versioned, stable)
			zork := filepath.Join(l.bin, "zork")
			symlink(t, stable, zork)
			l.b.Ctx.Exe = zork
			got := proc.SelfPath(l.b.Ctx)
			if got != stable {
				t.Errorf("SelfPath = %q, want %q", got, stable)
			}
			if filepath.Base(got) != "homebot" || strings.HasPrefix(got, filepath.Join(prefix, "Caskroom")) {
				t.Errorf("SelfPath = %q: its base must be homebot, and never the Caskroom path", got)
			}
		})
	}
}

func TestSelfPathNeverGivesTheNamesLink(t *testing.T) {
	t.Run("a relative hop out of the folder", func(t *testing.T) {
		l := newLayout(t)
		stable := filepath.Join(l.b.Dir, "opt", "bin", "homebot")
		executable(t, stable)
		zork := filepath.Join(l.bin, "zork")
		symlink(t, filepath.Join("..", "..", "..", "opt", "bin", "homebot"), zork)
		l.b.Ctx.Exe = zork
		if got := proc.SelfPath(l.b.Ctx); got != stable {
			t.Errorf("SelfPath = %q, want %q", got, stable)
		}
	})
	t.Run("a dangling hop still names homebot", func(t *testing.T) {
		l := newLayout(t)
		zork := filepath.Join(l.bin, "zork")
		symlink(t, "homebot", zork)
		l.b.Ctx.Exe = zork
		if got := proc.SelfPath(l.b.Ctx); got != filepath.Join(l.bin, "homebot") {
			t.Errorf("SelfPath = %q", got)
		}
	})
}

// Exactly one hop: a chain zork -> hb -> homebot is not followed to its
// end, and a binary with no path named homebot is all there is.
func TestSelfPathFallsBackToExeWhenNoHopNamesHomebot(t *testing.T) {
	t.Run("two hops", func(t *testing.T) {
		l := newLayout(t)
		executable(t, filepath.Join(l.bin, "homebot"))
		symlink(t, "homebot", filepath.Join(l.bin, "hb"))
		zork := filepath.Join(l.bin, "zork")
		symlink(t, "hb", zork)
		l.b.Ctx.Exe = zork
		if got := proc.SelfPath(l.b.Ctx); got != "" {
			t.Errorf("SelfPath = %q, want no unstable path", got)
		}
	})
	t.Run("a renamed build", func(t *testing.T) {
		l := newLayout(t)
		built := filepath.Join(l.b.Dir, "build", "hb")
		executable(t, built)
		l.b.Ctx.Exe = built
		if got := proc.SelfPath(l.b.Ctx); got != "" {
			t.Errorf("SelfPath = %q, want no unstable path", got)
		}
	})
	t.Run("no Exe", func(t *testing.T) {
		l := newLayout(t)
		l.b.Ctx.Exe = ""
		if got := proc.SelfPath(l.b.Ctx); got != "" {
			t.Errorf("SelfPath = %q, want empty", got)
		}
	})
}

// macOS behaviour 1 (naming plan, "Tests, everywhere"): os.Executable is
// the invoked link on macOS and the resolved binary on Linux. The test
// binary, copied as homebot and run through a zork link, reports both, and
// SelfPath is the copy on either system.
func TestSelfPathOfTheRunningBinary(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	stable := filepath.Join(dir, "bin", "homebot")
	copyFile(t, self, stable)
	zork := filepath.Join(dir, "local", "zork")
	symlink(t, stable, zork)

	cmd := exec.Command(zork)
	cmd.Env = append(os.Environ(), selfExeVar+"=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("running the helper: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		t.Fatalf("helper said %q", out)
	}
	exe, selfPath := lines[0], lines[1]
	switch runtime.GOOS {
	case "darwin":
		if exe != zork {
			t.Errorf("os.Executable on macOS = %q, want the invoked link %q", exe, zork)
		}
	case "linux":
		if !sameFile(t, exe, stable) || filepath.Base(exe) != "homebot" {
			t.Errorf("os.Executable on Linux = %q, want the resolved copy %q", exe, stable)
		}
	}
	if !sameFile(t, selfPath, stable) || filepath.Base(selfPath) != "homebot" {
		t.Errorf("SelfPath = %q, want %q", selfPath, stable)
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	mkdirs(t, filepath.Dir(to))
	in, err := os.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := in.Close(); err != nil {
			t.Errorf("close source: %v", err)
		}
	}()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func sameFile(t *testing.T, a, b string) bool {
	t.Helper()
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	return err == nil && os.SameFile(ai, bi)
}

// The program log (naming plan D3): every Run, RunAttached, LookPath and
// HasCommand writes its program's base name; SelfPath writes nothing.
func TestProgramLog(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	b.WithStubs(map[string]string{"fake-tool": "exit 0"})
	log := filepath.Join(b.Dir, "programs")
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{proc.ProgramLogVar: log})
	b.Ctx.Exe = filepath.Join(b.Dir, "build", "homebot")

	proc.Run(b.Ctx, []string{"fake-tool", "x"}, proc.Options{})
	proc.Run(b.Ctx, []string{"/bin/sh", "-c", "true"}, proc.Options{})
	proc.Run(b.Ctx, []string{"not-installed"}, proc.Options{})
	if _, err := proc.RunAttached(b.Ctx, []string{"true"}, proc.AttachedOptions{Env: b.Ctx.Env}); err != nil {
		t.Fatal(err)
	}
	if _, err := proc.LookPath(b.Ctx, "looked-up"); err == nil {
		t.Fatal("looked-up unexpectedly exists on PATH")
	}
	proc.HasCommand(b.Ctx, "asked-for")
	proc.SelfPath(b.Ctx)

	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(data))
	want := []string{"fake-tool", "sh", "not-installed", "true", "looked-up", "asked-for"}
	if !slices.Equal(got, want) {
		t.Errorf("logged %q, want %q", got, want)
	}
}

func TestVersioned(t *testing.T) {
	for _, path := range []string{
		"/opt/homebrew/Caskroom/homebot/0.1.0/homebot",
		"/usr/local/Cellar/homebot/0.1.0-rc.1/bin/homebot",
		"/home/linuxbrew/.linuxbrew/Caskroom/homebot/0.1.0_1/homebot",
		"/a prefix/Caskroom/homebot/0.1.0/homebot",
	} {
		if !proc.Versioned(path) {
			t.Errorf("Versioned(%q) = false", path)
		}
	}
	for _, path := range []string{
		"/opt/homebrew/bin/homebot",
		"/opt/homebrew/Caskroom/other/1.0/homebot",
		"/opt/homebrew/Caskroom/homebot/1.0/other",
		"/opt/homebrew/Cellar/other/1.0/bin/homebot",
		"Caskroom/homebot/1.0/homebot",
	} {
		if proc.Versioned(path) {
			t.Errorf("Versioned(%q) = true", path)
		}
	}
}

func TestSelfPathVersionedInstall(t *testing.T) {
	for _, prefix := range []string{"opt/homebrew", "usr/local", "home/linuxbrew/.linuxbrew", "custom prefix"} {
		for _, layout := range []string{"Caskroom", "Cellar"} {
			for _, version := range []string{"0.1.0", "0.1.0-rc.1", "0.1.0_1"} {
				t.Run(prefix+"/"+layout+"/"+version, func(t *testing.T) {
					b := testutil.Sandbox(t, testutil.Options{})
					root := filepath.Join(b.Dir, prefix)
					versioned := filepath.Join(root, layout, "homebot", version)
					if layout == "Cellar" {
						versioned = filepath.Join(versioned, "bin")
					}
					versioned = filepath.Join(versioned, "homebot")
					if err := os.MkdirAll(filepath.Dir(versioned), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(versioned, []byte("#!/bin/sh\n"), 0o755); err != nil {
						t.Fatal(err)
					}
					stable := filepath.Join(root, "bin", "homebot")
					if err := os.MkdirAll(filepath.Dir(stable), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(versioned, stable); err != nil {
						t.Fatal(err)
					}
					b.Ctx.Exe = versioned
					b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": b.Bin})
					if got := proc.SelfPath(b.Ctx); got != stable {
						t.Errorf("without homebot on PATH: got %q, want %q", got, stable)
					}
					// A PATH entry naming the Caskroom or Cellar bin itself is
					// versioned too; it must not be recorded in a unit or shim.
					b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": filepath.Dir(versioned)})
					if got := proc.SelfPath(b.Ctx); got != stable {
						t.Errorf("with versioned homebot on PATH: got %q, want %q", got, stable)
					}
					if err := os.Remove(stable); err != nil {
						t.Fatal(err)
					}
					if got := proc.SelfPath(b.Ctx); got != versioned {
						t.Errorf("without stable link: got %q, want %q", got, versioned)
					}
					if err := os.WriteFile(stable, []byte("not executable"), 0o644); err != nil {
						t.Fatal(err)
					}
					if got := proc.SelfPath(b.Ctx); got != versioned {
						t.Errorf("with nonexecutable stable link: got %q, want %q", got, versioned)
					}
				})
			}
		}
	}
}

func TestSelfPathSkipsDirectoriesOnPATH(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	if err := os.Mkdir(filepath.Join(b.Bin, "homebot"), 0o755); err != nil {
		t.Fatal(err)
	}
	b.Ctx.Exe = "/opt/elsewhere/homebot"
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": b.Bin})
	if got := proc.SelfPath(b.Ctx); got != b.Ctx.Exe {
		t.Errorf("SelfPath = %q, want %q", got, b.Ctx.Exe)
	}
}
