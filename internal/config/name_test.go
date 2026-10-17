package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/fail"
)

func TestValidName(t *testing.T) {
	for _, s := range []string{"hb", "zork", "qq-9000", "g9", "a-", strings.Repeat("a", 32), "homebot"} {
		if !ValidName(s) {
			t.Errorf("ValidName(%q) = false, want true", s)
		}
	}
	for _, s := range []string{
		"", "z", "g", "Zork", "../x", "-x", "9z", "z_k", "z k", "zork\n", "é", "zé",
		strings.Repeat("a", 33),
	} {
		if ValidName(s) {
			t.Errorf("ValidName(%q) = true, want false", s)
		}
	}
}

func TestNameKey(t *testing.T) {
	for _, c := range []struct{ yaml, want string }{
		{"", ""},
		{"profiles:\n  default: a\n", ""},
		{"name: zork\n", "zork"},
		{"name: hb # my bot\n", "hb"},
		{"name: \"zork\"\n", "zork"},
		{"name: 'zork'\n", "zork"},
		{"name: homebot\n", ""},
		{"persona: tester\nname: zork\n", "zork"},
	} {
		if got := parse(t, c.yaml).Name; got != c.want {
			t.Errorf("%q: Name = %q, want %q", c.yaml, got, c.want)
		}
	}
}

func TestNameKeyRefused(t *testing.T) {
	long := strings.Repeat("a", 33)
	for _, c := range []struct{ yaml, id string }{
		{"name: \"\"\n", "config.error.name_syntax"},
		{"name: Zork\n", "config.error.name_syntax"},
		{"name: ../x\n", "config.error.name_syntax"},
		{"name: -x\n", "config.error.name_syntax"},
		{"name: z\n", "config.error.name_syntax"},
		{"name: " + long + "\n", "config.error.name_syntax"},
		{"name: \"3\"\n", "config.error.name_syntax"},
		{"name: 3\n", "config.error.name"},
		{"name: true\n", "config.error.name"},
		{"name: [a]\n", "config.error.name"},
		{"name: {a: b}\n", "config.error.name"},
		{"name:\n", "config.error.name"},
		{"name: null\n", "config.error.name"},
	} {
		_, err := Parse([]byte(c.yaml), "homebot.yml")
		var ue *fail.UserError
		if !errors.As(err, &ue) {
			t.Errorf("%q: error %v is not a *fail.UserError", c.yaml, err)
			continue
		}
		if text, ok := ue.Text(); !ok || text.ID() != c.id {
			t.Errorf("%q: the error holds %s, want %s", c.yaml, text, c.id)
		}
		if !strings.HasPrefix(err.Error(), "homebot.yml:1:7: ") {
			t.Errorf("%q: error %q is not positioned at the value", c.yaml, err)
		}
	}
}
