package proc_test

import (
	"bytes"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func TestRunAttachedWiresTheStreamsAndReturnsTheExitCode(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	var out, errOut bytes.Buffer
	code, err := proc.RunAttached(b.Ctx, []string{"sh", "-c", "cat; echo out; echo err >&2; exit 3"}, proc.AttachedOptions{
		Env:    b.Ctx.Env,
		Stdin:  strings.NewReader("in\n"),
		Stdout: &out, Stderr: &errOut,
	})
	if err != nil || code != 3 {
		t.Fatalf("RunAttached = %d, %v, want 3 and no error", code, err)
	}
	if out.String() != "in\nout\n" || errOut.String() != "err\n" {
		t.Errorf("stdout %q, stderr %q", out.String(), errOut.String())
	}
	if code, err := proc.RunAttached(b.Ctx, []string{"true"}, proc.AttachedOptions{Env: b.Ctx.Env}); code != 0 || err != nil {
		t.Errorf("true: %d, %v", code, err)
	}
}

func TestRunAttachedKilledBySignalIsCode1(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	code, err := proc.RunAttached(b.Ctx, []string{"sh", "-c", "kill -9 $$"}, proc.AttachedOptions{Env: b.Ctx.Env})
	if code != 1 || err != nil {
		t.Errorf("RunAttached = %d, %v, want 1 and no error", code, err)
	}
}

func TestRunAttachedCannotStart(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	code, err := proc.RunAttached(b.Ctx, []string{"no-such-tool"}, proc.AttachedOptions{Env: b.Ctx.Env})
	if code != proc.NotFound || err == nil || !strings.Contains(err.Error(), `"no-such-tool"`) {
		t.Errorf("not on PATH: %d, %v", code, err)
	}
	code, err = proc.RunAttached(b.Ctx, []string{"true"}, proc.AttachedOptions{Env: b.Ctx.Env, Dir: filepath.Join(b.Dir, "gone")})
	if code != proc.NotFound || err == nil {
		t.Errorf("missing dir: %d, %v", code, err)
	}
	if code, err = proc.RunAttached(b.Ctx, nil, proc.AttachedOptions{Env: b.Ctx.Env}); code != proc.NotFound || err == nil {
		t.Errorf("no argv: %d, %v", code, err)
	}
}

func TestRunAttachedEnvironment(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{BehindLink: true})
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PWD": b.Dir, "KEPT": "context"})
	printenv := func(opts proc.AttachedOptions, name string) string {
		t.Helper()
		var out bytes.Buffer
		opts.Stdout = &out
		if code, err := proc.RunAttached(b.Ctx, []string{"printenv", name}, opts); err != nil || code != 0 {
			t.Fatalf("printenv %s: %d, %v", name, code, err)
		}
		return strings.TrimSpace(out.String())
	}
	// PWD names Dir as given, links and all.
	if got := printenv(proc.AttachedOptions{Env: b.Ctx.Env, Dir: b.Root}, "PWD"); got != b.Root {
		t.Errorf("PWD = %q, want %q", got, b.Root)
	}
	// An empty Env is an empty environment, not the Context's.
	var out bytes.Buffer
	if _, err := proc.RunAttached(b.Ctx, []string{"printenv", "KEPT"}, proc.AttachedOptions{Stdout: &out}); err != nil || out.Len() != 0 {
		t.Errorf("an empty Env: KEPT = %q, %v", out.String(), err)
	}
	// A given Env replaces it, and Vars win over both it and PWD.
	given := b.Ctx.Env.With(map[string]string{"KEPT": "given"})
	if got := printenv(proc.AttachedOptions{Env: given}, "KEPT"); got != "given" {
		t.Errorf("KEPT with Env = %q", got)
	}
	if got := printenv(proc.AttachedOptions{Env: given, Dir: b.Root, Vars: map[string]string{"KEPT": "var", "PWD": "/given"}}, "KEPT"); got != "var" {
		t.Errorf("KEPT with Vars = %q", got)
	}
	if got := printenv(proc.AttachedOptions{Env: b.Ctx.Env, Dir: b.Root, Vars: map[string]string{"PWD": "/given"}}, "PWD"); got != "/given" {
		t.Errorf("Vars PWD = %q", got)
	}
	// Without Dir the program runs where homebot does and PWD stays.
	if got := printenv(proc.AttachedOptions{Env: b.Ctx.Env}, "PWD"); got != b.Dir {
		t.Errorf("without Dir: PWD = %q, want %q", got, b.Dir)
	}
	if b.Ctx.Env.Get("PWD") != b.Dir {
		t.Error("RunAttached changed the Context's Env")
	}
}

// With a Grace, a stop is SIGTERM first, so the program can clean up; without
// one the program is killed at once.
func TestRunAttachedStopsWhenTheContextIsCancelled(t *testing.T) {
	script := `trap 'echo got TERM; exit 3' TERM; echo $$ > "$1"; while :; do sleep 0.05; done`
	for _, tc := range []struct {
		name   string
		grace  time.Duration
		code   int
		stdout string
	}{
		{"with a grace", 500 * time.Millisecond, 3, "got TERM\n"}, // its own exit code
		{"without", 0, 1, ""}, // killed by a signal
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := testutil.Sandbox(t, testutil.Options{})
			pidFile := filepath.Join(b.Dir, "child.pid")
			c := cancelOnFile(t, b.Ctx, pidFile)
			var out bytes.Buffer
			type result struct {
				code int
				err  error
			}
			done := make(chan result, 1)
			go func() {
				code, err := proc.RunAttached(c, []string{"sh", "-c", script, "sh", pidFile}, proc.AttachedOptions{Env: c.Env, Stdout: &out, Grace: tc.grace})
				done <- result{code, err}
			}()
			select {
			case r := <-done:
				if r.code != tc.code || r.err != nil {
					t.Errorf("RunAttached = %d, %v, want %d and no error", r.code, r.err, tc.code)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("RunAttached did not stop when the Context was cancelled")
			}
			if out.String() != tc.stdout {
				t.Errorf("stdout = %q, want %q", out.String(), tc.stdout)
			}
		})
	}
}

// A process the program leaves behind holding a stream that is not a file
// (here its stderr, as an ssh ProxyCommand might) does not hang RunAttached
// once the program has exited: its exit code stands, and what it wrote
// before it exited is kept.
func TestRunAttachedDoesNotWaitForAGrandchildHoldingThePipes(t *testing.T) {
	for _, code := range []int{0, 3} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			b := testutil.Sandbox(t, testutil.Options{})
			pidFile := filepath.Join(b.Dir, "grandchild.pid")
			killAtCleanup(t, pidFile)
			script := `sleep 30 & echo $! > "$1"; echo said >&2; exit ` + strconv.Itoa(code)
			var errOut bytes.Buffer
			type result struct {
				code int
				err  error
			}
			done := make(chan result, 1)
			go func() {
				code, err := proc.RunAttached(b.Ctx, []string{"sh", "-c", script, "sh", pidFile}, proc.AttachedOptions{Env: b.Ctx.Env, Stderr: &errOut})
				done <- result{code, err}
			}()
			select {
			case r := <-done:
				if r.code != code || r.err != nil {
					t.Errorf("RunAttached = %d, %v, want %d and no error", r.code, r.err, code)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("RunAttached waited for the process left holding stderr")
			}
			if errOut.String() != "said\n" {
				t.Errorf("stderr = %q, want %q", errOut.String(), "said\n")
			}
		})
	}
}
