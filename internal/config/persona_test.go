package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/yamledit"
)

func TestValidPersonaID(t *testing.T) {
	for _, s := range []string{
		"a", "tester", "zork", "ab-12", "qq-9000", "zed-5", "none", "neatpik",
		"a-", "a--b", "x9",
		strings.Repeat("a", 32),
	} {
		if !ValidPersonaID(s) {
			t.Errorf("ValidPersonaID(%q) = false, want true", s)
		}
	}
	for _, s := range []string{
		"", "-a", "9a", "Tester", "teSter", "a_b", "a.b", "a b", " a", "a ",
		"../x", "x/y", "x\\y", "..", ".", "a\x00", "a\n", "é", "aé",
		"ａ", // fullwidth a
		strings.Repeat("a", 33),
	} {
		if ValidPersonaID(s) {
			t.Errorf("ValidPersonaID(%q) = true, want false", s)
		}
	}
}

func TestPersonaAndNameDefaultToUnset(t *testing.T) {
	c := Default()
	if c.Persona != "" || c.Name != "" {
		t.Errorf("Default() has persona %q, name %q; want both unset", c.Persona, c.Name)
	}
}

func TestPersonaKey(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{"", ""},
		{"persona: none\n", "none"},
		{"persona: tester\n", "tester"},
		{"persona: x # short id\n", "x"},
		{"persona: a-\n", "a-"},
		{"persona: " + strings.Repeat("a", 32) + "\n", strings.Repeat("a", 32)},
	} {
		if got := parse(t, tc.source).Persona; got != tc.want {
			t.Errorf("Parse(%q).Persona = %q, want %q", tc.source, got, tc.want)
		}
	}
}

func TestPersonaKeyRejectsBadValues(t *testing.T) {
	for _, tc := range []struct{ source, id string }{
		{"persona: 3\n", "config.error.persona"},
		{"persona: true\n", "config.error.persona"},
		{"persona: [tester]\n", "config.error.persona"},
		{"persona: {name: tester}\n", "config.error.persona"},
		{"persona:\n", "config.error.persona"},
		{"persona: \"\"\n", "config.error.persona_syntax"},
		{"persona: ../x\n", "config.error.persona_syntax"},
		{"persona: Tester\n", "config.error.persona_syntax"},
		{"persona: " + strings.Repeat("a", 33) + "\n", "config.error.persona_syntax"},
		{"persona: \"te\\nster\"\n", "config.error.persona_syntax"},
		{"personaAlias: zork\n", "config.error.unknown_key"},
		{"personas: tester\n", "config.error.unknown_key"},
	} {
		_, err := Parse([]byte(tc.source), "homebot.yml")
		var user *fail.UserError
		if !errors.As(err, &user) {
			t.Errorf("Parse(%q) error = %v, want *fail.UserError", tc.source, err)
			continue
		}
		text, ok := user.Text()
		if !ok || text.ID() != tc.id {
			t.Errorf("Parse(%q) error ID = %s, want %s", tc.source, text, tc.id)
		}
		if !strings.HasPrefix(err.Error(), "homebot.yml:1:") {
			t.Errorf("Parse(%q) error lacks file position: %q", tc.source, err)
		}
	}
}

// The invalid value is echoed in the error, on one line and without anything
// a terminal would act on: a block scalar or an escape in a double-quoted
// string brings a newline, which StripUnsafe keeps.
func TestPersonaSyntaxErrorIsOneCleanLine(t *testing.T) {
	for _, tc := range []struct{ name, source, shown string }{
		{"escaped newline", "persona: \"te\\nster\"\n", `te\nster`},
		{"two escaped newlines", "persona: \"a\\n\\nb\"\n", `a\n\nb`},
		{"block scalar", "persona: |\n  tester\n", `tester\n`},
		{"folded scalar", "persona: >\n  Tes\n  ter\n", `Tes ter\n`},
		{"escape and bidi", "persona: \"x\\u001b[31m\\u202ey\"\n", `x[31my`},
		{"carriage return and tab", "persona: \"a\\rb\\tc\"\n", `abc`},
		{"newline between a backslash and an escape", "persona: \"\\\\\\n33\"\n", `\\n33`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.source), "homebot.yml")
			var user *fail.UserError
			if !errors.As(err, &user) {
				t.Fatalf("Parse(%q) error = %v, want *fail.UserError", tc.source, err)
			}
			if text, _ := user.Text(); text.ID() != "config.error.persona_syntax" {
				t.Fatalf("error ID = %s", text)
			}
			got := err.Error()
			if strings.ContainsAny(got, "\n\r\t") {
				t.Errorf("error runs over more than one line: %q", got)
			}
			if r, _, found := msg.UnsafeRune(got); found {
				t.Errorf("error holds unsafe %U: %q", r, got)
			}
			if want := `"` + tc.shown + `"`; !strings.Contains(got, want) {
				t.Errorf("error %q does not show the value as %s", got, want)
			}
		})
	}
}

func TestPersonaScalarEditKeepsCommentsAndParses(t *testing.T) {
	const before = "# chosen voice\npersona: tester # a note\nname: zork\n"
	updated, err := yamledit.SetScalar([]byte(before), []string{"persona"}, "none")
	if err != nil {
		t.Fatal(err)
	}
	const want = "# chosen voice\npersona: none # a note\nname: zork\n"
	if string(updated) != want {
		t.Errorf("SetScalar = %q, want %q", updated, want)
	}
	if got := parse(t, string(updated)).Persona; got != "none" {
		t.Errorf("edited persona = %q, want none", got)
	}
}
