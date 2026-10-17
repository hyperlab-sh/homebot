package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// tempOptions returns Options for a home under a fresh temp dir, with only
// HOME set, a fixed clock and no test hooks.
func tempOptions(t *testing.T) (Options, string) {
	t.Helper()
	work := t.TempDir()
	home := filepath.Join(work, "home")
	return Options{
		Env:  NewEnv(map[string]string{"HOME": home}),
		Host: "box",
		Now:  func() time.Time { return time.Unix(0, 0) },
	}, work
}

// releaseBuild makes the test hooks behave as in a binary built by go build.
func releaseBuild(t *testing.T) {
	t.Helper()
	saved := inTest
	inTest = func() bool { return false }
	t.Cleanup(func() { inTest = saved })
}

func mustNew(t *testing.T, o Options) *Context {
	t.Helper()
	c, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestDirsDefaultUnderHome(t *testing.T) {
	o, work := tempOptions(t)
	c := mustNew(t, o)
	home := filepath.Join(work, "home")
	for name, got := range map[string]string{
		filepath.Join(home, ".local/state/homebot"): c.StateDir,
		filepath.Join(home, ".cache/homebot"):       c.CacheDir,
		filepath.Join(home, ".local/share/homebot"): c.DataDir,
		filepath.Join(home, ".config/homebot"):      c.ConfigDir,
	} {
		if got != name {
			t.Errorf("got %s, want %s", got, name)
		}
	}
	if c.Home != home {
		t.Errorf("Home = %s, want $HOME %s", c.Home, home)
	}
}

func TestDirsFollowXDG(t *testing.T) {
	o, work := tempOptions(t)
	x := filepath.Join(work, "xdg")
	o.Env = o.Env.With(map[string]string{
		"XDG_STATE_HOME":  x + "/state",
		"XDG_CACHE_HOME":  x + "/cache",
		"XDG_DATA_HOME":   x + "/data",
		"XDG_CONFIG_HOME": x + "/config",
	})
	c := mustNew(t, o)
	if c.StateDir != x+"/state/homebot" || c.CacheDir != x+"/cache/homebot" ||
		c.DataDir != x+"/data/homebot" || c.ConfigDir != x+"/config/homebot" {
		t.Errorf("dirs = %s %s %s %s, want each under %s", c.StateDir, c.CacheDir, c.DataDir, c.ConfigDir, x)
	}
}

func TestEmptyOrRelativeXDGFallsBackToHome(t *testing.T) {
	o, work := tempOptions(t)
	o.Env = o.Env.With(map[string]string{"XDG_STATE_HOME": "", "XDG_CACHE_HOME": "relative/cache"})
	c := mustNew(t, o)
	if want := filepath.Join(work, "home", ".local/state/homebot"); c.StateDir != want {
		t.Errorf("empty XDG_STATE_HOME: StateDir = %s, want %s", c.StateDir, want)
	}
	if want := filepath.Join(work, "home", ".cache/homebot"); c.CacheDir != want {
		t.Errorf("relative XDG_CACHE_HOME: CacheDir = %s, want %s", c.CacheDir, want)
	}
}

func TestHomeOptionWinsAndMissingHomeFails(t *testing.T) {
	o, work := tempOptions(t)
	o.Home = filepath.Join(work, "other")
	if c := mustNew(t, o); c.Home != o.Home {
		t.Errorf("Home = %s, want the option %s", c.Home, o.Home)
	}
	o.Home = ""
	o.Env = NewEnv(nil)
	if _, err := New(o); err == nil || err.Error() != "HOME is not set." {
		t.Errorf("New without HOME: err = %v", err)
	}
}

func TestHostKeepsTheShortName(t *testing.T) {
	releaseBuild(t)
	for host, want := range map[string]string{"forge.local": "forge", "forge": "forge", "a.b.c": "a", "": ""} {
		o, _ := tempOptions(t)
		o.Host = host
		if got := mustNew(t, o).Host; got != want {
			t.Errorf("Host %q: got %q, want %q", host, got, want)
		}
	}
}

func TestOS(t *testing.T) {
	for goos, want := range map[string]OS{"linux": Linux, "darwin": MacOS} {
		if got, err := DetectOS(goos); err != nil || got != want {
			t.Errorf("DetectOS(%q) = %q, %v; want %q", goos, got, err, want)
		}
	}
	if _, err := DetectOS("windows"); err == nil || err.Error() != "homebot supports linux and macos, not windows." {
		t.Errorf("DetectOS(windows): err = %v", err)
	}

	o, _ := tempOptions(t)
	o.OS = MacOS
	if got := mustNew(t, o).OS; got != MacOS {
		t.Errorf("OS option: got %q", got)
	}
	o.OS = "plan9"
	if _, err := New(o); err == nil || err.Error() != "homebot supports linux and macos, not plan9." {
		t.Errorf("OS plan9: err = %v", err)
	}
}

func TestArchUsesNodeNames(t *testing.T) {
	for goarch, want := range map[string]string{"amd64": "x64", "arm64": "arm64", "386": "ia32", "riscv64": "riscv64"} {
		if got := nodeArch(goarch); got != want {
			t.Errorf("nodeArch(%q) = %q, want %q", goarch, got, want)
		}
	}
	o, _ := tempOptions(t)
	o.Arch = "x64"
	if got := mustNew(t, o).Arch; got != "x64" {
		t.Errorf("Arch option: got %q", got)
	}
}

func TestStreamsDefaultToEmpty(t *testing.T) {
	o, _ := tempOptions(t)
	c := mustNew(t, o)
	if n, _ := c.Stdin.Read(make([]byte, 1)); n != 0 {
		t.Error("default Stdin is not empty")
	}
	if _, err := c.Stdout.Write([]byte("x")); err != nil {
		t.Errorf("default Stdout: %v", err)
	}
	if _, err := c.Stderr.Write([]byte("x")); err != nil {
		t.Errorf("default Stderr: %v", err)
	}
}

func TestTestHooksInATestBinary(t *testing.T) {
	o, _ := tempOptions(t)
	o.Now = nil
	o.Host = "real.example.com"
	o.Env = o.Env.With(map[string]string{
		"HOMEBOT_TEST_NOW":  "1767225600000",
		"HOMEBOT_TEST_SEED": "1",
		"HOMEBOT_TEST_HOST": "testhost",
	})
	a, b := mustNew(t, o), mustNew(t, o)

	want := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := a.Now(); !got.Equal(want) {
		t.Errorf("Now() = %v, want %v", got, want)
	}
	if a.Host != "testhost" {
		t.Errorf("Host = %q, want testhost", a.Host)
	}
	for i := range 5 {
		if x, y := a.Rand.Uint64(), b.Rand.Uint64(); x != y {
			t.Fatalf("draw %d: %d != %d; the same seed must give the same sequence", i, x, y)
		}
	}

	o.Now = func() time.Time { return time.Unix(0, 0) }
	if got := mustNew(t, o).Now(); !got.Equal(want) {
		t.Errorf("HOMEBOT_TEST_NOW must win over Options.Now in a test binary; Now() = %v", got)
	}
}

func TestTestHooksRejectBadValues(t *testing.T) {
	for name, value := range map[string]string{"HOMEBOT_TEST_NOW": "yesterday", "HOMEBOT_TEST_SEED": "-1"} {
		o, _ := tempOptions(t)
		o.Env = o.Env.With(map[string]string{name: value})
		if _, err := New(o); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s=%s: err = %v, want one naming the variable", name, value, err)
		}
	}
}

