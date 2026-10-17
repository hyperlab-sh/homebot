package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

const cleanupBackup = ".a.homebot-backup-20261012-100000"

// cleanupBox is a sandbox with one backup, ~/.a's.
func cleanupBox(t *testing.T) *testutil.Box {
	t.Helper()
	sb := testutil.Sandbox(t, testutil.Options{Git: true})
	sb.Files(map[string]string{
		"home/.home/autolink/common/a": "r",
		"home/" + cleanupBackup:        "m",
	})
	return sb
}

func cleanupRun(t *testing.T, sb *testutil.Box, args ...string) string {
	t.Helper()
	sb.Stdout.Reset()
	sb.Stderr.Reset()
	root := NewRoot(sb.Ctx, Build{Version: "test"})
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.SetArgs(append([]string{"cleanup"}, args...))
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("cleanup %v: %v", args, err)
	}
	return ansi.Strip(sb.Stdout.String() + sb.Stderr.String())
}

func cleanupTerminal(sb *testutil.Box, input string) {
	yes := true
	sb.Ctx.StdinIsTerminal = &yes
	sb.Ctx.Stdin = strings.NewReader(input)
}

func TestCleanupWithoutATerminalListsThemAndSaysHowToReview(t *testing.T) {
	sb := cleanupBox(t)
	out := cleanupRun(t, sb)
	want := "  ~/" + cleanupBackup + "\nReview them in a terminal:\n  homebot cleanup\n"
	if out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
	if out := cleanupRun(t, sb, "--list"); out != "  ~/"+cleanupBackup+"\n" {
		t.Errorf("--list: out = %q", out)
	}
}

func TestCleanupListDoesNotAskEvenInATerminal(t *testing.T) {
	sb := cleanupBox(t)
	cleanupTerminal(sb, "1\n")
	if out := cleanupRun(t, sb, "--list"); out != "  ~/"+cleanupBackup+"\n" {
		t.Errorf("out = %q", out)
	}
	if _, err := os.Lstat(filepath.Join(sb.Home, cleanupBackup)); err != nil {
		t.Errorf("the backup is gone: %v", err)
	}
}

func TestCleanupWithNothingToDoSaysSo(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{Git: true})
	if out := cleanupRun(t, sb); !strings.Contains(out, "No backups to review.") {
		t.Errorf("out = %q", out)
	}
}

func TestCleanupInATerminalAsksWithFzfAndSumsUp(t *testing.T) {
	sb := cleanupBox(t)
	sb.WithStubs(map[string]string{"fzf": `cat >/dev/null; echo "delete  remove"`})
	cleanupTerminal(sb, "")
	out := cleanupRun(t, sb)
	if want := "deleted ~/" + cleanupBackup + "\nDeleted 1, used 0, kept 0.\n"; out != want {
		t.Errorf("out = %q, want %q", out, want)
	}
	if _, err := os.Lstat(filepath.Join(sb.Home, cleanupBackup)); err == nil {
		t.Error("the backup is still there")
	}
}

// cleanupWithoutFzf leaves only $WORK/bin and git on the PATH, so an fzf the
// host has in /usr/bin cannot count.
func cleanupWithoutFzf(t *testing.T, sb *testutil.Box) {
	t.Helper()
	git, err := proc.LookPath(sb.Ctx, "git")
	if err != nil {
		t.Fatal(err)
	}
	tools := filepath.Join(sb.Dir, "tools")
	if err := os.MkdirAll(tools, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(git, filepath.Join(tools, "git")); err != nil {
		t.Fatal(err)
	}
	sb.Ctx.Env = sb.Ctx.Env.With(map[string]string{"PATH": sb.Bin + string(filepath.ListSeparator) + tools})
}

func TestCleanupInATerminalWithoutFzfAsksWithTheSelect(t *testing.T) {
	sb := cleanupBox(t)
	cleanupWithoutFzf(t, sb)
	// Stdout is not a terminal, so the select asks on plain lines, as with
	// ACCESSIBLE set. Use is not offered for ~/.a, which is gone: delete,
	// keep, quit.
	cleanupTerminal(sb, "3\n")
	out := cleanupRun(t, sb)
	for _, want := range []string{"this backup is the only copy", "[1/1] ~/" + cleanupBackup, "3. quit", "Deleted 0, used 0, kept 1.\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("out = %q, want it to hold %q", out, want)
		}
	}
	if _, err := os.Lstat(filepath.Join(sb.Home, cleanupBackup)); err != nil {
		t.Errorf("the backup is gone: %v", err)
	}
}
