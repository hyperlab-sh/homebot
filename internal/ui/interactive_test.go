package ui_test

import (
	"os"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/ui"
)

func TestInteractive(t *testing.T) {
	yes, no := true, false

	t.Run("a reader that is not a file is not a terminal", func(t *testing.T) {
		b := testutil.Sandbox(t, testutil.Options{})
		b.Ctx.Stdin = strings.NewReader("y\n")
		if ui.Interactive(b.Ctx) {
			t.Error("Interactive = true for a strings.Reader, want false")
		}
	})
	t.Run("a pipe is not a terminal", func(t *testing.T) {
		b := testutil.Sandbox(t, testutil.Options{})
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
		b.Ctx.Stdin = r
		if ui.Interactive(b.Ctx) {
			t.Error("Interactive = true for a pipe, want false")
		}
	})
	t.Run("a terminal is", func(t *testing.T) {
		_, tty := testutil.OpenPTY(t)
		b := testutil.Sandbox(t, testutil.Options{})
		b.Ctx.Stdin = tty
		if !ui.Interactive(b.Ctx) {
			t.Error("Interactive = false for a pty, want true")
		}
	})
	t.Run("the override wins over what stdin is", func(t *testing.T) {
		b := testutil.Sandbox(t, testutil.Options{})
		b.Ctx.Stdin = strings.NewReader("")
		b.Ctx.StdinIsTerminal = &yes
		if !ui.Interactive(b.Ctx) {
			t.Error("Interactive = false with the override on, want true")
		}
		_, tty := testutil.OpenPTY(t)
		b.Ctx.Stdin = tty
		b.Ctx.StdinIsTerminal = &no
		if ui.Interactive(b.Ctx) {
			t.Error("Interactive = true with the override off, want false")
		}
	})
}
