package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/proc"
)

func TestSandboxLayout(t *testing.T) {
	b := Sandbox(t, Options{})
	files, err := fsx.WalkFiles(b.Dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"gitconfig", "home/.home/homebot.yml", "home/.home/private/.gitignore"}
	if strings.Join(files, " ") != strings.Join(want, " ") {
		t.Errorf("files = %q, want %q", files, want)
	}
	for _, d := range []string{"autolink/common", "autolink/linux", "autolink/macos", "private/common", "private/linux", "manifests/profiles"} {
		if !fsx.IsDir(filepath.Join(b.Root, d)) {
			t.Errorf("missing %s", d)
		}
	}
	if !fsx.IsDir(b.Bin) {
		t.Error("missing $WORK/bin")
	}
	for path, want := range map[string]string{
		filepath.Join(b.Root, "homebot.yml"):        BaseConfig,
		filepath.Join(b.Root, "private/.gitignore"): PrivateGitignore,
		filepath.Join(b.Dir, "gitconfig"):           GitConfig,
	} {
		if got, _ := os.ReadFile(path); string(got) != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
	if b.Home != filepath.Join(b.Dir, "home") || b.Root != filepath.Join(b.Home, ".home") {
		t.Errorf("Home %s, Root %s", b.Home, b.Root)
	}
}

func TestSandboxContext(t *testing.T) {
	b := Sandbox(t, Options{})
	c := b.Ctx
	if c.Root != b.Root || c.Home != b.Home || c.Cwd != b.Dir || c.OS != app.Linux {
		t.Errorf("Root %s, Home %s, Cwd %s, OS %s", c.Root, c.Home, c.Cwd, c.OS)
	}
	if c.Host != TestHost {
		t.Errorf("Host = %q", c.Host)
	}
	if got := c.Now(); !got.Equal(time.UnixMilli(TestNow)) || got.Location() != time.UTC {
		t.Errorf("Now() = %v, want %v in UTC", got, time.UnixMilli(TestNow).UTC())
	}
	for _, dir := range []string{c.StateDir, c.CacheDir, c.DataDir, c.ConfigDir} {
		if !app.IsInside(dir, filepath.Join(b.Dir, "xdg")) {
			t.Errorf("%s is not under $WORK/xdg", dir)
		}
	}
	if c.ConfigDir != filepath.Join(b.Dir, "xdg/config/homebot") || c.DataDir != filepath.Join(b.Dir, "xdg/data/homebot") {
		t.Errorf("ConfigDir %s, DataDir %s", c.ConfigDir, c.DataDir)
	}

	again := Sandbox(t, Options{})
	if c.Rand.Uint64() != again.Ctx.Rand.Uint64() {
		t.Error("two sandboxes drew different numbers; the seed is not fixed")
	}

	if _, err := c.Stdout.Write([]byte("out")); err != nil || b.Stdout.String() != "out" {
		t.Errorf("Stdout does not reach b.Stdout: %q", b.Stdout.String())
	}
	if _, err := c.Stderr.Write([]byte("err")); err != nil || b.Stderr.String() != "err" {
		t.Errorf("Stderr does not reach b.Stderr: %q", b.Stderr.String())
	}
}

func TestSandboxOptions(t *testing.T) {
	b := Sandbox(t, Options{Config: "profiles:\n  default: server\n", OS: app.MacOS})
	if got, _ := os.ReadFile(filepath.Join(b.Root, "homebot.yml")); string(got) != "profiles:\n  default: server\n" {
		t.Errorf("homebot.yml = %q", got)
	}
	if b.Ctx.OS != app.MacOS {
		t.Errorf("OS = %s", b.Ctx.OS)
	}
	if fsx.IsDir(filepath.Join(b.Root, ".git")) {
		t.Error("a repo without Git is a git repository")
	}
}

func TestSandboxBehindLink(t *testing.T) {
	b := Sandbox(t, Options{BehindLink: true})
	if st, err := os.Lstat(b.Dir); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("$WORK %s is not a link: %v", b.Dir, err)
	}
	real, err := filepath.EvalSymlinks(b.Root)
	if err != nil {
		t.Fatal(err)
	}
	if real == b.Root || filepath.Base(real) != ".home" {
		t.Errorf("the repo resolves to %s, want another path to %s", real, b.Root)
	}
	if b.Ctx.Root != b.Root || b.Ctx.Home != b.Home || b.Ctx.Env.Get("HOME") != b.Home {
		t.Errorf("the Context must name the link: Root %s, Home %s", b.Ctx.Root, b.Ctx.Home)
	}
	if got, _ := os.ReadFile(filepath.Join(real, "homebot.yml")); string(got) != BaseConfig {
		t.Errorf("the folder behind the link lacks the repo: %q", got)
	}
}

