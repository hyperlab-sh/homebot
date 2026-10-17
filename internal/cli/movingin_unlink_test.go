package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/helper"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func runUnlinkIn(t *testing.T, sb *testutil.Box) string {
	t.Helper()
	sb.Stdout.Reset()
	cmd := newUnlinkCmd(sb.Ctx)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unlink: %v", err)
	}
	return sb.Stdout.String()
}

func linkAllIn(t *testing.T, sb *testutil.Box) {
	t.Helper()
	cmd := newLinkCmd(sb.Ctx)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("link: %v", err)
	}
}

// unlink lasts only until the helper's next tidy, which links everything
// again. While the helper is installed, unlink says so and how to keep a
// file unlinked; without it, it does not.
func TestUnlinkSaysTheHelperLinksAgain(t *testing.T) {
	const hint = "The background helper links them again within the hour"

	t.Run("no helper", func(t *testing.T) {
		sb := testutil.Sandbox(t, testutil.Options{})
		sb.Files(map[string]string{"home/.home/autolink/common/zshrc": "z"})
		linkAllIn(t, sb)
		if out := runUnlinkIn(t, sb); strings.Contains(out, hint) {
			t.Errorf("hint without a helper:\n%s", out)
		}
	})

	t.Run("helper installed", func(t *testing.T) {
		sb := testutil.Sandbox(t, testutil.Options{})
		sb.Files(map[string]string{"home/.home/autolink/common/zshrc": "z"})
		for _, f := range helper.Files(sb.Ctx) {
			if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(f, []byte("unit"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if !helper.Installed(sb.Ctx) {
			t.Fatal("helper files written but not Installed")
		}
		linkAllIn(t, sb)
		out := runUnlinkIn(t, sb)
		if !strings.Contains(out, hint) || !strings.Contains(out, "take it out of the repo") {
			t.Errorf("no helper hint:\n%s", out)
		}
		if _, err := os.Lstat(filepath.Join(sb.Home, ".zshrc")); err == nil {
			t.Error(".zshrc still there after unlink")
		}

		// Nothing removed: nothing to say about the helper.
		if out := runUnlinkIn(t, sb); strings.Contains(out, hint) {
			t.Errorf("hint with nothing removed:\n%s", out)
		}
	})
}
