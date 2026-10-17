package proc

import (
	"os"
	"path/filepath"

	"github.com/hyperlab-sh/homebot/internal/app"
)

// selfName is the base name of every path SelfPath gives.
const selfName = "homebot"

// SelfPath is the path to run homebot by: the one on the Context's PATH, with
// symlinks left alone, so a package upgrade that moves the real binary (a
// versioned Cellar directory) does not break what points at it. A PATH entry
// inside a versioned Caskroom or Cellar directory is skipped. It falls back
// to the running binary, c.Exe, when homebot is not on PATH, and prefers the
// stable Homebrew link (<prefix>/bin/homebot) over a versioned c.Exe.
//
// Its base is homebot (naming plan D4). On macOS c.Exe is the path as
// invoked, so a run through the name's link (~/.local/bin/zork) gives the
// link: then SelfPath follows exactly one os.Readlink hop, to the link's
// target (~/.local/bin/homebot for the front door, <prefix>/bin/homebot for
// Homebrew). It never resolves every link (filepath.EvalSymlinks), which
// would reach the versioned Caskroom or Cellar path. The helper's unit and
// plist and the guard's shim record what it gives, so a rename or an upgrade
// must not break it. If no stable homebot path can be found, return empty:
// callers must not persist a name link or a versioned path as their command.
func SelfPath(c *app.Context) string {
	// Not LookPath: the program log is for programs homebot runs.
	if p, err := lookPath(c.Env, selfName); err == nil {
		if abs, err := filepath.Abs(p); err == nil && !Versioned(abs) {
			return abs
		}
	}
	exe := oneHop(c.Exe)
	if stable := stablePath(exe); stable != "" && isExecutable(stable) {
		return stable
	}
	return exe
}

// oneHop is exe when its base is homebot, or the target of exactly one
// symlink hop when that target's base is homebot. Otherwise no stable path
// exists yet, so return empty.
func oneHop(exe string) string {
	if filepath.Base(exe) == selfName {
		return exe
	}
	target, err := os.Readlink(exe)
	if err != nil {
		return ""
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(exe), target)
	}
	if target = filepath.Clean(target); filepath.Base(target) == selfName {
		return target
	}
	return ""
}

// Versioned reports whether path is a homebot binary inside a versioned
// Homebrew Caskroom or Cellar directory. It does not resolve symlinks.
func Versioned(path string) bool {
	return stablePath(path) != ""
}

func stablePath(path string) string {
	if !filepath.IsAbs(path) || filepath.Base(path) != "homebot" {
		return ""
	}
	dir := filepath.Dir(path)
	if filepath.Base(dir) == "bin" { // Cellar/homebot/<version>/bin/homebot
		dir = filepath.Dir(dir)
		if filepath.Base(filepath.Dir(dir)) != "homebot" {
			return ""
		}
		packageDir := filepath.Dir(dir)
		if filepath.Base(filepath.Dir(packageDir)) != "Cellar" {
			return ""
		}
		return filepath.Join(filepath.Dir(filepath.Dir(packageDir)), "bin", "homebot")
	}
	// Caskroom/homebot/<version>/homebot
	if filepath.Base(filepath.Dir(dir)) != "homebot" {
		return ""
	}
	packageDir := filepath.Dir(dir)
	if filepath.Base(filepath.Dir(packageDir)) != "Caskroom" {
		return ""
	}
	return filepath.Join(filepath.Dir(filepath.Dir(packageDir)), "bin", "homebot")
}
