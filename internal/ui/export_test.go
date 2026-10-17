package ui

import "testing"

// ForgetBackground makes the next Context on a terminal ask it again, as a
// new process would, and again after t.
func ForgetBackground(t testing.TB) {
	t.Helper()
	background = &backgroundAnswer{}
	t.Cleanup(func() { background = &backgroundAnswer{} })
}
