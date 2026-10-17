//go:build linux

package ui_test

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// setColumns sets the width of the terminal tty is.
func setColumns(t *testing.T, tty *os.File, columns uint16) {
	t.Helper()
	if err := unix.IoctlSetWinsize(int(tty.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 24, Col: columns}); err != nil {
		t.Fatal(err)
	}
}
