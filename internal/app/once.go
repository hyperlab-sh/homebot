package app

import "sync"

// memo holds values computed once per Context, such as the terminal's colour
// profile. Copies of the Context share it.
type memo struct {
	m sync.Map // key -> *memoEntry
}

type memoEntry struct {
	once sync.Once
	v    any
	// panicked is what f panicked with, if it did.
	panicked any
}

// Once returns f's result for key, calling f only the first time c is asked
// for key. Concurrent callers wait for that first call. If f panics, that
// call and every later one panic with the same value.
//
// Every copy of c shares its values: one made by WithContext, and one made
// by assigning *c and then changing Stdout, Stderr or Env. Such a copy still
// sees what was computed from the original's fields; build a new Context
// with New when the streams or the environment change. A Fork does not
// share them: it starts with none. A Context not built by New has no memo:
// f runs on every call.
//
// Use an unexported key type, as with context values; asking for one key
// with two types panics.
func Once[T any](c *Context, key any, f func() T) T {
	return load(c.memo, key, f)
}

// OncePerProcess is Once over values that every Context derived from c
// shares, Forks included: what New or ForReport built, and every copy and
// Fork made from it since. cmd/homebot builds one Context per process, so
// f runs once per process. persona hands out the unusable-persona warning
// once this way (plan S4). A Context not built by New or ForReport has no
// such store: f runs on every call.
func OncePerProcess[T any](c *Context, key any, f func() T) T {
	return load(c.process, key, f)
}

func load[T any](m *memo, key any, f func() T) T {
	if m == nil {
		return f()
	}
	e, _ := m.m.LoadOrStore(key, &memoEntry{})
	entry := e.(*memoEntry)
	entry.once.Do(func() {
		defer func() {
			if r := recover(); r != nil {
				entry.panicked = r
				panic(r)
			}
		}()
		entry.v = f()
	})
	if entry.panicked != nil {
		panic(entry.panicked)
	}
	return entry.v.(T)
}
