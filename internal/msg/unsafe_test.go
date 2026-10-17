package msg_test

import (
	"math/rand/v2"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/hyperlab-sh/homebot/internal/msg"
)

func TestUnsafeRuneFindsWhatV0Refuses(t *testing.T) {
	for _, c := range []struct {
		name, s string
		r       rune
		at      int
	}{
		{"ESC", "ab\x1b[31m", 0x1b, 2},
		{"ESC after a multi-byte rune", "é\x1b", 0x1b, 2},
		{"carriage return", "a\rb", '\r', 1},
		{"tab", "a\tb", '\t', 1},
		{"NUL", "\x00", 0, 0},
		{"DEL", "a\x7f", 0x7f, 1},
		{"8-bit CSI, as a code point", "a\u009b2J", 0x9b, 1},
		{"8-bit CSI, as a raw byte", "a\x9b2J", utf8.RuneError, 1},
		{"a bad byte", "ok\xff", utf8.RuneError, 2},
		{"a truncated rune", "ok\xf0\x9f", utf8.RuneError, 2},
		{"right-to-left override", "a\u202eb", 0x202e, 1},
		{"right-to-left isolate", "a\u2067b", 0x2067, 1},
		{"Arabic letter mark", "\u061c", 0x61c, 0},
		{"soft hyphen", "a\u00adb", 0xad, 1},
		{"zero-width space", "a\u200bb", 0x200b, 1},
		{"zero-width joiner", "a\u200db", 0x200d, 1},
		{"left-to-right mark", "\u200e", 0x200e, 0},
		{"word joiner", "\u2060", 0x2060, 0},
		{"byte order mark", "\ufeff", 0xfeff, 0},
		{"interlinear annotation", "\ufff9", 0xfff9, 0},
		{"line separator", "a\u2028b", 0x2028, 1},
		{"paragraph separator", "a\u2029b", 0x2029, 1},
		{"VS16", "☔\ufe0f", 0xfe0f, 3},
		{"a flag sequence: VS16 outside an emoji field", "🏳\ufe0f\u200d🌈", 0xfe0f, 4},
		// ESC written as text, every spelling printf, echo -e, print and
		// $'...' turn into the character.
		{`\e`, `red \e[31m`, '\\', 4},
		{`\E`, `\E[0m`, '\\', 0},
		{`\33`, `x\33[2J`, '\\', 1},
		{`\033`, `x\033[2J`, '\\', 1},
		{`\0033`, `x\0033[2J`, '\\', 1},
		{`\00000033`, `\00000033`, '\\', 0},
		{`\x1b`, `\x1b`, '\\', 0},
		{`\x1B`, `\x1B`, '\\', 0},
		{`\X1B`, `\X1B`, '\\', 0},
		{`\x01b`, `\x01b`, '\\', 0},
		{`\u1b`, `\u1b`, '\\', 0},
		{`\u001b`, `\u001b`, '\\', 0},
		{`\U001B`, `\U001B`, '\\', 0},
		{`\U0000001b`, `\U0000001b`, '\\', 0},
		{`\U0000001B`, `\U0000001B`, '\\', 0},
		{`\c[`, `a\c[`, '\\', 1},
		{`\C[`, `\C[`, '\\', 0},
		{`^[`, `^[[31m`, '^', 0},
		{`a written ESC after a backslash`, `\\033`, '\\', 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, at, found := msg.UnsafeRune(c.s)
			if !found || r != c.r || at != c.at {
				t.Errorf("UnsafeRune(%q) = %U, %d, %v; want %U at %d", c.s, r, at, found, c.r, c.at)
			}
		})
	}
}

func TestUnsafeRuneLetsTextThrough(t *testing.T) {
	for _, s := range []string{
		"",
		"plain words, a count 3 and {placeholders}",
		"two lines\n  the second",
		"\n",
		"accents é ü ñ, CJK 東京, emoji 🧪 ☔ 🚚 and ·→•",
		`a backslash \ alone, \\ doubled, \x1c, \u001c, \034, \U0000001c`,
		`^] and ^ alone, \c] and \ce`,
		`near misses: \3, \03, \003, \34, \x1, \x1c, \x33, \u1, \u1c, \0x1b, \1b, x1b, 033`,
		"100% and $HOME and `tick`",
	} {
		if r, at, found := msg.UnsafeRune(s); found {
			t.Errorf("UnsafeRune(%q) found %U at %d", s, r, at)
		}
	}
	// \0330 starts with \033: a written ESC followed by a 0.
	if _, at, found := msg.UnsafeRune(`is \0330`); !found || at != 3 {
		t.Errorf(`\0330 holds \033, at 3: found %v at %d`, found, at)
	}
	// \333 is octal 333 to printf, not ESC, but it starts like \33: it is
	// refused, which costs a persona no more than an odd spelling.
	if _, _, found := msg.UnsafeRune(`\333`); !found {
		t.Error(`\333 starts with \33 and is let through`)
	}
}

