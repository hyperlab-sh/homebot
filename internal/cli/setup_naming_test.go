package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/helper"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/selfname"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// setupClonedNameBox is a second machine with a real clone of a committed
// home repo. The source machine's config is shared; its name record is not.
func setupClonedNameBox(t *testing.T, name, persona string) *testutil.Box {
	t.Helper()
	source := setupSandbox(t, "")
	configFile := filepath.Join(source.Root, "homebot.yml")
	config := mustRead(t, configFile)
	if name != "" {
		config += "name: " + name + "\n"
	}
	if persona != "" {
		config += "persona: " + persona + "\n"
	}
	if err := os.WriteFile(configFile, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	source.GitIn(source.Root, "add", "-A")
	source.GitIn(source.Root, "commit", "-q", "-m", "shared config")
	bare := filepath.Join(source.Dir, "remote.git")
	source.GitIn(source.Dir, "clone", "-q", "--bare", source.Root, bare)
	second := guidedSandbox(t, nil)
	second.GitIn(second.Dir, "clone", "-q", bare, second.Root)
	return second
}

func TestSetupClonedRepoNameAndPersonaPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, flagName, wantName string
		tty                      bool
		answer                   string
	}{
		{"matching", "zork", "zork", false, ""},
		{"differing unattended", "zara", "zork", false, ""},
		{"differing accepted", "zara", "zara", true, "y"},
		{"differing declined", "zara", "zork", true, "n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := setupClonedNameBox(t, "zork", "neatpik")
			answering(b, tc.tty, tc.answer)
			if code := execute(b.Ctx, "setup", "--name="+tc.flagName, "--persona=tester"); code != 0 {
				t.Fatalf("setup: %d %s\n%s", code, b.Stderr, b.Stdout)
			}
			config := mustRead(t, filepath.Join(b.Root, "homebot.yml"))
			if !strings.Contains(config, "name: "+tc.wantName) || !strings.Contains(config, "persona: tester") {
				t.Errorf("config: %q", config)
			}
			if rec, ok := selfname.Record(b.Ctx); !ok || rec.Name != tc.wantName {
				t.Errorf("name record: %+v, %v", rec, ok)
			}
		})
	}
}

func TestSetupClonedBlockedNameKeepsSharedConfig(t *testing.T) {
	b := setupClonedNameBox(t, "git", "neatpik")
	answering(b, false)
	if code := execute(b.Ctx, "setup"); code != 0 {
		t.Fatalf("setup: %d %s", code, b.Stderr)
	}
	if got := mustRead(t, filepath.Join(b.Root, "homebot.yml")); !strings.Contains(got, "name: git") {
		t.Errorf("shared config: %q", got)
	}
	if _, ok := selfname.Record(b.Ctx); ok {
		t.Error("blocked name made a machine record")
	}
	if out := b.Stdout.String(); !strings.Contains(out, "The name git is blocked on this machine") || !strings.Contains(out, "no name link for git was made here") {
		t.Errorf("inbox and summary: %q", out)
	}
}

func TestSetupRejectsInvalidFlagsBeforeChangingMachine(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"short name", []string{"--name=x"}},
		{"invalid persona", []string{"--persona=Bad"}},
		{"remove with name", []string{"--remove", "--name=zork"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := guidedSandbox(t, nil)
			answering(b, false)
			if code := execute(b.Ctx, append([]string{"setup"}, tc.args...)...); code != 1 {
				t.Fatalf("exit %d, stderr %q", code, b.Stderr.String())
			}
			if b.Stdout.Len() != 0 || !strings.Contains(b.Stderr.String(), "homebot:") {
				t.Fatalf("stdout %q, stderr %q", b.Stdout.String(), b.Stderr.String())
			}
			if exists(filepath.Join(b.Home, ".home")) || exists(filepath.Join(b.Home, ".local", "bin")) {
				t.Fatal("invalid flags changed the home")
			}
		})
	}
}

