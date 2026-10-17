package cli

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestCompletionIsVisibleAndSideEffectFree(t *testing.T) {
	c, stdout, _ := noRepo(t)
	for _, path := range []string{c.StateDir, c.CacheDir, c.DataDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("sandbox folder exists before completion: %s: %v", path, err)
		}
	}
	if code := Execute(context.Background(), c, Build{}, []string{"__complete", ""}); code != 0 {
		t.Fatalf("completion exited %d", code)
	}
	var got []string
	var directive bool
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		if strings.HasPrefix(line, ":") {
			directive = true
			continue
		}
		name, _, _ := strings.Cut(line, "\t")
		got = append(got, name)
	}
	if !directive {
		t.Fatalf("no Cobra directive in %q", stdout.String())
	}
	var want []string
	for _, p := range pages {
		if p.group != Hidden {
			want = append(want, p.name)
		}
	}
	want = append(want, "help")
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("completed commands = %q, want %q", got, want)
	}
	for _, path := range []string{c.StateDir, c.CacheDir, c.DataDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("completion created %s: %v", path, err)
		}
	}
}

func TestCompletionListsSaveFlagsWithoutRepo(t *testing.T) {
	c, stdout, _ := noRepo(t)
	if code := Execute(context.Background(), c, Build{}, []string{"__complete", "save", "-"}); code != 0 {
		t.Fatalf("completion exited %d", code)
	}
	for _, flag := range []string{"--message", "--no-push"} {
		if !strings.Contains(stdout.String(), flag) {
			t.Errorf("missing %s in %q", flag, stdout.String())
		}
	}
}

func TestHiddenCommandsRemainRoutable(t *testing.T) {
	for _, name := range []string{"version", "refresh", "hook"} {
		t.Run(name, func(t *testing.T) {
			c, _, _ := noRepo(t)
			root := NewRoot(c, Build{})
			cmd := child(root, name)
			if cmd == nil || !cmd.Hidden {
				t.Fatalf("%s is not Cobra-hidden", name)
			}
			got, err := dispatch(c, root, []string{name})
			if err != nil || !slices.Equal(got, []string{name}) {
				t.Fatalf("dispatch hidden %s = %q, %v", name, got, err)
			}
		})
	}
}
