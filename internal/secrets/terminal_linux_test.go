//go:build linux

package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"filippo.io/age"
	"golang.org/x/sys/unix"

	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// pty is a pseudo-terminal ttyPath points at: the test types on master.
type pty struct {
	t      *testing.T
	master *os.File
	// tty is the program's side, held to check its settings.
	tty *os.File
	out bytes.Buffer
}

// openPTY opens a pseudo-terminal and points ttyPath at it. It skips the
// test when the system has none to give.
func openPTY(t *testing.T) *pty {
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
	if err := conn.Control(func(fd uintptr) {
		if ioErr = unix.IoctlSetPointerInt(int(fd), unix.TIOCSPTLCK, 0); ioErr == nil {
			n, ioErr = unix.IoctlGetInt(int(fd), unix.TIOCGPTN)
		}
	}); err != nil || ioErr != nil {
		t.Skipf("no pseudo-terminal: %v %v", err, ioErr)
	}
	path := fmt.Sprintf("/dev/pts/%d", n)
	tty, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	t.Cleanup(func() { _ = tty.Close() })
	saved := ttyPath
	ttyPath = path
	t.Cleanup(func() { ttyPath = saved })
	return &pty{t: t, master: master, tty: tty}
}

// expect reads what the program writes until it has written want.
func (p *pty) expect(want string) {
	p.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	buf := make([]byte, 256)
	for !strings.Contains(p.out.String(), want) {
		if err := p.master.SetReadDeadline(deadline); err != nil {
			p.t.Fatal(err)
		}
		n, err := p.master.Read(buf)
		p.out.Write(buf[:n])
		if err != nil {
			p.t.Fatalf("waiting for %q: %v; the terminal shows %q", want, err, p.out.String())
		}
	}
}

// drain reads what is left to read.
func (p *pty) drain() string {
	p.t.Helper()
	buf := make([]byte, 256)
	for {
		if err := p.master.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
			p.t.Fatal(err)
		}
		n, err := p.master.Read(buf)
		p.out.Write(buf[:n])
		if err != nil {
			return p.out.String()
		}
	}
}

func (p *pty) typ(s string) {
	p.t.Helper()
	if _, err := p.master.WriteString(s); err != nil {
		p.t.Fatal(err)
	}
}

// echoing reports whether the terminal echoes what is typed.
func (p *pty) echoing() bool {
	p.t.Helper()
	tios, err := unix.IoctlGetTermios(int(p.tty.Fd()), unix.TCGETS)
	if err != nil {
		p.t.Fatal(err)
	}
	return tios.Lflag&unix.ECHO != 0
}

// run runs fn in the background and returns where its error arrives.
func run(fn func() error) <-chan error {
	done := make(chan error, 1)
	go func() { done <- fn() }()
	return done
}

func wait(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(30 * time.Second):
		t.Fatal("the command never finished")
		return nil
	}
}

func (f *fixture) prompt(m msg.Text) string { return voice.Plain(f.ctx, m) + " " }

func (f *fixture) quiet() {
	f.t.Helper()
	if f.sb.Stdout.Len() > 0 || f.sb.Stderr.Len() > 0 {
		f.t.Errorf("stdout %q, stderr %q: the terminal is the only place to talk about the passphrase", f.sb.Stdout, f.sb.Stderr)
	}
}

func TestKeysInitOnATerminalNeverEchoesThePassphrase(t *testing.T) {
	p := openPTY(t)
	f := emptyRepo(t)
	f.ctx.Env = f.ctx.Env.With(map[string]string{"HOMEBOT_TEST_SCRYPT_WORK_FACTOR": "10"})
	var public string
	done := run(func() (err error) { public, err = InitKeys(f.ctx); return err })
	p.expect(f.prompt(msg.KeysPromptPassphrase()))
	p.typ("hunter2 is my passphrase\n")
	p.expect(f.prompt(msg.KeysPromptConfirm()))
	p.typ("hunter2 is my passphrase\n")
	if err := wait(t, done); err != nil {
		t.Fatal(err)
	}
	if shown := p.drain(); strings.Contains(shown, "hunter2") {
		t.Errorf("the terminal shows the passphrase: %q", shown)
	}
	if !p.echoing() {
		t.Error("the terminal no longer echoes")
	}
	f.quiet()
	id, err := age.NewScryptIdentity("hunter2 is my passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if key := open(t, f.p("identity.age"), []age.Identity{id}); !strings.Contains(key, "# public key: "+public+"\n") {
		t.Errorf("identity.age holds %q", key)
	}
}

func TestKeysInitOnATerminalRefusesPassphrasesThatDiffer(t *testing.T) {
	p := openPTY(t)
	f := emptyRepo(t)
	f.ctx.Env = f.ctx.Env.With(map[string]string{"HOMEBOT_TEST_SCRYPT_WORK_FACTOR": "10"})
	done := run(func() error { _, err := InitKeys(f.ctx); return err })
	p.expect(f.prompt(msg.KeysPromptPassphrase()))
	p.typ("one\n")
	p.expect(f.prompt(msg.KeysPromptConfirm()))
	p.typ("two\n")
	wantUserError(t, f.ctx, wait(t, done), msg.KeysErrorMismatch())
	if f.exists("identity.age") || f.exists("recipients.txt") {
		t.Error("saved keys")
	}
	if !p.echoing() {
		t.Error("the terminal no longer echoes")
	}
}

