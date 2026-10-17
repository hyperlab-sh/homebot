package tidy

import (
	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/exports"
)

// SetFlock replaces the flock call and returns what undoes it.
func SetFlock(fn func(fd, how int) error) (restore func()) {
	old := flock
	flock = fn
	return func() { flock = old }
}

// SetRunExports replaces exports.RunExports and returns what undoes it.
func SetRunExports(fn func(*app.Context, exports.Options) ([]exports.Result, error)) (restore func()) {
	old := runExports
	runExports = fn
	return func() { runExports = old }
}
