package ui

import "golang.org/x/sys/unix"

// fread is FREAD from <sys/fcntl.h>: TIOCFLUSH's argument for the input
// queue alone.
const fread = 1

// flushInput throws away what the terminal at fd has received but no one
// has read.
func flushInput(fd uintptr) error {
	return unix.IoctlSetPointerInt(int(fd), unix.TIOCFLUSH, fread)
}
