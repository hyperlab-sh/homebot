package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/diff"
	"github.com/rogpeppe/go-internal/testscript"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/guard"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// scriptParams runs files in the sandbox environment, with cmpwork,
// execcode and installguard.
func scriptParams(t *testing.T, files []string) testscript.Params {
	tools := hostTools(t, "age", "age-keygen", "age-plugin-batchpass")
	return testscript.Params{
		Files: files,
		Setup: func(env *testscript.Env) error { return setupSandbox(env, tools) },
		Cmds: map[string]func(*testscript.TestScript, bool, []string){
			"cmpwork":      cmpwork,
			"execcode":     execcode,
			"installguard": installguard,
		},
	}
}

// setupSandbox gives a script the sandbox environment: SandboxEnv with
// testscript's bin dir (which holds homebot) and tools on the PATH, plus
// $WORK/bin, $WORK/home and $WORK/gitconfig. A gitconfig in the script's
// archive wins.
func setupSandbox(env *testscript.Env, tools string) error {
	work := env.WorkDir
	tsBin, _, _ := strings.Cut(env.Getenv("PATH"), string(filepath.ListSeparator))
	if _, err := os.Stat(filepath.Join(tsBin, "homebot")); err != nil {
		return fmt.Errorf("expected testscript's bin dir first on PATH: %w", err)
	}
	sandbox := testutil.SandboxEnv(work, tsBin, tools)
	vars := make([]string, 0, len(env.Vars)+len(sandbox))
	for _, kv := range env.Vars {
		name, _, _ := strings.Cut(kv, "=")
		if _, replaced := sandbox[name]; replaced || name == "ZDOTDIR" || strings.HasPrefix(name, "HOMEBOT_") {
			continue
		}
		vars = append(vars, kv)
	}
	env.Vars = append(vars, app.NewEnv(sandbox).Environ()...)
	// The program log TestMain checks (naming plan D3).
	if log := os.Getenv(proc.ProgramLogVar); log != "" {
		env.Vars = append(env.Vars, proc.ProgramLogVar+"="+log)
	}

	for _, dir := range []string{"bin", "home"} {
		if err := os.MkdirAll(filepath.Join(work, dir), 0o755); err != nil {
			return err
		}
	}
	gitconfig := filepath.Join(work, "gitconfig")
	if _, err := os.Stat(gitconfig); errors.Is(err, fs.ErrNotExist) {
		return os.WriteFile(gitconfig, []byte(testutil.GitConfig), 0o644)
	}
	return nil
}

// cmpwork compares a file, stdout or stderr with a golden file after
// replacing every occurrence of the script's $WORK path in the actual text
// with the literal $WORK. The golden file is read as it is, so it can hold
// text like $EDITOR literally.
func cmpwork(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("unsupported: ! cmpwork")
	}
	if len(args) != 2 {
		ts.Fatalf("usage: cmpwork <file|stdout|stderr> <golden>")
	}
	work := ts.Getenv("WORK")
	if work == "" {
		ts.Fatalf("cmpwork: $WORK is not set")
	}
	got := strings.ReplaceAll(ts.ReadFile(args[0]), work, "$WORK")
	want, err := os.ReadFile(ts.MkAbs(args[1]))
	ts.Check(err)
	if got == string(want) {
		return
	}
	ts.Logf("[diff -%s +%s]\n%s", args[0], args[1], diff.Diff(args[0], []byte(got), args[1], want))
	ts.Fatalf("%s and %s differ", args[0], args[1])
}

// installguard installs the commit guard in a home repo of the sandbox
// ($WORK/home/.home, or the directory given), as homebot setup does, by
// calling guard.InstallHooks in-process with the script's environment.
func installguard(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("unsupported: ! installguard")
	}
	if len(args) > 1 {
		ts.Fatalf("usage: installguard [repo]")
	}
	root := ts.MkAbs(filepath.Join("home", ".home"))
	if len(args) == 1 {
		root = ts.MkAbs(args[0])
	}
	env := map[string]string{}
	for name := range testutil.SandboxEnv(ts.Getenv("WORK")) {
		if v := ts.Getenv(name); v != "" {
			env[name] = v
		}
	}
	c, err := app.New(app.Options{Env: app.NewEnv(env), Cwd: root, Root: root})
	ts.Check(err)
	changed, err := guard.InstallHooks(c)
	ts.Check(err)
	ts.Logf("installguard: changed=%v", changed)
}
