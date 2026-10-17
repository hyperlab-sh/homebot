package app

import (
	"slices"
	"testing"
)

func TestParseEnvironLaterPairWins(t *testing.T) {
	env := ParseEnviron([]string{"A=1", "B=x=y", "A=2", "NOEQUALS", "=hidden", "EMPTY="})
	if got := env.Get("A"); got != "2" {
		t.Errorf("A = %q, want the later pair's 2", got)
	}
	if got := env.Get("B"); got != "x=y" {
		t.Errorf("B = %q, want the text after the first =", got)
	}
	if _, ok := env.Lookup("NOEQUALS"); ok {
		t.Error("an entry without = was kept")
	}
	if v, ok := env.Lookup("EMPTY"); !ok || v != "" {
		t.Errorf("EMPTY = %q, %v; want set and empty", v, ok)
	}
	if _, ok := env.Lookup("MISSING"); ok {
		t.Error("MISSING reported as set")
	}
}

func TestEnvIsACopy(t *testing.T) {
	src := map[string]string{"A": "1"}
	env := NewEnv(src)
	src["A"] = "changed"
	if env.Get("A") != "1" {
		t.Error("NewEnv shares the caller's map")
	}

	with := env.With(map[string]string{"A": "2", "B": "3"})
	if env.Get("A") != "1" || env.Get("B") != "" {
		t.Error("With changed the Env it was called on")
	}
	if with.Get("A") != "2" || with.Get("B") != "3" {
		t.Errorf("With = A:%q B:%q, want the overrides", with.Get("A"), with.Get("B"))
	}
}

func TestEnvironIsSortedAndNeverNil(t *testing.T) {
	got := NewEnv(map[string]string{"B": "2", "A": "1", "C": ""}).Environ()
	if want := []string{"A=1", "B=2", "C="}; !slices.Equal(got, want) {
		t.Errorf("Environ() = %q, want %q", got, want)
	}
	if (Env{}).Environ() == nil {
		t.Error("an empty Env gives a nil list, which os/exec reads as: inherit the process environment")
	}
	if got := (Env{}).With(map[string]string{"A": "1"}).Get("A"); got != "1" {
		t.Errorf("With on the zero Env: A = %q", got)
	}
}

func TestEnvWithoutUnsetsOnlyTheKeysOnACopy(t *testing.T) {
	env := NewEnv(map[string]string{"A": "1", "B": "2", "C": ""})
	without := env.Without("A", "C", "MISSING")
	if got, want := without.Environ(), []string{"B=2"}; !slices.Equal(got, want) {
		t.Errorf("Without(A, C, MISSING) = %q, want %q", got, want)
	}
	if got, want := env.Environ(), []string{"A=1", "B=2", "C="}; !slices.Equal(got, want) {
		t.Errorf("Without changed the Env it was called on: %q", got)
	}
	if got := env.Without().Environ(); !slices.Equal(got, env.Environ()) {
		t.Errorf("Without() = %q, want everything kept", got)
	}
	if got := (Env{}).Without("A").Environ(); got == nil || len(got) != 0 {
		t.Errorf("Without on the zero Env = %#v, want an empty, non-nil list", got)
	}
}
