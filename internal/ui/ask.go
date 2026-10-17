package ui

import (
	"io"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
)

// Ask shows question and reads one line of ctx.Stdin, the answer trimmed.
// The question loses its trailing spaces and gains a single one, as Bun's
// prompt() writes it. A closed stdin is an empty answer, and so is a last
// line the input ends without finishing, as with prompt(). Ask reads one byte
// at a time, so the next Ask gets the next line.
func Ask(ctx *app.Context, question string) string {
	answer, _ := AskOrEnd(ctx, question)
	return answer
}

// AskOrEnd is Ask that also says whether the person answered: false when
// the input ended before a newline (Ctrl-D on a terminal, a closed pipe),
// with "" as the answer, as Ask gives it. A question where ending the input
// should mean "no" rather than Enter's default uses it.
func AskOrEnd(ctx *app.Context, question string) (string, bool) {
	_, _ = io.WriteString(Out(ctx), strings.TrimRightFunc(question, jsSpace)+" ")
	var line []byte
	var b [1]byte
	for {
		n, err := ctx.Stdin.Read(b[:])
		if n == 1 {
			if b[0] == '\n' {
				return strings.TrimFunc(string(line), jsSpace), true
			}
			line = append(line, b[0])
		}
		if err != nil {
			return "", false
		}
	}
}
