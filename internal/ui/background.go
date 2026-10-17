package ui

import (
	"image/color"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/muesli/cancelreader"
)

// queryTimeout is how long homebot waits for the terminal to say what its
// background is before taking it to be dark.
const queryTimeout = 150 * time.Millisecond

// backgroundAnswer is the terminal's background, asked at most once.
type backgroundAnswer struct {
	once sync.Once
	dark bool
}

// background is asked once per process: a process has one terminal, however
// many Contexts it builds.
var background = &backgroundAnswer{}

// terminalIsDark reports whether the terminal in and out are has a dark
// background. Only the first call asks; every later one, with any files,
// gets that answer. No answer counts as dark.
func terminalIsDark(in, out term.File) bool {
	b := background
	b.once.Do(func() {
		bg, ok := askBackground(in, out, queryTimeout)
		b.dark = !ok || darkColor(bg)
	})
	return b.dark
}

// askBackground asks the terminal for its background colour (OSC 11), then
// for its device attributes (DA1), which every terminal answers: a terminal
// that does not know OSC 11 still ends the wait. It waits at most timeout,
// with in in raw mode so the answer is not echoed. When the wait ends
// without the device attributes, what the terminal has sent but homebot
// has not read is thrown away before in leaves raw mode, so the shell does
// not get it as typed input.
func askBackground(in, out term.File, timeout time.Duration) (color.Color, bool) {
	state, err := term.MakeRaw(in.Fd())
	if err != nil {
		return nil, false
	}
	pending := false
	defer func() {
		if pending {
			_ = flushInput(in.Fd())
		}
		_ = term.Restore(in.Fd(), state)
	}()
	r, err := cancelreader.NewReader(in)
	if err != nil {
		return nil, false
	}
	defer r.Close() //nolint:errcheck // reading is over either way
	timer := time.AfterFunc(timeout, func() { r.Cancel() })
	defer timer.Stop()
	if _, err := io.WriteString(out, ansi.RequestBackgroundColor+ansi.RequestPrimaryDeviceAttributes); err != nil {
		return nil, false
	}
	bg, answered := readBackground(r)
	pending = !answered
	return bg, bg != nil
}

// readBackground reads the terminal's answers from r until the device
// attributes, or until r fails, and returns the background colour among
// them, or nil, and whether the device attributes came.
func readBackground(r io.Reader) (bg color.Color, answered bool) {
	p := ansi.GetParser()
	defer ansi.PutParser(p)
	var seq []byte
	var buf [256]byte
	var state byte
	for {
		n, err := r.Read(buf[:])
		in := buf[:n]
		for len(in) > 0 {
			s, _, read, next := ansi.DecodeSequence(in, state, p)
			seq = append(seq, s...)
			if next == ansi.NormalState {
				switch {
				case ansi.HasOscPrefix(seq) && p.Command() == 11:
					if parts := strings.Split(string(p.Data()), ";"); len(parts) == 2 {
						bg = ansi.XParseColor(parts[1])
					}
				case ansi.HasCsiPrefix(seq) && p.Command() == ansi.Command('?', 0, 'c'):
					return bg, true
				}
				seq = seq[:0]
			}
			state = next
			in = in[read:]
		}
		if err != nil {
			return bg, false
		}
	}
}

// darkColor reports whether c is dark: its HSL lightness is under one half.
func darkColor(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return max(r, g, b)+min(r, g, b) < 0xffff
}
