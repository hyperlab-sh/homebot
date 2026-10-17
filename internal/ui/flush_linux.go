package ui

import "golang.org/x/sys/unix"

// flushInput throws away what the terminal at fd has received but no one
// has read.
func flushInput(fd uintptr) error {
	return unix.IoctlSetInt(int(fd), unix.TCFLSH, unix.TCIFLUSH)
}
