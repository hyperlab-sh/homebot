package msg_test

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/msg/catalog"
)

// speaker is a persona whose lines are made from each entry's metadata.
type speaker struct {
	name string
	lang language.Tag
	line func(m msg.Meta) (string, bool)
}

func (s speaker) Name() string { return s.name }

func (s speaker) Line(id string) (string, language.Tag, bool) {
	m, ok := msg.Lookup(id)
	if !ok {
		return "", language.Und, false
	}
	line, ok := s.line(m)
	return line, s.lang, ok
}

// required are the slots every line of m must reference: those that are not
// optional, fixed ones included.
func required(m msg.Meta) []msg.Slot {
	var out []msg.Slot
	for _, sl := range m.Slots {
		if !sl.Optional {
			out = append(out, sl)
		}
	}
	return out
}

// refs writes a {name} for each slot.
func refs(slots []msg.Slot) string {
	var b strings.Builder
	for _, sl := range slots {
		b.WriteString(" {" + sl.Name + "}")
	}
	return b.String()
}

// sayAsNeutral fails for each sample s says otherwise than the neutral
// voice, styled or not.
func sayAsNeutral(t *testing.T, s msg.Speaker) {
	t.Helper()
	for _, sm := range samples {
		for _, style := range []func(string, string) string{nil, tag} {
			if got, want := msg.Speak(s, sm.text, style), msg.Speak(msg.Neutral{}, sm.text, style); got != want {
				t.Errorf("%s: %q, want the catalog's %q", sm.label, got, want)
			}
		}
	}
}

// A persona line the renderer cannot trust is never said, whatever loaded
// it: every sample of every entry, voiced or not, reads as the catalog's.
// The speaker is named homebot so {bot} reads as in the neutral voice.
func TestHostileSpeaker(t *testing.T) {
	cases := map[string]func(m msg.Meta) (string, bool){
		"an unknown placeholder": func(msg.Meta) (string, bool) { return "HOSTILE {zzz}", true },
		"an unknown placeholder beside the required ones": func(m msg.Meta) (string, bool) {
			return "HOSTILE" + refs(required(m)) + " {zzz}", true
		},
		"a line that does not parse": func(m msg.Meta) (string, bool) { return "HOSTILE" + refs(required(m)) + " {", true },
		"a selector on a slot that is not an int": func(m msg.Meta) (string, bool) {
			line := "HOSTILE" + refs(required(m))
			for _, sl := range m.Slots {
				if sl.Type != catalog.Int {
					return line + " {" + sl.Name + ":# a|# b}", true
				}
			}
			return "", false
		},
		"{bot} where the entry has no bot": func(m msg.Meta) (string, bool) {
			for _, sl := range m.Slots {
				if sl.Name == catalog.Bot {
					return "", false
				}
			}
			return "HOSTILE {bot}" + refs(required(m)), true
		},
	}
	for _, unsafe := range []string{"\x1b", "\x1b[2J", "\u009b2J", "\x9b", "\r", "\t", "\u202e", "\u200d", "\ufe0f", "\u2028", `\e[31m`, `\033[0m`, `^[`} {
		cases["HOSTILE with "+unsafe] = func(m msg.Meta) (string, bool) { return "HOSTILE" + refs(required(m)) + " " + unsafe, true }
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			sayAsNeutral(t, speaker{name: msg.NeutralName, lang: language.English, line: line})
		})
	}
	// One required or fixed slot left out, each in turn.
	t.Run("a required slot missing", func(t *testing.T) {
		most := 0
		for _, m := range msg.Entries() {
			most = max(most, len(required(m)))
		}
		if most < 2 {
			t.Fatalf("no entry has two required slots; the catalog decoder is broken")
		}
		for drop := range most {
			sayAsNeutral(t, speaker{name: msg.NeutralName, lang: language.English, line: func(m msg.Meta) (string, bool) {
				req := required(m)
				if drop >= len(req) {
					return "", false
				}
				return "HOSTILE" + refs(append(req[:drop:drop], req[drop+1:]...)), true
			}})
		}
	})
}

// The checks above are not what keeps the lines out: a line that passes
// them is said, \n included.
func TestASoundLineIsSaid(t *testing.T) {
	s := speaker{name: msg.NeutralName, lang: language.English, line: func(m msg.Meta) (string, bool) {
		return "SOUND" + refs(required(m)) + "\nnext line", true
	}}
	voiced := 0
	for _, sm := range samples {
		got := msg.Speak(s, sm.text, nil)
		if !sm.text.Persona() {
			if want := msg.Speak(msg.Neutral{}, sm.text, nil); got != want {
				t.Errorf("%s is not voiced, but reads %q", sm.label, got)
			}
			continue
		}
		voiced++
		wantAppend := strings.ReplaceAll(sm.text.Append(), "{self}", msg.NeutralName)
		if !strings.HasPrefix(got, "SOUND") || !strings.HasSuffix(got, "\nnext line"+wantAppend) {
			t.Errorf("%s: the sound line was not said: %q", sm.label, got)
		}
	}
	if voiced == 0 {
		t.Fatal("no voiced sample")
	}
	if got := msg.Speak(s, msg.ActionLinkWarningInWay(2), nil); got != "SOUND 2 homebot link\nnext line" {
		t.Errorf("a fixed slot in a persona line: %q", got)
	}
}

