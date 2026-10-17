package guard

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

const refusalLine = "homebot: homebot isn't installed, so this commit can't be checked. Refusing.\n"

func gitRepo(t *testing.T) *testutil.Box {
	t.Helper()
	b := testutil.Sandbox(t, testutil.Options{Git: true})
	b.GitIn(b.Root, "add", "-A")
	b.GitIn(b.Root, "commit", "-qm", "init")
	return b
}

func install(t *testing.T, b *testutil.Box) bool {
	t.Helper()
	changed, err := InstallHooks(b.Ctx)
	if err != nil {
		t.Fatalf("InstallHooks: %v", err)
	}
	return changed
}

func installedRecorded(t *testing.T, b *testutil.Box) string {
	t.Helper()
	recorded, ok := installedShim(b.Ctx)
	if !ok {
		t.Fatal("the shim is not installed")
	}
	return recorded
}

func TestInstallHooks(t *testing.T) {
	b := gitRepo(t)
	b.WithStubs(map[string]string{"homebot": "exit 0"})
	if Installed(b.Ctx) {
		t.Fatal("Installed before InstallHooks")
	}
	if !install(t, b) {
		t.Error("first InstallHooks reported no change")
	}
	dir := filepath.Join(b.Dir, "xdg", "data", "homebot", "hooks")
	if got := strings.TrimSpace(b.GitIn(b.Root, "config", "--local", "--get", "core.hooksPath")); got != dir {
		t.Errorf("core.hooksPath = %q, want %q", got, dir)
	}
	info, err := os.Stat(filepath.Join(dir, "pre-commit"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("shim mode %v, want 0755", info.Mode().Perm())
	}
	if got := installedRecorded(t, b); got != filepath.Join(b.Bin, "homebot") {
		t.Errorf("recorded %q, want the homebot on PATH", got)
	}
	if got, ok := RecordedPath(b.Ctx); !ok || got != filepath.Join(b.Bin, "homebot") {
		t.Errorf("RecordedPath = %q, %v", got, ok)
	}
	if !Installed(b.Ctx) {
		t.Error("Installed = false after InstallHooks")
	}
	if install(t, b) {
		t.Error("second InstallHooks reported a change")
	}
}

func TestInstalledIsFalseUnlessEverythingMatches(t *testing.T) {
	cases := []struct {
		name  string
		spoil func(t *testing.T, b *testutil.Box)
	}{
		{"hooksPath unset", func(t *testing.T, b *testutil.Box) {
			b.GitIn(b.Root, "config", "--local", "--unset", "core.hooksPath")
		}},
		{"hooksPath elsewhere", func(t *testing.T, b *testutil.Box) {
			b.GitIn(b.Root, "config", "--local", "core.hooksPath", filepath.Join(b.Dir, "elsewhere"))
		}},
		{"shim missing", func(t *testing.T, b *testutil.Box) {
			if err := os.Remove(shimPath(b.Ctx)); err != nil {
				t.Fatal(err)
			}
		}},
		{"shim not executable", func(t *testing.T, b *testutil.Box) {
			if err := os.Chmod(shimPath(b.Ctx), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"shim not executable by its owner", func(t *testing.T, b *testutil.Box) {
			// git runs a hook only when its user may execute it.
			if err := os.Chmod(shimPath(b.Ctx), 0o655); err != nil {
				t.Fatal(err)
			}
		}},
		{"shim edited", func(t *testing.T, b *testutil.Box) {
			appendTo(t, shimPath(b.Ctx), "echo extra\n")
		}},
		{"recorded path breaks out of its quotes", func(t *testing.T, b *testutil.Box) {
			text := shim(b.Ctx, "/x y/homebot")
			text = strings.Replace(text, "recorded='/x y/homebot'", "recorded='/x y/homebot'; touch /tmp/pwned; ''", 1)
			writeExec(t, shimPath(b.Ctx), text)
		}},
		{"recorded path unquoted", func(t *testing.T, b *testutil.Box) {
			text := strings.Replace(shim(b.Ctx, "/x/homebot"), "recorded=/x/homebot", "recorded=/x/homebot; touch /tmp/pwned", 1)
			writeExec(t, shimPath(b.Ctx), text)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := gitRepo(t)
			b.WithStubs(map[string]string{"homebot": "exit 0"})
			install(t, b)
			before := readFile(t, shimPath(b.Ctx))
			tc.spoil(t, b)
			if after, err := os.ReadFile(shimPath(b.Ctx)); err == nil && string(after) == before && strings.HasPrefix(tc.name, "recorded") {
				t.Fatal("the spoiler did not change the shim")
			}
			if Installed(b.Ctx) {
				t.Fatal("Installed = true")
			}
			if got, ok := RecordedPath(b.Ctx); ok || got != "" {
				t.Errorf("RecordedPath of spoiled shim = %q, %v", got, ok)
			}
			if !install(t, b) {
				t.Error("InstallHooks did not repair it")
			}
			if !Installed(b.Ctx) {
				t.Error("Installed = false after the repair")
			}
		})
	}
}

// A directory where the shim goes is not a shim, and InstallHooks does not
// delete it to make room: it says it could not write the shim.
func TestInstallHooksWontReplaceADirectory(t *testing.T) {
	b := gitRepo(t)
	b.WithStubs(map[string]string{"homebot": "exit 0"})
	install(t, b)
	if err := os.Remove(shimPath(b.Ctx)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(shimPath(b.Ctx), 0o755); err != nil {
		t.Fatal(err)
	}
	if Installed(b.Ctx) {
		t.Error("Installed = true with a directory for a shim")
	}
	if changed, err := InstallHooks(b.Ctx); err == nil || changed {
		t.Errorf("InstallHooks = %v, %v; want an error", changed, err)
	}
}

// Quoting round-trips through recordedIn for awkward paths.
func TestRecordedPathsRoundTrip(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	for _, p := range []string{"/usr/local/bin/homebot", "/Users/a b/bin/homebot", "/tmp/it's/homebot", "", "/a/$HOME/`x`/homebot"} {
		got, ok := recordedIn(b.Ctx, shim(b.Ctx, p))
		if !ok || got != p {
			t.Errorf("recordedIn(shim(%q)) = %q, %v", p, got, ok)
		}
	}
}

// A shim whose recorded homebot is gone is rewritten with where homebot is
// now.
func TestInstallHooksRewritesWhenTheRecordedHomebotIsGone(t *testing.T) {
	b := gitRepo(t)
	b.WithStubs(map[string]string{"homebot": "exit 0"})
	install(t, b)
	if err := os.Remove(filepath.Join(b.Bin, "homebot")); err != nil {
		t.Fatal(err)
	}
	b.Ctx.Exe = filepath.Join(b.Dir, "build", "homebot")
	if !install(t, b) {
		t.Fatal("InstallHooks left a shim pointing at a removed homebot")
	}
	if got := installedRecorded(t, b); got != b.Ctx.Exe {
		t.Errorf("recorded %q, want %q", got, b.Ctx.Exe)
	}
}

// A shim whose recorded homebot still runs is left alone when this run
// found homebot elsewhere: a terminal and the background helper can see
// different PATHs, and neither should keep rewriting the other's choice.
func TestInstallHooksKeepsARecordedHomebotThatStillRuns(t *testing.T) {
	b := gitRepo(t)
	built := filepath.Join(b.Dir, "build", "homebot")
	writeExec(t, built, "#!/bin/sh\nexit 0\n")
	b.Ctx.Exe = built
	install(t, b)
	b.WithStubs(map[string]string{"homebot": "exit 0"})
	if install(t, b) {
		t.Error("InstallHooks rewrote a shim whose homebot still runs")
	}
	if got := installedRecorded(t, b); got != built {
		t.Errorf("recorded %q, want %q", got, built)
	}
}

func TestInstallHooksRewritesExistingVersionedHomebot(t *testing.T) {
	b := gitRepo(t)
	prefix := filepath.Join(b.Dir, "brew prefix")
	versioned := filepath.Join(prefix, "Caskroom", "homebot", "0.1.0", "homebot")
	stable := filepath.Join(prefix, "bin", "homebot")
	writeExec(t, versioned, "#!/bin/sh\nexit 0\n")
	b.Ctx.Exe = versioned
	if !install(t, b) || installedRecorded(t, b) != versioned {
		t.Fatal("initial shim did not record the versioned binary")
	}
	if install(t, b) {
		t.Fatal("unchanged versioned shim was rewritten before a stable link existed")
	}
	if err := os.MkdirAll(filepath.Dir(stable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(versioned, stable); err != nil {
		t.Fatal(err)
	}
	if !install(t, b) {
		t.Fatal("versioned shim was not rewritten while its binary still existed")
	}
	if got := installedRecorded(t, b); got != stable {
		t.Errorf("recorded %q, want stable path %q", got, stable)
	}
	if install(t, b) {
		t.Fatal("stable shim was rewritten")
	}
}

func TestInstallHooksOutsideARepoFails(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	changed, err := InstallHooks(b.Ctx)
	if err == nil || changed {
		t.Errorf("InstallHooks = %v, %v; want an error", changed, err)
	}
	if Installed(b.Ctx) {
		t.Error("Installed outside a repo")
	}
}

func TestShimTemplate(t *testing.T) {
	for _, tok := range []string{recordedToken, refusalToken} {
		if n := strings.Count(shimTemplate, tok); n != 1 {
			t.Errorf("the template holds %s %d times, want once", tok, n)
		}
	}
	b := testutil.Sandbox(t, testutil.Options{})
	path := filepath.Join(b.Dir, "shim")
	writeExec(t, path, shim(b.Ctx, "/it's here/homebot"))
	if r := proc.Run(b.Ctx, []string{"sh", "-n", path}, proc.Options{}); r.Code != 0 {
		t.Errorf("sh -n: %s", r.Stderr)
	}
	if text := shim(b.Ctx, "/x"); strings.Contains(text, recordedToken) || strings.Contains(text, refusalToken) {
		t.Errorf("a token is left in the shim:\n%s", text)
	}
}

// shimBox is a repo with a shim written to $WORK/shim/pre-commit, run with
// only the system's directories on PATH unless a test says otherwise.
type shimBox struct {
	*testutil.Box
	shim string
}

// newShimBox writes the shim with the path recorded returns for the box.
func newShimBox(t *testing.T, recorded func(b *testutil.Box) string) *shimBox {
	t.Helper()
	for _, p := range []string{"/usr/bin/homebot", "/bin/homebot"} {
		if _, err := os.Stat(p); err == nil {
			t.Skipf("%s exists, so homebot is always on PATH", p)
		}
	}
	b := gitRepo(t)
	path := filepath.Join(b.Dir, "shim", "pre-commit")
	writeExec(t, path, shim(b.Ctx, recorded(b)))
	return &shimBox{Box: b, shim: path}
}

func fixed(path string) func(*testutil.Box) string {
	return func(*testutil.Box) string { return path }
}

// inRec is $WORK/rec/homebot, a recorded homebot off every PATH.
func inRec(b *testutil.Box) string { return filepath.Join(b.Dir, "rec", "homebot") }

func (s *shimBox) run(t *testing.T, env map[string]string) proc.Result {
	t.Helper()
	full := map[string]string{"PATH": "/usr/bin:/bin"}
	for k, v := range env {
		full[k] = v
	}
	return proc.Run(s.Ctx, []string{s.shim, "arg1"}, proc.Options{Dir: s.Root, Env: full})
}

func (s *shimBox) withPath() map[string]string {
	return map[string]string{"PATH": s.Bin + ":/usr/bin:/bin"}
}

func stub(t *testing.T, path, body string) {
	t.Helper()
	writeExec(t, path, "#!/bin/sh\n"+body+"\n")
}

func TestShimPrefersTheHomebotOnPath(t *testing.T) {
	s := newShimBox(t, inRec)
	stub(t, filepath.Join(s.Dir, "rec", "homebot"), `echo "recorded $*" >&2`)
	stub(t, filepath.Join(s.Bin, "homebot"), `echo "on path $*" >&2`)
	r := s.run(t, s.withPath())
	if r.Code != 0 || r.Stderr != "on path hook pre-commit\n" {
		t.Errorf("shim = %d, %q", r.Code, r.Stderr)
	}
}

func TestShimFallsBackToTheRecordedHomebot(t *testing.T) {
	s := newShimBox(t, inRec)
	stub(t, filepath.Join(s.Dir, "rec", "homebot"), `echo "recorded $*" >&2`)
	r := s.run(t, nil)
	if r.Code != 0 || r.Stderr != "recorded hook pre-commit\n" {
		t.Errorf("shim = %d, %q", r.Code, r.Stderr)
	}
}

func TestShimFallsBackToLocalBin(t *testing.T) {
	skipIfInstalled(t)
	s := newShimBox(t, fixed("/nowhere/homebot"))
	home := filepath.Join(s.Dir, "fakehome")
	stub(t, filepath.Join(home, ".local", "bin", "homebot"), `echo "local $*" >&2`)
	r := s.run(t, map[string]string{"HOME": home})
	if r.Code != 0 || r.Stderr != "local hook pre-commit\n" {
		t.Errorf("shim = %d, %q", r.Code, r.Stderr)
	}
}

// A recorded path that is not an executable file is passed over.
func TestShimSkipsARecordedPathThatIsNotExecutable(t *testing.T) {
	skipIfInstalled(t)
	s := newShimBox(t, inRec)
	recorded := filepath.Join(s.Dir, "rec", "homebot")
	if err := os.MkdirAll(filepath.Dir(recorded), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recorded, []byte("#!/bin/sh\necho ran >&2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := s.run(t, map[string]string{"HOME": filepath.Join(s.Dir, "empty")})
	if r.Code != 1 || r.Stderr != refusalLine {
		t.Errorf("shim = %d, %q", r.Code, r.Stderr)
	}
}

func TestShimRefusesWhenNoHomebotIsFound(t *testing.T) {
	skipIfInstalled(t)
	s := newShimBox(t, fixed("/nowhere/homebot"))
	writeExec(t, filepath.Join(s.Root, ".git", "hooks", "pre-commit"), "#!/bin/sh\necho own >&2\n")
	r := s.run(t, map[string]string{"HOME": filepath.Join(s.Dir, "empty")})
	if r.Code != 1 || r.Stderr != refusalLine {
		t.Errorf("shim = %d, %q; want 1 and %q", r.Code, r.Stderr, refusalLine)
	}
}

func TestShimPropagatesTheGuardsRefusal(t *testing.T) {
	s := newShimBox(t, fixed(""))
	stub(t, filepath.Join(s.Bin, "homebot"), `echo refused >&2; exit 3`)
	writeExec(t, filepath.Join(s.Root, ".git", "hooks", "pre-commit"), "#!/bin/sh\necho own >&2\n")
	r := s.run(t, s.withPath())
	if r.Code != 3 || r.Stderr != "refused\n" {
		t.Errorf("shim = %d, %q; want 3 and only the refusal", r.Code, r.Stderr)
	}
}

func TestShimRunsTheReposOwnHookAfterTheGuard(t *testing.T) {
	s := newShimBox(t, fixed(""))
	stub(t, filepath.Join(s.Bin, "homebot"), `echo guard >&2`)
	writeExec(t, filepath.Join(s.Root, ".git", "hooks", "pre-commit"), "#!/bin/sh\necho \"own $*\" >&2\nexit 4\n")
	r := s.run(t, s.withPath())
	if r.Code != 4 || r.Stderr != "guard\nown arg1\n" {
		t.Errorf("shim = %d, %q; want 4 and the guard then the own hook", r.Code, r.Stderr)
	}
}

// A repo's own hook that is not executable is skipped, as git skips it.
func TestShimSkipsANonExecutableOwnHook(t *testing.T) {
	s := newShimBox(t, fixed(""))
	stub(t, filepath.Join(s.Bin, "homebot"), `exit 0`)
	p := filepath.Join(s.Root, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := s.run(t, s.withPath()); r.Code != 0 || r.Stderr != "" {
		t.Errorf("shim = %d, %q", r.Code, r.Stderr)
	}
}

// The fixed fallbacks are real system paths; a machine with homebot in one
// of them can't test what comes after.
func skipIfInstalled(t *testing.T) {
	t.Helper()
	for _, p := range []string{"/opt/homebrew/bin/homebot", "/usr/local/bin/homebot"} {
		if _, err := os.Stat(p); err == nil {
			t.Skipf("%s exists", p)
		}
	}
}

func writeExec(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// A core.hooksPath that beats the local one (here a worktree config) would
// keep the guard from running; InstallHooks says so instead of reporting
// success.
func TestInstallHooksFailsWhenAnotherHooksPathWins(t *testing.T) {
	b := gitRepo(t)
	b.WithStubs(map[string]string{"homebot": "exit 0"})
	b.GitIn(b.Root, "config", "extensions.worktreeConfig", "true")
	b.GitIn(b.Root, "config", "--worktree", "core.hooksPath", "/elsewhere")
	changed, err := InstallHooks(b.Ctx)
	if err == nil || changed {
		t.Fatalf("InstallHooks = %v, %v; want an error", changed, err)
	}
	want := "core.hooksPath is /elsewhere after homebot set it to " + hooksDir(b.Ctx)
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q, want it to hold %q", err, want)
	}
	if Installed(b.Ctx) {
		t.Error("Installed = true")
	}
}

// When the shim finds homebot in a fixed directory, the caller's PATH was
// too thin to find it, and too thin to find gitleaks: the shim adds that
// directory (and, on macOS, Homebrew's) at the end of PATH, so the
// caller's own tools still win.
func TestShimWidensAThinPath(t *testing.T) {
	s := newShimBox(t, inRec)
	rec := filepath.Join(s.Dir, "rec")
	stub(t, filepath.Join(rec, "homebot"), `echo "$PATH" >&2`)
	r := s.run(t, nil)
	want := "/usr/bin:/bin:" + rec + "\n"
	if runtime.GOOS == "darwin" {
		want = "/usr/bin:/bin:" + rec + ":/opt/homebrew/bin:/usr/local/bin\n"
	}
	if r.Code != 0 || r.Stderr != want {
		t.Errorf("shim = %d, PATH %q; want %q", r.Code, r.Stderr, want)
	}
}

// homebot found on PATH runs with PATH as it was.
func TestShimKeepsAPathThatFoundHomebot(t *testing.T) {
	s := newShimBox(t, fixed(""))
	stub(t, filepath.Join(s.Bin, "homebot"), `echo "$PATH" >&2`)
	if r := s.run(t, s.withPath()); r.Code != 0 || r.Stderr != s.Bin+":/usr/bin:/bin\n" {
		t.Errorf("shim = %d, PATH %q", r.Code, r.Stderr)
	}
}

// Run through the user's name on macOS, c.Exe is the name's link
// (~/.local/bin/zork -> homebot): the shim records the stable homebot it
// points at, never the name, which a rename would break (naming plan D4).
func TestInstallHooksRecordsTheStablePathWhenRunThroughTheName(t *testing.T) {
	for name, target := range map[string]func(b *testutil.Box) (link, stable string){
		"front door": func(b *testutil.Box) (string, string) {
			return "homebot", filepath.Join(b.Home, ".local", "bin", "homebot")
		},
		"cask": func(b *testutil.Box) (string, string) {
			stable := filepath.Join(b.Dir, "opt", "homebrew", "bin", "homebot")
			return stable, stable
		},
	} {
		t.Run(name, func(t *testing.T) {
			b := gitRepo(t)
			link, stable := target(b)
			writeExec(t, stable, "#!/bin/sh\nexit 0\n")
			zork := filepath.Join(b.Home, ".local", "bin", "zork")
			if err := os.MkdirAll(filepath.Dir(zork), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(link, zork); err != nil {
				t.Fatal(err)
			}
			b.Ctx.Exe = zork
			install(t, b)
			if got := installedRecorded(t, b); got != stable {
				t.Errorf("recorded %q, want %q", got, stable)
			}
		})
	}
}
