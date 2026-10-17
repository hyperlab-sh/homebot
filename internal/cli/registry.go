package cli

import (
	"fmt"
	"slices"

	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
)

// Group is the section of the help overview a command is listed under, in
// the order help.ts shows them. Hidden commands are not listed.
type Group string

const (
	Attention    Group = "attention"
	MovingIn     Group = "movingin"
	Secrets      Group = "secrets"
	KeepingOrder Group = "order"
	Hidden       Group = "hidden"
)

// Each top-level command lives in its own file, named <group>_<command>.go,
// and registers itself from that file's init:
//
//	func init() { register(KeepingOrder, "save", newSaveCmd) }
//
// The groups, as help.ts has them:
//
//	attention  inbox next
//	movingin   unpack pack link unlink cleanup
//	secrets    keys unlock lock
//	order      triage inventory remote save export tidy setup actions action shell
//	hidden     refresh hook
//
// A builder reads nothing from c while building; the command uses c when it
// runs. It must not set PersistentPreRunE: the root's would be replaced.

// command is one registered top-level command.
type command struct {
	group Group
	name  string
	build func(c *app.Context) *cobra.Command
}

var registry []command

// register adds a top-level command. It panics on a duplicate name or an
// unknown group, so a mistake fails every test rather than one.
func register(group Group, name string, build func(c *app.Context) *cobra.Command) {
	if !slices.Contains([]Group{Attention, MovingIn, Secrets, KeepingOrder, Hidden}, group) {
		panic(fmt.Sprintf("cli: command %q registered with unknown group %q", name, group))
	}
	for _, r := range registry {
		if r.name == name {
			panic(fmt.Sprintf("cli: command %q registered twice", name))
		}
	}
	registry = append(registry, command{group: group, name: name, build: build})
}

// addRegistered builds every registered command and adds it to root, in name
// order so the tree does not depend on file order.
func addRegistered(root *cobra.Command, c *app.Context) {
	sorted := slices.SortedFunc(slices.Values(registry), func(a, b command) int {
		switch {
		case a.name < b.name:
			return -1
		case a.name > b.name:
			return 1
		}
		return 0
	})
	for _, r := range sorted {
		cmd := r.build(c)
		if cmd.Name() != r.name {
			panic(fmt.Sprintf("cli: command registered as %q builds %q", r.name, cmd.Name()))
		}
		cmd.Annotations = withGroup(cmd.Annotations, r.group)
		root.AddCommand(cmd)
	}
}

// groupAnnotation records a command's Group on the cobra command. Help
// lists commands by their page's group, which must agree with it.
const groupAnnotation = "homebot.group"

func withGroup(a map[string]string, g Group) map[string]string {
	if a == nil {
		a = map[string]string{}
	}
	a[groupAnnotation] = string(g)
	return a
}