func TestReleaseBuildIgnoresTestHooks(t *testing.T) {
	releaseBuild(t)
	o, _ := tempOptions(t)
	o.Host = "forge.local"
	o.Env = o.Env.With(map[string]string{
		"HOMEBOT_TEST_NOW":  "1767225600000",
		"HOMEBOT_TEST_SEED": "1",
		"HOMEBOT_TEST_HOST": "testhost",
	})
	a, b := mustNew(t, o), mustNew(t, o)
	if got := a.Now(); !got.Equal(time.Unix(0, 0)) {
		t.Errorf("Now() = %v; a release build must use Options.Now", got)
	}
	if a.Host != "forge" {
		t.Errorf("Host = %q; a release build must use Options.Host", a.Host)
	}
	if a.Rand.Uint64() == b.Rand.Uint64() {
		t.Error("two release Contexts drew the same number; HOMEBOT_TEST_SEED must not seed them")
	}

	o.Now = nil
	if _, err := New(o); err == nil {
		t.Error("a release build without Options.Now must fail, whatever HOMEBOT_TEST_NOW says")
	}
}

func TestGuardPanicsOnDirsOutsideTemp(t *testing.T) {
	o, _ := tempOptions(t)
	o.Env = o.Env.With(map[string]string{"XDG_STATE_HOME": "/home/someone/.local/state"})
	defer func() {
		msg, _ := recover().(string)
		if !strings.Contains(msg, "StateDir") || !strings.Contains(msg, "/home/someone/.local/state/homebot") {
			t.Errorf("recovered %q, want a panic naming StateDir and its path", msg)
		}
	}()
	_, _ = New(o)
	t.Error("New returned for a state dir outside the temp dir")
}