func TestSetupNotNowKeepsValidatedFlagsInLaterCommand(t *testing.T) {
	b := guidedSandbox(t, nil)
	answering(b, true, "")
	if code := execute(b.Ctx, "setup", "--persona", "tester", "--name", "zork"); code != 0 {
		t.Fatalf("exit %d: %s", code, b.Stderr)
	}
	if out := b.Stdout.String(); !strings.Contains(out, "homebot setup --persona=tester --name=zork") {
		t.Errorf("later command: %q", out)
	}
	if exists(filepath.Join(b.Home, ".home")) || exists(filepath.Join(b.Home, ".local", "bin")) {
		t.Error("not now changed this machine")
	}
}

func TestSetupNewRepoFlagsMakeNameAndPersonaBeforeHelper(t *testing.T) {
	b := guidedSandbox(t, map[string]string{"SHELL": "/bin/zsh"})
	answering(b, true, "n", "n", "y") // new repo, decline keys, accept PATH line
	if code := execute(b.Ctx, "setup", "--name=zork", "--persona=tester"); code != 0 {
		t.Fatalf("exit %d: %s\n%s", code, b.Stderr, b.Stdout)
	}
	config := mustRead(t, filepath.Join(b.Root, "homebot.yml"))
	if !strings.Contains(config, "name: zork") || !strings.Contains(config, "persona: tester") {
		t.Errorf("homebot.yml: %q", config)
	}
	if rec, ok := selfname.Record(b.Ctx); !ok || rec.Name != "zork" || !rec.Made {
		t.Errorf("name record: %+v, %v", rec, ok)
	}
	if !exists(filepath.Join(b.Home, ".local", "bin", "zork")) {
		t.Error("name link missing")
	}
	if got := mustRead(t, filepath.Join(b.Home, ".zshrc")); !strings.Contains(got, `export PATH="$HOME/.local/bin:$PATH"`) {
		t.Errorf(".zshrc: %q", got)
	}
	if !strings.Contains(b.Stdout.String(), "Changed on this machine:") || !strings.Contains(b.Stdout.String(), "From now on, type zork") {
		t.Errorf("closing summary: %q", b.Stdout.String())
	}
}

