package cli

import (
	"slices"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/selfname"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func TestReservedNamesHoldEveryCommand(t *testing.T) {
	got := reservedNames()
	if !slices.IsSorted(got) {
		t.Errorf("reservedNames is not sorted: %q", got)
	}
	if len(slices.Compact(slices.Clone(got))) != len(got) {
		t.Errorf("reservedNames has duplicates: %q", got)
	}
	want := []string{"help", "version", "completion", "man", "homebot", "hook", "refresh"}
	for _, r := range registry {
		want = append(want, r.name)
	}
	for _, name := range want {
		if !slices.Contains(got, name) {
			t.Errorf("reservedNames lacks %q", name)
		}
	}
}

// The shells' words have their own list and reason in selfname (D3), so
// reservedNames leaves them out: "if" is the shell's, not homebot's.
func TestReservedNamesLeaveTheShellWordsOut(t *testing.T) {
	for _, w := range []string{"if", "cd", "test", "time"} {
		if slices.Contains(reservedNames(), w) {
			t.Errorf("reservedNames holds the shell word %q", w)
		}
	}
}

// Every command the built tree has, hidden ones included, is reserved: a
// command added later can't be taken as a name.
func TestEveryCommandOfTheTreeIsReserved(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	reserved := reservedNames()
	root := NewRoot(ctx, Build{})
	for _, cmd := range root.Commands() {
		if !slices.Contains(reserved, cmd.Name()) {
			t.Errorf("the command %q is not reserved", cmd.Name())
		}
		for _, alias := range cmd.Aliases {
			if !slices.Contains(reserved, alias) {
				t.Errorf("the alias %q of %q is not reserved", alias, cmd.Name())
			}
		}
	}
}

func TestCheckRefusesReservedNamesAndAcceptsOthers(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": b.Bin})
	taken := selfname.Taken{Reserved: reservedNames()}
	for name, want := range map[string]selfname.Reason{
		"save":    selfname.Reserved,
		"version": selfname.Reserved,
		"refresh": selfname.Reserved,
		"homebot": selfname.Reserved,
		"if":      selfname.Shell,
		"git":     selfname.Tool,
		"zork":    "",
		"ab-12":   "",
	} {
		if got := selfname.Check(b.Ctx, name, taken).Reason; got != want {
			t.Errorf("Check(%q) = %q, want %q", name, got, want)
		}
	}
}
