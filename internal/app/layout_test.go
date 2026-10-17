package app

import (
	"slices"
	"strings"
	"testing"
)

func TestLayoutPaths(t *testing.T) {
	c := &Context{Root: "/r"}
	want := Layout{
		Autolink:      "/r/autolink",
		Private:       "/r/private",
		Manifests:     "/r/manifests",
		Profiles:      "/r/manifests/profiles",
		CustomActions: "/r/manifests/actions",
		Furniture:     "/r/furniture",
		Decisions:     "/r/decisions.yml",
		Identity:      "/r/private/identity.age",
		Recipients:    "/r/private/recipients.txt",
	}
	if got := c.Layout(); got != want {
		t.Errorf("Layout() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestLayoutPanicsWithoutRoot(t *testing.T) {
	defer func() {
		msg, _ := recover().(string)
		if !strings.Contains(msg, "before the home repo was located") {
			t.Errorf("recovered %q", msg)
		}
	}()
	(&Context{}).Layout()
	t.Error("Layout returned without a Root")
}

func TestNewCleansRoot(t *testing.T) {
	o, work := tempOptions(t)
	writeFile(t, work+"/repo/"+ConfigFile, "")
	o.Root = work + "/repo/./sub/../"
	if got := mustNew(t, o).Root; got != work+"/repo" {
		t.Errorf("Root = %q, want %q", got, work+"/repo")
	}
}

func TestActiveLayers(t *testing.T) {
	for osName, want := range map[OS][]string{Linux: {"common", "linux"}, MacOS: {"common", "macos"}} {
		if got := (&Context{OS: osName}).ActiveLayers(); !slices.Equal(got, want) {
			t.Errorf("%s: ActiveLayers() = %q, want %q", osName, got, want)
		}
	}
	for _, layer := range (&Context{OS: MacOS}).ActiveLayers() {
		if !slices.Contains(Layers, layer) {
			t.Errorf("active layer %q is not in Layers", layer)
		}
	}
}

func TestIsInside(t *testing.T) {
	for _, tc := range []struct {
		path, dir string
		want      bool
	}{
		{"/a/b", "/a/b", true},
		{"/a/b/c", "/a/b", true},
		{"/a/b/c/d.txt", "/a", true},
		{"/a/bc", "/a/b", false},
		{"/a", "/a/b", false},
		{"/a/b/", "/a/b", true},
		{"/a/b/c", "/a/b/", false},
		{"/a/b/../x", "/a/b", true},
		{"a/b", "a", true},
		{"/a/b", "", true},
		{"", "", true},
		{"", "/a", false},
	} {
		if got := IsInside(tc.path, tc.dir); got != tc.want {
			t.Errorf("IsInside(%q, %q) = %v, want %v", tc.path, tc.dir, got, tc.want)
		}
	}
}
