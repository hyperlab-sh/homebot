//go:build linux || darwin

package secrets

import "golang.org/x/sys/unix"

// echoOff stops the terminal at fd echoing what is typed, keeping line
// editing and Ctrl-C, and returns the function that puts it back.
func echoOff(fd int) (restore func() error, err error) {
	old, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	if err != nil {
		return nil, err
	}
	quiet := *old
	quiet.Lflag &^= unix.ECHO
	quiet.Lflag |= unix.ICANON | unix.ISIG
	quiet.Iflag |= unix.ICRNL
	if err := unix.IoctlSetTermios(fd, ioctlSetTermios, &quiet); err != nil {
		return nil, err
	}
	return func() error { return unix.IoctlSetTermios(fd, ioctlSetTermios, old) }, nil
}
