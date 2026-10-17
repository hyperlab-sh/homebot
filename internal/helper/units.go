package helper

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func uidString() string { return strconv.Itoa(os.Getuid()) }

func systemdQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `%%`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// execQuote quotes one ExecStart word; systemd also expands $ there.
func execQuote(s string) string {
	return systemdQuote(strings.ReplaceAll(s, `$`, `$$`))
}

var xmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// xdgNames are the XDG base directory variables homebot reads, in the order
// the helper's unit and plist carry them.
var xdgNames = []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"}

var updateEnvNames = []string{"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy", "SSL_CERT_FILE", "SSL_CERT_DIR"}

// updateEnv carries only the proxy and certificate settings captured at
// setup. Invalid XML characters cannot enter a launchd plist; control
// characters also have surprising systemd Environment= semantics.
func updateEnv(c *app.Context) [][2]string {
	var out [][2]string
	for _, name := range updateEnvNames {
		value, ok := c.Env.Lookup(name)
		if !ok || !safeUnitEnv(value) {
			continue
		}
		out = append(out, [2]string{name, value})
	}
	return out
}

func safeUnitEnv(value string) bool {
	if value == "" || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r >= 0xd800 && r <= 0xdfff || r == 0xfffe || r == 0xffff {
			return false
		}
	}
	return true
}

// xdgEnv is the XDG_* variables set in setup's environment, cleaned, for the
// helper to run with: without them it would use the default folders while
// the shell and the CLI use these, two sets of state, cache and lock (final
// review home I2). A relative one is left out; CheckEnv refuses it first.
func xdgEnv(c *app.Context) [][2]string {
	var out [][2]string
	for _, name := range xdgNames {
		if v := c.Env.Get(name); filepath.IsAbs(v) {
			out = append(out, [2]string{name, filepath.Clean(v)})
		}
	}
	return out
}

func tidyArgv(exe string) []string { return []string{exe, "tidy", "--notify", "--quiet", "--helper"} }

// SystemdUnits renders the service and timer unit text. exe is the stable
// binary path (risk R7), never a versioned install directory.
func SystemdUnits(c *app.Context, exe, path string) (service, timer string) {
	var argv []string
	for _, a := range tidyArgv(exe) {
		argv = append(argv, execQuote(a))
	}
	service = "[Unit]\n" +
		"Description=" + voice.Record(c, msg.HelperUnitServiceDescription()) + "\n" +
		"\n[Service]\n" +
		"Type=oneshot\n" +
		"Environment=" + systemdQuote("PATH="+path) + "\n" +
		"Environment=" + systemdQuote("HOMEBOT_ROOT="+c.Root) + "\n"
	for _, kv := range xdgEnv(c) {
		service += "Environment=" + systemdQuote(kv[0]+"="+kv[1]) + "\n"
	}
	for _, kv := range updateEnv(c) {
		service += "Environment=" + systemdQuote(kv[0]+"="+kv[1]) + "\n"
	}
	service += "ExecStart=" + strings.Join(argv, " ") + "\n"
	timer = "[Unit]\n" +
		"Description=" + voice.Record(c, msg.HelperUnitTimerDescription()) + "\n" +
		"\n[Timer]\n" +
		"OnBootSec=5min\n" +
		"OnUnitActiveSec=1h\n" +
		"\n[Install]\n" +
		"WantedBy=timers.target\n"
	return service, timer
}

// LaunchdPlist renders the launchd agent's plist.
func LaunchdPlist(c *app.Context, exe, path string) string {
	var args []string
	for _, a := range tidyArgv(exe) {
		args = append(args, "    <string>"+xmlEscaper.Replace(a)+"</string>")
	}
	var xdg strings.Builder
	for _, kv := range xdgEnv(c) {
		xdg.WriteString("\n    <key>" + kv[0] + "</key>\n    <string>" + xmlEscaper.Replace(kv[1]) + "</string>")
	}
	for _, kv := range updateEnv(c) {
		xdg.WriteString("\n    <key>" + kv[0] + "</key>\n    <string>" + xmlEscaper.Replace(kv[1]) + "</string>")
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>` + label + `</string>
  <key>ProgramArguments</key>
  <array>
` + strings.Join(args, "\n") + `
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>` + xmlEscaper.Replace(path) + `</string>
    <key>HOMEBOT_ROOT</key>
    <string>` + xmlEscaper.Replace(c.Root) + `</string>` + xdg.String() + `
  </dict>
  <key>StartInterval</key>
  <integer>3600</integer>
  <key>RunAtLoad</key>
  <true/>
  <key>StandardErrorPath</key>
  <string>` + xmlEscaper.Replace(filepath.Join(c.StateDir, "agent.log")) + `</string>
</dict>
</plist>
`
}
