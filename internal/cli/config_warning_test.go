package cli

import (
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func TestConfigUpdateWarningPrintsOnceWhenACommandLoadsTheRepo(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{Config: "update: atuo\n"})
	b.Ctx.Cwd, b.Ctx.Root, b.Ctx.Config = b.Root, "", nil
	if code := execute(b.Ctx, "cleanup", "--list"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, b.Stderr.String())
	}
	if b.Ctx.Config == nil || b.Ctx.Config.Update != config.UpdateOff {
		t.Fatalf("bad mode: %+v", b.Ctx.Config)
	}
	if n := strings.Count(b.Stderr.String(), "Unknown update value"); n != 1 {
		t.Errorf("warning printed %d times: %q", n, b.Stderr.String())
	}
	if !strings.Contains(b.Stderr.String(), "homebot: Unknown update value") {
		t.Errorf("warning lacks the catalog prefix: %q", b.Stderr.String())
	}
	reportConfigWarnings(b.Ctx)
	if n := strings.Count(b.Stderr.String(), "Unknown update value"); n != 1 {
		t.Errorf("warning repeated: %q", b.Stderr.String())
	}
}

func TestQuietConfigWarningUsesDebugLog(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{Config: "update: [auto]\n"})
	b.Ctx.Cwd, b.Ctx.Root, b.Ctx.Config = b.Root, "", nil
	b.Ctx.ConfigWarningsQuiet = true
	if err := b.Ctx.LocateRoot(); err != nil {
		t.Fatal(err)
	}
	reportConfigWarnings(b.Ctx)
	if b.Stderr.Len() != 0 {
		t.Errorf("quiet warning reached stderr: %q", b.Stderr.String())
	}
}

func TestConfigWarningIsNotSilencedByArgumentAfterOptionTerminator(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{Config: "update: atuo\n"})
	b.Ctx.Cwd, b.Ctx.Root, b.Ctx.Config = b.Root, "", nil
	if code := execute(b.Ctx, "tidy", "--", "--quiet"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, b.Stderr.String())
	}
	if n := strings.Count(b.Stderr.String(), "Unknown update value"); n != 1 {
		t.Errorf("warning printed %d times: %q", n, b.Stderr.String())
	}
}