func TestGuardChecksEveryDir(t *testing.T) {
	for _, name := range []string{"XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME"} {
		t.Run(name, func(t *testing.T) {
			o, _ := tempOptions(t)
			o.Env = o.Env.With(map[string]string{name: "/var/lib/elsewhere"})
			defer func() {
				if recover() == nil {
					t.Errorf("no panic for %s outside the temp dir", name)
				}
			}()
			_, _ = New(o)
		})
	}
}

func TestGuardAllowsTheScriptWorkDir(t *testing.T) {
	work := "/srv/scripts/work"
	o, _ := tempOptions(t)
	o.Env = NewEnv(map[string]string{"HOME": work + "/home", "WORK": work})
	defer func() {
		if recover() == nil {
			t.Error("$WORK was trusted while TMPDIR is not $WORK/.tmp")
		}
		t.Setenv("TMPDIR", work+"/.tmp")
		c := mustNew(t, o)
		if c.StateDir != work+"/home/.local/state/homebot" {
			t.Errorf("StateDir = %s", c.StateDir)
		}
	}()
	_, _ = New(o)
}

func TestGuardIsOffInARelease(t *testing.T) {
	releaseBuild(t)
	o, _ := tempOptions(t)
	o.Env = NewEnv(map[string]string{"HOME": "/home/someone"})
	if c := mustNew(t, o); c.StateDir != "/home/someone/.local/state/homebot" {
		t.Errorf("StateDir = %s", c.StateDir)
	}
}

func TestDebugLogger(t *testing.T) {
	o, _ := tempOptions(t)
	var stderr bytes.Buffer
	o.Stderr = &stderr
	mustNew(t, o).Debug.Debug("quiet")
	if stderr.Len() != 0 {
		t.Errorf("debug line written without HOMEBOT_DEBUG: %q", stderr.String())
	}
	o.Env = o.Env.With(map[string]string{"HOMEBOT_DEBUG": "0"})
	mustNew(t, o).Debug.Debug("quiet")
	if stderr.Len() != 0 {
		t.Errorf("debug line written with HOMEBOT_DEBUG=0: %q", stderr.String())
	}
	o.Env = o.Env.With(map[string]string{"HOMEBOT_DEBUG": "1"})
	mustNew(t, o).Debug.Debug("loud", "code", 3)
	if got := stderr.String(); !strings.Contains(got, "loud") || !strings.Contains(got, "code=3") {
		t.Errorf("HOMEBOT_DEBUG=1: stderr = %q, want the debug line", got)
	}
}

