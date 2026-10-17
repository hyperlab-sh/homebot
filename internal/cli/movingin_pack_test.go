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

// packCancelOnWrite is a writer that cancels a context, with a cause, when
// the first thing is written to it: the first path was packed and reported.
type packCancelOnWrite struct {
	sb     *testutil.Box
	cancel context.CancelCauseFunc
	cause  error
}

func (w *packCancelOnWrite) Write(p []byte) (int, error) {
	w.cancel(w.cause)
	return w.sb.Stdout.Write(p)
}

// After Ctrl-C no later path is packed, and the command ends with the
// cancellation's cause (a silent 130), as the other commands do.
func TestPackStopsBetweenPathsOnCtrlC(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{})
	sb.Files(map[string]string{"home/.first": "1", "home/.second": "2"})
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	c := sb.Ctx.WithContext(ctx)
	c.Stdout = &packCancelOnWrite{sb: sb, cancel: cancel, cause: fail.Silently(130)}

	cmd := newPackCmd(c)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs([]string{filepath.Join(sb.Home, ".first"), filepath.Join(sb.Home, ".second")})
	err := cmd.Execute()

	var exit *fail.Exit
	if !errors.As(err, &exit) || exit.Code != 130 {
		t.Fatalf("err = %v, want a silent exit 130", err)
	}
	if link, lerr := os.Readlink(filepath.Join(sb.Home, ".first")); lerr != nil || !strings.HasSuffix(link, "autolink/common/first") {
		t.Errorf(".first = %q, %v; want a link into the repo", link, lerr)
	}
	if st, serr := os.Lstat(filepath.Join(sb.Home, ".second")); serr != nil || st.Mode()&os.ModeSymlink != 0 {
		t.Errorf(".second was packed: %v, %v", st, serr)
	}
	if _, serr := os.Lstat(filepath.Join(sb.Root, "autolink/common/second")); serr == nil {
		t.Error(".second reached the repo")
	}
	if strings.Contains(sb.Stdout.String(), "second") {
		t.Errorf("stdout names .second:\n%s", sb.Stdout)
	}
}

// A path that fails after it was moved says where it went, on stderr, before
// the error; stdout stays empty for it.
func TestPackSaysWhereAMovedFileWentWhenALaterStepFails(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{Git: true})
	testutil.TestKeys(t, sb.Root)
	// Keys exist but cannot seal.
	sb.Files(map[string]string{
		"home/.home/private/recipients.txt": "this is not a key\n",
		"home/.netrc":                       "machine x password canary-5a3e91c4",
	})
	cmd := newPackCmd(sb.Ctx)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs([]string{"--private", filepath.Join(sb.Home, ".netrc")})
	if err := cmd.Execute(); err == nil {
		t.Fatal("pack succeeded with a recipients file that is not a key")
	}
	want := "moved ~/.netrc → private/common/netrc\n"
	if got := sb.Stderr.String(); !strings.HasPrefix(got, want) {
		t.Errorf("stderr = %q, want it to start with %q", got, want)
	}
	if sb.Stdout.Len() != 0 {
		t.Errorf("stdout = %q", sb.Stdout)
	}
	if strings.Contains(sb.Stderr.String(), "canary-5a3e91c4") {
		t.Error("the secret was printed")
	}
}

// Refusals before the move print no "moved" line.
func TestPackPrintsNoMovedLineForARefusal(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{})
	sb.Files(map[string]string{"home/.x": "x"})
	cmd := newPackCmd(sb.Ctx)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs([]string{"--layer", "macos", filepath.Join(sb.Home, ".x")})
	if err := cmd.Execute(); err == nil {
		t.Fatal("pack succeeded")
	}
	if sb.Stderr.Len() != 0 || sb.Stdout.Len() != 0 {
		t.Errorf("stdout %q, stderr %q", sb.Stdout, sb.Stderr)
	}
}
