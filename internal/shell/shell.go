// Package shell holds the zsh integration `homebot shell zsh` prints and the
// check for whether the user's zsh already loads it.
package shell

import (
	_ "embed"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/selfname"
)

//go:embed homebot.zsh
var zsh string

// Zsh is the integration script, to be loaded with eval "$(homebot shell zsh)".
func Zsh() string { return zsh }

// ZshFor adds completion for a recorded name when its link resolves to the
// homebot binary, and only once a completion for homebot itself exists. It
// deliberately does not inspect PATH: zsh commonly loads this integration
// before the rc file adds ~/.local/bin to PATH.
func ZshFor(c *app.Context) string {
	rec, ok := selfname.Record(c)
	if !ok || rec.Pending {
		return zsh
	}
	link := filepath.Join(c.Home, ".local", "bin", rec.Name)
	fi, err := os.Lstat(link)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return zsh
	}
	linked, err := os.Stat(link)
	if err != nil {
		return zsh
	}
	self, err := os.Stat(proc.SelfPath(c))
	if err != nil || !os.SameFile(linked, self) {
		return zsh
	}
	return zsh + compdefLine(rec.Name)
}

// compdefLine registers name's completion as homebot's. compdef copies the
// service's function when it runs and fails (status 1, a message on stderr)
// when no completion for homebot is registered yet, so the line waits for
// _comps[homebot]. It is an if, not `a && b`: the && form is silent but still
// leaves status 1 as the script's last, and eval hands that to the caller.
func compdefLine(name string) string {
	return "if (( $+functions[compdef] && $+_comps[homebot] )); then compdef " + name + "=homebot; fi\n"
}

var evalLine = regexp.MustCompile(`(?m)^[^#\n]*eval\s+["']?\$\(\s*(?:\S*/)?homebot\s+shell\s+zsh\s*\)`)

// Integrated says whether zsh already loads the integration: the eval line is
// in ~/.zshrc, or in $ZDOTDIR's .zshrc or its rc.d/ or conf.d/ *.zsh files
// (ZDOTDIR defaults to ~/.config/zsh, where this repo's zsh config lives).
func Integrated(c *app.Context) bool {
	dir := c.Env.Get("ZDOTDIR")
	if dir == "" {
		dir = filepath.Join(c.Home, ".config", "zsh")
	}
	files := []string{filepath.Join(c.Home, ".zshrc"), filepath.Join(dir, ".zshrc")}
	for _, sub := range []string{"rc.d", "conf.d"} {
		d := filepath.Join(dir, sub)
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".zsh") {
				files = append(files, filepath.Join(d, e.Name()))
			}
		}
	}
	for _, f := range files {
		if b, err := os.ReadFile(f); err == nil && evalLine.Match(b) {
			return true
		}
	}
	return false
}
