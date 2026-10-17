package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// withRegistry runs fn with an empty registry, then restores it.
func withRegistry(t *testing.T, fn func()) {
	t.Helper()
	saved := registry
	registry = nil
	t.Cleanup(func() { registry = saved })
	fn()
}

func leaf(name string) func(*app.Context) *cobra.Command {
	return func(*app.Context) *cobra.Command {
		return &cobra.Command{Use: name, RunE: func(*cobra.Command, []string) error { return nil }}
	}
}

// fake builds a command name with flags. Each time it runs, it appends its
// name and arguments to ran, when ran is not nil.
func fake(name string, flags func(*pflag.FlagSet), ran *[]string) func(*app.Context) *cobra.Command {
	return func(*app.Context) *cobra.Command {
		cmd := &cobra.Command{Use: name, RunE: func(_ *cobra.Command, args []string) error {
			if ran != nil {
				*ran = append(*ran, strings.TrimSpace(name+" "+strings.Join(args, " ")))
			}
			return nil
		}}
		if flags != nil {
			flags(cmd.Flags())
		}
		return cmd
	}
}

func TestRegisteredCommandsJoinTheRootInNameOrder(t *testing.T) {
	withRegistry(t, func() {
		register(KeepingOrder, "save", leaf("save"))
		register(Attention, "inbox", leaf("inbox"))
		register(Hidden, "hook", leaf("hook"))
		root := NewRoot(testutil.Sandbox(t, testutil.Options{}).Ctx, Build{Version: "test"})
		var names []string
		for _, cmd := range root.Commands() {
			names = append(names, cmd.Name()+"="+cmd.Annotations[groupAnnotation])
		}
		got := strings.Join(names, " ")
		// version is the root's own; cobra adds completion and help lazily.
		for _, want := range []string{"hook=hidden", "inbox=attention", "save=order"} {
			if !strings.Contains(got, want) {
				t.Errorf("root commands %q lack %q", got, want)
			}
		}
	})
}

func TestRegisterRefusesMistakes(t *testing.T) {
	cases := []struct {
		name string
		fn   func()
		want string
	}{
		{"duplicate", func() {
			register(Secrets, "lock", leaf("lock"))
			register(Secrets, "lock", leaf("lock"))
		}, `"lock" registered twice`},
		{"unknown group", func() { register("Moving in", "pack", leaf("pack")) }, `unknown group "Moving in"`},
		{"name mismatch", func() {
			register(MovingIn, "pack", leaf("unpack"))
			NewRoot(testutil.Sandbox(t, testutil.Options{}).Ctx, Build{Version: "test"})
		}, `registered as "pack" builds "unpack"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withRegistry(t, func() {
				defer func() {
					r := recover()
					if r == nil || !strings.Contains(r.(string), tc.want) {
						t.Errorf("panic = %v, want one containing %q", r, tc.want)
					}
				}()
				tc.fn()
			})
		})
	}
}
