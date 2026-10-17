package selfname

import (
	"slices"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// Under the sandbox there is no record, so the effective name is homebot,
// and every parity golden keeps printing homebot (D7, "Parity").
func TestEffectiveIsHomebotWithoutARecord(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	if got := Effective(b.Ctx); got != "homebot" {
		t.Errorf("Effective = %q, want homebot", got)
	}
	if _, ok := Record(b.Ctx); ok {
		t.Error("Record found a record in a fresh sandbox")
	}
}

func TestWordListsHaveNoDuplicates(t *testing.T) {
	for name, list := range map[string][]string{"ShellWords": ShellWords, "ToolNames": ToolNames} {
		if len(slices.Compact(slices.Sorted(slices.Values(list)))) != len(list) {
			t.Errorf("%s has duplicates", name)
		}
	}
}
