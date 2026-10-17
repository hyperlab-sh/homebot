// Package gitx runs git for homebot. It shells out to the git binary, so
// hooks, signing and the user's config behave as they do on the command
// line, and parses the machine-readable outputs into typed results.
package gitx

import (
	"fmt"
	"maps"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/proc"
)

// base is put before every git subcommand: paths come back as they are on
// disk, never quoted and octal-escaped, and git takes no optional locks.
//
// git status, diff and friends try to refresh the index's stat cache and take
// index.lock to write it back. That lock is optional for them but not for
// git add, so a read-only call racing the hourly autosave would make the
// autosave fail with "index.lock: File exists". --no-optional-locks makes the
// read-only calls leave the index alone; commands that need the lock still
// take it.
var base = []string{"git", "--no-optional-locks", "-c", "core.quotePath=false"}

func argv(args []string) []string {
	return append(append([]string(nil), base...), args...)
}

// Run runs git in dir and captures its output. A failing git is a Result
// with a non-zero Code, never an error.
func Run(c *app.Context, dir string, args ...string) proc.Result {
	return proc.Run(c, argv(args), proc.Options{Dir: dir})
}

// RunIn is Run with stdin fed to git.
func RunIn(c *app.Context, dir, stdin string, args ...string) proc.Result {
	return proc.Run(c, argv(args), proc.Options{Dir: dir, Stdin: strings.NewReader(stdin)})
}

// RunNet is Run for commands that may touch the network: git is told never
// to prompt for credentials on a terminal. opts bounds the call (Timeout,
// Group) or feeds it stdin; its Dir and Env are merged with the defaults.
func RunNet(c *app.Context, dir string, opts proc.Options, args ...string) proc.Result {
	opts.Dir = dir
	env := map[string]string{}
	maps.Copy(env, opts.Env)
	env["GIT_TERMINAL_PROMPT"] = "0"
	opts.Env = env
	return proc.Run(c, argv(args), opts)
}

// failed turns a failed git Result into the user error "git <what> failed."
// (git.error.failed) plus what git said.
func failed(r proc.Result, what string) error {
	said := strings.TrimSpace(r.Stderr)
	if said == "" {
		said = r.Stdout
	}
	return fmt.Errorf("%w%s", fail.Say(msg.GitErrorFailed(what)), fail.Failure("", said, "git"))
}

// IsRepo reports whether dir is inside a git work tree.
func IsRepo(c *app.Context, dir string) bool {
	return strings.TrimSpace(Run(c, dir, "rev-parse", "--is-inside-work-tree").Stdout) == "true"
}

// HasCommits reports whether HEAD points at a commit.
func HasCommits(c *app.Context, dir string) bool {
	return Run(c, dir, "rev-parse", "--verify", "--quiet", "HEAD").Code == 0
}

// UntrackedFiles lists the untracked, not ignored paths under pathspec; a
// wholly untracked directory is listed once, without its trailing slash.
func UntrackedFiles(c *app.Context, dir, pathspec string) ([]string, error) {
	r := Run(c, dir, "ls-files", "--others", "--exclude-standard", "--directory", "--no-empty-directory", "-z", "--", pathspec)
	if r.Code != 0 {
		return nil, failed(r, "ls-files")
	}
	var paths []string
	for _, p := range splitNUL(r.Stdout) {
		paths = append(paths, strings.TrimSuffix(p, "/"))
	}
	return paths, nil
}

// splitNUL splits NUL-terminated output, dropping empty fields.
func splitNUL(s string) []string {
	var out []string
	for _, f := range strings.Split(s, "\x00") {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}
