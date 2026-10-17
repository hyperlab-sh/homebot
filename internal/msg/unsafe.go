package msg

import (
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// The characters a persona's text may not hold (plan S5, V0), in one place,
// so the persona validator, the renderer and the writers of the nudge,
// symbol and notification caches apply the very same rule.

const (
	// zwj is U+200D, the zero-width joiner of emoji sequences.
	zwj = '\u200d'
	// vs16 is U+FE0F, the variation selector that asks for an emoji's
	// colour presentation.
	vs16 = '\ufe0f'
)

// UnsafeRune finds the first thing in s that could change a terminal's
// state, reorder or hide text, or break a line where layout does not
// expect it: a code point of Unicode category Cc, Cf, Zl or Zp other than
// \n (C0, DEL and C1 among them; \r and \t are refused), the variation
// selector U+FE0F, a byte that is not valid UTF-8 (a lone 0x9B is CSI to
// an 8-bit terminal), or ESC written as text, as printf, echo -e, print and
// $'...' would turn it into the character (see escFormLen). It returns
// that rune and its byte offset; for an ESC written as text, the \ or ^
// that starts it, and for a bad byte, utf8.RuneError.
//
// The zero-width joiner and U+FE0F are unsafe here; only ValidEmoji allows
// them, inside an emoji.
func UnsafeRune(s string) (r rune, at int, found bool) {
	for i := 0; i < len(s); {
		c, size := utf8.DecodeRuneInString(s[i:])
		if unsafeRune(c, size) {
			return c, i, true
		}
		if (c == '\\' || c == '^') && escFormLen(s[i:]) > 0 {
			return c, i, true
		}
		i += size
	}
	return 0, 0, false
}

// StripUnsafe is s without what UnsafeRune finds, so UnsafeRune finds
// nothing in the result: removing a character never leaves a new ESC
// written as text behind.
func StripUnsafe(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		c, size := utf8.DecodeRuneInString(s[i:])
		if unsafeRune(c, size) {
			i += size
			continue
		}
		out = append(out, s[i:i+size]...)
		i += size
		// Every ESC form ends in an ASCII character, and out held none
		// before this one: only a form ending here can be new.
		if c < utf8.RuneSelf {
			out = out[:len(out)-escFormSuffix(out)]
		}
	}
	return string(out)
}

// unsafeRune reports whether the rune c, decoded from size bytes, is unsafe
// by itself.
func unsafeRune(c rune, size int) bool {
	switch {
	case c == utf8.RuneError && size <= 1:
		return true
	case c == '\n':
		return false
	case c == vs16:
		return true
	}
	return unicode.In(c, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp)
}

// escFormLen is the length of the ESC written as text that s starts with,
// or 0. The forms, in any case:
//
//	\33, \033, \0033, ...   octal 33 after any number of zeros (printf takes
//	                        \NNN, echo -e and print \0NNN)
//	\x1b, \x01b, ...        hex 1b after any number of zeros
//	\u1b, \u001b, \U0000001b, ...
//	\e                      also \E, as $'...' and print read it
//	\c[                     control-[, as $'...' reads it
//	^[                      caret notation, as terminals and tools echo it
//
// A form may be longer in s (\0330 is \033 then 0, and \333 starts with
// \33): that is still refused, which only costs a persona an odd spelling.
func escFormLen(s string) int {
	if len(s) < 2 {
		return 0
	}
	switch {
	case s[0] == '^':
		if s[1] == '[' {
			return 2
		}
		return 0
	case s[0] != '\\':
		return 0
	}
	switch lower(s[1]) {
	case 'e':
		return 2
	case 'c':
		if len(s) > 2 && s[2] == '[' {
			return 3
		}
		return 0
	case 'x', 'u':
		return zerosThen(s, 2, "1b")
	}
	return zerosThen(s, 1, "33")
}

// zerosThen is i plus the zeros at s[i:] plus len(digits), if digits follow
// the zeros (in any case), or 0.
func zerosThen(s string, i int, digits string) int {
	for i < len(s) && s[i] == '0' {
		i++
	}
	if len(s)-i < len(digits) || asciiLower(s[i:i+len(digits)]) != digits {
		return 0
	}
	return i + len(digits)
}

