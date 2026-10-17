package ui

import (
	"fmt"
	"io"

	"github.com/charmbracelet/colorprofile"

	"github.com/hyperlab-sh/homebot/internal/app"
)

// Out is ctx.Stdout as homebot prints to it: styled text fitted to what the
// stream shows, plain when it is not a terminal. The writer (its stream and
// its colour profile) is fixed the first time ctx is asked for it, and every
// copy of ctx shares it: a copy with Stdout or Env changed still writes to
// the first stream, with the first profile (see app.Once).
func Out(ctx *app.Context) io.Writer {
	return writer(ctx, stdoutStream)
}

// Err is ctx.Stderr as homebot prints to it, fixed per Context as Out is.
func Err(ctx *app.Context) io.Writer {
	return writer(ctx, stderrStream)
}

// Println prints a to Out, as fmt.Println does. Like console.log, it
// ignores write errors.
func Println(ctx *app.Context, a ...any) {
	_, _ = fmt.Fprintln(Out(ctx), a...)
}

// Eprintln prints a to Err, as fmt.Println does; see Println.
func Eprintln(ctx *app.Context, a ...any) {
	_, _ = fmt.Fprintln(Err(ctx), a...)
}

// writer is the colour writer of one of ctx's streams.
func writer(ctx *app.Context, s stream) *colorprofile.Writer {
	return app.Once(ctx, s, func() *colorprofile.Writer {
		w := ctx.Stdout
		if s == stderrStream {
			w = ctx.Stderr
		}
		return &colorprofile.Writer{Forward: w, Profile: detect(w, ctx.Env)}
	})
}

// Attached is the terminal itself, for a child process homebot hands it to
// (sudo, a package manager, an editor, fzf): the Context's own streams, not
// Out and Err, so the child sees a terminal where there is one and does its
// own colour and progress. Homebot's own text never goes here.
func Attached(ctx *app.Context) (stdin io.Reader, stdout, stderr io.Writer) {
	return ctx.Stdin, ctx.Stdout, ctx.Stderr
}
