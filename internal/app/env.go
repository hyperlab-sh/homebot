package app

import (
	"maps"
	"slices"
	"strings"
)

// Env is a fixed set of environment variables. A Context carries one so that
// no code reads the process environment; it never changes after it is built.
type Env struct {
	vars map[string]string
}

// NewEnv returns an Env holding a copy of vars.
func NewEnv(vars map[string]string) Env {
	return Env{vars: maps.Clone(vars)}
}

// ParseEnviron returns an Env from KEY=VALUE pairs, as os.Environ gives them.
// A later pair wins over an earlier one with the same key; entries without
// "=" are ignored.
func ParseEnviron(environ []string) Env {
	vars := make(map[string]string, len(environ))
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" {
			vars[k] = v
		}
	}
	return Env{vars: vars}
}

// Lookup returns the value of name and whether it is set.
func (e Env) Lookup(name string) (string, bool) {
	v, ok := e.vars[name]
	return v, ok
}

// Get returns the value of name, or "" when it is unset.
func (e Env) Get(name string) string {
	return e.vars[name]
}

// With returns a copy of e with vars set over it.
func (e Env) With(vars map[string]string) Env {
	out := maps.Clone(e.vars)
	if out == nil {
		out = make(map[string]string, len(vars))
	}
	maps.Copy(out, vars)
	return Env{vars: out}
}

// Without returns a copy of e with keys unset. Keys that are not set are
// ignored.
func (e Env) Without(keys ...string) Env {
	out := maps.Clone(e.vars)
	for _, k := range keys {
		delete(out, k)
	}
	return Env{vars: out}
}

// Environ returns the variables as sorted KEY=VALUE pairs, the form os/exec
// takes. It never returns nil, so a child process started with it never
// falls back to the parent's environment.
func (e Env) Environ() []string {
	out := make([]string, 0, len(e.vars))
	for _, k := range slices.Sorted(maps.Keys(e.vars)) {
		out = append(out, k+"="+e.vars[k])
	}
	return out
}
