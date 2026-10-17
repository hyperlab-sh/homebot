package ui_test

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/ui"
)

// onTerminal is a Context whose streams are the given ones, in a colour
// terminal's environment without TTY_FORCE, so only a real terminal counts.
func onTerminal(t *testing.T, stdin io.Reader, stdout, stderr io.Writer, env map[string]string) *app.Context {
	t.Helper()
	work := t.TempDir()
	vars := testutil.SandboxEnv(work)
	delete(vars, "NO_COLOR")
	vars["TERM"] = "xterm-256color"
	vars["COLORTERM"] = "truecolor"
	for k, v := range env {
		vars[k] = v
	}
	ctx, err := app.New(app.Options{Env: app.NewEnv(vars), Cwd: work, OS: app.Linux, Stdin: stdin, Stdout: stdout, Stderr: stderr})
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

// readUntil reads what the program wrote to the terminal until it holds
// want, or fails the test after a few seconds.
func readUntil(t *testing.T, master *os.File, want string) string {
	t.Helper()
	got, err := read(master, want)
	if err != nil {
		t.Fatalf("the terminal got %q, then: %v", got, err)
	}
	return got
}

// read reads master until it holds want, giving up after a few seconds.
func read(master *os.File, want string) (string, error) {
	if err := master.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return "", err
	}
	var got []byte
	buf := make([]byte, 256)
	for !bytes.Contains(got, []byte(want)) {
		n, err := master.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil {
			return string(got), err
		}
	}
	return string(got), nil
}

func TestEachStreamIsDetectedOnItsOwn(t *testing.T) {
	const urgent = darkUrgent
	t.Run("stdout piped, stderr a terminal", func(t *testing.T) {
		master, tty := testutil.OpenPTY(t)
		var piped bytes.Buffer
		ctx := onTerminal(t, strings.NewReader(""), &piped, tty, nil)
		failed := ui.Styles(ctx).Error.Render("failed")
		ui.Println(ctx, failed)
		ui.Eprintln(ctx, failed)
		if piped.String() != "failed\n" {
			t.Errorf("stdout = %q, want it plain", piped.String())
		}
		if got := readUntil(t, master, "failed"); !strings.Contains(got, urgent) {
			t.Errorf("stderr = %q, want it in colour", got)
		}
	})
	t.Run("stdout a terminal, stderr piped", func(t *testing.T) {
		master, tty := testutil.OpenPTY(t)
		var piped bytes.Buffer
		ctx := onTerminal(t, strings.NewReader(""), tty, &piped, map[string]string{"HOMEBOT_THEME": "dark"})
		failed := ui.Styles(ctx).Error.Render("failed")
		ui.Eprintln(ctx, failed)
		ui.Println(ctx, failed)
		if piped.String() != "failed\n" {
			t.Errorf("stderr = %q, want it plain", piped.String())
		}
		if got := readUntil(t, master, "failed"); !strings.Contains(got, urgent) {
			t.Errorf("stdout = %q, want it in colour", got)
		}
	})
}

func TestColumnsOnATerminal(t *testing.T) {
	for columns, want := range map[uint16]int{120: 100, 100: 100, 72: 72} {
		_, tty := testutil.OpenPTY(t)
		setColumns(t, tty, columns)
		if got := ui.Columns(onTerminal(t, tty, tty, tty, nil)); got != want {
			t.Errorf("%d columns: Columns = %d, want %d", columns, got, want)
		}
	}
}

// The palette's urgent colour on a light and on a dark background.
const (
	lightUrgent = "38;2;198;40;40"
	darkUrgent  = "38;2;255;107;107"
)

