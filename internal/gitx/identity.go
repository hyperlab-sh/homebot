package gitx

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fsx"
)

// Author is the name and email commits will carry; a field is empty where
// git has none.
type Author struct {
	Name  string
	Email string
}

// Identity reads the author git would commit with in dir: its config, then
// the environment variables git falls back on.
func Identity(c *app.Context, dir string) Author {
	get := func(key string) string {
		return strings.TrimSpace(Run(c, dir, "config", "--get", key).Stdout)
	}
	first := func(vals ...string) string {
		for _, v := range vals {
			if v != "" {
				return v
			}
		}
		return ""
	}
	return Author{
		Name:  first(get("user.name"), c.Env.Get("GIT_AUTHOR_NAME")),
		Email: first(get("user.email"), c.Env.Get("GIT_AUTHOR_EMAIL"), c.Env.Get("EMAIL")),
	}
}

var inlineKey = regexp.MustCompile(`^(ssh|ecdsa|sk)-`)

// MissingSigningKey returns the ssh key file commits in dir are signed with
// when signing is on and that file is not on this machine: every commit
// fails then. It returns "" otherwise, and for keys given inline or through
// an agent.
func MissingSigningKey(c *app.Context, dir string) string {
	get := func(args ...string) string {
		return strings.TrimSpace(Run(c, dir, append([]string{"config"}, args...)...).Stdout)
	}
	if get("--type=bool", "--get", "commit.gpgsign") != "true" {
		return ""
	}
	if get("--get", "gpg.format") != "ssh" {
		return ""
	}
	key := get("--type=path", "--get", "user.signingkey")
	if key == "" || strings.HasPrefix(key, "key::") || inlineKey.MatchString(key) {
		return ""
	}
	if _, err := os.Stat(key); err == nil {
		return ""
	}
	return key
}

// ShadowingGitconfig returns ~/.gitconfig when it would override the repo's
// git config, else "". Git reads ~/.config/git/config first and ~/.gitconfig
// after it, so the later file wins wherever both set something. It only
// matters when ~/.config/git/config comes from the repo.
func ShadowingGitconfig(c *app.Context) string {
	home := filepath.Join(c.Home, ".gitconfig")
	if info, err := fsx.LstatIfExists(home); err != nil || info == nil {
		return ""
	}
	real := func(p string) string {
		r, err := filepath.EvalSymlinks(p)
		if err != nil {
			return ""
		}
		return r
	}
	root := real(c.Root)
	if root == "" {
		root = c.Root
	}
	xdg := real(filepath.Join(c.Home, ".config", "git", "config"))
	if xdg == "" || !app.IsInside(xdg, root) {
		return ""
	}
	if mine := real(home); mine != "" && app.IsInside(mine, root) {
		return ""
	}
	return home
}
