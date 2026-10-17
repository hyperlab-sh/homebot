package app

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/layout"
	"github.com/hyperlab-sh/homebot/internal/msg"
)

// ConfigFile is the file that marks a directory as a home repo.
const ConfigFile = layout.ConfigFile

// oldConfigFile also marks a home repo, so that loading it can say to rename it.
const oldConfigFile = "homebot.yaml"

// noRoot is the error when no home repo is found, made anew each time: a
// *fail.UserError's Msg can be changed, so one shared value is not safe.
func noRoot() error { return fail.Say(msg.ContextErrorNoConfig()) }

// IsNoRoot reports whether err is the error of finding no home repo, as
// opposed to a repo whose homebot.yml cannot be loaded. A command that is
// useful without a repo (persona list) tells them apart.
func IsNoRoot(err error) bool {
	var user *fail.UserError
	if !errors.As(err, &user) {
		return false
	}
	t, ok := user.Text()
	return ok && t.ID() == msg.ContextErrorNoConfig().ID()
}

func isRoot(dir string) bool {
	for _, name := range []string{ConfigFile, oldConfigFile} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// FindRoot returns the nearest home repo at or above start, walking up to /.
// A relative start is taken from the process's working directory.
func FindRoot(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		if isRoot(dir) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// LocateRoot sets Root to the home repo when it is not set yet, and loads its
// homebot.yml into Config. It takes the first of these that holds
// homebot.yml: $HOMEBOT_ROOT, the nearest repo at or above Cwd, the nearest
// at or above the running binary's directory, and ~/.home. Root and Config
// are set together, and only when the config loads.
func (c *Context) LocateRoot() error {
	if c.Root != "" {
		return nil
	}
	root, err := locateRoot(c.Env, c.Cwd, c.Exe, c.Home)
	if err != nil {
		return err
	}
	cfg, err := config.LoadDebug(root, c.Debug)
	if err != nil {
		return err
	}
	c.Root, c.Config = root, cfg
	return nil
}

// TakeConfigWarnings returns recoverable config warnings once per command.
func (c *Context) TakeConfigWarnings() []msg.Text {
	if c.Config == nil || c.warningsReported {
		return nil
	}
	c.warningsReported = true
	return c.Config.Warnings
}

func locateRoot(env Env, cwd, exe, home string) (string, error) {
	// The working directory as the kernel reports it, without symlinks, the
	// way process.cwd() gave it to the TypeScript build.
	if cwd != "" {
		if real, err := filepath.EvalSymlinks(cwd); err == nil {
			cwd = real
		}
	}
	var candidates []string
	if dir := env.Get("HOMEBOT_ROOT"); dir != "" {
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(cwd, dir)
		}
		candidates = append(candidates, filepath.Clean(dir))
	}
	if cwd != "" {
		if dir, ok := FindRoot(cwd); ok {
			candidates = append(candidates, dir)
		}
	}
	if exe != "" {
		if real, err := filepath.EvalSymlinks(exe); err == nil {
			if dir, ok := FindRoot(filepath.Dir(real)); ok {
				candidates = append(candidates, dir)
			}
		}
	}
	candidates = append(candidates, filepath.Join(home, ".home"))
	for _, dir := range candidates {
		if isRoot(dir) {
			return dir, nil
		}
	}
	return "", noRoot()
}
