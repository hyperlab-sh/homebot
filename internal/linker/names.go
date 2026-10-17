// Package linker links autolink/<layer> and private/<layer> into $HOME.
//
// Names at the top of a layer gain a dot (config is ~/.config); deeper names
// are literal. A folder whose name is wrapped in () must stay a real folder in
// $HOME, never a link, so apps can keep their own files next to the repo's.
// Every other folder that only one layer provides becomes one link. homebot
// remembers which links it owns (links.json), prunes stale ones, and reports
// what is in the way without touching it.
package linker

import (
	"slices"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/msg"
)

// skip holds the names never linked: VCS and OS litter.
var skip = map[string]bool{".git": true, ".gitkeep": true, ".DS_Store": true}

// NoDot holds the top-level names that keep their spelling in $HOME instead
// of gaining a dot.
var NoDot = []string{"Library"}

// AlwaysReal holds the home folders that never become links, as if their
// names were wrapped in (). Apps write their own files into these.
var AlwaysReal = []string{
	".config",
	".local",
	".local/bin",
	".local/share",
	".local/state",
	".cache",
	".ssh",
	".gnupg",
	"Library",
	"Library/Application Support",
	"Library/Preferences",
}

// LooksMarked reports whether name is wrapped in (), well-formed or not.
func LooksMarked(name string) bool {
	return len(name) >= 2 && strings.HasPrefix(name, "(") && strings.HasSuffix(name, ")")
}

// Name is a name from the repo as ParseName reads it.
type Name struct {
	// Home is the name in $HOME.
	Home string
	// Marked is true for a (name) folder, which stays real in $HOME.
	Marked bool
	// Problem says why the layout forbids the name; nil when it is allowed.
	Problem *msg.Text
}

// ParseName reads one name from the repo. top is true at the top of a layer,
// where names gain a dot. ok is false for names that are ignored (dot-names at
// the top of a layer). A name the layout forbids comes back with Problem set.
func ParseName(raw string, top bool) (n Name, ok bool) {
	if top && strings.HasPrefix(raw, ".") {
		return Name{}, false
	}
	if top && strings.HasPrefix(raw, "dot-") {
		return forbidden(msg.LayoutOldName(raw, raw[len("dot-"):])), true
	}
	name := raw
	marked := LooksMarked(raw)
	if marked {
		name = raw[1 : len(raw)-1]
		switch {
		case name == "":
			return forbidden(msg.LayoutEmptyParens()), true
		case name == "." || name == "..":
			// Not a name: (..) below the top of a layer would link in the
			// folder above, or above $HOME.
			return forbidden(msg.LayoutDotParens(raw)), true
		case strings.HasPrefix(name, "(") || strings.HasSuffix(name, ")"):
			bare := strings.TrimRight(strings.TrimLeft(name, "("), ")")
			return forbidden(msg.LayoutDoubleParens(raw, bare)), true
		case top && strings.HasPrefix(name, "."):
			return forbidden(msg.LayoutTopDot(raw, name[1:])), true
		}
	}
	home := name
	if top && !slices.Contains(NoDot, name) {
		home = "." + name
	}
	return Name{Home: home, Marked: marked}, true
}

func forbidden(problem msg.Text) Name {
	return Name{Problem: &problem}
}