// A persona's line is formatted with the language it comes with; the
// catalog's text with English rules, whatever the speaker's language.
func TestPluralRulesFollowWhoWroteTheLine(t *testing.T) {
	broken := speaker{name: "x", lang: language.Japanese, line: func(msg.Meta) (string, bool) { return "{", true }}
	if got := msg.Speak(broken, msg.InboxHeaderCount(1), nil); got != "1 thing needs you" {
		t.Errorf("fallback formatted with the speaker's language: %q", got)
	}
	own := speaker{name: "x", lang: language.Japanese, line: func(msg.Meta) (string, bool) { return "{n:# one|# other}", true }}
	if got := msg.Speak(own, msg.InboxHeaderCount(1), nil); got != "1 other" {
		t.Errorf("a line formatted with English rules, not Japanese: %q", got)
	}
	// The zero language.Tag means English: Und has no plural rules, and
	// would pick other for every count.
	for _, und := range []language.Tag{language.Und, {}} {
		s := speaker{name: "x", lang: und, line: func(msg.Meta) (string, bool) { return "{n:# one|# other}", true }}
		if got := msg.Speak(s, msg.InboxHeaderCount(1), nil); got != "1 one" {
			t.Errorf("a line in %v: %q, want English plurals", und, got)
		}
	}
}

// A speaker's name reaches the output through {bot} only when it is safe;
// otherwise the speaker says nothing of its own, its lines included.
func TestUnsafeNameSpeaksAsNeutral(t *testing.T) {
	sound := func(m msg.Meta) (string, bool) { return "SOUND" + refs(required(m)), true }
	for _, name := range []string{"\x1b]0;pwn\a", "Tes\u202eter", "Tester\r", `Tester\e[2J`, "bad\xffbyte"} {
		t.Run(name, func(t *testing.T) {
			sayAsNeutral(t, speaker{name: name, lang: language.English, line: sound})
		})
	}
	// A safe name is said, and so are the lines.
	s := speaker{name: "Tester", lang: language.English, line: sound}
	if got := msg.Speak(s, msg.KeysStatusCreating(), nil); got != "SOUND" {
		t.Errorf("a safe speaker: %q", got)
	}
	s.line = func(msg.Meta) (string, bool) { return "{bot} asks", true }
	if got := msg.Speak(s, msg.KeysStatusCreating(), nil); got != "Tester asks" {
		t.Errorf("a safe name: %q", got)
	}
}

// CheckLine is the renderer's rule, with the reason: a line it passes is
// said, and one it refuses falls back.
func TestCheckLine(t *testing.T) {
	for _, c := range []struct {
		name, id, line string
		want           msg.LineProblem
		slot           string
	}{
		{"a sound line", "action.link.warning.in_way", "; {n} blocking, see {cmd}", 0, ""},
		{"{bot} where declared", "keys.status.creating", "{bot} wants a passphrase.", 0, ""},
		{"an optional slot left out", "keys.status.creating", "Making your key.", 0, ""},
		{"an unknown id", "no.such.entry", "x", msg.LineUnknownID, ""},
		{"an entry not voiced", "inbox.header.urgent", "{n}!", msg.LineNotVoiced, ""},
		{"an unsafe character", "action.link.warning.in_way", "{n} {cmd} \x1b[2J", msg.LineUnsafe, ""},
		{"a written ESC", "action.link.warning.in_way", `{n} {cmd} \0033[2J`, msg.LineUnsafe, ""},
		{"a line that does not parse", "action.link.warning.in_way", "{n} {cmd", msg.LineSyntax, ""},
		{"an unknown name", "action.link.warning.in_way", "{n} {cmd} {zzz}", msg.LineUnknownName, "zzz"},
		{"{bot} where not declared", "action.link.warning.in_way", "{bot}: {n} {cmd}", msg.LineUnknownName, "bot"},
		{"a selector on a string", "action.link.warning.in_way", "{n} {cmd:# a|# b}", msg.LineSelector, "cmd"},
		{"a fixed slot left out", "action.link.warning.in_way", "{n} in the way", msg.LineMissing, "cmd"},
		{"a required slot left out", "action.link.warning.in_way", "see {cmd}", msg.LineMissing, "n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := msg.CheckLine(c.id, c.line)
			if c.want == 0 {
				if err != nil {
					t.Fatalf("CheckLine refused a sound line: %v", err)
				}
				return
			}
			var le *msg.LineError
			if !errors.As(err, &le) {
				t.Fatalf("CheckLine = %v, want a *msg.LineError", err)
			}
			if le.ID != c.id || le.Problem != c.want || le.Name != c.slot {
				t.Errorf("CheckLine = %+v, want problem %d on {%s}", le, c.want, c.slot)
			}
			if c.want == msg.LineUnsafe && le.Rune != '\\' && le.Rune != 0x1b {
				t.Errorf("unsafe rune %U", le.Rune)
			}
			if c.want == msg.LineSyntax && errors.Unwrap(err) == nil {
				t.Error("a syntax error does not wrap the parser's")
			}
			if err.Error() == "" || !strings.Contains(err.Error(), c.id) {
				t.Errorf("message %q does not name the id", err.Error())
			}
		})
	}
	// A line CheckLine refuses is not said.
	s := speaker{name: "Tester", lang: language.English, line: func(msg.Meta) (string, bool) { return "{n} in the way", true }}
	if got, want := msg.Speak(s, msg.ActionLinkWarningInWay(2), nil), msg.Speak(msg.Neutral{}, msg.ActionLinkWarningInWay(2), nil); got != want {
		t.Errorf("a refused line was said: %q", got)
	}
}
