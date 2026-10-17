package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// linkCancelOn cancels a context, with a cause, the first time a line
// containing word is written: the command reported one step and the user
// pressed Ctrl-C right then.
type linkCancelOn struct {
	sb     *testutil.Box
	word   string
	cancel context.CancelCauseFunc
	cause  error
}

func (w *linkCancelOn) Write(p []byte) (int, error) {
	if strings.Contains(string(p), w.word) {
		w.cancel(w.cause)
	}
	return w.sb.Stdout.Write(p)
}

func runLinkCancelled(t *testing.T, sb *testutil.Box, word string, args ...string) error {
	t.Helper()
	ctx, cancel := context.WithCancelCause(context.Background())
	t.Cleanup(func() { cancel(nil) })
	c := sb.Ctx.WithContext(ctx)
	c.Stdout = &linkCancelOn{sb: sb, word: word, cancel: cancel, cause: fail.Silently(130)}
	cmd := newLinkCmd(c)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs(args)
	return cmd.Execute()
}

func wantExit130(t *testing.T, err error) {
	t.Helper()
	var exit *fail.Exit
	if !errors.As(err, &exit) || exit.Code != 130 {
		t.Fatalf("err = %v, want a silent exit 130", err)
	}
}

func isLink(path string) bool {
	st, err := os.Lstat(path)
	return err == nil && st.Mode()&os.ModeSymlink != 0
}

// Ctrl-C while links are being made: the link being made finishes, no
// other is made, the summary says what was done and the command exits 130.
func TestLinkStopsBetweenLinksOnCtrlC(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{})
	sb.Files(map[string]string{
		"home/.home/autolink/common/a": "a",
		"home/.home/autolink/common/b": "b",
		"home/.home/autolink/common/c": "c",
	})

	wantExit130(t, runLinkCancelled(t, sb, "link"))

	made := 0
	for _, n := range []string{".a", ".b", ".c"} {
		if isLink(filepath.Join(sb.Home, n)) {
			made++
		}
	}
	if made != 1 {
		t.Errorf("%d links made after Ctrl-C at the first, want 1", made)
	}
	out := sb.Stdout.String()
	if !strings.Contains(out, "1 change") {
		t.Errorf("no summary of the one change made:\n%s", out)
	}
	if !strings.Contains(out, "nothing else was moved or linked") {
		t.Errorf("no word that it stopped:\n%s", out)
	}
}

// Ctrl-C after files were moved aside: nothing is linked.
func TestLinkBackupStopsBeforeLinkingOnCtrlC(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{})
	sb.Files(map[string]string{
		"home/.home/autolink/common/a": "repo a",
		"home/.home/autolink/common/b": "repo b",
		"home/.a":                      "mine a",
		"home/.b":                      "mine b",
	})

	wantExit130(t, runLinkCancelled(t, sb, "moved aside", "--backup"))

	for _, n := range []string{".a", ".b"} {
		if isLink(filepath.Join(sb.Home, n)) {
			t.Errorf("%s was linked after Ctrl-C", n)
		}
	}
	out := sb.Stdout.String()
	if !strings.Contains(out, "nothing else was moved or linked") {
		t.Errorf("no word that it stopped:\n%s", out)
	}
}

// Ctrl-C before anything starts: nothing is moved aside or linked.
func TestLinkBackupDoesNothingWhenAlreadyCancelled(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{})
	sb.Files(map[string]string{
		"home/.home/autolink/common/a": "repo a",
		"home/.a":                      "mine a",
	})
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(fail.Silently(130))
	cmd := newLinkCmd(sb.Ctx.WithContext(ctx))
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs([]string{"--backup"})
	wantExit130(t, cmd.Execute())

	if got, err := os.ReadFile(filepath.Join(sb.Home, ".a")); err != nil || string(got) != "mine a" || isLink(filepath.Join(sb.Home, ".a")) {
		t.Errorf(".a = %q, %v; want the user's file left in place", got, err)
	}
	if strings.Contains(sb.Stdout.String(), "moved aside") {
		t.Errorf("something was moved aside:\n%s", sb.Stdout)
	}
}
