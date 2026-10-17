package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/hyperlab-sh/homebot/internal/fail"
)

// stopOnSignal is ctx cancelled by the first SIGINT or SIGTERM, with the
// code a shell reports for a process that signal killed, 128 plus its
// number, as a *fail.Exit cause: the TypeScript build died of the signal
// without a word, and cli.Execute ends such a run with that code, silently.
// The first signal gives both back their default action, so a second one
// kills homebot at once, however long the command takes to stop. stop
// releases the signals and ctx.
func stopOnSignal(ctx context.Context) (_ context.Context, stop func()) {
	ctx, cancel := context.WithCancelCause(ctx)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case s := <-signals:
			signal.Stop(signals)
			cancel(fail.Silently(exitCodeOf(s)))
		case <-ctx.Done():
		}
	}()
	return ctx, func() {
		signal.Stop(signals)
		cancel(nil)
	}
}

// exitCodeOf is the code a shell reports for a process s killed: 130 for
// SIGINT, 143 for SIGTERM.
func exitCodeOf(s os.Signal) int {
	if n, ok := s.(syscall.Signal); ok {
		return 128 + int(n)
	}
	return 1
}