func TestPlainFollowsHomebotPlain(t *testing.T) {
	o, _ := tempOptions(t)
	for _, c := range []struct {
		value string
		want  bool
	}{{"", false}, {"0", false}, {"1", true}, {"yes", true}} {
		if c.value != "" {
			o.Env = o.Env.With(map[string]string{"HOMEBOT_PLAIN": c.value})
		}
		if got := mustNew(t, o).Plain; got != c.want {
			t.Errorf("HOMEBOT_PLAIN=%q: Plain = %v, want %v", c.value, got, c.want)
		}
	}
}

func TestGuardAcceptsATempDirWithATrailingSlash(t *testing.T) {
	o, work := tempOptions(t)
	t.Setenv("TMPDIR", work+"/")
	if c := mustNew(t, o); c.StateDir != work+"/home/.local/state/homebot" {
		t.Errorf("StateDir = %s", c.StateDir)
	}
}

func TestGuardAcceptsBothFormsOfASymlinkedTempDir(t *testing.T) {
	real, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "tmp-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(real, "work", ".tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", link+"/")
	o, _ := tempOptions(t)
	for _, home := range []string{link + "/home", real + "/home"} {
		o.Env = NewEnv(map[string]string{"HOME": home})
		mustNew(t, o)
	}

	// testscript's layout: WORK as the test sees it, TMPDIR resolved below it.
	t.Setenv("TMPDIR", filepath.Join(real, "work", ".tmp"))
	o.Env = NewEnv(map[string]string{"HOME": link + "/work/home", "WORK": link + "/work"})
	mustNew(t, o)

	o.Env = NewEnv(map[string]string{"HOME": "/home/someone"})
	defer func() {
		if recover() == nil {
			t.Error("no panic for a home outside the temp dir")
		}
	}()
	_, _ = New(o)
}

func TestContextCarriesCancellation(t *testing.T) {
	o, _ := tempOptions(t)
	c := mustNew(t, o)
	if c.Context() != context.Background() {
		t.Error("a new Context's context is not context.Background()")
	}
	ctx, cancel := context.WithCancel(context.Background())
	c2 := c.WithContext(ctx)
	if c2.Context() != ctx || c.Context() != context.Background() {
		t.Error("WithContext must set the copy's context and leave the original alone")
	}
	if c2.Home != c.Home || c2.Rand != c.Rand || c2.Debug != c.Debug {
		t.Error("WithContext dropped fields")
	}
	cancel()
	if c2.Context().Err() == nil {
		t.Error("cancelling ctx did not reach the Context")
	}
	defer func() {
		if recover() == nil {
			t.Error("WithContext(nil) did not panic")
		}
	}()
	c.WithContext(nil) //nolint:staticcheck // a nil context is the case under test
}

func TestForReportNeedsNothingAndKeepsTheStreams(t *testing.T) {
	c := ForReport(Options{})
	if c.Stdout == nil || c.Stderr == nil || c.Stdin == nil || c.Now == nil || c.Debug == nil {
		t.Errorf("ForReport(Options{}) left a stream, the clock or the logger nil: %+v", c)
	}
	var out, errs strings.Builder
	c = ForReport(Options{Env: NewEnv(map[string]string{"TERM": "dumb"}), Stdout: &out, Stderr: &errs})
	if c.Stdout != &out || c.Stderr != &errs {
		t.Error("ForReport replaced the streams it was given")
	}
	if got := c.Env.Get("TERM"); got != "dumb" {
		t.Errorf("ForReport's Env has TERM %q, want the %q it was given", got, "dumb")
	}
	// TestForReportIsAlwaysPlain (voice_test.go) checks that it is Plain.
	if c.Root != "" || c.Config != nil {
		t.Errorf("Root %q, Config %v: want neither, as ForReport has no config to read", c.Root, c.Config)
	}
}