func TestSandboxRepoBehindLink(t *testing.T) {
	b := Sandbox(t, Options{RepoBehindLink: true})
	if st, err := os.Lstat(b.Root); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the repo %s is not a link: %v", b.Root, err)
	}
	if st, err := os.Lstat(b.Home); err != nil || st.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("$HOME %s must not be a link: %v", b.Home, err)
	}
	real, err := filepath.EvalSymlinks(b.Root)
	if err != nil {
		t.Fatal(err)
	}
	if rel, err := filepath.Rel(b.Home, real); err != nil || !strings.HasPrefix(rel, "..") {
		t.Errorf("the repo resolves to %s, want a folder outside %s", real, b.Home)
	}
	if got, _ := os.ReadFile(filepath.Join(real, "homebot.yml")); string(got) != BaseConfig {
		t.Errorf("the folder behind the link lacks the repo: %q", got)
	}
}

func TestSandboxRepoLinkedFromOutside(t *testing.T) {
	b := Sandbox(t, Options{RepoLinkedFromOutside: true})
	if st, err := os.Lstat(b.Root); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the repo %s is not a link: %v", b.Root, err)
	}
	if strings.HasPrefix(b.Root, b.Home) {
		t.Errorf("Root %s must be outside Home %s", b.Root, b.Home)
	}
	real, err := filepath.EvalSymlinks(b.Root)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(filepath.Join(b.Home, "dotfiles"))
	if err != nil {
		t.Fatal(err)
	}
	if real != want {
		t.Errorf("the repo resolves to %s, want %s", real, want)
	}
	if b.Ctx.Root != b.Root {
		t.Errorf("Context Root = %s, want %s", b.Ctx.Root, b.Root)
	}
	if got, _ := os.ReadFile(filepath.Join(real, "homebot.yml")); string(got) != BaseConfig {
		t.Errorf("the folder behind the link lacks the repo: %q", got)
	}
}

func TestSandboxGit(t *testing.T) {
	if !proc.HasCommand(Sandbox(t, Options{}).Ctx, "git") {
		t.Skip("git is not installed")
	}
	b := Sandbox(t, Options{Git: true})
	if got := b.GitIn(b.Root, "symbolic-ref", "--short", "HEAD"); got != "main\n" {
		t.Errorf("branch = %q, want main", got)
	}
	if got := b.GitIn(b.Root, "config", "user.email"); got != "test@example.com\n" {
		t.Errorf("user.email = %q", got)
	}
	if got := b.GitIn(b.Root, "config", "--global", "user.name"); got != "Test User\n" {
		t.Errorf("global user.name = %q; GIT_CONFIG_GLOBAL must point at $WORK/gitconfig", got)
	}
	b.GitIn(b.Root, "add", "-A")
	b.GitIn(b.Root, "commit", "-q", "-m", "first")
	// git 2.45 and later print UTC as "Z" in %aI; earlier versions print "+00:00".
	got := strings.ReplaceAll(b.GitIn(b.Root, "log", "-1", "--format=%an %aI %cI"), "+00:00", "Z")
	if got != "Test 2026-01-01T00:00:00Z 2026-01-01T00:00:00Z\n" {
		t.Errorf("commit = %q, want the fixed dates", got)
	}
}

