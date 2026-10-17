// Package unpack runs a profile's steps on this machine, in order, and
// records that the profile was unpacked.
package unpack

import (
	"context"

	"github.com/hyperlab-sh/homebot/internal/actions"
	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/profile"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// Options shape one run.
type Options struct {
	// DryRun lists the steps and runs none.
	DryRun bool
	// From is the one-based step to start at; nil starts at step 1. Steps
	// before it are listed as skipped. A value outside 1..len(steps) is a
	// user error, so a caller passes along whatever the user typed.
	From *int
}

// Run runs the steps of the named profile, or of the default profile for this
// host when name is empty. A caller that takes the
// name from the user must refuse a "" the user typed itself: it asks for the
// default here. The first failing step stops the run with an error
// that says which step to resume from. After the last step Run records the
// profile and its steps' hash in state.json.
func Run(c *app.Context, name string, opts Options) error {
	if name == "" {
		def, ok := profile.DefaultProfile(c)
		if !ok || def == "" {
			return fail.User(voice.Plain(c, msg.UnpackErrorNoProfile()))
		}
		name = def
	}
	steps, err := profile.ResolveFor(c, name)
	if err != nil {
		return err
	}
	from := 1
	if opts.From != nil {
		from = *opts.From
	}
	if from < 1 || from > max(len(steps), 1) {
		return fail.User(voice.Plain(c, msg.UnpackErrorFromRange(len(steps))))
	}

	st := ui.Styles(c)
	header := msg.UnpackStatusHeader
	if opts.DryRun {
		header = msg.UnpackStatusHeaderDryRun
	}
	ui.Println(c, st.Accent.Render(voice.Render(c, header(name, c.Host, string(c.OS), len(steps)))))

	for i, step := range steps {
		n := i + 1
		label := profile.DescribeStep(step)
		if n < from {
			ui.Println(c, st.Muted.Render(voice.Render(c, msg.UnpackStatusStepSkipped(n, len(steps), label))))
			continue
		}
		if err := stopped(c); err != nil {
			return err
		}
		ui.Println(c, st.Command.Render(voice.Render(c, msg.UnpackStatusStep(n, len(steps), label))))
		if opts.DryRun {
			continue
		}
		if err := runStep(c, name, n, step); err != nil {
			return err
		}
	}
	if opts.DryRun {
		return nil
	}
	if err := stopped(c); err != nil {
		return err
	}
	if err := Record(c, name, steps); err != nil {
		return err
	}
	ui.Println(c, st.OK.Render(voice.Render(c, msg.UnpackSuccessDone(name))))
	return nil
}

// Record writes to state.json that the named profile, with these steps, is
// unpacked on this machine as of now: what Run does after its last step,
// and what the inbox compares the profile against to ask for an unpack.
func Record(c *app.Context, name string, steps []profile.Step) error {
	now := c.Now().UnixMilli()
	hash := profile.StepsHash(steps)
	return state.Update(c, func(s *state.State) {
		s.Unpack = &state.Unpack{Profile: name, Hash: hash, At: now}
	})
}

// stopped is why the run was told to stop (Ctrl-C), or nil. After a Ctrl-C
// no later step runs and nothing is recorded, as when the TypeScript build
// died of the signal.
func stopped(c *app.Context) error {
	if ctx := c.Context(); ctx.Err() != nil {
		return context.Cause(ctx)
	}
	return nil
}

// runStep runs step n of profile name. An internal action's error and a
// script's non-zero exit become errors that end with how to resume; an
// action that does not exist does not, since resuming cannot fix it.
func runStep(c *app.Context, name string, n int, step profile.Step) error {
	if actions.Wired(step.Action) {
		if err := actions.Run(c, step.Action, step.Args); err != nil {
			return voice.NestWith(err, func(inner string) msg.Text { return msg.UnpackErrorStepFailed(n, inner) }, resumeHint(name, n))
		}
		return nil
	}
	code, found, err := actions.RunScript(c, step.Action, step.Args)
	switch {
	case err != nil:
		return err
	case !found:
		return fail.User(voice.Plain(c, msg.UnpackErrorNoAction(n, step.Action)))
	case code != 0:
		return voice.NestWith(nil, func(string) msg.Text {
			return msg.UnpackErrorStepExit(n, profile.DescribeStep(step), code)
		}, resumeHint(name, n))
	}
	return nil
}

// resumeHint is the lines that follow a failed step: what to run to carry on
// from it. It is built with say, so the error's stored form has the English
// and its printed form has the persona's words for the hint (voice.NestWith).
func resumeHint(name string, n int) func(say func(msg.Text) string) string {
	return func(say func(msg.Text) string) string {
		cmd := say(msg.UnpackHintResumeCmd(name, n))
		return "\n" + say(msg.UnpackHintResume()) + "\n" + say(msg.FormatCommand(cmd))
	}
}
