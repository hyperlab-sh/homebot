package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func TestSetupParseGitVersion(t *testing.T) {
	cases := []struct {
		out          string
		version      string
		major, minor int
		ok           bool
	}{
		{"git version 2.43.0\n", "2.43.0", 2, 43, true},
		{"git version 2.39.5 (Apple Git-154)\n", "2.39.5", 2, 39, true},
		{"git version 2.50.1 (Apple Git-155)", "2.50.1", 2, 50, true},
		{"git version 2.40.0.windows.1\n", "2.40.0.windows.1", 2, 40, true},
		{"git version 2.40\n", "2.40", 2, 40, true},
		{"git version 2.9.5\n", "2.9.5", 2, 9, true},
		{"git version 10.2.0\n", "10.2.0", 10, 2, true},
		{"git version 1.8.3.1\n", "1.8.3.1", 1, 8, true},
		{"  git version 2.41.0  \n", "2.41.0", 2, 41, true},
		{"git version 2.45.0-rc1\n", "2.45.0-rc1", 2, 45, true},
		{"git version 2\n", "", 0, 0, false},
		{"git version\n", "", 0, 0, false},
		{"git version x.y.z\n", "", 0, 0, false},
		{"hub version 2.14.2\n", "", 0, 0, false},
		{"", "", 0, 0, false},
	}
	for _, tc := range cases {
		version, major, minor, ok := setupParseGitVersion(tc.out)
		if version != tc.version || major != tc.major || minor != tc.minor || ok != tc.ok {
			t.Errorf("setupParseGitVersion(%q) = %q, %d, %d, %v; want %q, %d, %d, %v",
				tc.out, version, major, minor, ok, tc.version, tc.major, tc.minor, tc.ok)
		}
	}
}

func TestSetupGitTooOld(t *testing.T) {
	cases := []struct {
		major, minor int
		old          bool
	}{
		{2, 39, true}, {2, 40, false}, {2, 41, false}, {2, 9, true}, {2, 100, false},
		{1, 99, true}, {3, 0, false}, {10, 2, false}, {0, 50, true},
	}
	for _, tc := range cases {
		if got := setupGitTooOld(tc.major, tc.minor); got != tc.old {
			t.Errorf("setupGitTooOld(%d, %d) = %v, want %v", tc.major, tc.minor, got, tc.old)
		}
	}
}

// setupSandbox is a home repo with a homebot and a systemctl that work, and
// a git that reports gitVersion and otherwise is the real one.
func setupSandbox(t *testing.T, gitVersion string) *testutil.Box {
	t.Helper()
	sb := testutil.Sandbox(t, testutil.Options{Git: true})
	stubs := map[string]string{"homebot": "exit 0", "systemctl": "exit 0", "gitleaks": "exit 0"}
	if gitVersion != "" {
		stubs["git"] = "if [ \"$1\" = --version ]; then echo '" + gitVersion + "'; exit 0; fi\nexec /usr/bin/git \"$@\""
	}
	sb.WithStubs(stubs)
	return sb
}

// beforeInbox is setup's output up to the inbox it ends on, after a blank
// line. Output that does not end on the inbox fails the test.
func beforeInbox(t *testing.T, out string) string {
	t.Helper()
	i := strings.LastIndex(out, "\nhomebot · testhost · ")
	if i < 0 {
		t.Errorf("setup does not end on the inbox:\n%s", out)
		return out
	}
	return out[:i]
}

func setupRunOK(t *testing.T, sb *testutil.Box, args ...string) string {
	t.Helper()
	sb.Stdout.Reset()
	sb.Stderr.Reset()
	if code := execute(sb.Ctx, append([]string{"setup"}, args...)...); code != 0 {
		t.Fatalf("setup %v: exit %d, stderr:\n%s", args, code, sb.Stderr)
	}
	return sb.Stdout.String()
}

