package update

import (
	"errors"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/ui"
)

type Build struct {
	Version string
	Channel string
}

type Method string

const (
	Source  Method = "source"
	Go      Method = "go"
	Cask    Method = "cask"
	Formula Method = "formula"
	Script  Method = "script"
	Deb     Method = "deb"
	RPM     Method = "rpm"
	Unknown Method = "unknown"
)

type Install struct {
	Method  Method
	Version string
	Binary  string
	Prefix  string
	Arch    string
	OS      app.OS
}

var systemBinary = "/usr/bin/homebot" // overridden only by package tests

func resolved(path string) string {
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	return p
}

func brewPrefix(path, layout string) string {
	if !filepath.IsAbs(path) || filepath.Base(path) != "homebot" {
		return ""
	}
	dir := filepath.Dir(path)
	if layout == "Cellar" {
		if filepath.Base(dir) != "bin" {
			return ""
		}
		dir = filepath.Dir(dir)
	}
	if filepath.Base(filepath.Dir(dir)) != "homebot" {
		return ""
	}
	packageDir := filepath.Dir(dir)
	if filepath.Base(filepath.Dir(packageDir)) != layout {
		return ""
	}
	return filepath.Dir(filepath.Dir(packageDir))
}

func receiptBinary(c *app.Context) string {
	data, err := os.ReadFile(filepath.Join(c.DataDir, "install-receipt"))
	if err != nil {
		return ""
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" || value == "" || strings.ContainsAny(value, "\r\n") {
			return ""
		}
		if _, duplicate := values[key]; duplicate {
			return ""
		}
		values[key] = value
	}
	if values["method"] != "script" || !filepath.IsAbs(values["bin"]) || values["version"] == "" || values["installed"] == "" {
		return ""
	}
	if _, err := parseVersion(values["version"]); err != nil {
		return ""
	}
	if _, err := strconv.ParseInt(values["installed"], 10, 64); err != nil {
		return ""
	}
	return values["bin"]
}

// Detect classifies the running executable in the order specified by S3.
func Detect(c *app.Context, b Build, info *debug.BuildInfo) Install {
	i := Install{Method: Unknown, Version: b.Version, Binary: c.Exe, Arch: c.Arch, OS: c.OS}
	if b.Channel != "release" {
		i.Method = Source
		if info != nil && info.Main.Sum != "" {
			i.Method = Go
		}
		return i
	}
	bin := resolved(c.Exe)
	if bin == "" {
		return i
	}
	if prefix := brewPrefix(bin, "Caskroom"); prefix != "" {
		i.Method, i.Prefix, i.Binary = Cask, prefix, filepath.Join(prefix, "bin", "homebot")
		return i
	}
	if prefix := brewPrefix(bin, "Cellar"); prefix != "" {
		i.Method, i.Prefix, i.Binary = Formula, prefix, filepath.Join(prefix, "bin", "homebot")
		return i
	}
	if rb := receiptBinary(c); rb != "" && resolved(rb) == bin {
		i.Method, i.Binary = Script, rb
		return i
	}
	// bin is symlink-resolved, so the configured system path must be too
	// (macOS /var -> /private/var, merged-/usr distros, ...).
	if sys := resolved(systemBinary); sys != "" && bin == sys {
		if r := proc.Run(c, []string{"dpkg-query", "-S", bin}, proc.Options{}); r.Code == 0 && strings.Contains(r.Stdout, "homebot:") {
			i.Method = Deb
			return i
		}
		if r := proc.Run(c, []string{"rpm", "-qf", bin}, proc.Options{}); r.Code == 0 && strings.Contains(r.Stdout, "homebot") {
			i.Method = RPM
			return i
		}
	}
	return i
}

var (
	ErrNeedsRoot = errors.New("upgrade needs root")
	ErrManual    = errors.New("manual upgrade required")
)

type Upgrade struct {
	Method  Method
	Version string
	Current string
	Binary  string
	Prefix  string
	Argv    []string
	Text    string
	BaseURL string  // test-only release seam
	Found   *string // optional result sink for the post-check version
}

// Plan returns the command to run or show. It never executes a command.
func Plan(i Install, r Release) Upgrade {
	u := Upgrade{Method: i.Method, Version: r.Version, Current: i.Version, Binary: i.Binary, Prefix: i.Prefix}
	switch i.Method {
	case Cask:
		u.Argv = []string{filepath.Join(i.Prefix, "bin", "brew"), "upgrade", "--cask", "hyperlab-sh/tap/homebot"}
	case Formula:
		u.Argv = []string{filepath.Join(i.Prefix, "bin", "brew"), "upgrade", "homebot"}
	case Script:
		u.Argv = []string{"/bin/sh", "-s", "--", "--version", r.Tag, "--bin-dir", filepath.Dir(i.Binary), "--require-signature", "--max-time", "60"}
	case Deb, RPM:
		arch := i.Arch
		switch arch {
		case "x64", "amd64":
			arch = "amd64"
		}
		if i.Method == Deb {
			name := "homebot_" + r.Version + "_" + arch + ".deb"
			u.Text = "curl -LO " + releaseBase + "/releases/download/" + r.Tag + "/" + name + " && sudo apt install ./" + name
		} else {
			switch arch {
			case "amd64":
				arch = "x86_64"
			case "arm64":
				arch = "aarch64"
			}
			name := "homebot-" + r.Version + "-1." + arch + ".rpm"
			u.Text = "curl -LO " + releaseBase + "/releases/download/" + r.Tag + "/" + name + " && sudo dnf install ./" + name
		}
	case Go:
		u.Text = "go install github.com/hyperlab-sh/homebot/cmd/homebot@" + r.Tag
	case Unknown:
		u.Text = r.URL
	}
	return u
}

// ManualCommand is the command the inbox and version page can show a person.
// It never invokes a downloader or an upgrade.
func ManualCommand(i Install, r Release) string {
	u := Plan(i, r)
	if u.Text != "" {
		return u.Text
	}
	if i.Method == Script {
		installer := releaseBase + "/releases/download/" + r.Tag + "/install.sh"
		return "curl --proto '=https' --tlsv1.2 -fsSL " + ui.ShellQuote(installer) + " | /bin/sh -s -- --version " + ui.ShellQuote(r.Tag) + " --bin-dir " + ui.ShellQuote(filepath.Dir(i.Binary)) + " --require-signature"
	}
	var words []string
	for _, arg := range u.Argv {
		words = append(words, ui.ShellQuote(arg))
	}
	return strings.Join(words, " ")
}
