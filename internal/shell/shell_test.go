package shell_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/shell"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

const eval = "(( $+commands[homebot] )) && eval \"$(homebot shell zsh)\"\n"

func TestIntegrated(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		zdot  string // relative to home
		want  bool
	}{
		{"nothing set up", nil, "", false},
		{"the line in ~/.zshrc", map[string]string{"home/.zshrc": "export X=1\n" + eval}, "", true},
		{"rc.d under ~/.config/zsh with ZDOTDIR unset", map[string]string{
			"home/.config/zsh/.zshrc":             "for f in $ZDOTDIR/rc.d/*.zsh; do source $f; done\n",
			"home/.config/zsh/rc.d/90-prompt.zsh": eval,
		}, "", true},
		{"conf.d under a custom ZDOTDIR", map[string]string{"home/dots/zsh/conf.d/homebot.zsh": "eval \"$(homebot shell zsh)\"\n"}, "dots/zsh", true},
		{"custom ZDOTDIR hides the default location", map[string]string{"home/.config/zsh/rc.d/a.zsh": eval}, "dots/zsh", false},
		{"absolute binary path", map[string]string{"home/.zshrc": "eval \"$(/home/me/.local/bin/homebot shell zsh)\"\n"}, "", true},
		{"mentioning homebot is not the integration", map[string]string{"home/.zshrc": "alias dh=homebot\n# homebot is great\n"}, "", false},
		{"the old source line no longer counts", map[string]string{"home/.zshrc": "source ~/.home/homebot/shell/homebot.zsh\n"}, "", false},
		{"a commented-out line does not count", map[string]string{"home/.zshrc": "# " + eval + "  # eval \"$(homebot shell zsh)\"\n"}, "", false},
		{"other shells do not count", map[string]string{"home/.zshrc": "eval \"$(homebot shell bash)\"\n"}, "", false},
		{"files zsh never sources", map[string]string{
			"home/.config/zsh/rc.d/90-prompt.zsh.off": eval,
			"home/.config/zsh/notes.md":               eval,
		}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := testutil.Sandbox(t, testutil.Options{})
			b.Files(tc.files)
			c := *b.Ctx
			if tc.zdot != "" {
				c.Env = c.Env.With(map[string]string{"ZDOTDIR": filepath.Join(b.Home, tc.zdot)})
			}
			if got := shell.Integrated(&c); got != tc.want {
				t.Fatalf("Integrated = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestZshReadsTheCacheFiles(t *testing.T) {
	z := shell.Zsh()
	for _, want := range []string{"/nudge", "/count", "/symbol", "HOMEBOT_COUNT", "XDG_CACHE_HOME"} {
		if !strings.Contains(z, want) {
			t.Errorf("script lacks %q", want)
		}
	}
	if strings.Contains(z, "source ~/.home") {
		t.Error("script still tells you to source it from the repo")
	}
}

const compdef = "if (( $+functions[compdef] && $+_comps[homebot] )); then compdef zork=homebot; fi\n"

// namedSandbox is a sandbox with a homebot on PATH, ~/.local/bin/zork linked to
// the homebot, and a record for zork with the given extra lines.
func namedSandbox(t *testing.T, extra string) (*testutil.Box, string) {
	t.Helper()
	b := testutil.Sandbox(t, testutil.Options{})
	stable := filepath.Join(b.Bin, "homebot")
	if err := os.WriteFile(stable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := stable
	link := filepath.Join(b.Home, ".local", "bin", "zork")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(b.Ctx.StateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(b.Ctx.StateDir, "name")
	if err := os.WriteFile(record, []byte("name=zork\ntarget="+target+"\n"+extra), 0o600); err != nil {
		t.Fatal(err)
	}
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": filepath.Dir(link) + ":" + b.Bin + ":/usr/bin:/bin"})
	return b, link
}

func TestZshForRecordedName(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	if got := shell.ZshFor(b.Ctx); got != shell.Zsh() {
		t.Fatal("no record changed the neutral zsh script")
	}

	b, link := namedSandbox(t, "made=true\n")
	if got := shell.ZshFor(b.Ctx); got != shell.Zsh()+compdef {
		t.Fatalf("working link: script suffix = %q, want %q", strings.TrimPrefix(got, shell.Zsh()), compdef)
	}
	c := *b.Ctx
	c.Env = c.Env.With(map[string]string{"PATH": b.Bin + ":/usr/bin:/bin"})
	if got := shell.ZshFor(&c); got != shell.Zsh()+compdef {
		t.Fatal("PATH without ~/.local/bin removed named completion")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(b.Bin, "missing"), link); err != nil {
		t.Fatal(err)
	}
	if got := shell.ZshFor(b.Ctx); got != shell.Zsh() {
		t.Fatal("broken link added named completion")
	}
}

func TestZshForRecordsThatDoNotCount(t *testing.T) {
	t.Run("pending", func(t *testing.T) {
		b, _ := namedSandbox(t, "made=false\npending=true\n")
		if got := shell.ZshFor(b.Ctx); got != shell.Zsh() {
			t.Fatalf("a pending record added %q", strings.TrimPrefix(got, shell.Zsh()))
		}
	})
	t.Run("link to another binary", func(t *testing.T) {
		b, _ := namedSandbox(t, "made=true\n")
		other := filepath.Join(b.Bin, "other")
		if err := os.WriteFile(other, []byte("#!/bin/sh\n# other\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(b.Home, ".local", "bin", "zork")
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(other, link); err != nil {
			t.Fatal(err)
		}
		if got := shell.ZshFor(b.Ctx); got != shell.Zsh() {
			t.Fatalf("a link to another binary added %q", strings.TrimPrefix(got, shell.Zsh()))
		}
	})
	t.Run("link is a regular file", func(t *testing.T) {
		b, link := namedSandbox(t, "made=true\n")
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(link, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := shell.ZshFor(b.Ctx); got != shell.Zsh() {
			t.Fatalf("a regular file added %q", strings.TrimPrefix(got, shell.Zsh()))
		}
	})
}

// TestZshForRunsCleanUnderCompinit sources the output in a real zsh whose
// compinit ran first, as oh-my-zsh, prezto and zim do. Before the guard, the
// compdef line printed "unknown command or service: homebot" and made the
// eval fail whenever no completion for homebot was registered.
func TestZshForRunsCleanUnderCompinit(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not installed")
	}
	cases := []struct {
		name       string
		recorded   bool
		registered bool
		wantComp   string // _comps[zork] after sourcing
	}{
		{"no name, no homebot completion", false, false, "unset"},
		{"no name, homebot completion", false, true, "unset"},
		{"name, no homebot completion", true, false, "unset"},
		{"name, homebot completion", true, true, "_files"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var script string
			if tc.recorded {
				b, _ := namedSandbox(t, "made=true\n")
				script = shell.ZshFor(b.Ctx)
			} else {
				script = shell.ZshFor(testutil.Sandbox(t, testutil.Options{}).Ctx)
			}
			dir := t.TempDir()
			file := filepath.Join(dir, "homebot.zsh")
			if err := os.WriteFile(file, []byte(script), 0o600); err != nil {
				t.Fatal(err)
			}
			// -u: accept insecure fpath dirs without prompting. Hosted CI runners
			// have group/world-writable dirs there, and compaudit would abort;
			// this test checks homebot's output, not the host's permissions.
			init := "autoload -Uz compinit; compinit -u -D\n"
			if tc.registered {
				init += "_comps[homebot]=_files\n"
			}
			// eval of the output, as the user's rc file does, then its status.
			run := init + "eval \"$(<$1)\"; rc=$?\nprint -r -- \"comp=${_comps[zork]-unset}\"\nexit $rc\n"
			cmd := exec.Command(zsh, "-f", "-c", run, "zsh", file)
			cmd.Env = []string{"HOME=" + dir, "PATH=/usr/bin:/bin"}
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("eval failed: %v\nstderr: %s", err, stderr.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
			if got := strings.TrimSpace(stdout.String()); got != "comp="+tc.wantComp {
				t.Fatalf("stdout = %q, want comp=%s", got, tc.wantComp)
			}
		})
	}
}
