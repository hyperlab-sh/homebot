package ui_test

import (
	"testing"

	"github.com/hyperlab-sh/homebot/internal/ui"
)

// The expected values below are what src/ui.ts returns for the same input.

func TestTildify(t *testing.T) {
	for _, c := range []struct{ path, home, want string }{
		{"/home/u", "/home/u", "~"},
		{"/home/u/x", "/home/u", "~/x"},
		{"/home/user2", "/home/u", "/home/user2"},
		{"/home/u/", "/home/u", "~/"},
		{"rel/x", "/home/u", "rel/x"},
		{"/abs", "", "~/abs"},
		{"", "", "~"},
	} {
		if got := ui.Tildify(c.path, c.home); got != c.want {
			t.Errorf("Tildify(%q, %q) = %q, want %q", c.path, c.home, got, c.want)
		}
	}
}

func TestExpandHome(t *testing.T) {
	for _, c := range []struct{ path, want string }{
		{"~", "/h"},
		{"~/a", "/h/a"},
		{"~/", "/h/"},
		{"~user", "~user"},
		{"a/~/b", "a/~/b"},
		{"", ""},
	} {
		if got := ui.ExpandHome(c.path, "/h"); got != c.want {
			t.Errorf("ExpandHome(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"abc":                   "abc",
		"":                      "''",
		"a b":                   "'a b'",
		"it's":                  `'it'\''s'`,
		"a'b'c":                 `'a'\''b'\''c'`,
		"a/b.c-d_e@f%g+h=i:j,k": "a/b.c-d_e@f%g+h=i:j,k",
		"-flag":                 "-flag",
		"ünï":                   "'ünï'",
		"$HOME":                 "'$HOME'",
		"~":                     "'~'",
		"x\ny":                  "'x\ny'",
		"*":                     "'*'",
		`a\b`:                   `'a\b'`,
	} {
		if got := ui.ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
