// Package helper is the background helper: a systemd user timer on Linux or a
// launchd agent on macOS that runs `homebot tidy --notify --quiet` every hour.
package helper

import (
	"html"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

const (
	unitName = "homebot-tidy"
	label    = "sh.homebot.tidy"
	// StaleAfter is how long since the last tidy run before the helper counts as stale.
	StaleAfter = 6 * time.Hour
)

type unitPaths struct{ service, timer, plist string }

func pathsFor(c *app.Context) unitPaths {
	systemd := filepath.Join(c.Home, ".config", "systemd", "user")
	return unitPaths{
		service: filepath.Join(systemd, unitName+".service"),
		timer:   filepath.Join(systemd, unitName+".timer"),
		plist:   filepath.Join(c.Home, "Library", "LaunchAgents", label+".plist"),
	}
}

// Files lists every file the helper puts in $HOME, including the link that
// `systemctl enable` makes.
func Files(c *app.Context) []string {
	p := pathsFor(c)
	if c.OS != app.Linux {
		return []string{p.plist}
	}
	wants := filepath.Join(c.Home, ".config", "systemd", "user", "timers.target.wants", unitName+".timer")
	return []string{p.service, p.timer, wants}
}

// Installed says whether the helper's timer (Linux) or plist (macOS) exists.
func Installed(c *app.Context) bool {
	p := pathsFor(c)
	file := p.plist
	if c.OS == app.Linux {
		file = p.timer
	}
	_, err := os.Stat(file)
	return err == nil
}

// RecordedPath reads the PATH in an installed helper unit. A missing or
// unfamiliar unit is not treated as a reason to replace a working service.
func RecordedPath(c *app.Context) (string, bool) {
	p := pathsFor(c)
	file := p.plist
	if c.OS == app.Linux {
		file = p.service
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return "", false
	}
	if c.OS == app.Linux {
		for line := range strings.SplitSeq(string(data), "\n") {
			if quoted, ok := strings.CutPrefix(line, "Environment="); ok {
				value, err := strconv.Unquote(quoted)
				if err == nil {
					if path, ok := strings.CutPrefix(value, "PATH="); ok {
						return strings.ReplaceAll(path, "%%", "%"), true
					}
				}
			}
		}
		return "", false
	}
	_, after, ok := strings.Cut(string(data), "<key>PATH</key>")
	if !ok {
		return "", false
	}
	_, after, ok = strings.Cut(after, "<string>")
	if !ok {
		return "", false
	}
	path, _, ok := strings.Cut(after, "</string>")
	return html.UnescapeString(path), ok
}

// LastRun is when the helper last finished a tidy, and false when it never did.
func LastRun(c *app.Context) (time.Time, bool) {
	s, err := state.Load(c)
	if err != nil || s.LastTidy == 0 {
		return time.Time{}, false
	}
	return time.UnixMilli(s.LastTidy), true
}

// Stale says the helper ran before but not within StaleAfter.
func Stale(c *app.Context) bool {
	t, ok := LastRun(c)
	return ok && c.Now().Sub(t) > StaleAfter
}

// NeedsLinger is a heuristic: it says a headless Linux user has no lingering
// session, so the user timer would stop when the last login ends. It checks
// that loginctl exists, that no DISPLAY or WAYLAND_DISPLAY is set, and that
// `loginctl show-user` reports Linger=no. It can misfire (an SSH session into
// a desktop has no display), so callers present it as a hint, not a fault.
func NeedsLinger(c *app.Context) bool {
	if c.OS != app.Linux || !proc.HasCommand(c, "loginctl") {
		return false
	}
	if c.Env.Get("DISPLAY") != "" || c.Env.Get("WAYLAND_DISPLAY") != "" {
		return false
	}
	argv := []string{"loginctl", "show-user"}
	if u := c.Env.Get("USER"); u != "" {
		argv = append(argv, u)
	}
	r := proc.Run(c, append(argv, "--property=Linger"), proc.Options{})
	return r.Code == 0 && strings.TrimSpace(r.Stdout) == "Linger=no"
}

func systemdUserSession(c *app.Context) error {
	if proc.HasCommand(c, "systemctl") && proc.Run(c, []string{"systemctl", "--user", "show-environment"}, proc.Options{}).Code == 0 {
		return nil
	}
	return fail.User(voice.Plain(c, msg.HelperErrorNoSystemd()))
}

func envPath(c *app.Context) string {
	if p := c.Env.Get("PATH"); p != "" {
		return p
	}
	return "/usr/bin:/bin"
}

// CheckEnv refuses a relative XDG_* variable. homebot ignores one (as the
// XDG spec says) and uses the default folder, the shell may not, and the
// helper, which runs from no folder in particular, can't use it: refusing
// keeps setup from installing a helper that works in other folders than the
// user's shell.
func CheckEnv(c *app.Context) error {
	for _, name := range xdgNames {
		if v := c.Env.Get(name); v != "" && !filepath.IsAbs(v) {
			return fail.User(voice.Plain(c, msg.SetupErrorXdgRelative(name, v)))
		}
	}
	return nil
}

// Install writes the unit files (or plist) and starts the timer. On Linux it
// refuses before writing anything when there is no systemd user session.
func Install(c *app.Context) error {
	if err := CheckEnv(c); err != nil {
		return err
	}
	p := pathsFor(c)
	if c.OS == app.Linux {
		if err := systemdUserSession(c); err != nil {
			return err
		}
		service, timer := SystemdUnits(c, proc.SelfPath(c), envPath(c))
		if err := fsx.WriteFileAtomic(p.service, []byte(service), 0o644); err != nil {
			return err
		}
		if err := fsx.WriteFileAtomic(p.timer, []byte(timer), 0o644); err != nil {
			return err
		}
		for _, args := range [][]string{{"daemon-reload"}, {"enable", "--now", unitName + ".timer"}} {
			r := proc.Run(c, append([]string{"systemctl", "--user"}, args...), proc.Options{})
			if r.Code != 0 {
				return fail.User(fail.Failure(voice.Plain(c, msg.HelperErrorSystemctl(strings.Join(args, " "))), r.Stderr, "systemctl"))
			}
		}
		return nil
	}
	if err := os.MkdirAll(c.StateDir, 0o755); err != nil {
		return err
	}
	if err := fsx.WriteFileAtomic(p.plist, []byte(LaunchdPlist(c, proc.SelfPath(c), envPath(c))), 0o644); err != nil {
		return err
	}
	domain := "gui/" + uidString()
	proc.Run(c, []string{"launchctl", "bootout", domain + "/" + label}, proc.Options{})
	r := proc.Run(c, []string{"launchctl", "bootstrap", domain, p.plist}, proc.Options{})
	if r.Code != 0 {
		return fail.User(fail.Failure(voice.Plain(c, msg.HelperErrorLaunchctl()), r.Stderr, "launchctl"))
	}
	return nil
}

// Uninstall stops the helper and removes its files. Failures of the service
// manager are ignored: the helper may already be gone.
func Uninstall(c *app.Context) error {
	p := pathsFor(c)
	if c.OS == app.Linux {
		proc.Run(c, []string{"systemctl", "--user", "disable", "--now", unitName + ".timer"}, proc.Options{})
		wants := filepath.Join(c.Home, ".config", "systemd", "user", "timers.target.wants", unitName+".timer")
		for _, f := range []string{p.service, p.timer, wants} {
			if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		proc.Run(c, []string{"systemctl", "--user", "daemon-reload"}, proc.Options{})
		return nil
	}
	proc.Run(c, []string{"launchctl", "bootout", "gui/" + uidString() + "/" + label}, proc.Options{})
	if err := os.Remove(p.plist); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
