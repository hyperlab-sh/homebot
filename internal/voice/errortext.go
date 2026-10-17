package voice

import (
	"errors"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
)

// nestedError is a message wrapped around another error: "Step 2 failed:
// <inner>", then a suffix such as the resume hint. Error() is neutral all
// the way down, so it is safe to store; it keeps the parts so ErrorText can
// say the message, the inner error and the suffix in the voice when the line
// is printed. spoken is the whole error said in a Context's voice, from the
// same parts.
type nestedError struct {
	message *fail.UserError
	inner   error
	suffix  string
	spoken  func(*app.Context) string
}

func (e *nestedError) Error() string { return e.message.Error() + e.suffix }

// Unwrap lists the wrapper's own message first, so errors.As finds it before
// anything the inner error holds.
func (e *nestedError) Unwrap() []error {
	if e.inner == nil {
		return []error{e.message}
	}
	return []error{e.message, e.inner}
}

// Nest is the error build(inner.Error()) followed by suffix, where inner is
// the error it wraps. Its Error() is neutral and is the form to store, as long
// as inner's Error() is neutral too; ErrorText builds the message again from
// ErrorText(inner), so a fail.Say error inside inner is said in the voice at
// print. suffix is added as it is, in both forms. A nil inner is the message
// built from an empty string. Use NestWith for a suffix made of catalog text.
func Nest(inner error, build func(inner string) msg.Text, suffix string) error {
	return NestWith(inner, build, func(func(msg.Text) string) string { return suffix })
}

// NestWith is Nest with a suffix made of catalog text. suffix gets the
// function that says a text, and returns the whole suffix built with it:
// Error() calls it with the neutral voice (what is stored) and ErrorText with
// ctx's (what is printed), so a voiced text in the suffix, such as the resume
// hint of a failed unpack step, is in the persona's words on the terminal and
// the English in the stored error.
func NestWith(inner error, build func(inner string) msg.Text, suffix func(say func(msg.Text) string) string) error {
	neutral := suffix(func(t msg.Text) string { return render(Neutral{}, t) })
	if inner == nil {
		return &nestedError{
			message: fail.Say(build("")), suffix: neutral,
			spoken: func(ctx *app.Context) string {
				return Plain(ctx, build("")) + suffix(func(t msg.Text) string { return Plain(ctx, t) })
			},
		}
	}
	return &nestedError{
		message: fail.Say(build(inner.Error())), inner: inner, suffix: neutral,
		spoken: func(ctx *app.Context) string {
			return Plain(ctx, build(ErrorText(ctx, inner))) + suffix(func(t msg.Text) string { return Plain(ctx, t) })
		},
	}
}

// SayWith is a user error whose message holds texts of its own, such as the
// planner's reasons in the link action's layout error. build makes the message
// and gets the function that says a text: Error() calls it with the neutral
// voice, so the error is safe to store, and ErrorText with ctx's, so every
// text in it, and the message around them, is in the persona's words when the
// error is printed. build must be pure: it runs once for each form.
func SayWith(build func(say func(msg.Text) string) msg.Text) error {
	return &nestedError{
		message: fail.Say(build(func(t msg.Text) string { return render(Neutral{}, t) })),
		spoken: func(ctx *app.Context) string {
			return Plain(ctx, build(func(t msg.Text) string { return Plain(ctx, t) }))
		},
	}
}

// Attach is summary, then separator, then inner: the summary in front of an
// error that already says why. Error() is neutral for both parts and is the
// form to store; ErrorText says the summary and inner in the voice at print.
// Use it where a catalog message names the thing that failed (a secret's
// file) and inner, a user error, says the reason, so neither is lost. A nil
// inner is the summary alone.
func Attach(inner error, summary msg.Text, separator string) error {
	if inner == nil {
		return fail.Say(summary)
	}
	return &nestedError{
		message: fail.Say(summary), inner: inner, suffix: separator + inner.Error(),
		spoken: func(ctx *app.Context) string { return Plain(ctx, summary) + separator + ErrorText(ctx, inner) },
	}
}

// ErrorText is err's message as ctx's voice says it. A fail.Say error in
// err's chain is said in ctx's voice, and what wraps it stays around it:
// "loading: <message>". A Nest or Attach error is said whole, its inner error
// too. Any other error is its Error() text.
func ErrorText(ctx *app.Context, err error) string {
	text := err.Error()
	var nested *nestedError
	if errors.As(err, &nested) {
		neutral := nested.Error()
		if i := strings.LastIndex(text, neutral); i >= 0 {
			return text[:i] + nested.spoken(ctx) + text[i+len(neutral):]
		}
	}
	var ue *fail.UserError
	if !errors.As(err, &ue) {
		return text
	}
	t, ok := ue.Text()
	if !ok {
		return text
	}
	neutral := ue.Error()
	i := strings.LastIndex(text, neutral)
	if i < 0 {
		return text
	}
	return text[:i] + Plain(ctx, t) + text[i+len(neutral):]
}
