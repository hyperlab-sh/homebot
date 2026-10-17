package app

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

func newTestContext(t *testing.T) *Context {
	t.Helper()
	home := t.TempDir()
	c, err := New(Options{Env: NewEnv(map[string]string{"HOME": home, "HOMEBOT_TEST_NOW": "1767225600000"}), OS: Linux})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestOnceComputesOncePerKey(t *testing.T) {
	c := newTestContext(t)
	calls := 0
	f := func() int { calls++; return calls }
	if a, b := Once(c, "k", f), Once(c, "k", f); a != 1 || b != 1 {
		t.Errorf("same key: %d then %d, want 1 and 1", a, b)
	}
	if got := Once(c, "other", f); got != 2 {
		t.Errorf("another key: %d, want 2", got)
	}
	type key struct{ stream string }
	if got := Once(c, key{"k"}, f); got != 3 {
		t.Errorf("a key of another type: %d, want 3", got)
	}
}

func TestOnceIsSharedWithCopies(t *testing.T) {
	c := newTestContext(t)
	Once(c, "k", func() string { return "first" })
	cp := c.WithContext(context.Background())
	if got := Once(cp, "k", func() string { return "second" }); got != "first" {
		t.Errorf("a WithContext copy computed again: %q", got)
	}
}

func TestOnceIsPerContext(t *testing.T) {
	a, b := newTestContext(t), newTestContext(t)
	Once(a, "k", func() string { return "a" })
	if got := Once(b, "k", func() string { return "b" }); got != "b" {
		t.Errorf("Contexts share values: %q", got)
	}
}

func TestOnceWithoutMemoAlwaysComputes(t *testing.T) {
	c := &Context{}
	calls := 0
	for range 3 {
		Once(c, "k", func() int { calls++; return calls })
	}
	if calls != 3 {
		t.Errorf("f ran %d times, want 3", calls)
	}
}

func TestOnceUnderConcurrency(t *testing.T) {
	c := newTestContext(t)
	var calls atomic.Int32
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := Once(c, "k", func() int { calls.Add(1); return 42 }); got != 42 {
				t.Errorf("got %d", got)
			}
		}()
	}
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Errorf("f ran %d times, want 1", n)
	}
}

func TestOnceRepeatsAPanicToLaterCallers(t *testing.T) {
	c := newTestContext(t)
	boom := func() int { panic("no terminal") }
	for i := range 3 {
		func() {
			defer func() {
				if r := recover(); r != "no terminal" {
					t.Errorf("call %d panicked with %v, want the first call's value", i, r)
				}
			}()
			Once(c, "k", boom)
		}()
	}
}
