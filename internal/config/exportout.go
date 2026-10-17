package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/layout"
	"github.com/hyperlab-sh/homebot/internal/msg"
)

// ExportOutError says what is wrong with out, an export's output path, or
// returns nil. An export replaces its out wholesale (a file by one rename, a
// folder by moving the old one aside and renaming the new one in), so out
// must be a relative path that stays inside the repo once cleaned, and must
// not be the repo itself, nor manifests, nor be or sit inside any other path
// homebot or git own (layout.Owned: .git, private, autolink, furniture,
// homebot.yml and the rest), in any case: on a case-insensitive filesystem
// Private is private. When root is not empty, the same holds once the
// symlinks among out's existing parent folders are resolved; out itself may
// be a link, as the swap replaces the link itself and never follows it.
// where and name only word the error.
func ExportOutError(root, where, name, out string) error {
	if filepath.IsAbs(out) {
		return fail.Say(msg.ConfigErrorExportOutAbsolute(where, out, name))
	}
	clean := filepath.Clean(out)
	if t := outProblem(clean); t != nil {
		return fail.Say(t(where, out, name))
	}
	if root == "" {
		return nil
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		// A repo that cannot be resolved is not out's problem.
		return nil
	}
	parent := filepath.Dir(clean)
	for parent != "." {
		if _, err := os.Lstat(filepath.Join(root, parent)); err == nil {
			break
		}
		parent = filepath.Dir(parent)
	}
	if parent == "." {
		return nil
	}
	target, err := filepath.EvalSymlinks(filepath.Join(root, parent))
	if err != nil {
		// A dangling link: os.MkdirAll refuses to go through it, so the
		// export fails before it writes or removes anything.
		return nil
	}
	rel, err := filepath.Rel(realRoot, target)
	if err == nil && filepath.IsLocal(rel) {
		rest, _ := filepath.Rel(parent, clean)
		if outProblem(filepath.Join(rel, rest)) == nil {
			return nil
		}
	}
	return fail.Say(msg.ConfigErrorExportOutSymlink(where, out, name, target))
}

// outProblem is the message for what is wrong with clean, a cleaned
// relative out, or nil. Names compare without regard to case, as the guard
// compares private/: on a case-insensitive filesystem Autolink is autolink.
func outProblem(clean string) func(where, out, name string) msg.Text {
	if !filepath.IsLocal(clean) {
		return msg.ConfigErrorExportOutOutside
	}
	if clean == "." {
		return msg.ConfigErrorExportOutRoot
	}
	p := strings.ToLower(filepath.ToSlash(clean))
	within := func(dir string) bool { return p == dir || strings.HasPrefix(p, dir+"/") }
	switch {
	case within(layout.Git):
		return msg.ConfigErrorExportOutGit
	case within(layout.Private):
		return msg.ConfigErrorExportOutPrivate
	case p == layout.OwnedFolder:
		return managed(msg.ConfigErrorExportOutManaged, layout.OwnedFolder)
	}
	for _, owned := range layout.Owned {
		switch {
		case p == owned:
			return managed(msg.ConfigErrorExportOutManaged, owned)
		case within(owned):
			return managed(msg.ConfigErrorExportOutManagedInside, owned)
		}
	}
	return nil
}

func managed(text func(where, out, name, managed string) msg.Text, path string) func(where, out, name string) msg.Text {
	return func(where, out, name string) msg.Text { return text(where, out, name, path) }
}
