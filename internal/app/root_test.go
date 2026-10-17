package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
)

// rootFixture is a temp tree with four home repos, one for each place root
// location looks, plus a bare directory and a binary inside the exe repo.
type rootFixture struct {
	envRoot, cwdRoot, exeRoot, home, homeRoot string
	cwd, exe, bare                            string
}

func newRootFixture(t *testing.T) rootFixture {
	t.Helper()
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := rootFixture{
		envRoot:  filepath.Join(tmp, "env-repo"),
		cwdRoot:  filepath.Join(tmp, "cwd-repo"),
		exeRoot:  filepath.Join(tmp, "exe-repo"),
		home:     filepath.Join(tmp, "home"),
		homeRoot: filepath.Join(tmp, "home", ".home"),
		bare:     filepath.Join(tmp, "bare"),
	}
	f.cwd = filepath.Join(f.cwdRoot, "a", "b")
	f.exe = filepath.Join(f.exeRoot, "bin", "homebot")
	for _, dir := range []string{f.envRoot, f.cwdRoot, f.exeRoot, f.homeRoot} {
		writeFile(t, filepath.Join(dir, ConfigFile), "")
	}
	for _, dir := range []string{f.cwd, f.bare} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, f.exe, "#!/bin/sh\n")
	return f
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLocateRootOrder(t *testing.T) {
	f := newRootFixture(t)
	missing := filepath.Join(f.bare, "missing")
	cases := []struct {
		name     string
		envRoot  string
		cwd, exe string
		home     string
		want     string
	}{
		{"HOMEBOT_ROOT first", f.envRoot, f.cwd, f.exe, f.home, f.envRoot},
		{"cwd ancestors next", "", f.cwd, f.exe, f.home, f.cwdRoot},
		{"cwd itself", "", f.cwdRoot, f.exe, f.home, f.cwdRoot},
		{"binary's dir ancestors next", "", f.bare, f.exe, f.home, f.exeRoot},
		{"~/.home last", "", f.bare, filepath.Join(f.bare, "homebot"), f.home, f.homeRoot},
		{"HOMEBOT_ROOT without homebot.yml falls through", f.bare, f.cwd, f.exe, f.home, f.cwdRoot},
		{"missing HOMEBOT_ROOT falls through", missing, f.bare, f.exe, f.home, f.exeRoot},
		{"missing cwd is skipped", "", missing, f.exe, f.home, f.exeRoot},
		{"no cwd or exe", "", "", "", f.home, f.homeRoot},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := NewEnv(map[string]string{"HOMEBOT_ROOT": tc.envRoot})
			got, err := locateRoot(env, tc.cwd, tc.exe, tc.home)
			if err != nil || got != tc.want {
				t.Errorf("locateRoot = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestLocateRootNotFound(t *testing.T) {
	f := newRootFixture(t)
	_, err := locateRoot(NewEnv(nil), f.bare, filepath.Join(f.bare, "homebot"), f.bare)
	want := "No homebot.yml found. Run homebot inside your home repo, or set HOMEBOT_ROOT."
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}

// The CLI says these in the Context's voice, so they carry their message.
func TestStartupErrorsCarryTheirCatalogMessage(t *testing.T) {
	o, _ := tempOptions(t)
	o.OS = "plan9"
	_, platform := New(o)
	o.OS, o.Env = "", NewEnv(nil)
	_, home := New(o)
	for want, err := range map[string]error{
		"context.error.no_config": noRoot(),
		"context.error.platform":  platform,
		"context.error.no_home":   home,
	} {
		var ue *fail.UserError
		if !errors.As(err, &ue) {
			t.Errorf("%s: %v is not a fail.UserError", want, err)
			continue
		}
		if text, ok := ue.Text(); !ok || text.ID() != want {
			t.Errorf("%s: Text() = %q, %v", want, text.ID(), ok)
		}
	}
}

func TestRelativeHomebotRootResolvesAgainstCwd(t *testing.T) {
	f := newRootFixture(t)
	env := NewEnv(map[string]string{"HOMEBOT_ROOT": "../env-repo/."})
	got, err := locateRoot(env, f.bare, "", f.home)
	if err != nil || got != f.envRoot {
		t.Errorf("locateRoot = %q, %v; want %q", got, err, f.envRoot)
	}
}

func TestOldConfigFileMarksARoot(t *testing.T) {
	f := newRootFixture(t)
	old := filepath.Join(f.bare, "old")
	writeFile(t, filepath.Join(old, "homebot.yaml"), "")
	got, err := locateRoot(NewEnv(nil), filepath.Join(old), "", f.home)
	if err != nil || got != old {
		t.Errorf("locateRoot = %q, %v; want %q", got, err, old)
	}
}

func TestSymlinksResolveToThePhysicalRepo(t *testing.T) {
	f := newRootFixture(t)

	linkedCwd := filepath.Join(f.bare, "cwd-link")
	if err := os.Symlink(f.cwd, linkedCwd); err != nil {
		t.Fatal(err)
	}
	got, err := locateRoot(NewEnv(nil), linkedCwd, "", f.home)
	if err != nil || got != f.cwdRoot {
		t.Errorf("symlinked cwd: locateRoot = %q, %v; want the physical %q", got, err, f.cwdRoot)
	}

	linkedExe := filepath.Join(f.bare, "bin", "homebot")
	if err := os.MkdirAll(filepath.Dir(linkedExe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.exe, linkedExe); err != nil {
		t.Fatal(err)
	}
	got, err = locateRoot(NewEnv(nil), f.bare, linkedExe, f.home)
	if err != nil || got != f.exeRoot {
		t.Errorf("symlinked binary: locateRoot = %q, %v; want the target's repo %q", got, err, f.exeRoot)
	}
}

func TestLocateRootMethod(t *testing.T) {
	f := newRootFixture(t)
	c := &Context{Env: NewEnv(nil), Cwd: f.cwd, Home: f.home}
	if err := c.LocateRoot(); err != nil || c.Root != f.cwdRoot {
		t.Errorf("LocateRoot: Root = %q, err = %v; want %q", c.Root, err, f.cwdRoot)
	}

	c = &Context{Root: f.bare, Env: NewEnv(nil), Cwd: f.cwd, Home: f.home}
	if err := c.LocateRoot(); err != nil || c.Root != f.bare {
		t.Errorf("LocateRoot with Root set: Root = %q, err = %v; want it left alone", c.Root, err)
	}

	c = &Context{Env: NewEnv(nil), Cwd: f.bare, Home: f.bare}
	if err := c.LocateRoot(); !saysNoRoot(err) || c.Root != "" {
		t.Errorf("LocateRoot with no repo: Root = %q, err = %v", c.Root, err)
	}
}

func TestFindRoot(t *testing.T) {
	f := newRootFixture(t)
	for _, tc := range []struct {
		start, want string
		ok          bool
	}{
		{f.cwdRoot, f.cwdRoot, true},
		{f.cwd, f.cwdRoot, true},
		{f.cwd + "/../b/./", f.cwdRoot, true},
		{filepath.Join(f.cwd, "not", "made"), f.cwdRoot, true},
		{f.bare, "", false},
	} {
		got, ok := FindRoot(tc.start)
		if got != tc.want || ok != tc.ok {
			t.Errorf("FindRoot(%q) = %q, %v; want %q, %v", tc.start, got, ok, tc.want, tc.ok)
		}
	}
}

func TestRelativeHomebotRootResolvesAgainstThePhysicalCwd(t *testing.T) {
	f := newRootFixture(t)
	// $tmp/links/cwd -> $tmp/cwd-repo/a/b; "../../../env-repo" is env-repo
	// from the physical path and a missing dir from the link.
	link := filepath.Join(filepath.Dir(f.bare), "links", "cwd")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.cwd, link); err != nil {
		t.Fatal(err)
	}
	env := NewEnv(map[string]string{"HOMEBOT_ROOT": "../../../env-repo"})
	got, err := locateRoot(env, link, "", f.home)
	if err != nil || got != f.envRoot {
		t.Errorf("locateRoot = %q, %v; want %q", got, err, f.envRoot)
	}
}

func TestFindRootFromARelativeStart(t *testing.T) {
	f := newRootFixture(t)
	t.Chdir(f.cwd)
	for _, start := range []string{".", "", "b/..", "../b", "not/made"} {
		if got, ok := FindRoot(start); !ok || got != f.cwdRoot {
			t.Errorf("FindRoot(%q) = %q, %v; want %q", start, got, ok, f.cwdRoot)
		}
	}
	t.Chdir(f.bare)
	if got, ok := FindRoot("."); ok {
		t.Errorf("FindRoot(.) in a bare dir = %q", got)
	}
}

func TestLocateRootLoadsConfig(t *testing.T) {
	f := newRootFixture(t)
	writeFile(t, filepath.Join(f.cwdRoot, ConfigFile), "limits:\n  maxFileMB: 9\n")
	o, _ := tempOptions(t)
	o.Cwd, o.Exe = f.cwd, f.exe
	c := mustNew(t, o)
	if c.Config != nil {
		t.Fatalf("Config = %+v before LocateRoot, want nil", c.Config)
	}
	if err := c.LocateRoot(); err != nil {
		t.Fatalf("LocateRoot: %v", err)
	}
	if c.Config == nil || c.Config.Limits.MaxFileMB != 9 {
		t.Errorf("Config = %+v, want maxFileMB 9", c.Config)
	}
}

func TestLocateRootReportsABadConfig(t *testing.T) {
	f := newRootFixture(t)
	writeFile(t, filepath.Join(f.cwdRoot, ConfigFile), "colour: blue\n")
	o, _ := tempOptions(t)
	o.Cwd, o.Exe = f.cwd, f.exe
	c := mustNew(t, o)
	err := c.LocateRoot()
	want := filepath.Join(f.cwdRoot, ConfigFile) + `:1:1: unknown key "colour"`
	if err == nil || !strings.HasPrefix(err.Error(), want) {
		t.Errorf("LocateRoot error = %v, want prefix %q", err, want)
	}
	if c.Root != "" || c.Config != nil {
		t.Errorf("after a failed load Root = %q, Config = %v; want both unset", c.Root, c.Config)
	}
}

func TestLocateRootRenameHintForOldConfigName(t *testing.T) {
	f := newRootFixture(t)
	if err := os.Remove(filepath.Join(f.cwdRoot, ConfigFile)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(f.cwdRoot, "homebot.yaml"), "")
	o, _ := tempOptions(t)
	o.Cwd, o.Exe = f.cwd, f.exe
	err := mustNew(t, o).LocateRoot()
	if err == nil || !strings.Contains(err.Error(), "rename "+filepath.Join(f.cwdRoot, "homebot.yaml")+" to homebot.yml") {
		t.Errorf("LocateRoot error = %v, want the rename hint", err)
	}
}

func TestNewWithRootLoadsConfig(t *testing.T) {
	o, work := tempOptions(t)
	writeFile(t, work+"/repo/"+ConfigFile, "autosave:\n  push: true\n")
	o.Root = work + "/repo"
	if c := mustNew(t, o); c.Config == nil || !c.Config.Autosave.Push {
		t.Errorf("Config = %+v, want autosave.push", c.Config)
	}
	if err := os.Remove(work + "/repo/" + ConfigFile); err != nil {
		t.Fatal(err)
	}
	if _, err := New(o); err == nil {
		t.Error("New with a Root that has no homebot.yml succeeded")
	}
}

// saysNoRoot reports whether err is the error noRoot makes.
func saysNoRoot(err error) bool {
	var ue *fail.UserError
	if !errors.As(err, &ue) {
		return false
	}
	text, ok := ue.Text()
	return ok && text.ID() == msg.ContextErrorNoConfig().ID()
}

// One caller changing the error it got does not change the next caller's.
func TestEachNoRootErrorIsItsOwn(t *testing.T) {
	f := newRootFixture(t)
	c := &Context{Env: NewEnv(nil), Cwd: f.bare, Home: f.bare}
	first := c.LocateRoot()
	var ue *fail.UserError
	if !errors.As(first, &ue) {
		t.Fatalf("premise: %v is not a fail.UserError", first)
	}
	ue.Msg = "changed"
	second := c.LocateRoot()
	if !saysNoRoot(second) || second.Error() == "changed" {
		t.Errorf("the second LocateRoot = %v, the first's error edited", second)
	}
}
