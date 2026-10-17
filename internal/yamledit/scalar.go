package yamledit

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// render returns value as YAML scalar text: plain when that reads back as the
// same string, double-quoted otherwise. inFlow also quotes flow punctuation.
func render(value string, inFlow bool) string {
	if plainSafe(value, inFlow) {
		return value
	}
	return quote(value)
}

func plainSafe(v string, inFlow bool) bool {
	if v == "" || v != strings.TrimSpace(v) || !utf8.ValidString(v) {
		return false
	}
	if strings.ContainsAny(v[:1], "-?:,[]{}#&*!|>'\"%@`") {
		return false
	}
	if strings.Contains(v, ": ") || strings.Contains(v, " #") || strings.HasSuffix(v, ":") {
		return false
	}
	if inFlow && strings.ContainsAny(v, ",[]{}") {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f || r == 0x85 || r == 0x2028 || r == 0x2029 {
			return false
		}
	}
	switch strings.ToLower(v) {
	case "y", "yes", "n", "no", "on", "off":
		return false // booleans in YAML 1.1, which other tools still read
	}
	// Plain text that YAML reads as a number, boolean or null must be quoted.
	f, err := parser.ParseBytes([]byte("k: "+v), 0)
	if err != nil || len(f.Docs) != 1 {
		return false
	}
	entries, _, ok := mapping(f.Docs[0].Body)
	if !ok || len(entries) != 1 {
		return false
	}
	s, ok := entries[0].Value.(*ast.StringNode)
	return ok && s.Value == v
}

func quote(v string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range v {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// scalarEnd returns the byte offset just past the scalar that starts at start.
// It handles double-quoted, single-quoted and plain scalars on one line.
func scalarEnd(text []byte, start int, inFlow bool) (int, error) {
	if start >= len(text) {
		return start, nil
	}
	switch text[start] {
	case '"':
		for i := start + 1; i < len(text); i++ {
			switch text[i] {
			case '\\':
				i++
			case '"':
				return i + 1, nil
			}
		}
		return 0, fmt.Errorf("yamledit: unterminated quoted scalar")
	case '\'':
		for i := start + 1; i < len(text); i++ {
			if text[i] == '\'' {
				if i+1 < len(text) && text[i+1] == '\'' {
					i++
					continue
				}
				return i + 1, nil
			}
		}
		return 0, fmt.Errorf("yamledit: unterminated quoted scalar")
	case '|', '>':
		return 0, fmt.Errorf("yamledit: block scalars are not supported")
	}
	end := start
	for i := start; i < len(text); i++ {
		c := text[i]
		if c == '\n' || c == '\r' {
			break
		}
		if c == '#' && i > start && (text[i-1] == ' ' || text[i-1] == '\t') {
			break
		}
		if inFlow && (c == ',' || c == ']' || c == '}') {
			break
		}
		end = i + 1
	}
	for end > start && (text[end-1] == ' ' || text[end-1] == '\t') {
		end--
	}
	return end, nil
}

// flowClose returns the offset of the bracket that closes the flow collection
// opened at open, skipping quoted text and comments.
func flowClose(text []byte, open int) (int, error) {
	depth := 0
	for i := open; i < len(text); i++ {
		switch c := text[i]; c {
		case '[', '{':
			depth++
		case ']', '}':
			depth--
			if depth == 0 {
				return i, nil
			}
		case '"', '\'':
			end, err := scalarEnd(text, i, true)
			if err != nil {
				return 0, err
			}
			i = end - 1
		case '#':
			if i > 0 && (text[i-1] == ' ' || text[i-1] == '\t' || text[i-1] == '\n') {
				for i < len(text) && text[i] != '\n' {
					i++
				}
			}
		}
	}
	return 0, fmt.Errorf("yamledit: unterminated flow collection")
}
