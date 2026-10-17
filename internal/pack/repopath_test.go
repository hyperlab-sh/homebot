package pack

import (
	"testing"
)

func TestRepoPathFor(t *testing.T) {
	b := newBox(t, false)
	for _, c := range []struct {
		name string
		path string
		o    Options
		want string
	}{
		{"built-ins stay plain, new folders between them and the item are marked", ".config/app/sub/file", Options{}, "autolink/common/config/(app)/(sub)/file"},
		{"local/bin is built in", ".local/bin/tool", Options{Layer: "linux"}, "autolink/linux/local/bin/tool"},
		{"Library keeps its name", "Library/Application Support/Code", Options{Layer: "macos"}, "autolink/macos/Library/Application Support/Code"},
		{"private goes under private", ".netrc", Options{Private: true}, "private/common/netrc"},
		{"a dotfile folder", ".config/nvim", Options{}, "autolink/common/config/nvim"},
		{"~ expands to the home folder", "~/.zshrc", Options{}, "autolink/common/zshrc"},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := b.h(c.path)
			if c.path[0] == '~' {
				path = c.path
			}
			got, err := RepoPathFor(b.Ctx, path, c.o)
			if err != nil {
				t.Fatal(err)
			}
			if want := b.r(c.want); got != want {
				t.Errorf("= %q, want %q", got, want)
			}
		})
	}
}

func TestRepoPathForFoldersTheRepoAlreadyHasKeepTheirSpellingFromAnyLayerOrTree(t *testing.T) {
	b := newBox(t, false)
	b.repo(map[string]string{
		"autolink/common/(claude)/a":           "a",
		"private/common/config/(gh)/hosts.yml": "h",
	})
	for path, want := range map[string]string{
		".claude/b":             "autolink/linux/(claude)/b",
		".config/gh/config.yml": "autolink/common/config/(gh)/config.yml",
	} {
		layer := "common"
		if path == ".claude/b" {
			layer = "linux"
		}
		got, err := RepoPathFor(b.Ctx, b.h(path), Options{Layer: layer})
		if err != nil {
			t.Fatal(err)
		}
		if got != b.r(want) {
			t.Errorf("%s = %q, want %q", path, got, b.r(want))
		}
	}
}

func TestRepoPathForAFilePackedIntoAnExistingPlainFolderStaysPlain(t *testing.T) {
	b := newBox(t, false)
	b.repo(map[string]string{
		"autolink/common/config/wezterm/wezterm.lua": "w",
		"autolink/linux/config/wezterm/platform.lua": "p",
	})
	got, err := RepoPathFor(b.Ctx, b.h(".config/wezterm/keys.lua"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if want := b.r("autolink/common/config/wezterm/keys.lua"); got != want {
		t.Errorf("= %q, want %q", got, want)
	}
}

func TestRepoPathForRefusesToAddAMarkedFolderUnderAPlainOne(t *testing.T) {
	b := newBox(t, false)
	b.repo(map[string]string{"autolink/common/config/wezterm/wezterm.lua": "w"})
	_, err := RepoPathFor(b.Ctx, b.h(".config/wezterm/sub/x.lua"), Options{})
	wantUserError(t, err, `^config/wezterm is a plain folder, so it can't hold a new folder that must stay real\. Rename it to \(wezterm\) in every layer first\.$`)
}

func TestRepoPathForRefuses(t *testing.T) {
	b := newBox(t, false)
	for _, c := range []struct{ path, re string }{
		{"visible/x", `^Only dotfiles and Library can be tracked at the top of your home folder \(got ~/visible\)\.$`},
		{"..odd", `^Only dotfiles and Library can be tracked`},
		{".dot-zshrc", `^Only dotfiles and Library can be tracked at the top of your home folder \(got ~/\.dot-zshrc\)\.$`},
		{".config/(weird)/x", `^"\(weird\)" is wrapped in \(\), which the repo uses to mark folders that must not be links, so it can't be tracked\.$`},
	} {
		_, err := RepoPathFor(b.Ctx, b.h(c.path), Options{})
		if err == nil {
			t.Errorf("%s: no error", c.path)
			continue
		}
		wantUserError(t, err, c.re)
	}
	for _, p := range []string{"/etc/hostname", b.Home, b.h(".."), "/"} {
		_, err := RepoPathFor(b.Ctx, p, Options{})
		wantUserError(t, err, `^Only things inside your home folder can be packed`)
	}
	_, err := RepoPathFor(b.Ctx, b.h(".x"), Options{Layer: "nope"})
	wantUserError(t, err, `^Unknown layer "nope"`)
}