func TestSetupPersonaFlagOverridesRepoWithAndWithoutTerminal(t *testing.T) {
	for _, tty := range []bool{false, true} {
		b := nameBox(t)
		b.WithStubs(map[string]string{"gitleaks": "exit 0"})
		file := filepath.Join(b.Root, "homebot.yml")
		if err := os.WriteFile(file, append([]byte(mustRead(t, file)), []byte("persona: neatpik\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
		fresh(t, b)
		answering(b, tty)
		if code := execute(b.Ctx, "setup", "--persona=tester", "--name=homebot"); code != 0 {
			t.Fatalf("tty %v: exit %d: %s", tty, code, b.Stderr)
		}
		if got := mustRead(t, file); !strings.Contains(got, "persona: tester") {
			t.Errorf("tty %v: config %q", tty, got)
		}
		if !strings.Contains(b.Stdout.String(), "Persona in this repo: tester") {
			t.Errorf("tty %v: output %q", tty, b.Stdout.String())
		}
	}
}

func TestSetupBlockedNameFlagContinuesWithoutName(t *testing.T) {
	b := guidedSandbox(t, nil)
	answering(b, true, "n", "n")
	if code := execute(b.Ctx, "setup", "--name=git", "--persona=tester"); code != 0 {
		t.Fatalf("exit %d: %s", code, b.Stderr)
	}
	config := mustRead(t, filepath.Join(b.Root, "homebot.yml"))
	if strings.Contains(config, "name:") {
		t.Error("blocked name was stored")
	}
	if !strings.Contains(config, "persona: tester") {
		t.Errorf("explicit persona was not stored: %q", config)
	}
	if _, ok := selfname.Record(b.Ctx); ok {
		t.Error("blocked name made a record")
	}
	if !strings.Contains(b.Stdout.String(), "git cannot be used as a command here") {
		t.Errorf("missing refusal: %q", b.Stdout.String())
	}
	if strings.Contains(b.Stdout.String(), "homebot.yml is unchanged") {
		t.Errorf("false whole-config claim: %q", b.Stdout.String())
	}
}

func TestSetupDifferingRepoNameNeedsInteractiveAgreement(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tty    bool
		answer string
		want   string
	}{
		{"unattended", false, "", "zork"},
		{"declined", true, "n", "zork"},
		{"accepted", true, "y", "zara"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := nameBox(t)
			b.WithStubs(map[string]string{"gitleaks": "exit 0"})
			if code, _, stderr := runName(t, b, "zork"); code != 0 {
				t.Fatalf("initial name: %d %s", code, stderr)
			}
			fresh(t, b)
			answering(b, tc.tty, tc.answer)
			if code := execute(b.Ctx, "setup", "--name=zara"); code != 0 {
				t.Fatalf("setup: %d %s", code, b.Stderr)
			}
			if got := mustRead(t, filepath.Join(b.Root, "homebot.yml")); !strings.Contains(got, "name: "+tc.want) {
				t.Errorf("config: %q", got)
			}
			if rec, ok := selfname.Record(b.Ctx); !ok || rec.Name != tc.want {
				t.Errorf("record: %+v, %v", rec, ok)
			}
			if !tc.tty && !strings.Contains(b.Stdout.String(), "was ignored without a terminal") {
				t.Errorf("missing repo precedence: %q", b.Stdout.String())
			}
		})
	}
}

func TestSetupSummaryCoversAdoptedNameLink(t *testing.T) {
	b := nameBox(t)
	b.WithStubs(map[string]string{"gitleaks": "exit 0"})
	if code, _, stderr := runName(t, b, "zork"); code != 0 {
		t.Fatalf("initial name: %d %s", code, stderr)
	}
	adopted := filepath.Join(b.Home, ".local", "bin", "zara")
	if err := os.Symlink(filepath.Join(b.Bin, "homebot"), adopted); err != nil {
		t.Fatal(err)
	}
	fresh(t, b)
	answering(b, true, "y", "n")
	if code := execute(b.Ctx, "setup", "--name=zara"); code != 0 {
		t.Fatalf("setup: %d %s", code, b.Stderr)
	}
	if rec, ok := selfname.Record(b.Ctx); !ok || rec.Name != "zara" || rec.Made {
		t.Errorf("adopted record: %+v, %v", rec, ok)
	}
	if out := b.Stdout.String(); !strings.Contains(out, "name link: ~/.local/bin/zara points to homebot") || strings.Contains(out, "made ~/.local/bin/zara") {
		t.Errorf("name summary: %q", out)
	}
}

func TestSetupKeepsBlockedRepoNameAndAddsInboxItem(t *testing.T) {
	b := nameBox(t)
	b.WithStubs(map[string]string{"gitleaks": "exit 0"})
	setConfigName(t, b, "git")
	fresh(t, b)
	answering(b, false)
	if code := execute(b.Ctx, "setup"); code != 0 {
		t.Fatalf("setup: %d %s", code, b.Stderr)
	}
	if !strings.Contains(mustRead(t, filepath.Join(b.Root, "homebot.yml")), "name: git") {
		t.Error("blocked shared name was deleted")
	}
	if _, ok := selfname.Record(b.Ctx); ok {
		t.Error("blocked name made a link record")
	}
	if out := b.Stdout.String(); !strings.Contains(out, "git cannot be used as a command here") || !strings.Contains(out, "The name git is blocked on this machine") || !strings.Contains(out, "no name link for git was made here") {
		t.Errorf("refusal and inbox item: %q", out)
	}
}

func TestSetupLeavesNewNameUncommittedWithExistingWork(t *testing.T) {
	b := setupSandbox(t, "")
	b.GitIn(b.Root, "add", "-A")
	b.GitIn(b.Root, "commit", "-q", "-m", "baseline")
	configFile := filepath.Join(b.Root, "homebot.yml")
	if err := os.WriteFile(configFile, []byte(mustRead(t, configFile)+"# owner's pending edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := strings.TrimSpace(b.GitIn(b.Root, "rev-parse", "HEAD"))
	answering(b, false)
	if code := execute(b.Ctx, "setup", "--name=zork"); code != 0 {
		t.Fatalf("exit %d: %s", code, b.Stderr)
	}
	if after := strings.TrimSpace(b.GitIn(b.Root, "rev-parse", "HEAD")); after != before {
		t.Errorf("setup committed own work: %s -> %s", before, after)
	}
	if got := mustRead(t, filepath.Join(b.Root, "homebot.yml")); !strings.Contains(got, "name: zork") {
		t.Errorf("name missing from uncommitted config: %q", got)
	}
	if out := b.Stdout.String(); !strings.Contains(out, "stays uncommitted with your other work") {
		t.Errorf("uncommitted explanation: %q", out)
	}
}

func TestSetupNamePathUsesShellFileAndHelperPathOrder(t *testing.T) {
	for _, tc := range []struct {
		name, shell, file string
		os                app.OS
		line              string
	}{
		{"zsh", "/bin/zsh", ".zshrc", app.Linux, `export PATH="$HOME/.local/bin:$PATH"`},
		{"bash-linux", "/bin/bash", ".bashrc", app.Linux, `export PATH="$HOME/.local/bin:$PATH"`},
		{"bash-macos", "/bin/bash", ".bash_profile", app.MacOS, `export PATH="$HOME/.local/bin:$PATH"`},
		{"fish", "/bin/fish", ".config/fish/config.fish", app.Linux, `fish_add_path $HOME/.local/bin`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := nameBox(t)
			b.WithStubs(map[string]string{"gitleaks": "exit 0", "launchctl": "exit 0"})
			b.Ctx.OS = tc.os
			b.Ctx.Env = b.Ctx.Env.With(map[string]string{"SHELL": tc.shell})
			answering(b, true, "y")
			if code := execute(b.Ctx, "setup", "--name=zork"); code != 0 {
				t.Fatalf("setup: %d %s\n%s", code, b.Stderr, b.Stdout)
			}
			if got := mustRead(t, filepath.Join(b.Home, tc.file)); !strings.Contains(got, tc.line) {
				t.Errorf("rc file: %q", got)
			}
			path, ok := helper.RecordedPath(b.Ctx)
			if !ok || !strings.HasPrefix(path, helper.LocalBin(b.Ctx)+string(os.PathListSeparator)) {
				t.Errorf("helper PATH = %q, %v", path, ok)
			}
		})
	}
}

func TestSetupNamePathDeclinedAndAlreadyPresent(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		accept     bool
	}{
		{"declined", "", false},
		{"already later", "later", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := nameBox(t)
			b.WithStubs(map[string]string{"gitleaks": "exit 0"})
			path := b.Bin
			if tc.path != "" {
				path += string(os.PathListSeparator) + helper.LocalBin(b.Ctx)
			}
			b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": path, "SHELL": "/bin/bash"})
			answering(b, true, "n")
			if code := execute(b.Ctx, "setup", "--name=zork"); code != 0 {
				t.Fatalf("setup: %d %s", code, b.Stderr)
			}
			if got, ok := helper.RecordedPath(b.Ctx); !ok || got != path {
				t.Errorf("helper PATH %q, %v; want %q", got, ok, path)
			}
			if tc.path == "" && exists(filepath.Join(b.Home, ".bashrc")) {
				t.Error("declined PATH line was written")
			}
			if tc.path != "" && strings.Contains(b.Stdout.String(), "Put ~/.local/bin on your PATH") {
				t.Error("PATH offer repeated although bin was already later")
			}
		})
	}
}

