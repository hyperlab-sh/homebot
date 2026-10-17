package profile

import (
	"strings"

	"github.com/hyperlab-sh/homebot/internal/fsx"
)

// StepsHash identifies what a profile would do, to notice when it changed
// since the last unpack. It is the SHA-256 of what JavaScript's
// JSON.stringify gives for [[action, args], ...]; labels and sources don't
// count.
func StepsHash(steps []Step) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, s := range steps {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('[')
		writeJSString(&b, s.Action)
		b.WriteString(",[")
		for j, a := range s.Args {
			if j > 0 {
				b.WriteByte(',')
			}
			writeJSString(&b, a)
		}
		b.WriteString("]]")
	}
	b.WriteByte(']')
	return fsx.Sha256([]byte(b.String()))
}

// writeJSString writes s as JSON.stringify does: only quotes, backslashes and
// control characters are escaped. "<", ">", "&", U+2028 and U+2029 stay raw.
func writeJSString(b *strings.Builder, s string) {
	const hex = "0123456789abcdef"
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20:
			b.WriteString(`\u00`)
			b.WriteByte(hex[r>>4])
			b.WriteByte(hex[r&0xf])
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
}