// escFormSuffix is the length of the ESC written as text that s ends with,
// or 0: escFormLen read backwards, so a form ends at the end of s exactly
// when escFormLen finds it at the start this returns.
func escFormSuffix(s []byte) int {
	n := len(s)
	switch {
	case n >= 3 && s[n-1] == '[' && lower(s[n-2]) == 'c' && s[n-3] == '\\':
		return 3
	case n >= 2 && s[n-1] == '[' && s[n-2] == '^',
		n >= 2 && lower(s[n-1]) == 'e' && s[n-2] == '\\':
		return 2
	}
	for _, f := range []struct {
		digits string
		lead   bool // x or u before the zeros
	}{{"33", false}, {"1b", true}} {
		if n < len(f.digits) || asciiLower(string(s[n-len(f.digits):])) != f.digits {
			continue
		}
		i := n - len(f.digits)
		for i > 0 && s[i-1] == '0' {
			i--
		}
		if f.lead {
			if i == 0 || lower(s[i-1]) != 'x' && lower(s[i-1]) != 'u' {
				continue
			}
			i--
		}
		if i > 0 && s[i-1] == '\\' {
			return n - i + 1
		}
	}
	return 0
}

// lower lowers an ASCII letter.
func lower(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

// asciiLower lowers A to Z only, so byte offsets stay put.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		b[i] = lower(c)
	}
	return string(b)
}

// ValidEmoji reports whether s may be a persona's emoji (plan S5, V0 and
// V6). The emoji lands in the shell prompt, so the rule is tight: s is
// exactly one grapheme cluster, and one of
//
//   - an emoji: an Extended_Pictographic code point, then at most a skin
//     tone (U+1F3FB to U+1F3FF), then at most U+FE0F; or several of those
//     joined by the zero-width joiner, as in the rainbow flag (U+1F3F3
//     U+FE0F U+200D U+1F308);
//   - a flag: two regional indicators;
//
// Keycaps are refused: their ASCII base is not a persona emoji.
//
// Nothing else may follow the base: no other combining mark, no tag
// sequence, no second character. So "é", the Hangul filler U+3164 and an
// emoji under a stack of marks are refused.
func ValidEmoji(s string) bool {
	if s == "" || !utf8.ValidString(s) || uniseg.GraphemeClusterCount(s) != 1 {
		return false
	}
	rs := []rune(s)
	switch {
	case regionalIndicator(rs[0]):
		return len(rs) == 2 && regionalIndicator(rs[1])
	case keycapBase(rs[0]):
		return false
	}
	for i := 0; ; i++ {
		if i >= len(rs) || !extendedPictographic(rs[i]) {
			return false
		}
		i++
		if i < len(rs) && skinTone(rs[i]) {
			i++
		}
		if i < len(rs) && rs[i] == vs16 {
			i++
		}
		if i == len(rs) {
			return true
		}
		if rs[i] != zwj {
			return false
		}
	}
}

func regionalIndicator(c rune) bool { return '\U0001F1E6' <= c && c <= '\U0001F1FF' }

func skinTone(c rune) bool { return '\U0001F3FB' <= c && c <= '\U0001F3FF' }

func keycapBase(c rune) bool { return '0' <= c && c <= '9' || c == '#' || c == '*' }

// extendedPictographic reports whether c has the Unicode property
// Extended_Pictographic. Go has no table for it; uniseg has one but does
// not export it, so this asks uniseg's grapheme rules instead: c stands
// alone after a letter (it is no mark, joiner or modifier), and c, the
// zero-width joiner and c again make one cluster, which rule GB11 allows
// only between Extended_Pictographic code points.
func extendedPictographic(c rune) bool {
	if c < utf8.RuneSelf || unsafeRune(c, utf8.RuneLen(c)) || unicode.In(c, unicode.Mn, unicode.Me, unicode.Mc) {
		return false
	}
	r := string(c)
	return uniseg.GraphemeClusterCount("x"+r) == 2 && uniseg.GraphemeClusterCount(r+string(zwj)+r) == 1
}
