package secrets

import (
	"errors"
	"io"
	"os"

	"github.com/charmbracelet/x/term"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
)

// ttyPath is the terminal passphrases are read from: the controlling
// terminal, never stdin, which may be a pipe. Release code never changes it;
// tests point it at a pseudo-terminal.
var ttyPath = "/dev/tty"

// terminal is where homebot talks to the person at the keyboard about the
// passphrase. Nothing said there reaches stdout, stderr or a log.
type terminal interface {
	// secret shows prompt and reads one line without echoing it.
	secret(prompt string) ([]byte, error)
	// show writes a line.
	show(line string) error
	close()
}

// openTerminal opens the terminal. In a test binary, HOMEBOT_TEST_PASSPHRASE
// replaces it with one that answers every prompt with that passphrase.
func openTerminal(ctx *app.Context) (terminal, error) {
	if pass, ok := testPassphrase(ctx.Env); ok {
		return scripted(pass), nil
	}
	f, err := os.OpenFile(ttyPath, os.O_RDWR, 0)
	if err != nil {
		return nil, fail.Say(msg.SecretsErrorNoTerminal())
	}
	if !term.IsTerminal(f.Fd()) {
		_ = f.Close()
		return nil, fail.Say(msg.SecretsErrorNoTerminal())
	}
	return &tty{ctx: ctx, f: f}, nil
}

type tty struct {
	ctx *app.Context
	f   *os.File
	// abandoned is a read cancelled by Ctrl-C that still holds f.
	abandoned <-chan answer
}

type answer struct {
	line []byte
	err  error
}

// secret prints prompt, reads a line with echo off and erases the prompt
// line after, as the age CLI does. Ctrl-C (the command's context being
// cancelled) stops it and puts the terminal back as it was.
func (t *tty) secret(prompt string) ([]byte, error) {
	restore, err := echoOff(int(t.f.Fd()))
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(t.f, prompt+" "); err != nil {
		_ = restore()
		return nil, err
	}
	defer t.clearLine()
	read := make(chan answer, 1)
	go func() {
		line, err := readLine(t.f)
		read <- answer{line, err}
	}()
	select {
	case a := <-read:
		if err := restore(); err != nil {
			clear(a.line)
			return nil, err
		}
		if a.err != nil {
			clear(a.line)
			return nil, t.cancelled()
		}
		return a.line, nil
	case <-t.ctx.Context().Done():
		_ = restore()
		t.abandoned = read
		return nil, t.cancelled()
	}
}

// readLine reads up to a newline, which it drops along with any carriage
// return. Ctrl-D on an empty line is io.EOF.
func readLine(r io.Reader) ([]byte, error) {
	line := make([]byte, 0, 128)
	var b [1]byte
	for {
		n, err := r.Read(b[:])
		if n == 1 {
			switch b[0] {
			case '\n':
				return line, nil
			case '\r':
			default:
				line = append(line, b[0])
			}
			continue
		}
		if err != nil {
			if errors.Is(err, io.EOF) && len(line) > 0 {
				return line, nil
			}
			return line, err
		}
	}
}

func (t *tty) cancelled() error {
	return fail.Say(msg.SecretsErrorCancelled())
}

// clearLine opens a new line, then moves up and erases it, so neither the
// prompt nor the typing stays on screen.
func (t *tty) clearLine() {
	_, _ = io.WriteString(t.f, "\r\n\x1b[F\x1b[K")
}

func (t *tty) show(line string) error {
	_, err := io.WriteString(t.f, line+"\n")
	return err
}

// close closes the terminal, or, while a cancelled read still blocks on it,
// leaves that to the read.
func (t *tty) close() {
	if t.abandoned == nil {
		_ = t.f.Close()
		return
	}
	go func(f *os.File, read <-chan answer) {
		a := <-read
		clear(a.line)
		_ = f.Close()
	}(t.f, t.abandoned)
}

// scripted is the terminal of HOMEBOT_TEST_PASSPHRASE: every prompt gets the
// passphrase, and what is shown goes nowhere.
type scripted string

func (s scripted) secret(string) ([]byte, error) { return []byte(s), nil }
func (scripted) show(string) error               { return nil }
func (scripted) close()                          {}