func TestKeysInitOnATerminalMakesUpAPassphraseAndShowsItThereOnly(t *testing.T) {
	p := openPTY(t)
	f := emptyRepo(t)
	f.ctx.Env = f.ctx.Env.With(map[string]string{"HOMEBOT_TEST_SCRYPT_WORK_FACTOR": "10"})
	done := run(func() error { _, err := InitKeys(f.ctx); return err })
	p.expect(f.prompt(msg.KeysPromptPassphrase()))
	p.typ("\n")
	if err := wait(t, done); err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`using autogenerated passphrase "([a-z]+(?:-[a-z]+){9})"`).FindStringSubmatch(p.drain())
	if m == nil {
		t.Fatalf("the terminal shows %q, want the made-up passphrase", p.out.String())
	}
	f.quiet()
	id, err := age.NewScryptIdentity(m[1])
	if err != nil {
		t.Fatal(err)
	}
	open(t, f.p("identity.age"), []age.Identity{id})
}

func TestUnlockOnATerminal(t *testing.T) {
	p := openPTY(t)
	f := newFixture(t)
	f.write("common/a", "the plaintext")
	f.seal()
	f.remove("common/a")
	done := run(func() error { _, err := Unlock(f.ctx, UnlockOptions{}); return err })
	p.expect(f.prompt(msg.UnlockPromptPassphrase()))
	p.typ(testutil.TestPassphrase + "\n")
	if err := wait(t, done); err != nil {
		t.Fatal(err)
	}
	if shown := p.drain(); strings.Contains(shown, testutil.TestPassphrase) || strings.Contains(shown, "the plaintext") {
		t.Errorf("the terminal shows %q", shown)
	}
	f.quiet()
	if got := f.read("common/a"); got != "the plaintext" {
		t.Errorf("plaintext = %q", got)
	}
}

func TestUnlockOnATerminalWithAWrongPassphrase(t *testing.T) {
	p := openPTY(t)
	f := newFixture(t)
	done := run(func() error { _, err := Unlock(f.ctx, UnlockOptions{}); return err })
	p.expect(f.prompt(msg.UnlockPromptPassphrase()))
	p.typ("not it\n")
	wantUserError(t, f.ctx, wait(t, done), msg.SecretsErrorPassphrase())
	if !p.echoing() {
		t.Error("the terminal no longer echoes")
	}
}

func TestCtrlCAtThePromptPutsTheTerminalBack(t *testing.T) {
	p := openPTY(t)
	f := emptyRepo(t)
	cancelled, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := f.ctx.WithContext(cancelled)
	done := run(func() error { _, err := InitKeys(ctx); return err })
	p.expect(f.prompt(msg.KeysPromptPassphrase()))
	if p.echoing() {
		t.Error("the terminal echoes at the prompt")
	}
	p.typ("half typed")
	cancel()
	wantUserError(t, f.ctx, wait(t, done), msg.SecretsErrorCancelled())
	if !p.echoing() {
		t.Error("the terminal no longer echoes")
	}
	if f.exists("identity.age") {
		t.Error("saved a key")
	}
}

func TestCtrlDAtThePromptStops(t *testing.T) {
	p := openPTY(t)
	f := newFixture(t)
	done := run(func() error { _, err := Unlock(f.ctx, UnlockOptions{}); return err })
	p.expect(f.prompt(msg.UnlockPromptPassphrase()))
	p.typ("\x04")
	err := wait(t, done)
	wantUserError(t, f.ctx, err, msg.SecretsErrorCancelled())
	if !p.echoing() {
		t.Error("the terminal no longer echoes")
	}
	if errors.Is(err, age.ErrIncorrectIdentity) {
		t.Error("Ctrl-D reads as a wrong passphrase")
	}
}

// The passphrase prompt can sit for a long time: keys that appear meanwhile,
// from a second keys init or a pull, are never written over.
func TestKeysInitNeverWritesOverKeysThatAppearAtThePrompt(t *testing.T) {
	for _, name := range []string{"identity.age", "recipients.txt"} {
		t.Run(name, func(t *testing.T) {
			p := openPTY(t)
			f := emptyRepo(t)
			f.ctx.Env = f.ctx.Env.With(map[string]string{"HOMEBOT_TEST_SCRYPT_WORK_FACTOR": "10"})
			done := run(func() error { _, err := InitKeys(f.ctx); return err })
			p.expect(f.prompt(msg.KeysPromptPassphrase()))
			f.write(name, "someone else's\n")
			p.typ("a passphrase\n")
			p.expect(f.prompt(msg.KeysPromptConfirm()))
			p.typ("a passphrase\n")
			wantUserError(t, f.ctx, wait(t, done), msg.SecretsErrorKeysExist())
			if got := f.read(name); got != "someone else's\n" {
				t.Errorf("%s = %q: written over", name, got)
			}
			// Neither half of a new pair is left behind.
			entries, err := os.ReadDir(f.p(""))
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if e.Name() != name && (e.Name() == "identity.age" || e.Name() == "recipients.txt" || strings.Contains(e.Name(), ".tmp-")) {
					t.Errorf("left private/%s", e.Name())
				}
			}
		})
	}
}
