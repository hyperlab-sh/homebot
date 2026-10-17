// Package actions is what profile steps call. Three kinds:
//
//	internal  built into homebot (link, unlock, run)
//	builtin   shell scripts shipped with homebot, embedded in the binary
//	custom    shell scripts in manifests/actions/<name>.sh, which override builtins
//
// Scripts run with the working directory set to manifests/, the step's
// arguments as "$@", and HOMEBOT_* variables describing the repo and machine.
package actions

import (
	"embed"
	"errors"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

//go:embed scripts/*.sh
var scriptFS embed.FS

// Kind says where an action comes from.
type Kind string

// The kinds of action, in the words `homebot actions` shows.
const (
	Internal Kind = "internal"
	Builtin  Kind = "builtin"
	Custom   Kind = "custom"
)

// Info describes one action for `homebot actions`.
type Info struct {
	Name    string
	Kind    Kind
	Summary string
}

// InternalFunc is the body of an internal action.
type InternalFunc func(c *app.Context, args []string) error

type internalAction struct {
	summary msg.Text
	run     InternalFunc // nil until the owning package registers it
}

var (
	mu        sync.Mutex
	internals = map[string]internalAction{
		"run": {summary: msg.ActionRunSummary(), run: runCommands},
	}
)

// RegisterInternal fills the internal action name, replacing what was there.
// link and unlock are registered this way from wire.go; a package that wants
// to add an action does the same. summary is what `homebot actions` shows.
func RegisterInternal(name string, summary msg.Text, run InternalFunc) {
	mu.Lock()
	defer mu.Unlock()
	internals[name] = internalAction{summary: summary, run: run}
}

// StubInternal registers a stub for the internal action name until the test
// ends, then puts back whatever was registered before: a real action such as
// link, or nothing. It panics outside a test binary, so no production code
// can swap an action out from under a user's scripts. t is a *testing.T or
// *testing.B; the interface keeps "testing" out of this package's API.
func StubInternal(t interface {
	Helper()
	Cleanup(func())
}, name string, summary msg.Text, run InternalFunc) {
	t.Helper()
	if !testing.Testing() {
		panic("actions: StubInternal is for tests")
	}
	mu.Lock()
	prev, had := internals[name]
	internals[name] = internalAction{summary: summary, run: run}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		if had {
			internals[name] = prev
		} else {
			delete(internals, name)
		}
	})
}

// Wired reports whether name is an internal action with a body.
func Wired(name string) bool {
	a, ok := internal(name)
	return ok && a.run != nil
}

func internal(name string) (internalAction, bool) {
	mu.Lock()
	defer mu.Unlock()
	a, ok := internals[name]
	return a, ok
}

// builtinScripts maps each embedded script's action name to its text.
func builtinScripts() map[string]string {
	entries, err := scriptFS.ReadDir("scripts")
	if err != nil {
		panic(err) // the directory is embedded at build time
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		text, err := scriptFS.ReadFile(path.Join("scripts", e.Name()))
		if err != nil {
			panic(err)
		}
		out[strings.TrimSuffix(e.Name(), ".sh")] = string(text)
	}
	return out
}

// summaryOf is the first "# " comment after the shebang.
func summaryOf(script string) string {
	for line := range strings.SplitSeq(script, "\n") {
		if strings.HasPrefix(line, "#!") {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "# "); ok {
			return strings.TrimSpace(rest)
		}
		if strings.TrimSpace(line) != "" {
			break
		}
	}
	return ""
}

// Env is the HOMEBOT_* variables every script action gets.
func Env(c *app.Context) map[string]string {
	l := c.Layout()
	return map[string]string{
		"HOMEBOT_ROOT":      c.Root,
		"HOMEBOT_OS":        string(c.OS),
		"HOMEBOT_HOST":      c.Host,
		"HOMEBOT_ARCH":      c.Arch,
		"HOMEBOT_MANIFESTS": l.Manifests,
		"HOMEBOT_FURNITURE": l.Furniture,
		"HOMEBOT_AUTOLINK":  l.Autolink,
		"HOMEBOT_PRIVATE":   l.Private,
	}
}