func TestSetupExistingRCLineUpdatesHelperPATHWithoutDuplicate(t *testing.T) {
	b := nameBox(t)
	b.WithStubs(map[string]string{"gitleaks": "exit 0"})
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": b.Bin, "SHELL": "/bin/zsh"})
	rcFile := filepath.Join(b.Home, ".zshrc")
	line := `export PATH="$HOME/.local/bin:$PATH"`
	if err := os.WriteFile(rcFile, []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	answering(b, false)
	if code := execute(b.Ctx, "setup", "--name=zork"); code != 0 {
		t.Fatalf("setup: %d %s", code, b.Stderr)
	}
	if got := mustRead(t, rcFile); got != line+"\n" {
		t.Errorf("rc line changed or duplicated: %q", got)
	}
	want := helper.LocalBin(b.Ctx) + string(os.PathListSeparator) + b.Bin
	if got, ok := helper.RecordedPath(b.Ctx); !ok || got != want {
		t.Errorf("helper PATH = %q, %v; want %q", got, ok, want)
	}
}

func TestSetupExplainsUnsafeLinkedStartupFileWithoutClaimingItIsOutsideRepo(t *testing.T) {
	b := nameBox(t)
	b.WithStubs(map[string]string{"gitleaks": "exit 0"})
	target := filepath.Join(b.Root, "autolink", "common", "missing-zshrc")
	if err := os.Symlink(target, filepath.Join(b.Home, ".zshrc")); err != nil {
		t.Fatal(err)
	}
	answering(b, true)
	if code := execute(b.Ctx, "setup", "--name=zork"); code != 0 {
		t.Fatalf("setup: %d %s", code, b.Stderr)
	}
	if out := b.Stdout.String(); !strings.Contains(out, "Cannot safely edit ~/.zshrc (link target:") || strings.Contains(out, "outside the home repo") {
		t.Errorf("linked startup file advice: %q", out)
	}
	if exists(target) {
		t.Error("dangling link target was created")
	}
}

func TestSetupRemoveOnlyOwnedNameLinks(t *testing.T) {
	for _, adopted := range []bool{false, true} {
		b := nameBox(t)
		link := filepath.Join(b.Home, ".local", "bin", "zork")
		if adopted {
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(b.Bin, "homebot"), link); err != nil {
				t.Fatal(err)
			}
		}
		if code, _, stderr := runName(t, b, "zork"); code != 0 {
			t.Fatalf("initial name: %d %s", code, stderr)
		}
		if code := execute(b.Ctx, "setup", "--remove"); code != 0 {
			t.Fatalf("remove: %d %s", code, b.Stderr)
		}
		if _, ok := selfname.Record(b.Ctx); ok {
			t.Error("record remains")
		}
		if exists(link) != adopted {
			t.Errorf("adopted=%v link exists=%v", adopted, exists(link))
		}
	}
}