func TestFiles(t *testing.T) {
	b := Sandbox(t, Options{})
	b.Files(map[string]string{
		"home/.home/autolink/common/zshrc": "# zsh\n",
		"deep/new/dir/file":                "x",
	})
	for rel, want := range map[string]string{"home/.home/autolink/common/zshrc": "# zsh\n", "deep/new/dir/file": "x"} {
		if got, err := os.ReadFile(filepath.Join(b.Dir, rel)); err != nil || string(got) != want {
			t.Errorf("%s = %q, %v", rel, got, err)
		}
	}
}

func TestWithStubs(t *testing.T) {
	b := Sandbox(t, Options{})
	dir := b.WithStubs(map[string]string{"gitleaks": `echo "leaks $1"; exit 1`})
	if dir != b.Bin {
		t.Errorf("WithStubs returned %s, want %s", dir, b.Bin)
	}
	r := proc.Run(b.Ctx, []string{"gitleaks", "detect"}, proc.Options{})
	if r.Code != 1 || r.Stdout != "leaks detect\n" {
		t.Errorf("stub run = %+v", r)
	}

	if err := os.Chmod(filepath.Join(b.Bin, "gitleaks"), 0o644); err != nil {
		t.Fatal(err)
	}
	b.WithStubs(map[string]string{"gitleaks": "echo replaced"})
	if r := proc.Run(b.Ctx, []string{"gitleaks"}, proc.Options{}); r.Code != 0 || r.Stdout != "replaced\n" {
		t.Errorf("a rewritten stub must replace the old one and be executable: %+v", r)
	}

	b.WithStubs(map[string]string{"sh": "echo fake sh"})
	if r := proc.Run(b.Ctx, []string{"sh", "-c", "echo real"}, proc.Options{}); r.Stdout != "fake sh\n" {
		t.Errorf("a stub must shadow /bin tools: %+v", r)
	}
}

func TestSandboxEnv(t *testing.T) {
	env := SandboxEnv("/w", "/tools/a", "/tools/b")
	for name, want := range map[string]string{
		"HOME":                "/w/home",
		"XDG_CONFIG_HOME":     "/w/xdg/config",
		"XDG_STATE_HOME":      "/w/xdg/state",
		"XDG_CACHE_HOME":      "/w/xdg/cache",
		"XDG_DATA_HOME":       "/w/xdg/data",
		"HOMEBOT_TEST_NOW":    "1767225600000",
		"HOMEBOT_TEST_SEED":   "1",
		"HOMEBOT_TEST_HOST":   "testhost",
		"HOMEBOT_PERSONA":     "none",
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_CONFIG_GLOBAL":   "/w/gitconfig",
		"GIT_AUTHOR_DATE":     "2026-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE":  "2026-01-01T00:00:00Z",
		"PATH":                "/w/bin:/tools/a:/tools/b:/usr/bin:/bin",
		"NO_COLOR":            "1",
		"TZ":                  "UTC",
		"LANG":                "C.UTF-8",
		"LC_ALL":              "C.UTF-8",
	} {
		if got := env[name]; got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if len(env) != 18 {
		t.Errorf("SandboxEnv has %d variables, want 18", len(env))
	}
	if _, ok := env["ZDOTDIR"]; ok {
		t.Error("ZDOTDIR is set")
	}
	if got := SandboxEnv("/w")["PATH"]; got != "/w/bin:/usr/bin:/bin" {
		t.Errorf("PATH without extra dirs = %q", got)
	}
	if time.UnixMilli(TestNow).UTC().Format(time.RFC3339) != GitDate {
		t.Error("TestNow and GitDate are different instants")
	}
}

func TestSandboxDoesNotSeeTheProcessEnv(t *testing.T) {
	t.Setenv("HOMEBOT_ROOT", "/somewhere/real")
	t.Setenv("ZDOTDIR", "/somewhere/zsh")
	b := Sandbox(t, Options{})
	for _, name := range []string{"HOMEBOT_ROOT", "ZDOTDIR", "USER"} {
		if v, ok := b.Ctx.Env.Lookup(name); ok {
			t.Errorf("%s = %q leaked into the sandbox", name, v)
		}
	}
}
