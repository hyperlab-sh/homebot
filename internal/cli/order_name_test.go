package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/helper"
	"github.com/hyperlab-sh/homebot/internal/selfname"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func nameBox(t *testing.T) *testutil.Box {
	t.Helper()
	b := testutil.Sandbox(t, testutil.Options{})
	b.WithStubs(map[string]string{"homebot": "exit 0", "systemctl": "exit 0"})
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": b.Bin, "SHELL": "/bin/zsh"})
	installTesterPersona(t, b.Ctx)
	return b
}

func TestNameKeepsAdoptedOldLink(t *testing.T) {
	b := nameBox(t)
	old := filepath.Join(b.Home, ".local", "bin", "zork")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(b.Bin, "homebot"), old); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runName(t, b, "zork"); code != 0 {
		t.Fatalf("adopt: %d %s", code, stderr)
	}
	if rec, ok := selfname.Record(b.Ctx); !ok || rec.Made {
		t.Fatalf("record = %+v, %v", rec, ok)
	}
	if code, out, stderr := runName(t, b, "zara"); code != 0 || strings.Contains(out, "zork stops working") {
		t.Fatalf("rename adopted: %d %q %q", code, out, stderr)
	}
	if _, err := os.Lstat(old); err != nil {
		t.Fatalf("adopted link removed: %v", err)
	}
}

