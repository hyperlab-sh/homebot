package cli

import (
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// help-remote.test.ts "homebot remote from the CLI, without a terminal".
// The sandbox's Stdin is not a terminal, and a gh and a glab nobody is
// logged in for stand in for the real ones.
func TestRemoteFromTheCLIWithoutATerminal(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{Git: true})
	sb.WithStubs(map[string]string{"gh": "exit 1", "glab": "exit 1"})

	if code := execute(sb.Ctx, "remote"); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, sb.Stderr)
	}
	if !strings.Contains(sb.Stdout.String(), "Nothing is backed up yet") {
		t.Errorf("stdout:\n%s", sb.Stdout)
	}
	if sb.Stderr.Len() != 0 {
		t.Errorf("stderr:\n%s", sb.Stderr)
	}
	sb.Stdout.Reset()

	if code := execute(sb.Ctx, "remote", "a", "b"); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if got := sb.Stderr.String(); !strings.Contains(got, "Usage: homebot remote [url]") {
		t.Errorf("stderr:\n%s", got)
	}
	if sb.Stdout.Len() != 0 {
		t.Errorf("stdout:\n%s", sb.Stdout)
	}
}

func TestRemoteWithAURLSetsOriginThroughTheCLI(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{Git: true})
	bare := sb.Dir + "/remote.git"
	sb.GitIn(sb.Dir, "init", "-q", "--bare", "-b", "main", bare)
	if code := execute(sb.Ctx, "remote", bare); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, sb.Stderr)
	}
	want := "remote " + bare + "\nreachable " + bare + "\nNothing committed yet. This commits and pushes:\n  homebot save\n"
	if sb.Stdout.String() != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", sb.Stdout, want)
	}
}

func TestRemoteTakesNoOptions(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{Git: true})
	if code := execute(sb.Ctx, "remote", "--x"); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(sb.Stderr.String(), "Unknown option '--x'") {
		t.Errorf("stderr:\n%s", sb.Stderr)
	}
}