func TestSetupRemoveLeavesForeignReplacement(t *testing.T) {
	b := nameBox(t)
	if code, _, stderr := runName(t, b, "zork"); code != 0 {
		t.Fatalf("initial name: %d %s", code, stderr)
	}
	link := filepath.Join(b.Home, ".local", "bin", "zork")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(link, []byte("foreign\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := execute(b.Ctx, "setup", "--remove"); code != 0 {
		t.Fatalf("remove: %d %s", code, b.Stderr)
	}
	if got := mustRead(t, link); got != "foreign\n" {
		t.Errorf("foreign file changed: %q", got)
	}
	if _, ok := selfname.Record(b.Ctx); ok {
		t.Error("record remains")
	}
}

func TestSetupRemoveStillUninstallsHelperAfterNameRefusal(t *testing.T) {
	b := nameBox(t)
	if code, _, stderr := runName(t, b, "zork"); code != 0 {
		t.Fatalf("initial name: %d %s", code, stderr)
	}
	if err := helper.Install(b.Ctx); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(b.Home, ".local", "bin")
	realBin := filepath.Join(b.Home, ".local", "bin-real")
	if err := os.Rename(bin, realBin); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realBin, bin); err != nil {
		t.Fatal(err)
	}
	if err := setupRun(b.Ctx, true, setupOptions{}); err == nil {
		t.Fatal("synced bin was not refused")
	} else {
		var nameErr *selfname.Error
		if !errors.As(err, &nameErr) || nameErr.Reason != selfname.SyncedBin {
			t.Errorf("name refusal: %v", err)
		}
	}
	if helper.Installed(b.Ctx) {
		t.Error("helper remained after name unlink refusal")
	}
	if !exists(filepath.Join(bin, "zork")) {
		t.Error("name link was changed in synced bin")
	}
	if _, ok := selfname.Record(b.Ctx); !ok {
		t.Error("name record was discarded after refusal")
	}
}

