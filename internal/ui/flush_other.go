//go:build !linux && !darwin

package ui

// flushInput does nothing where homebot does not know how to flush a
// terminal's input.
func flushInput(uintptr) error { return nil }