// A COLORFGBG that names no background colour is ignored: the terminal is
// asked, and here it answers light.
func TestTheThemeIsAskedOfTheTerminal(t *testing.T) {
	for _, c := range []struct {
		name, background, urgent string
		env                      map[string]string
	}{
		{"light", "rgb:ffff/ffff/ffff", lightUrgent, nil},
		{"dark", "rgb:1e1e/1e1e/1e1e", darkUrgent, nil},
		{"COLORFGBG with a default background", "rgb:ffff/ffff/ffff", lightUrgent, map[string]string{"COLORFGBG": "0;default"}},
		{"COLORFGBG past 15", "rgb:ffff/ffff/ffff", lightUrgent, map[string]string{"COLORFGBG": "15;16"}},
		{"COLORFGBG with one field", "rgb:ffff/ffff/ffff", lightUrgent, map[string]string{"COLORFGBG": "0"}},
		{"COLORFGBG empty", "rgb:ffff/ffff/ffff", lightUrgent, map[string]string{"COLORFGBG": ""}},
	} {
		t.Run(c.name, func(t *testing.T) {
			ui.ForgetBackground(t)
			master, tty := testutil.OpenPTY(t)
			answered := make(chan string, 1)
			go func() {
				// The query is OSC 11 then a device attributes request, which
				// the answer ends with.
				asked, err := read(master, "\x1b[c")
				if err == nil {
					_, err = master.WriteString("\x1b]11;" + c.background + "\x1b\\\x1b[?62;c")
				}
				if err != nil {
					asked += " (" + err.Error() + ")"
				}
				answered <- asked
			}()
			ctx := onTerminal(t, tty, tty, tty, c.env)
			got := ui.Styles(ctx).Urgent.Render("x")
			if asked := <-answered; !strings.Contains(asked, "\x1b]11;?") {
				t.Errorf("the terminal was asked %q, want an OSC 11 query", asked)
			}
			if !strings.Contains(got, c.urgent) {
				t.Errorf("urgent on a %s terminal = %q, want %s", c.name, got, c.urgent)
			}
		})
	}
}

// The terminal is not asked when the environment says what its
// background is, over ssh, where a late answer would land at the shell's
// prompt, or when stdout shows no colour.
func TestTheThemeIsNotAskedWhenTheAnswerIsKnownOrUnneeded(t *testing.T) {
	for _, c := range []struct {
		name, urgent string
		env          map[string]string
	}{
		{"HOMEBOT_THEME=light", lightUrgent, map[string]string{"HOMEBOT_THEME": "light"}},
		{"HOMEBOT_THEME=dark", darkUrgent, map[string]string{"HOMEBOT_THEME": "dark", "COLORFGBG": "0;15"}},
		{"COLORFGBG light", lightUrgent, map[string]string{"COLORFGBG": "0;15"}},
		{"COLORFGBG 7 is light", lightUrgent, map[string]string{"COLORFGBG": "0;7"}},
		{"COLORFGBG with a pixmap, light", lightUrgent, map[string]string{"COLORFGBG": "0;default;9"}},
		{"COLORFGBG dark", darkUrgent, map[string]string{"COLORFGBG": "15;0"}},
		{"COLORFGBG 8 is dark", darkUrgent, map[string]string{"COLORFGBG": "15;8"}},
		{"COLORFGBG with a pixmap, dark", darkUrgent, map[string]string{"COLORFGBG": "15;default;6"}},
		{"COLORFGBG over ssh", lightUrgent, map[string]string{"COLORFGBG": "0;15", "SSH_TTY": "/dev/pts/3"}},
		{"SSH_CONNECTION", darkUrgent, map[string]string{"SSH_CONNECTION": "192.0.2.1 50000 192.0.2.2 22"}},
		{"SSH_TTY", darkUrgent, map[string]string{"SSH_TTY": "/dev/pts/3"}},
		{"NO_COLOR", darkUrgent, map[string]string{"NO_COLOR": "1"}},
		{"TERM=dumb", darkUrgent, map[string]string{"TERM": "dumb"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			ui.ForgetBackground(t)
			master, tty := testutil.OpenPTY(t)
			ctx := onTerminal(t, tty, tty, tty, c.env)
			if got := ui.Styles(ctx).Urgent.Render("x"); !strings.Contains(got, c.urgent) {
				t.Errorf("urgent = %q, want %s", got, c.urgent)
			}
			ui.Println(ctx, "done")
			if got := readUntil(t, master, "done"); strings.Contains(got, "\x1b]11") || strings.Contains(got, "\x1b[c") {
				t.Errorf("the terminal was asked for its background: %q", got)
			}
		})
	}
}

