//go:build linux

package testutil

import (
	"fmt"
	"os"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// OpenPTY opens a pseudo-terminal: master is the terminal's side, tty the
// program's. Reads from master can time out (SetReadDeadline). It skips the
// test when the system has none to give.
func OpenPTY(t testing.TB) (master, tty *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	conn, err := master.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var n int
	var ioErr error
	// Control keeps master non-blocking, so reads can time out.
	if err := conn.Control(func(fd uintptr) {
		if ioErr = unix.IoctlSetPointerInt(int(fd), unix.TIOCSPTLCK, 0); ioErr == nil {
			n, ioErr = unix.IoctlGetInt(int(fd), unix.TIOCGPTN)
		}
	}); err != nil || ioErr != nil {
		t.Skipf("no pseudo-terminal: %v %v", err, ioErr)
	}
	tty, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	t.Cleanup(func() { _ = tty.Close() })
	return master, tty
}
