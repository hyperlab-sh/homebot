//go:build !linux

package testutil

import (
	"os"
	"testing"
)

// OpenPTY skips the test: pseudo-terminals are opened on Linux only.
func OpenPTY(t testing.TB) (master, tty *os.File) {
	t.Helper()
	t.Skip("pseudo-terminals are opened on Linux only")
	return nil, nil
}