// Text for stderr asks nothing when stderr shows no colour, even with
// stdin and stdout on the terminal.
func TestErrStylesAskNothingForAPlainStderr(t *testing.T) {
	ui.ForgetBackground(t)
	master, tty := testutil.OpenPTY(t)
	var stderr bytes.Buffer
	ctx := onTerminal(t, tty, tty, &stderr, nil)
	ui.Eprintln(ctx, ui.ErrStyles(ctx).Error.Render("failed"))
	if stderr.String() != "failed\n" {
		t.Errorf("stderr = %q, want it plain", stderr.String())
	}
	ui.Println(ctx, "done")
	if got := readUntil(t, master, "done"); strings.Contains(got, "\x1b]11") {
		t.Errorf("the terminal was asked for its background: %q", got)
	}
	// The premise: styling text for stdout does ask.
	ui.Styles(ctx)
	readUntil(t, master, "\x1b]11;?")
}

// A process has one terminal: the second Context on it takes the first
// one's answer instead of asking again.
func TestTheTerminalIsAskedOncePerProcess(t *testing.T) {
	ui.ForgetBackground(t)
	master, tty := testutil.OpenPTY(t)
	answered := make(chan string, 1)
	go func() {
		asked, err := read(master, "\x1b[c")
		if err == nil {
			_, err = master.WriteString("\x1b]11;rgb:ffff/ffff/ffff\x1b\\\x1b[?62;c")
		}
		if err != nil {
			asked += " (" + err.Error() + ")"
		}
		answered <- asked
	}()
	const light = lightUrgent
	first := onTerminal(t, tty, tty, tty, nil)
	if got := ui.Styles(first).Urgent.Render("x"); !strings.Contains(got, light) {
		t.Errorf("the first Context's urgent = %q, want the light %s", got, light)
	}
	asked := <-answered
	second := onTerminal(t, tty, tty, tty, nil)
	if got := ui.Styles(second).Urgent.Render("x"); !strings.Contains(got, light) {
		t.Errorf("the second Context's urgent = %q, want the first answer, light", got)
	}
	ui.Println(second, "done")
	asked += readUntil(t, master, "done")
	if n := strings.Count(asked, "\x1b]11;?"); n != 1 {
		t.Errorf("the terminal was asked %d times: %q", n, asked)
	}
}

// A terminal that never answers costs a short wait, once, and the
// background is taken to be dark.
func TestASilentTerminalIsNotWaitedOnLong(t *testing.T) {
	ui.ForgetBackground(t)
	master, tty := testutil.OpenPTY(t)
	start := time.Now()
	first := ui.Styles(onTerminal(t, tty, tty, tty, nil)).Urgent.Render("x")
	second := ui.Styles(onTerminal(t, tty, tty, tty, nil)).Urgent.Render("x")
	if took := time.Since(start); took > time.Second {
		t.Errorf("two Contexts on a silent terminal took %v", took)
	}
	for _, got := range []string{first, second} {
		if !strings.Contains(got, darkUrgent) {
			t.Errorf("urgent = %q, want the dark colour", got)
		}
	}
	ctx := onTerminal(t, tty, tty, tty, nil)
	ui.Println(ctx, "done")
	if got := readUntil(t, master, "done"); strings.Count(got, "\x1b]11;?") != 1 || strings.Count(got, "\x1b[c") != 1 {
		t.Errorf("the terminal got %q, want one OSC 11 and one DA1 query", got)
	}
}
