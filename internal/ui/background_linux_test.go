package ui

import (
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// What the terminal sent and no one read is gone after flushInput, so it
// cannot reach the shell as typed input.
func TestFlushInputEmptiesWhatWasNotRead(t *testing.T) {
	master, tty := testutil.OpenPTY(t)
	if _, err := master.WriteString("\x1b]11;rgb:0000/0000/0000\x1b\\\x1b[?62;c\n"); err != nil {
		t.Fatal(err)
	}
	waiting := func() int {
		n, err := unix.IoctlGetInt(int(tty.Fd()), unix.TIOCINQ)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	for deadline := time.Now().Add(5 * time.Second); waiting() == 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("premise: the answer never reached the terminal's input")
		}
	}
	if err := flushInput(tty.Fd()); err != nil {
		t.Fatal(err)
	}
	if n := waiting(); n != 0 {
		t.Errorf("%d bytes still wait to be read", n)
	}
}