func TestSetupRemoveReportsNameAndHelperFailures(t *testing.T) {
	b := nameBox(t)
	if code, _, stderr := runName(t, b, "zork"); code != 0 {
		t.Fatalf("initial name: %d %s", code, stderr)
	}
	if err := helper.Install(b.Ctx); err != nil {
		t.Fatal(err)
	}
	service := filepath.Join(b.Home, ".config", "systemd", "user", "homebot-tidy.service")
	if err := os.Remove(service); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(service, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(service, "foreign"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(b.Home, ".local", "bin")
	realBin := filepath.Join(b.Home, ".local", "bin-real")
	if err := os.Rename(bin, realBin); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realBin, bin); err != nil {
		t.Fatal(err)
	}
	err := setupRun(b.Ctx, true, setupOptions{})
	var nameErr *selfname.Error
	var pathErr *os.PathError
	if !errors.As(err, &nameErr) || nameErr.Reason != selfname.SyncedBin || !errors.As(err, &pathErr) || pathErr.Path != service {
		t.Errorf("wanted both name and helper errors, got %v", err)
	}
	if mustRead(t, filepath.Join(service, "foreign")) != "keep" {
		t.Error("foreign helper path changed")
	}
}

func TestSetupRemoveFinishesOldNameRecord(t *testing.T) {
	b := nameBox(t)
	if code, _, stderr := runName(t, b, "zork"); code != 0 {
		t.Fatalf("initial name: %d %s", code, stderr)
	}
	previous := selfname.AfterStep
	selfname.AfterStep = func(step int) error {
		if step == 3 {
			return errors.New("simulated interruption")
		}
		return nil
	}
	t.Cleanup(func() { selfname.AfterStep = previous })
	if code, _, _ := runName(t, b, "zara"); code == 0 {
		t.Fatal("rename unexpectedly completed")
	}
	selfname.AfterStep = previous
	if rec, ok := selfname.Record(b.Ctx); !ok || rec.Old != "zork" {
		t.Fatalf("interrupted record: %+v, %v", rec, ok)
	}
	if code := execute(b.Ctx, "setup", "--remove"); code != 0 {
		t.Fatalf("remove: %d %s", code, b.Stderr)
	}
	for _, name := range []string{"zork", "zara"} {
		if exists(filepath.Join(b.Home, ".local", "bin", name)) {
			t.Errorf("%s link remains", name)
		}
	}
	if _, ok := selfname.Record(b.Ctx); ok {
		t.Error("record remains")
	}
}

// setup --persona is judged like persona use: the persona must exist and
// load, and a refusal changes nothing on the machine.
func TestSetupRefusesAPersonaThatPersonaUseWouldRefuse(t *testing.T) {
	for _, tc := range []struct {
		name, id, file, want string
	}{
		{"missing", "nosuch", "", "No persona named nosuch"},
		{"unusable", "broken", "extends = \"nosuch-parent\"\n", "Persona broken is unusable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := guidedSandbox(t, nil)
			if tc.file != "" {
				dir := filepath.Join(b.Ctx.ConfigDir, "personas")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, tc.id+".toml"), []byte(tc.file), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			answering(b, false)
			if code := execute(b.Ctx, "setup", "--persona="+tc.id); code != 1 {
				t.Fatalf("exit %d, stderr %q", code, b.Stderr.String())
			}
			if b.Stdout.Len() != 0 || !strings.Contains(b.Stderr.String(), tc.want) {
				t.Fatalf("stdout %q, stderr %q, want %q", b.Stdout.String(), b.Stderr.String(), tc.want)
			}
			if exists(filepath.Join(b.Home, ".home")) || exists(filepath.Join(b.Home, ".local", "bin")) || exists(b.Root) {
				t.Fatal("a refused persona changed the home")
			}
		})
	}
}