func TestNameLeavesForeignTemporaryPathAndCurrentLink(t *testing.T) {
	b := nameBox(t)
	if code, _, stderr := runName(t, b, "zork"); code != 0 {
		t.Fatalf("initial: %d %s", code, stderr)
	}
	link := filepath.Join(b.Home, ".local", "bin", "zork")
	before, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	newBin := filepath.Join(b.Dir, "new-bin")
	if err := os.Mkdir(newBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newBin, "homebot"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": newBin})
	temp := filepath.Join(b.Home, ".local", "bin", ".zork.homebot-tmp")
	if err := os.WriteFile(temp, []byte("foreign"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runName(t, b, "zork"); code != 1 || !strings.Contains(stderr, temp) {
		t.Fatalf("blocked repair: %d %q", code, stderr)
	}
	if got, err := os.Readlink(link); err != nil || got != before {
		t.Errorf("current link = %q, %v; want %q", got, err, before)
	}
	if data, _ := os.ReadFile(temp); string(data) != "foreign" {
		t.Errorf("foreign temp = %q", data)
	}
}

func TestNameResumesRenameAfterCrashSteps(t *testing.T) {
	for _, step := range []int{2, 3} {
		t.Run(string(rune('0'+step)), func(t *testing.T) {
			b := nameBox(t)
			if code, _, stderr := runName(t, b, "zork"); code != 0 {
				t.Fatalf("initial: %d %s", code, stderr)
			}
			previous := selfname.AfterStep
			selfname.AfterStep = func(got int) error {
				if got == step {
					return errors.New("simulated crash")
				}
				return nil
			}
			t.Cleanup(func() { selfname.AfterStep = previous })
			if code, _, _ := runName(t, b, "zara"); code == 0 {
				t.Fatal("the fault did not stop rename")
			}
			if !strings.Contains(readNameFile(t, b), "name: zara") {
				t.Fatal("resumable rename lost its config name")
			}
			selfname.AfterStep = previous
			if code, _, stderr := runName(t, b, "zara"); code != 0 {
				t.Fatalf("resume: %d %s", code, stderr)
			}
			if _, err := os.Lstat(filepath.Join(b.Home, ".local", "bin", "zork")); !os.IsNotExist(err) {
				t.Errorf("old link left behind: %v", err)
			}
			if rec, ok := selfname.Record(b.Ctx); !ok || rec.Name != "zara" || rec.Old != "" {
				t.Errorf("record = %+v, %v", rec, ok)
			}
		})
	}
}

func runName(t *testing.T, b *testutil.Box, args ...string) (int, string, string) {
	t.Helper()
	b.Stdout.Reset()
	b.Stderr.Reset()
	code := execute(b.Ctx, append([]string{"name"}, args...)...)
	return code, b.Stdout.String(), b.Stderr.String()
}

func readNameFile(t *testing.T, b *testutil.Box) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(b.Root, "homebot.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestNameCommandSetRepairRenameOff(t *testing.T) {
	b := nameBox(t)
	if code, _, stderr := runName(t, b, "zork"); code != 0 {
		t.Fatalf("set: %d %s", code, stderr)
	}
	link := filepath.Join(b.Home, ".local", "bin", "zork")
	if _, err := os.Lstat(link); err != nil {
		t.Fatal(err)
	}
	before := readNameFile(t, b)
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runName(t, b, "zork"); code != 0 {
		t.Fatalf("repair: %d %s", code, stderr)
	}
	if got := readNameFile(t, b); got != before {
		t.Errorf("same name changed config: %q -> %q", before, got)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("did not repair: %v", err)
	}
	if code, out, stderr := runName(t, b, "zara"); code != 0 || !strings.Contains(out, "zork stops working") {
		t.Fatalf("rename: %d %q %q", code, out, stderr)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("old link remains: %v", err)
	}
	if code, _, stderr := runName(t, b, "--off"); code != 0 {
		t.Fatalf("off: %d %s", code, stderr)
	}
	if strings.Contains(readNameFile(t, b), "name:") {
		t.Error("name key remains")
	}
}

func TestNameExplainsConfigDifferentFromWorkingRecord(t *testing.T) {
	b := nameBox(t)
	if code, _, stderr := runName(t, b, "zork"); code != 0 {
		t.Fatalf("set: %d %s", code, stderr)
	}
	file := filepath.Join(b.Root, "homebot.yml")
	before := readNameFile(t, b)
	if err := os.WriteFile(file, []byte(strings.Replace(before, "name: zork", "name: zara", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(b.Home, ".local", "bin")
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": bin + string(os.PathListSeparator) + b.Bin})
	if code, out, stderr := runName(t, b); code != 0 || !strings.Contains(out, "Configured name: zara") || !strings.Contains(out, "Effective name here: zork") || !strings.Contains(out, "This machine still uses zork; run homebot name zara") {
		t.Fatalf("show mismatch: %d %q %q", code, out, stderr)
	}
}

func TestNameRefusalKeepsConfigAndForeignFile(t *testing.T) {
	b := nameBox(t)
	foreign := filepath.Join(b.Home, ".local", "bin", "zork")
	if err := os.MkdirAll(filepath.Dir(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreign, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := readNameFile(t, b)
	if code, _, _ := runName(t, b, "zork"); code == 0 {
		t.Fatal("foreign file was accepted")
	}
	if got := readNameFile(t, b); got != before {
		t.Error("refusal changed config")
	}
	if data, _ := os.ReadFile(foreign); string(data) != "keep" {
		t.Errorf("foreign file = %q", data)
	}
}

func TestNameChecksMachineReservationsBeforeEditing(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		prepare      func(t *testing.T, b *testutil.Box)
	}{
		{"x", "2 to 32 lowercase", nil},
		{"save", "homebot uses it as a command", nil},
		{"alias", "shell uses that word", nil},
		{"git", "runs a program by that name", nil},
		{"zork", "on PATH", func(t *testing.T, b *testutil.Box) {
			b.WithStubs(map[string]string{"zork": "exit 0"})
		}},
		{"zork", "home repo already places", func(t *testing.T, b *testutil.Box) {
			path := filepath.Join(b.Root, "autolink", "common", "local", "bin", "zork")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("repo file"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			b := nameBox(t)
			if tc.prepare != nil {
				tc.prepare(t, b)
			}
			before := readNameFile(t, b)
			if code, _, stderr := runName(t, b, tc.name); code != 1 || !strings.Contains(stderr, tc.reason) {
				t.Errorf("name %s: exit %d, stderr %q", tc.name, code, stderr)
			}
			if got := readNameFile(t, b); got != before {
				t.Errorf("refusal changed config: %q", got)
			}
		})
	}
}

func TestNameNeedsAHomeRepo(t *testing.T) {
	c, _, stderr := noRepo(t)
	if code := execute(c, "name", "zork"); code != 1 || stderr.Len() == 0 {
		t.Errorf("without repo: exit %d, stderr %q", code, stderr)
	}
}

func TestNameReinstallsHelperOnlyWhenShellWillFindIt(t *testing.T) {
	for _, rc := range []bool{false, true} {
		t.Run(map[bool]string{false: "declined", true: "rc line"}[rc], func(t *testing.T) {
			b := nameBox(t)
			if err := helper.Install(b.Ctx); err != nil {
				t.Fatal(err)
			}
			before, ok := helper.RecordedPath(b.Ctx)
			if !ok || strings.Contains(before, ".local/bin") {
				t.Fatalf("initial helper PATH = %q", before)
			}
			if rc {
				if err := os.WriteFile(filepath.Join(b.Home, ".zshrc"), []byte("export PATH=\"$HOME/.local/bin:$PATH\"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if code, _, stderr := runName(t, b, "zork"); code != 0 {
				t.Fatalf("name: %d %s", code, stderr)
			}
			got, ok := helper.RecordedPath(b.Ctx)
			if !ok || strings.Contains(got, ".local/bin") != rc {
				t.Errorf("helper PATH = %q, rc=%v", got, rc)
			}
		})
	}
}

func TestNameDoesNotReinstallHelperForInertRCMentions(t *testing.T) {
	for _, tc := range []struct{ name, shell, file, line string }{
		{"echo", "zsh", ".zshrc", "echo 'PATH=$HOME/.local/bin:$PATH'\n"},
		{"other variable", "zsh", ".zshrc", "OTHER_PATH=$HOME/.local/bin:$PATH\n"},
		{"comment", "zsh", ".zshrc", "# export PATH=$HOME/.local/bin:$PATH\n"},
		{"inline PATH comment", "zsh", ".zshrc", "PATH=/usr/bin # TODO add $HOME/.local/bin later\n"},
		{"inline fish comment", "fish", ".config/fish/config.fish", "fish_add_path /usr/bin # $HOME/.local/bin later\n"},
		{"PATH then echo", "zsh", ".zshrc", "PATH=/usr/bin; echo \"$HOME/.local/bin\"\n"},
		{"fish then echo", "fish", ".config/fish/config.fish", "fish_add_path /usr/bin; echo \"$HOME/.local/bin\"\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := nameBox(t)
			b.Ctx.Env = b.Ctx.Env.With(map[string]string{"SHELL": "/bin/" + tc.shell})
			if err := helper.Install(b.Ctx); err != nil {
				t.Fatal(err)
			}
			rcFile := filepath.Join(b.Home, tc.file)
			if err := os.MkdirAll(filepath.Dir(rcFile), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(rcFile, []byte(tc.line), 0o644); err != nil {
				t.Fatal(err)
			}
			if code, _, stderr := runName(t, b, "zork"); code != 0 {
				t.Fatalf("name: %d %s", code, stderr)
			}
			if got, ok := helper.RecordedPath(b.Ctx); !ok || strings.Contains(got, ".local/bin") {
				t.Errorf("helper PATH unexpectedly changed: %q, %v", got, ok)
			}
		})
	}
}

func TestRCPathLine(t *testing.T) {
	for _, tc := range []struct {
		name, shell, line string
		want              bool
	}{
		{"installed zsh", "zsh", `export PATH="$HOME/.local/bin:$PATH"`, true},
		{"installed fish", "fish", `fish_add_path $HOME/.local/bin`, true},
		{"assignment", "bash", `PATH="$PATH:${HOME}/.local/bin"`, true},
		{"unquoted assignment", "zsh", `PATH=$HOME/.local/bin:$PATH`, true},
		{"inline comment", "zsh", `export PATH="$HOME/.local/bin:$PATH" # installed`, true},
		{"fish quoted", "fish", `fish_add_path "$HOME/.local/bin" # installed`, true},
		{"fish second argument", "fish", `fish_add_path /usr/bin $HOME/.local/bin`, true},
		{"full comment", "zsh", `# export PATH="$HOME/.local/bin:$PATH"`, false},
		{"quoted prose", "zsh", `echo 'export PATH="$HOME/.local/bin:$PATH"'`, false},
		{"other assignment", "zsh", `OTHER_PATH=$HOME/.local/bin:$PATH`, false},
		{"inline comment mention", "zsh", `PATH=/usr/bin # $HOME/.local/bin later`, false},
		{"PATH then echo", "zsh", `PATH=/usr/bin; echo "$HOME/.local/bin"`, false},
		{"PATH then chained echo", "zsh", `PATH=/usr/bin && echo "$HOME/.local/bin"`, false},
		{"single quoted PATH", "zsh", `PATH='$HOME/.local/bin:$PATH'`, false},
		{"command substitution", "zsh", `PATH="$(echo "$HOME/.local/bin"):$PATH"`, false},
		{"quoted path fragment", "zsh", `PATH="/usr/bin:$HOME/.local/bin-not"`, false},
		{"fish comment mention", "fish", `fish_add_path /usr/bin # $HOME/.local/bin later`, false},
		{"fish then echo", "fish", `fish_add_path /usr/bin; echo "$HOME/.local/bin"`, false},
		{"fish quoted prose", "fish", `echo 'fish_add_path $HOME/.local/bin'`, false},
		{"fish single quoted", "fish", `fish_add_path '$HOME/.local/bin'`, false},
		{"fish unbalanced quote", "fish", `fish_add_path "$HOME/.local/bin`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := helper.RCPathLine(tc.line, tc.shell); got != tc.want {
				t.Errorf("RCPathLine(%q, %q) = %t, want %t", tc.line, tc.shell, got, tc.want)
			}
		})
	}
}
