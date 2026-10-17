package cli

import (
	"context"
	"errors"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// Execute runs the command line args and returns homebot's exit code. It
// prints what went wrong, if anything, through c. ctx stops the command:
// cmd/homebot cancels it on Ctrl-C and SIGTERM, with a *fail.Exit as the
// cause. A run so stopped ends with that code and prints nothing more,
// whatever the command returned: what failed was stopped on purpose.
func Execute(ctx context.Context, c *app.Context, build Build, args []string) int {
	defer voice.SaveChoices(c)
	c.Debug.Debug("build", "version", build.Version, "commit", build.Commit, "date", build.Date, "channel", build.Channel)
	root := NewRoot(c, build)
	argv, err := dispatch(c, root, args)
	reportConfigWarnings(c)
	if err == nil {
		root.SetArgs(argv)
		err = root.ExecuteContext(ctx)
	}
	reportConfigWarnings(c)
	var stopped *fail.Exit
	if errors.As(context.Cause(ctx), &stopped) {
		return stopped.Code
	}
	Report(c, err)
	return ExitCode(err)
}

// reportConfigWarnings prints the config warnings collected so far, once each.
// Like Report, it voices the text at print time and puts it behind the
// catalog's "homebot: " prefix; a quiet run sends the text to the debug log.
func reportConfigWarnings(c *app.Context) {
	for _, warning := range c.TakeConfigWarnings() {
		text := voice.Plain(c, warning)
		if c.ConfigWarningsQuiet {
			c.Debug.Debug("config warning", "text", text)
		} else {
			ui.Eprintln(c, ui.ErrStyles(c).Muted.Render(voice.Plain(c, msg.CLIErrorPrefix(text))))
		}
	}
}
