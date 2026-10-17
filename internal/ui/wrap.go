package ui

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
)

// WrapText word-wraps prose at width, every line starting with indent. Each
// line of text is a paragraph of its own. A word longer than the line (a URL,
// a path) stays whole. Widths count as src/ui.ts counts them, in UTF-16 code
// units, so the output matches it byte for byte; escape sequences of styled
// words count for nothing. Pass Columns(ctx) for the terminal's width.
//
// A styled span that a break falls inside is closed at the end of its line
// and opened again after the next line's indent, so each line carries its own
// styles and the indent is never styled. Text without escape sequences wraps
// exactly as src/ui.ts wraps it.
func WrapText(text, indent string, width int) string {
	paragraphs := strings.Split(text, "\n")
	indentLen := jsLen(indent)
	var open sgrState
	for i, paragraph := range paragraphs {
		var lines []string
		line, lineLen := "", 0
		for _, word := range strings.FieldsFunc(paragraph, jsSpace) {
			wordLen := jsLen(sgr.ReplaceAllString(word, ""))
			if line != "" && indentLen+lineLen+1+wordLen > width {
				lines = append(lines, indent+line+open.closer())
				line, lineLen = open.opener()+word, wordLen
			} else if line != "" {
				line, lineLen = line+" "+word, lineLen+1+wordLen
			} else {
				line, lineLen = word, wordLen
			}
			open.apply(word)
		}
		paragraphs[i] = strings.Join(append(lines, indent+line), "\n")
	}
	return strings.Join(paragraphs, "\n")
}

// sgrState is the styles an escape-sequenced text has open at some point:
// one entry per kind of style (foreground, bold or faint, underline...), in
// the order they were turned on.
type sgrState struct {
	open []sgrStyle
}

// sgrStyle is one open style: kind says what turning it off takes, params is
// what turned it on (the "1" or "38;5;208" of "\x1b[1m").
type sgrStyle struct {
	kind   string
	params string
}

// apply moves the state past every escape sequence in text.
func (s *sgrState) apply(text string) {
	for _, m := range sgr.FindAllStringSubmatch(text, -1) {
		s.applyParams(m[1])
	}
}

func (s *sgrState) applyParams(params string) {
	fields := strings.Split(params, ";")
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		if field == "" {
			field = "0"
		}
		// A sub-parameter form such as 4:3 (curly underline) or
		// 38:2::r:g:b is one unit, named by the code before the colon.
		code, _, colon := strings.Cut(field, ":")
		n, err := strconv.Atoi(code)
		if err != nil {
			return
		}
		switch {
		case colon:
			if field == "4:0" {
				// No underline, of any style: what 24 ends.
				s.turnOff(offKinds[24]...)
				continue
			}
			s.turnOn(kindOf(n), field)
		case n == 0:
			s.open = nil
		case n == 38 || n == 48 || n == 58:
			// The colour is in the fields after: 5;n or 2;r;g;b.
			count := 2
			if i+1 < len(fields) && fields[i+1] == "2" {
				count = 4
			}
			end := min(i+1+count, len(fields))
			s.turnOn(kindOf(n), strings.Join(fields[i:end], ";"))
			i = end - 1
		default:
			if off, ok := offKinds[n]; ok {
				s.turnOff(off...)
			} else {
				s.turnOn(kindOf(n), strconv.Itoa(n))
			}
		}
	}
}

// offKinds are the kinds of style that codes such as 22 turn off. Bold and
// faint are two styles at once on a terminal: 1;2 is both, and 22 ends both.
var offKinds = map[int][]string{
	22: {"1", "2"},
	23: {"3", "20"}, // italic and Fraktur
	24: {"4", "21"},
	25: {"5", "6"},
	27: {"7"},
	28: {"8"},
	29: {"9"},
	39: {"fg"},
	49: {"bg"},
	59: {"underline colour"},
}

// kindOf is the kind of style code n turns on: codes of one kind replace
// each other. Codes that are not known are their own kind and stay open until
// a reset.
func kindOf(n int) string {
	switch {
	case n == 38 || (n >= 30 && n <= 37) || (n >= 90 && n <= 97):
		return "fg"
	case n == 48 || (n >= 40 && n <= 47) || (n >= 100 && n <= 107):
		return "bg"
	case n == 58:
		return "underline colour"
	}
	return strconv.Itoa(n)
}

func (s *sgrState) turnOn(kind, params string) {
	s.turnOff(kind)
	s.open = append(s.open, sgrStyle{kind, params})
}

func (s *sgrState) turnOff(kinds ...string) {
	s.open = slices.DeleteFunc(s.open, func(o sgrStyle) bool { return slices.Contains(kinds, o.kind) })
}

// opener turns the open styles on again, one sequence each; "" when none is open.
func (s *sgrState) opener() string {
	var b strings.Builder
	for _, o := range s.open {
		b.WriteString("\x1b[" + o.params + "m")
	}
	return b.String()
}

// closer is the reset that ends a line with styles open, and "" otherwise.
func (s *sgrState) closer() string {
	if len(s.open) == 0 {
		return ""
	}
	return "\x1b[0m"
}

// jsSpace reports whether JavaScript's \s matches r: Go's spaces, without
// U+0085 and with U+FEFF.
func jsSpace(r rune) bool {
	return r == '\uFEFF' || (r != '\u0085' && unicode.IsSpace(r))
}

// jsLen is the length of s in UTF-16 code units, as JavaScript counts it.
func jsLen(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// detailIndent puts an inbox item's details under its title.
const detailIndent = "       "

var sgr = regexp.MustCompile(`\x1b\[([0-9;:]*)m`)

// Detail lays out an inbox item's detail under its title. Colours stored
// with it are removed: details can come from a tool or from an earlier run in
// a terminal. The first line is wrapped at width; at most two more follow as
// they are.
func Detail(detail string, width int) string {
	lines := strings.Split(sgr.ReplaceAllString(detail, ""), "\n")
	out := []string{WrapText(lines[0], detailIndent, width)}
	for _, line := range lines[1:min(len(lines), 3)] {
		out = append(out, detailIndent+line)
	}
	return strings.Join(out, "\n")
}