func TestStripUnsafe(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", ""},
		{"safe text\nkept", "safe text\nkept"},
		{"a\x1b[31mred\x1b[0m", "a[31mred[0m"},
		{"bi\u202edi\u2066rect", "bidirect"},
		{"bad\xffbyte\x9b", "badbyte"},
		{"tab\there\r", "tabhere"},
		{"🏳\ufe0f\u200d🌈", "🏳🌈"},
		{`x\e[31m`, "x[31m"},
		{`\E\033\x1b\u001B\U0000001b\c[^[`, ""},
		{`a\33b\0033c\x1Bd\u1be`, "abcde"},
		{`\x\x1b1b`, ""},
		{`\0\0333`, `\03`},
		{`\000\033`, `\000`},
		// Removing a character can write a new ESC: it goes too.
		{"\\\u200be", ""},
		{"\\\x1be[1m", "[1m"},
		{`\0\03333`, ""},
		{`^\e[`, ""},
		{`\\e`, `\`},
	} {
		got := msg.StripUnsafe(c.in)
		if got != c.want {
			t.Errorf("StripUnsafe(%q) = %q, want %q", c.in, got, c.want)
		}
		if r, at, found := msg.UnsafeRune(got); found {
			t.Errorf("StripUnsafe(%q) = %q still holds %U at %d", c.in, got, r, at)
		}
	}
}

// Whatever goes in, StripUnsafe leaves nothing UnsafeRune finds, and it
// leaves safe text alone.
func TestStripUnsafeAlwaysCleans(t *testing.T) {
	pieces := []string{
		`\`, `^`, "e", "E", "0", "3", "33", "x", "X", "1", "b", "B", "u", "U", "00", "c", "[", "]", "1b", `\0`, "x1b",
		"\x1b", "\u200b", "\u200d", "\ufe0f", "\xff", "\x9b", "\u009b", "\n", "\r", "é", "🧪", " ",
	}
	rng := rand.New(rand.NewPCG(1, 2))
	for range 20000 {
		var b strings.Builder
		for range rng.IntN(16) {
			b.WriteString(pieces[rng.IntN(len(pieces))])
		}
		in := b.String()
		out := msg.StripUnsafe(in)
		if r, at, found := msg.UnsafeRune(out); found {
			t.Fatalf("StripUnsafe(%q) = %q still holds %U at %d", in, out, r, at)
		}
		if _, _, found := msg.UnsafeRune(in); !found && out != in {
			t.Fatalf("StripUnsafe(%q) changed safe text to %q", in, out)
		}
	}
}

func TestValidEmoji(t *testing.T) {
	for _, s := range []string{
		"🧪", "🚚", "☔", "⏰", "🤖",
		"\u00a9",                // a copyright sign: Extended_Pictographic
		"❤\ufe0f",               // VS16 after an emoji
		"🏳\ufe0f\u200d🌈",        // VS16, then the joiner
		"🧑\u200d💻",              // two emoji joined
		"👍\U0001F3FD",           // a skin tone
		"👍\U0001F3FD\ufe0f",     // a skin tone, then VS16
		"👨\U0001F3FD\u200d💻",    // a skin tone, then the joiner
		"👩\u200d❤\ufe0f\u200d👨", // a joiner after a VS16, twice
		"🇧🇷",                    // a regional-indicator pair
		"\U0001F3F3\ufe0f",      // a white flag, written out
	} {
		if !msg.ValidEmoji(s) {
			t.Errorf("ValidEmoji(%q) = false, want true", s)
		}
	}
	for name, s := range map[string]string{
		"empty":                       "",
		"ASCII":                       "x",
		"a digit without a keycap":    "1",
		"digit keycap":                "1\ufe0f\u20e3",
		"hash keycap":                 "#\u20e3",
		"star keycap":                 "*\ufe0f\u20e3",
		"a letter keycap":             "a\ufe0f\u20e3",
		"a keycap in the wrong order": "1\u20e3\ufe0f",
		"a keycap on an emoji":        "🧪\u20e3",
		"an emoji and ASCII":          "🧪a",
		"two emoji":                   "🧪🧪",
		"a dollar":                    "$",
		"a letter with an accent":     "é",
		"a decomposed accent":         "e\u0301",
		"a CJK character":             "中",
		"the Hangul filler":           "\u3164",
		"a mark on an emoji":          "🧪\u0301",
		"a stack of marks":            "🧪\u0301\u0302\u0303\u0304\u0305",
		"an enclosing circle":         "🧪\u20dd",
		"a skin tone alone":           "\U0001F3FD",
		"VS16 before a skin tone":     "👍\ufe0f\U0001F3FD",
		"one regional indicator":      "🇧",
		"a flag with VS16":            "🇧🇷\ufe0f",
		"a leading joiner":            "\u200d🧪",
		"a trailing joiner":           "🧪\u200d",
		"a joiner before a letter":    "🧪\u200dé",
		"a joiner after a letter":     "é\u200d🧪",
		"a joiner before VS16":        "🏳\u200d\ufe0f🌈",
		"two joiners":                 "🧑\u200d\u200d💻",
		"VS16 alone":                  "\ufe0f",
		"VS16 twice":                  "☔\ufe0f\ufe0f",
		"VS16 after a letter":         "é\ufe0f",
		"a zero-width space":          "🧪\u200b",
		"a bidi override":             "\u202e🧪",
		"ESC":                         "🧪\x1b",
		"a tag sequence":              "🏴\U000E0067\U000E0062\U000E0065\U000E006E\U000E0067\U000E007F",
		"a combining mark on ASCII":   "a\u0301",
		"bad UTF-8":                   "\xf0\x9f",
		"a newline":                   "🧪\n",
	} {
		if msg.ValidEmoji(s) {
			t.Errorf("%s: ValidEmoji(%q) = true, want false", name, s)
		}
	}
}
