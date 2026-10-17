package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// hostTools returns a directory holding links to the named programs as the
// developer's PATH finds them, and nothing else. Scripts get it on their
// PATH so [exec:age] can see the age CLI without the rest of the host's
// tools leaking in.
//
// Each link points at the program itself, its symlinks resolved, and the
// program must answer --version with only that directory and the system's
// on its PATH and a fresh HOME, as in a script. A version manager's shim
// does not, nor does a program the host lacks: either is left out, and the
// scripts that need it skip.
func hostTools(t *testing.T, names ...string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "host-tools")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	env := []string{
		"PATH=" + strings.Join([]string{dir, "/usr/bin", "/bin"}, string(filepath.ListSeparator)),
		"HOME=" + t.TempDir(),
	}
	for _, name := range names {
		found, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		real, err := filepath.EvalSymlinks(found)
		if err != nil {
			continue
		}
		link := filepath.Join(dir, name)
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}
		probe := exec.Command(link, "--version")
		probe.Env = env
		probe.Dir = dir
		if err := probe.Run(); err != nil {
			t.Logf("leaving %s out: %s --version: %v", name, real, err)
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

func TestHostToolsLinksOnlyWhatRunsInAScript(t *testing.T) {
	bin := t.TempDir()
	scripts := map[string]string{
		"real-tool": "#!/bin/sh\necho v1\n",
		// A version manager's shim: it needs the developer's shell.
		"shim-tool": "#!/bin/sh\n[ -n \"$SHIM_CONFIG\" ] || exit 1\necho v1\n",
	}
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("real-tool", filepath.Join(bin, "linked-tool")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHIM_CONFIG", "set")
	t.Setenv("PATH", bin)

	dir := hostTools(t, "linked-tool", "shim-tool", "missing-tool")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"linked-tool"}) {
		t.Fatalf("host tools = %v, want only linked-tool", names)
	}
	got, err := os.Readlink(filepath.Join(dir, "linked-tool"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(filepath.Join(bin, "real-tool"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("linked-tool -> %s, want the program itself, %s", got, want)
	}
}