// List returns every action, sorted by name: the internal ones, the built-in
// scripts, and the custom scripts in manifests/actions, which override a
// built-in of the same name but never an internal action.
func List(c *app.Context) ([]Info, error) {
	out := map[string]Info{}
	mu.Lock()
	for name, a := range internals {
		out[name] = Info{Name: name, Kind: Internal, Summary: voice.Plain(c, a.summary)}
	}
	mu.Unlock()
	for name, script := range builtinScripts() {
		out[name] = Info{Name: name, Kind: Builtin, Summary: summaryOf(script)}
	}
	dir := c.Layout().CustomActions
	files, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, f := range files {
		name, ok := strings.CutSuffix(f.Name(), ".sh")
		if !ok {
			continue
		}
		if info, err := os.Stat(filepath.Join(dir, f.Name())); err != nil || !info.Mode().IsRegular() {
			continue
		}
		if out[name].Kind == Internal {
			continue
		}
		text, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			return nil, err
		}
		out[name] = Info{Name: name, Kind: Custom, Summary: summaryOf(string(text))}
	}
	list := make([]Info, 0, len(out))
	for _, i := range out {
		list = append(list, i)
	}
	slices.SortFunc(list, func(a, b Info) int { return strings.Compare(a.Name, b.Name) })
	return list, nil
}

// Run runs the action name with args. An internal action's error is returned
// as it is; a script that exits non-zero gives fail.Silently(code), since the script has already spoken; an unknown name
// is a user error.
func Run(c *app.Context, name string, args []string) error {
	if a, ok := internal(name); ok {
		if a.run == nil {
			return fail.Say(msg.ActionErrorUnavailable(name))
		}
		return a.run(c, args)
	}
	code, found, err := RunScript(c, name, args)
	if err != nil {
		return err
	}
	if !found {
		return fail.Say(msg.CLIErrorUnknownAction(name))
	}
	if code != 0 {
		return fail.Silently(code)
	}
	return nil
}

// RunScript runs the script action name (custom first, then built-in) with
// the terminal attached, and returns its exit code. found is false when no
// script has that name; unpack uses that to say which step has no action.
func RunScript(c *app.Context, name string, args []string) (code int, found bool, err error) {
	custom := filepath.Join(c.Layout().CustomActions, name+".sh")
	var argv []string
	if info, statErr := os.Stat(custom); statErr == nil && info.Mode().IsRegular() {
		argv = append([]string{"bash", custom}, args...)
	} else if text, ok := builtinScripts()[name]; ok {
		argv = append([]string{"bash", "-c", text, "homebot-" + name}, args...)
	} else {
		return 0, false, nil
	}
	code, err = runAttached(c, argv)
	return code, true, err
}

// runAttached runs argv in manifests/ with the HOMEBOT_* variables, wired to
// the terminal itself (ui.Attached), so the program can prompt and sees a
// TTY where there is one.
func runAttached(c *app.Context, argv []string) (int, error) {
	// $PWD is the folder as HOMEBOT_MANIFESTS names it, links and all.
	in, out, errOut := ui.Attached(c)
	code, err := proc.RunAttached(c, argv, proc.AttachedOptions{
		Dir:   c.Layout().Manifests,
		Env:   c.Env,
		Vars:  Env(c),
		Stdin: in, Stdout: out, Stderr: errOut,
	})
	if err != nil {
		return proc.NotFound, fail.User(err.Error())
	}
	return code, nil
}

// runCommands is the internal run action: each argument is a shell command.
func runCommands(c *app.Context, args []string) error {
	for _, command := range args {
		code, err := runAttached(c, []string{"bash", "-c", command})
		if err != nil {
			return err
		}
		if code != 0 {
			return fail.Say(msg.ActionRunErrorExit(command, code))
		}
	}
	return nil
}
