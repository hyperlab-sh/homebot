package app

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestStdoutIsTerminalAsksStdout(t *testing.T) {
	o, work := tempOptions(t)
	file, err := os.Create(filepath.Join(work, "out"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	for name, out := range map[string]io.Writer{
		"a buffer":       &bytes.Buffer{},
		"a regular file": file,
		"a pipe":         w,
	} {
		o.Stdout = out
		if mustNew(t, o).StdoutIsTerminal() {
			t.Errorf("stdout to %s counts as a terminal", name)
		}
	}
}

func TestStdoutTerminalOverridesStdout(t *testing.T) {
	o, _ := tempOptions(t)
	c := mustNew(t, o)
	if c.StdoutTerminal != nil {
		t.Fatalf("StdoutTerminal = %v without HOMEBOT_TEST_TTY, want nil", *c.StdoutTerminal)
	}
	yes, no := true, false
	c.StdoutTerminal = &yes
	if !c.StdoutIsTerminal() {
		t.Error("StdoutTerminal true: StdoutIsTerminal is false")
	}
	c.StdoutTerminal = &no
	if c.StdoutIsTerminal() {
		t.Error("StdoutTerminal false: StdoutIsTerminal is true")
	}
}

func TestHomebotTestTTYSetsTheSeam(t *testing.T) {
	for v, want := range map[string]bool{"1": true, "0": false} {
		o, _ := tempOptions(t)
		o.Env = o.Env.With(map[string]string{"HOMEBOT_TEST_TTY": v})
		c := mustNew(t, o)
		if c.StdoutTerminal == nil || *c.StdoutTerminal != want {
			t.Errorf("HOMEBOT_TEST_TTY=%s: StdoutTerminal = %v, want %v", v, c.StdoutTerminal, want)
		}
		if c.StdoutIsTerminal() != want {
			t.Errorf("HOMEBOT_TEST_TTY=%s: StdoutIsTerminal = %v, want %v", v, !want, want)
		}
	}
}

func TestHomebotTestTTYRefusesOtherValues(t *testing.T) {
	for _, v := range []string{"", "yes", "true", "2"} {
		o, _ := tempOptions(t)
		o.Env = o.Env.With(map[string]string{"HOMEBOT_TEST_TTY": v})
		if _, err := New(o); err == nil || !strings.Contains(err.Error(), "HOMEBOT_TEST_TTY") {
			t.Errorf("HOMEBOT_TEST_TTY=%q: err = %v, want one naming the variable", v, err)
		}
	}
}

func TestHomebotTestTTYDoesNothingInARelease(t *testing.T) {
	releaseBuild(t)
	o, _ := tempOptions(t)
	o.Env = o.Env.With(map[string]string{"HOMEBOT_TEST_TTY": "1"})
	c := mustNew(t, o)
	if c.StdoutTerminal != nil || c.StdoutIsTerminal() {
		t.Error("HOMEBOT_TEST_TTY=1 made stdout a terminal in a release build")
	}
	o.Env = o.Env.With(map[string]string{"HOMEBOT_TEST_TTY": "junk"})
	if _, err := New(o); err != nil {
		t.Errorf("a release build read HOMEBOT_TEST_TTY: %v", err)
	}
}

type forkKey struct{}

func TestForkLaysEnvOverTheCopyOnly(t *testing.T) {
	o, _ := tempOptions(t)
	o.Env = o.Env.With(map[string]string{"KEEP": "k", "HOMEBOT_PERSONA": "none"})
	c := mustNew(t, o)
	f := c.Fork(map[string]string{"HOMEBOT_PERSONA": "tester", "HOMEBOT_THEME": "light"})
	if f == c {
		t.Fatal("Fork returned c itself")
	}
	for name, want := range map[string]string{"KEEP": "k", "HOMEBOT_PERSONA": "tester", "HOMEBOT_THEME": "light"} {
		if got := f.Env.Get(name); got != want {
			t.Errorf("fork: %s = %q, want %q", name, got, want)
		}
	}
	if got := c.Env.Get("HOMEBOT_PERSONA"); got != "none" {
		t.Errorf("the original's HOMEBOT_PERSONA became %q", got)
	}
	if _, ok := c.Env.Lookup("HOMEBOT_THEME"); ok {
		t.Error("the fork's HOMEBOT_THEME leaked into the original")
	}
	f.ForceVoice = true
	if c.ForceVoice {
		t.Error("setting ForceVoice on the fork set it on the original")
	}
	if f.Stdout != c.Stdout || f.Rand != c.Rand || f.Config != c.Config || f.Plain != c.Plain {
		t.Error("the fork does not keep the original's streams, Rand, Config and Plain")
	}
}

func TestForkStartsWithAFreshMemo(t *testing.T) {
	o, _ := tempOptions(t)
	c := mustNew(t, o)
	if got := Once(c, forkKey{}, func() string { return "original" }); got != "original" {
		t.Fatalf("Once = %q", got)
	}
	f := c.Fork(nil)
	if got := Once(f, forkKey{}, func() string { return "fork" }); got != "fork" {
		t.Errorf("the fork's Once = %q, want it worked out again", got)
	}
	if got := Once(c, forkKey{}, func() string { return "again" }); got != "original" {
		t.Errorf("the fork's Once changed the original's: %q", got)
	}
	// A WithContext copy still shares the memo, unlike a fork.
	w := c.WithContext(context.Background())
	if got := Once(w, forkKey{}, func() string { return "copy" }); got != "original" {
		t.Errorf("WithContext's Once = %q, want the original's", got)
	}
}

type processKey struct{}

func TestOncePerProcessIsSharedByForksAndCopies(t *testing.T) {
	o, _ := tempOptions(t)
	c := mustNew(t, o)
	var calls atomic.Int32
	f := func() *atomic.Bool { calls.Add(1); return new(atomic.Bool) }
	flag := OncePerProcess(c, processKey{}, f)
	for name, d := range map[string]*Context{
		"a fork":             c.Fork(map[string]string{"HOMEBOT_PERSONA": "tester"}),
		"a fork of a fork":   c.Fork(nil).Fork(nil),
		"a WithContext copy": c.WithContext(context.Background()),
	} {
		if got := OncePerProcess(d, processKey{}, f); got != flag {
			t.Errorf("%s got its own value", name)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("f ran %d times, want 1", n)
	}
	// It is a store of its own: Once with the same key does not see it.
	if got := Once(c, processKey{}, func() *atomic.Bool { return nil }); got != nil {
		t.Error("Once sees OncePerProcess's value")
	}
}

func TestOncePerProcessIsNotSharedBetweenNews(t *testing.T) {
	o, _ := tempOptions(t)
	a, b := mustNew(t, o), mustNew(t, o)
	if OncePerProcess(a, processKey{}, func() *int { return new(int) }) ==
		OncePerProcess(b, processKey{}, func() *int { return new(int) }) {
		t.Error("two Contexts from New share a value")
	}
}

func TestForReportHasAProcessStore(t *testing.T) {
	c := ForReport(Options{Env: NewEnv(nil)})
	var calls int
	for range 2 {
		OncePerProcess(c.Fork(nil), processKey{}, func() *int { calls++; return new(int) })
	}
	if calls != 1 {
		t.Errorf("f ran %d times on ForReport's forks, want 1", calls)
	}
}

// A startup error is always said in the neutral voice: ForReport is Plain
// whatever HOMEBOT_PLAIN says, so it never loads a persona.
func TestForReportIsAlwaysPlain(t *testing.T) {
	for _, env := range []map[string]string{
		nil,
		{"HOMEBOT_PLAIN": "0"},
		{"HOMEBOT_PLAIN": ""},
		{"HOMEBOT_PLAIN": "1"},
		{"HOMEBOT_PERSONA": "tester"},
	} {
		c := ForReport(Options{Env: NewEnv(env)})
		if !c.Plain {
			t.Errorf("ForReport with %v is not Plain", env)
		}
		if c.ConfigDir != "" || c.Config != nil || c.Root != "" {
			t.Errorf("ForReport with %v has a place to read a persona from: ConfigDir %q, Root %q, Config %v", env, c.ConfigDir, c.Root, c.Config)
		}
		if f := c.Fork(map[string]string{"HOMEBOT_PERSONA": "tester"}); !f.Plain {
			t.Errorf("a Fork of ForReport with %v is not Plain", env)
		}
	}
}
