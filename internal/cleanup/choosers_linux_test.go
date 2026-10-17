package cleanup

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// screen keeps everything a program draws on the far end of a pty.
type screen struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// watch copies what master reads into s until the test ends.
func (s *screen) watch(t *testing.T, master *os.File) {
	stop := make(chan struct{})
	done := make(chan struct{})
	t.Cleanup(func() { close(stop); <-done })
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = master.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, _ := master.Read(buf)
			s.mu.Lock()
			s.buf.Write(buf[:n])
			s.mu.Unlock()
		}
	}()
}

func (s *screen) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// waitFor waits until text is on s.
func (s *screen) waitFor(t *testing.T, text string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(s.String(), text) {
		if time.Now().After(deadline) {
			t.Fatalf("%q never showed; the screen has %q", text, s.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Without fzf, on a terminal, the question is huh's full-screen select:
// arrows move from keep, Enter answers, choosing quit quits (exit 0), and
// Ctrl-C stops the command with 130, as triage's picker does.
func TestSelectOnARealTerminal(t *testing.T) {
	for _, tc := range []struct {
		name, keys string
		want       Answer
		exit       int
	}{
		{"enter keeps", "\r", Keep, 0},
		{"down then enter uses", "\x1b[B\r", Use, 0},
		{"up then enter deletes", "\x1b[A\r", Delete, 0},
		{"quit is a clean stop", strings.Repeat("\x1b[B", 8) + "\r", Quit, 0},
		{"ctrl-c stops with 130", "\x03", Quit, 130},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newBox(t)
			got, q := asked(b)
			b.pinPath()
			master, tty := testutil.OpenPTY(t)
			b.Ctx.Stdin, b.Ctx.Stdout = tty, tty
			b.Ctx.Env = b.Ctx.Env.With(map[string]string{"TERM": "xterm-256color"})
			var s screen
			s.watch(t, master)

			type result struct {
				answer Answer
				err    error
			}
			done := make(chan result, 1)
			go func() {
				answer, err := DefaultChooser(b.Ctx)(got, q)
				done <- result{answer, err}
			}()
			s.waitFor(t, "[1/2]")
			// Let the select read its first frame's replies before the keys.
			time.Sleep(200 * time.Millisecond)
			if _, err := master.WriteString(tc.keys); err != nil {
				t.Fatal(err)
			}
			select {
			case r := <-done:
				if r.answer != tc.want {
					t.Errorf("answer = %q, want %q", r.answer, tc.want)
				}
				var exit *fail.Exit
				switch {
				case tc.exit == 0 && r.err != nil:
					t.Errorf("err = %v, want none", r.err)
				case tc.exit != 0 && (!errors.As(r.err, &exit) || exit.Code != tc.exit):
					t.Errorf("err = %v, want a silent exit %d", r.err, tc.exit)
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("the select never returned; the screen has %q", s.String())
			}
			if strings.Contains(s.String(), "Enter a number") {
				t.Error("asked on plain lines on a terminal")
			}
		})
	}
}
