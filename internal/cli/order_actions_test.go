package cli

import (
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// The TypeScript ignored anything after `actions` and listed the actions.
// An argument here is almost always a slip for `homebot action <name>`, so it
// is a usage error in the catalog's words (exit 1, as every usage error), not
// cobra's "unknown command".
func TestActionsWithAnArgumentIsAUsageError(t *testing.T) {
	for _, args := range [][]string{{"actions", "list"}, {"actions", "apt", "extra"}, {"actions", ""}} {
		sb := testutil.Sandbox(t, testutil.Options{Git: true})
		if code := execute(sb.Ctx, args...); code != 1 {
			t.Errorf("%q: exit %d, want 1", args, code)
		}
		got := sb.Stderr.String()
		if !strings.Contains(got, "Usage: homebot actions") || !strings.Contains(got, "homebot action <name>") {
			t.Errorf("%q: stderr:\n%s", args, got)
		}
		if strings.Contains(got, "unknown command") {
			t.Errorf("%q: cobra's own error leaked:\n%s", args, got)
		}
		if sb.Stdout.Len() != 0 {
			t.Errorf("%q: stdout:\n%s", args, sb.Stdout)
		}
	}
}

func TestActionsListsEveryKind(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{Git: true})
	sb.Files(map[string]string{"home/.home/manifests/actions/greet.sh": "#!/bin/sh\n# Say hello.\necho hi\n"})
	if code := execute(sb.Ctx, "actions"); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, sb.Stderr)
	}
	out := sb.Stdout.String()
	for _, want := range []string{"internal", "builtin", "custom", "greet"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
}
