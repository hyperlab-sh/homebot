package yamledit

import (
	"bytes"
	"strings"
	"unicode/utf8"
)

// source is the file text plus an index of its lines. Positions from go-yaml
// are 1-based line and rune column; everything here works in byte offsets.
type source struct {
	text  []byte
	lines []line
	eol   string
}

// line is one line's byte range, without its line terminator.
type line struct{ start, end int }

func newSource(text []byte) *source {
	s := &source{text: text, eol: "\n"}
	if i := bytes.IndexByte(text, '\n'); i > 0 && text[i-1] == '\r' {
		s.eol = "\r\n"
	}
	start := 0
	for i, b := range text {
		if b == '\n' {
			end := i
			if end > start && text[end-1] == '\r' {
				end--
			}
			s.lines = append(s.lines, line{start, end})
			start = i + 1
		}
	}
	if start < len(text) {
		s.lines = append(s.lines, line{start, len(text)})
	}
	return s
}

// offset converts a go-yaml line and column (both 1-based, column counted in
// runes) to a byte offset.
func (s *source) offset(lineNo, col int) int {
	if lineNo < 1 || lineNo > len(s.lines) {
		return len(s.text)
	}
	l := s.lines[lineNo-1]
	off := l.start
	for i := 1; i < col && off < l.end; i++ {
		_, size := utf8.DecodeRune(s.text[off:])
		off += size
	}
	return off
}

func (s *source) lineText(i int) string { return string(s.text[s.lines[i].start:s.lines[i].end]) }

// indentOf returns the number of leading spaces of line i and whether the
// line has any other content.
func (s *source) indentOf(i int) (indent int, blank bool) {
	t := s.lineText(i)
	trimmed := strings.TrimLeft(t, " ")
	return len(t) - len(trimmed), strings.TrimSpace(trimmed) == ""
}

// blockEnd returns the last line of the block that starts at line first, whose
// owner (a key or a "-") sits at column indent. Lines belong to the block while
// they are indented deeper than indent. Trailing blank and comment-only lines
// do not. dashesBelong lets "-" lines at indent itself belong too (a sequence
// written at its key's indent); rawLines treats comment-looking lines as
// content, as inside a block scalar. Comment lines never end a block; they
// belong to it only when content follows them.
func (s *source) blockEnd(first, indent int, dashesBelong, rawLines bool) int {
	last := first
	for i := first + 1; i < len(s.lines); i++ {
		ind, blank := s.indentOf(i)
		if blank {
			continue
		}
		text := strings.TrimLeft(s.lineText(i), " ")
		if strings.HasPrefix(text, "#") && (!rawLines || ind <= indent) {
			continue
		}
		switch {
		case ind > indent:
			last = i
		case ind == indent && dashesBelong && (text == "-" || strings.HasPrefix(text, "- ")):
			last = i
		default:
			return last
		}
	}
	return last
}

// insertAfterLine returns the text with newLines added as whole lines after
// line i, in the file's line-ending style.
func (s *source) insertAfterLine(i int, newLines []string) []byte {
	at := s.lines[i].end
	return splice(s.text, at, at, s.eol+strings.Join(newLines, s.eol))
}

// appendLines adds newLines at the end of the file, each terminated, after
// making sure the last existing line is.
func (s *source) appendLines(newLines []string) []byte {
	out := append([]byte(nil), s.text...)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, s.eol...)
	}
	for _, l := range newLines {
		out = append(out, l...)
		out = append(out, s.eol...)
	}
	return out
}

func splice(text []byte, from, to int, with string) []byte {
	out := make([]byte, 0, len(text)-(to-from)+len(with))
	out = append(out, text[:from]...)
	out = append(out, with...)
	return append(out, text[to:]...)
}