func TestSetupAcceptsPersonaNone(t *testing.T) {
	b := guidedSandbox(t, map[string]string{"SHELL": "/bin/zsh"})
	answering(b, true, "n", "n", "y") // new repo, decline keys, accept PATH line
	if code := execute(b.Ctx, "setup", "--persona=none"); code != 0 {
		t.Fatalf("exit %d: %s", code, b.Stderr)
	}
	out := b.Stdout.String()
	if strings.Contains(out, "Robot choice") || !strings.Contains(out, "Persona in this repo: none") {
		t.Errorf("status line in %q", out)
	}
	if config := mustRead(t, filepath.Join(b.Root, "homebot.yml")); !strings.Contains(config, "persona: none") {
		t.Errorf("homebot.yml: %q", config)
	}
}

// The setup warning says the reason inside a sentence of its own, so it must
// be given the bare reason, not the whole refusal a failing command prints:
// that one ends with the persona's line and punctuation, and nested it
// doubled the stops ("can't answer to that one.. Homebot will keep working")
// and said the refusal twice. The preview goldens fill {reason} with a
// sample, so only a real refusal shows it.
func TestSetupBlockedNameWarningReadsAsOneMessage(t *testing.T) {
	b := personaBox(t)
	onPath := filepath.Join(b.Dir, "elsewhere")
	if err := os.MkdirAll(onPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(onPath, "zork"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": onPath + string(os.PathListSeparator) + b.Ctx.Env.Get("PATH")})
	taken, err := nameTaken(b.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[selfname.Reason]string{}
	for _, name := range []string{"BAD NAME", "man", "git", "zork"} {
		v := selfname.Check(b.Ctx, name, taken)
		if v.OK() {
			t.Fatalf("%s is not refused", name)
		}
		reasons[v.Reason] = name
	}
	for _, name := range selfname.ShellWords {
		if v := selfname.Check(b.Ctx, name, taken); !v.OK() && v.Reason == selfname.Shell {
			reasons[v.Reason] = name
			break
		}
	}
	if len(reasons) < 5 {
		t.Fatalf("want a syntax, reserved, shell, tool and on-PATH refusal, got %v", reasons)
	}

	voices := []string{""}
	voices = append(voices, persona.Builtins()...)
	for _, id := range voices {
		name := id
		if id == "" {
			name = "plain"
		}
		for reason, blocked := range reasons {
			t.Run(name+"/"+string(reason), func(t *testing.T) {
				fork := personaPreviewFork(b.Ctx, id, "light")
				if id == "" {
					fork.Plain = true
				}
				verdict := selfname.Check(fork, blocked, taken)
				if verdict.Reason != reason {
					t.Fatalf("%s is refused as %s, want %s", blocked, verdict.Reason, reason)
				}
				why, known := nameReason(fork, verdict.Reason)
				if !known || why == "" {
					t.Fatalf("no reason text for %s: %q", reason, why)
				}
				got := ansi.Strip(voice.Render(fork, msg.SetupWarningNameBlocked(blocked, why)))
				for _, bad := range []string{"..", ".!", "!.", "?.", "!!.", "]]", "[[", "))", ")]", "].", "Cannot use"} {
					if strings.Contains(got, bad) {
						t.Errorf("%q in %q", bad, got)
					}
				}
				if n := strings.Count(got, why); n != 1 {
					t.Errorf("reason %q appears %d times in %q", why, n, got)
				}
				if n := strings.Count(got, "cannot be used as a command here"); n != 1 {
					t.Errorf("refusal said %d times in %q", n, got)
				}
				if !strings.Contains(got, "Homebot will keep working") {
					t.Errorf("lost the reassurance: %q", got)
				}
			})
		}
	}
}