// D11: an old git stops setup before it installs anything.
func TestSetupStopsOnAGitOlderThan240(t *testing.T) {
	for _, v := range []string{"git version 2.39.5 (Apple Git-154)", "git version 2.30.2", "git version 1.8.3.1"} {
		t.Run(v, func(t *testing.T) {
			sb := setupSandbox(t, v)
			if code := execute(sb.Ctx, "setup"); code != 1 {
				t.Fatalf("exit %d, want 1; stdout:\n%s", code, sb.Stdout)
			}
			found := strings.Fields(strings.TrimPrefix(v, "git version "))[0]
			want := "homebot: Your git is " + found + ", and homebot needs 2.40 or newer: its commit guard relies on a git command that older versions lack, so every save would be refused. Update git, then run homebot setup again.\n"
			if sb.Stderr.String() != want {
				t.Errorf("stderr:\n%s\nwant:\n%s", sb.Stderr, want)
			}
			if sb.Stdout.Len() != 0 {
				t.Errorf("stdout:\n%s", sb.Stdout)
			}
			cfg, err := os.ReadFile(filepath.Join(sb.Root, ".git", "config"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(strings.ToLower(string(cfg)), "hookspath") {
				t.Errorf("setup set core.hooksPath:\n%s", cfg)
			}
			for _, rel := range []string{"home/.config/systemd", "xdg/data/homebot"} {
				if _, err := os.Stat(filepath.Join(sb.Dir, rel)); err == nil {
					t.Errorf("setup installed %s before stopping", rel)
				}
			}
		})
	}
}

// With brew on PATH, an old git gets the command that installs a newer
// one. setup never runs it.
func TestSetupGivesTheBrewCommandForAnOldGit(t *testing.T) {
	sb := setupSandbox(t, "git version 2.39.5 (Apple Git-154)")
	sb.WithStubs(map[string]string{"brew": `echo "$@" >> "$HOME/../brew.log"; exit 0`})
	if code := execute(sb.Ctx, "setup"); code != 1 {
		t.Fatalf("exit %d, want 1; stdout:\n%s", code, sb.Stdout)
	}
	want := "homebot: Your git is 2.39.5, and homebot needs 2.40 or newer: its commit guard relies on a git command that older versions lack, so every save would be refused. Install a newer one with Homebrew, then run homebot setup again:\n  brew install git\n"
	if sb.Stderr.String() != want {
		t.Errorf("stderr:\n%s\nwant:\n%s", sb.Stderr, want)
	}
	if exists(filepath.Join(sb.Dir, "brew.log")) {
		t.Error("setup ran brew")
	}
}

func TestSetupRunsOnAGitThatIsNewEnough(t *testing.T) {
	for _, v := range []string{"git version 2.40.0", "git version 2.50.1 (Apple Git-155)", "git version 3.0.1"} {
		t.Run(v, func(t *testing.T) {
			sb := setupSandbox(t, v)
			if out := setupRunOK(t, sb); !strings.HasPrefix(out, "installed commit guard (core.hooksPath)\n") {
				t.Errorf("stdout:\n%s", out)
			}
		})
	}
}

// A git that does not say its version in the usual way is not judged.
func TestSetupDoesNotJudgeAGitItCannotRead(t *testing.T) {
	sb := setupSandbox(t, "something else entirely")
	if out := setupRunOK(t, sb); !strings.HasPrefix(out, "installed commit guard") {
		t.Errorf("stdout:\n%s", out)
	}
}

// The helper's files are in $HOME, so --remove needs no repo: it takes them
// away after the repo is gone, and never points at homebot setup.
func TestSetupRemoveWorksWithoutARepo(t *testing.T) {
	sb := guidedSandbox(t, nil)
	units := filepath.Join(sb.Home, ".config", "systemd", "user")
	sb.Files(map[string]string{
		"home/.config/systemd/user/homebot-tidy.timer":   "[Timer]\n",
		"home/.config/systemd/user/homebot-tidy.service": "[Service]\n",
	})
	if out := setupRunOK(t, sb, "--remove"); out != "Background helper removed.\n" {
		t.Errorf("stdout:\n%s", out)
	}
	if sb.Stderr.Len() != 0 {
		t.Errorf("stderr:\n%s", sb.Stderr)
	}
	for _, f := range []string{"homebot-tidy.timer", "homebot-tidy.service"} {
		if exists(filepath.Join(units, f)) {
			t.Errorf("%s is still there", f)
		}
	}
	if exists(filepath.Join(sb.Home, ".home")) {
		t.Error("--remove created a repo")
	}
}

func TestSetupRemoveDoesNotCheckGit(t *testing.T) {
	sb := setupSandbox(t, "git version 2.30.2")
	if out := setupRunOK(t, sb, "--remove"); out != "Background helper removed.\n" {
		t.Errorf("stdout:\n%s", out)
	}
}

const (
	setupGuardLine  = "installed commit guard (core.hooksPath)\n"
	setupHelperLine = "installed hourly systemd timer homebot-tidy.timer\n" +
		"The helper keeps the XDG_* folders it was set up with. After changing XDG_CONFIG_HOME, XDG_DATA_HOME, XDG_STATE_HOME or XDG_CACHE_HOME, run homebot setup again.\n"
	setupKeysHint = "\nSecrets aren't set up yet. To keep passwords and tokens in the repo, encrypted, create the keys:\n  homebot keys init\n"
	setupZshHint  = "\nAdd this to your .zshrc so new terminals tell you when something needs you:\n  eval \"$(homebot shell zsh)\"\n"
)

func TestSetupInstallsAndSaysWhatIsLeft(t *testing.T) {
	sb := setupSandbox(t, "")
	want := setupGuardLine + setupHelperLine + setupKeysHint + setupZshHint
	if out := beforeInbox(t, setupRunOK(t, sb)); out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
	for _, rel := range []string{"home/.config/systemd/user/homebot-tidy.timer", "home/.config/systemd/user/homebot-tidy.service", "xdg/data/homebot/hooks/pre-commit"} {
		if _, err := os.Stat(filepath.Join(sb.Dir, rel)); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}
}

// The helper's unit carries the XDG_* folders setup ran with, so the helper
// and the shell share state, cache and lock (final review home I2).
func TestSetupGivesTheHelperTheXDGFolders(t *testing.T) {
	sb := setupSandbox(t, "")
	setupRunOK(t, sb)
	service, err := os.ReadFile(filepath.Join(sb.Home, ".config/systemd/user/homebot-tidy.service"))
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range []string{"XDG_CONFIG_HOME=" + sb.Dir + "/xdg/config", "XDG_STATE_HOME=" + sb.Dir + "/xdg/state", "XDG_CACHE_HOME=" + sb.Dir + "/xdg/cache", "XDG_DATA_HOME=" + sb.Dir + "/xdg/data"} {
		if !strings.Contains(string(service), `Environment="`+kv+`"`) {
			t.Errorf("service lacks %s:\n%s", kv, service)
		}
	}
}

// A relative XDG_* stops setup before it installs anything: no guard, no
// helper.
func TestSetupRefusesARelativeXDGVariable(t *testing.T) {
	sb := setupSandbox(t, "")
	sb.Ctx.Env = sb.Ctx.Env.With(map[string]string{"XDG_CACHE_HOME": ".cache"})
	if code := execute(sb.Ctx, "setup"); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if want := `homebot: XDG_CACHE_HOME is ".cache", which is not a full path`; !strings.HasPrefix(sb.Stderr.String(), want) {
		t.Errorf("stderr:\n%s", sb.Stderr)
	}
	if sb.Stdout.String() != "" {
		t.Errorf("stdout:\n%s", sb.Stdout)
	}
	if _, err := os.Stat(filepath.Join(sb.Dir, "xdg/data/homebot/hooks")); err == nil {
		t.Error("the guard was installed")
	}
	if _, err := os.Stat(filepath.Join(sb.Home, ".config/systemd")); err == nil {
		t.Error("units were written")
	}
}

func TestSetupIsIdempotent(t *testing.T) {
	sb := setupSandbox(t, "")
	setupRunOK(t, sb)
	timer := filepath.Join(sb.Home, ".config/systemd/user/homebot-tidy.timer")
	first, err := os.ReadFile(timer)
	if err != nil {
		t.Fatal(err)
	}
	hooks := sb.GitIn(sb.Root, "config", "--local", "--get-all", "core.hooksPath")

	want := "Commit guard already installed.\n" + setupHelperLine + setupKeysHint + setupZshHint
	if out := beforeInbox(t, setupRunOK(t, sb)); out != want {
		t.Errorf("second run stdout:\n%s\nwant:\n%s", out, want)
	}
	second, _ := os.ReadFile(timer)
	if string(first) != string(second) {
		t.Error("the timer unit changed between two runs")
	}
	if got := sb.GitIn(sb.Root, "config", "--local", "--get-all", "core.hooksPath"); got != hooks {
		t.Errorf("core.hooksPath changed: %q -> %q", hooks, got)
	}
}

func TestSetupStaysQuietAboutWhatIsAlreadyDone(t *testing.T) {
	sb := setupSandbox(t, "")
	testutil.TestKeys(t, sb.Root)
	sb.Files(map[string]string{"home/.zshrc": "eval \"$(homebot shell zsh)\"\n"})
	if out := beforeInbox(t, setupRunOK(t, sb)); out != setupGuardLine+setupHelperLine {
		t.Errorf("stdout:\n%s", out)
	}
}

func TestSetupOutsideARepoSkipsTheGuard(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{})
	sb.WithStubs(map[string]string{"systemctl": "exit 0", "gitleaks": "exit 0"})
	want := "~/.home is not a git repo yet; skipped the commit guard.\n" + setupHelperLine + setupKeysHint + setupZshHint
	if out := beforeInbox(t, setupRunOK(t, sb)); out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
}

func TestSetupRemoveTakesTheHelperAway(t *testing.T) {
	sb := setupSandbox(t, "")
	setupRunOK(t, sb)
	if out := setupRunOK(t, sb, "--remove"); out != "Background helper removed.\n" {
		t.Errorf("stdout:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(sb.Home, ".config/systemd/user/homebot-tidy.timer")); err == nil {
		t.Error("the timer is still there")
	}
	// The guard stays: --remove is about the helper.
	if got := strings.TrimSpace(sb.GitIn(sb.Root, "config", "--local", "--get", "core.hooksPath")); got == "" {
		t.Error("--remove took the guard away")
	}
}

func TestSetupWithoutASystemdSessionStopsBeforeWritingUnits(t *testing.T) {
	sb := setupSandbox(t, "")
	sb.WithStubs(map[string]string{"systemctl": "exit 1"})
	if code := execute(sb.Ctx, "setup"); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.HasPrefix(sb.Stderr.String(), "homebot: No systemd user session here") {
		t.Errorf("stderr:\n%s", sb.Stderr)
	}
	if sb.Stdout.String() != setupGuardLine {
		t.Errorf("stdout:\n%s", sb.Stdout)
	}
	if _, err := os.Stat(filepath.Join(sb.Home, ".config/systemd")); err == nil {
		t.Error("units were written")
	}
}

// emptyGitConfig leaves git without a name or email.
func emptyGitConfig(t *testing.T, sb *testutil.Box) {
	t.Helper()
	sb.Files(map[string]string{"gitconfig": ""})
	sb.GitIn(sb.Root, "config", "--local", "--unset", "user.name")
	sb.GitIn(sb.Root, "config", "--local", "--unset", "user.email")
}

const setupIdentityFix = `mkdir -p ~/.config/git && git config --file ~/.config/git/config user.name "Your Name" && git config --file ~/.config/git/config user.email you@example.com`

func TestSetupExplainsAMissingIdentityWithoutATerminal(t *testing.T) {
	sb := setupSandbox(t, "")
	emptyGitConfig(t, sb)
	want := setupGuardLine +
		"Git doesn't know who you are yet, and homebot commits as you.\nSet it once:\n  " + setupIdentityFix + "\n" +
		setupHelperLine + setupKeysHint + setupZshHint
	if out := beforeInbox(t, setupRunOK(t, sb)); out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
}

func TestSetupAsksForAMissingIdentityOnATerminal(t *testing.T) {
	sb := setupSandbox(t, "")
	emptyGitConfig(t, sb)
	tty := true
	sb.Ctx.StdinIsTerminal = &tty
	sb.Ctx.Stdin = strings.NewReader("\nAda Lovelace\nada@example.com\n")
	out := setupRunOK(t, sb)
	want := "Give your homebot a command name? For example zork (Enter keeps homebot): " + setupGuardLine +
		"Git doesn't know who you are yet, and homebot commits as you.\n" +
		"Your name: Your email: saved Ada Lovelace <ada@example.com> in your global git config\n" +
		setupHelperLine
	if !strings.HasPrefix(out, want) {
		t.Errorf("stdout:\n%s\nwant it to start:\n%s", out, want)
	}
	// GIT_CONFIG_GLOBAL is set in the sandbox, so --global wrote there.
	got, err := os.ReadFile(filepath.Join(sb.Dir, "gitconfig"))
	if err != nil || !strings.Contains(string(got), "name = Ada Lovelace") || !strings.Contains(string(got), "email = ada@example.com") {
		t.Errorf("gitconfig = %q, %v", got, err)
	}
}

func TestSetupAsksOnlyForWhatIsMissing(t *testing.T) {
	sb := setupSandbox(t, "")
	emptyGitConfig(t, sb)
	sb.GitIn(sb.Root, "config", "--local", "user.name", "Already Known")
	tty := true
	sb.Ctx.StdinIsTerminal = &tty
	sb.Ctx.Stdin = strings.NewReader("\nada@example.com\n")
	out := setupRunOK(t, sb)
	if strings.Contains(out, "Your name:") || !strings.Contains(out, "Your email: saved Already Known <ada@example.com>") {
		t.Errorf("stdout:\n%s", out)
	}
}

func TestSetupSkipsAnIdentityNobodyAnswers(t *testing.T) {
	sb := setupSandbox(t, "")
	emptyGitConfig(t, sb)
	tty := true
	sb.Ctx.StdinIsTerminal = &tty
	sb.Ctx.Stdin = strings.NewReader("\nAda\n\n")
	out := setupRunOK(t, sb)
	want := "Your name: Your email: Skipped. Set it later:\n  " + setupIdentityFix + "\n"
	if !strings.Contains(out, want) {
		t.Errorf("stdout:\n%s\nwant it to contain:\n%s", out, want)
	}
}

// Without GIT_CONFIG_GLOBAL the identity goes into ~/.config/git/config, and
// setup offers to track that file in the repo.
func TestSetupSavesTheIdentityInTheXDGGitConfig(t *testing.T) {
	sb := setupSandbox(t, "")
	emptyGitConfig(t, sb)
	// With XDG_CONFIG_HOME out of the way git also reads ~/.config/git/config.
	sb.Ctx.Env = sb.Ctx.Env.Without("GIT_CONFIG_GLOBAL", "XDG_CONFIG_HOME")
	tty := true
	sb.Ctx.StdinIsTerminal = &tty
	sb.Ctx.Stdin = strings.NewReader("\nAda\nada@example.com\n")
	out := setupRunOK(t, sb)
	want := "saved Ada <ada@example.com> in ~/.config/git/config\nTrack it so every machine gets it:\n  homebot pack ~/.config/git/config\n"
	if !strings.Contains(out, want) {
		t.Errorf("stdout:\n%s\nwant it to contain:\n%s", out, want)
	}
	got, err := os.ReadFile(filepath.Join(sb.Home, ".config/git/config"))
	if err != nil || !strings.Contains(string(got), "name = Ada") {
		t.Errorf("~/.config/git/config = %q, %v", got, err)
	}
}
