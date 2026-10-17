package guard

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/gitx"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// shimTemplate is the pre-commit shim, with @RECORDED@ standing for the
// shell-quoted path of homebot at install time and @REFUSAL@ for the
// shell-quoted line it prints when it finds no homebot.
//
//go:embed pre-commit.sh
var shimTemplate string

const (
	recordedToken = "@RECORDED@"
	refusalToken  = "@REFUSAL@"
)

// hooksDir is the directory core.hooksPath points at.
func hooksDir(c *app.Context) string {
	return filepath.Join(c.DataDir, "hooks")
}

// shimPath is the pre-commit shim git runs.
func shimPath(c *app.Context) string {
	return filepath.Join(hooksDir(c), "pre-commit")
}

// shimParts are the shim's text before and after the recorded path.
func shimParts(c *app.Context) (before, after string) {
	text := strings.Replace(shimTemplate, refusalToken, ui.ShellQuote(voice.Record(c, msg.GuardScriptNoRuntime())), 1)
	before, after, _ = strings.Cut(text, recordedToken)
	return before, after
}

// shim is the shim's text with recorded as the path homebot was at.
func shim(c *app.Context, recorded string) string {
	before, after := shimParts(c)
	return before + ui.ShellQuote(recorded) + after
}

// recordedIn returns the path recorded in content, when content is the shim
// as shim writes it, for some path.
func recordedIn(c *app.Context, content string) (string, bool) {
	before, after := shimParts(c)
	if len(content) < len(before)+len(after) || !strings.HasPrefix(content, before) || !strings.HasSuffix(content, after) {
		return "", false
	}
	quoted := content[len(before) : len(content)-len(after)]
	recorded := quoted
	if inner, ok := strings.CutPrefix(quoted, "'"); ok {
		inner, ok = strings.CutSuffix(inner, "'")
		if !ok {
			return "", false
		}
		recorded = strings.ReplaceAll(inner, `'\''`, "'")
	}
	if ui.ShellQuote(recorded) != quoted {
		return "", false
	}
	return recorded, true
}

// hooksPath is the repo's core.hooksPath as git sees it, "" when unset.
func hooksPath(c *app.Context) string {
	r := gitx.Run(c, c.Root, "config", "--get", "core.hooksPath")
	if r.Code != 0 {
		return ""
	}
	return strings.TrimSuffix(r.Stdout, "\n")
}

// installedShim returns the path recorded in the installed shim, when
// hooksPath, the shim, its exec bit and its content are all as
// InstallHooks leaves them.
func installedShim(c *app.Context) (string, bool) {
	if c.Root == "" || hooksPath(c) != hooksDir(c) {
		return "", false
	}
	info, err := os.Stat(shimPath(c))
	// git runs a hook only when its owner may execute it.
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o100 == 0 {
		return "", false
	}
	content, err := os.ReadFile(shimPath(c))
	if err != nil {
		return "", false
	}
	return recordedIn(c, string(content))
}

// InstallHooks points the repo's local core.hooksPath at DataDir/hooks
// and writes the shim. changed is false when it was already installed, and
// whenever err is set.
//
// The shim records where homebot is now (proc.SelfPath). An installed shim
// is left alone while the homebot it recorded is still there, unless the
// recorded path is versioned. That path must be refreshed after an upgrade.
func InstallHooks(c *app.Context) (changed bool, err error) {
	if err := c.LocateRoot(); err != nil {
		return false, err
	}
	self := proc.SelfPath(c)
	if recorded, ok := installedShim(c); ok && (recorded == self || (!proc.Versioned(recorded) && isExecutable(recorded))) {
		return false, nil
	}
	// The shim first: a hooksPath pointing at a directory without one makes
	// git run no hook at all, silently.
	if err := fsx.WriteFileAtomic(shimPath(c), []byte(shim(c, self)), 0o755); err != nil {
		return false, err
	}
	if r := gitx.Run(c, c.Root, "config", "--local", "core.hooksPath", hooksDir(c)); r.Code != 0 {
		return false, gitFailed(r, "config")
	}
	// A worktree config or an include beats --local; git would then run
	// some other hooks directory, or none, and never the guard.
	if got := hooksPath(c); got != hooksDir(c) {
		return false, fail.Say(msg.GuardHooksPathOverridden(got, hooksDir(c)))
	}
	return true, nil
}

// Installed reports whether hooksPath, the shim, its exec bit and its
// content all match what InstallHooks writes.
func Installed(c *app.Context) bool {
	_, ok := installedShim(c)
	return ok
}

// RecordedPath returns the binary path in an installed guard shim. It only
// reports shims that pass the same content and hooksPath checks as Installed.
func RecordedPath(c *app.Context) (string, bool) { return installedShim(c) }

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}
