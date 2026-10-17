package app

import (
	"path/filepath"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/layout"
)

// Layout is the repo's folder contract. The names are fixed: the structure
// says what happens.
type Layout struct {
	Autolink      string
	Private       string
	Manifests     string
	Profiles      string
	CustomActions string
	Furniture     string
	Decisions     string
	Identity      string
	Recipients    string
}

// Layout returns the paths of the home repo's folders and files. It panics
// when Root is not set: call LocateRoot first.
func (c *Context) Layout() Layout {
	if c.Root == "" {
		panic("app: Layout called before the home repo was located")
	}
	at := func(rel string) string { return filepath.Join(c.Root, filepath.FromSlash(rel)) }
	return Layout{
		Autolink:      at(layout.Autolink),
		Private:       at(layout.Private),
		Manifests:     at(layout.Manifests),
		Profiles:      at(layout.Profiles),
		CustomActions: at(layout.CustomActions),
		Furniture:     at(layout.Furniture),
		Decisions:     at(layout.Decisions),
		Identity:      at(layout.Identity),
		Recipients:    at(layout.Recipients),
	}
}

// ActiveLayers returns the layers that apply to this machine, in link order.
func (c *Context) ActiveLayers() []string {
	return []string{"common", string(c.OS)}
}

// IsInside reports whether path is dir or below it. Both are compared as
// strings, as the TypeScript build did: neither is cleaned or resolved.
func IsInside(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+"/")
}
