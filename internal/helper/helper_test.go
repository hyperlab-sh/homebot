package helper_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/helper"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/selfname"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

const exe = "/home/linuxbrew/.linuxbrew/bin/homebot"

func box(t *testing.T, os app.OS) *testutil.Box {
	t.Helper()
	b := testutil.Sandbox(t, testutil.Options{OS: os})
	b.Ctx.Exe = exe
	// Only $WORK/bin is on PATH, so a test can never reach the host's systemctl,
	// launchctl or loginctl; what a test needs it stubs.
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": b.Bin})
	return b
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func calls(t *testing.T, b *testutil.Box) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(b.Dir, "calls"))
	if err != nil {
		return ""
	}
	return string(data)
}

func recorder(b *testutil.Box, name, exit string) {
	b.WithStubs(map[string]string{name: `echo "` + name + ` $*" >> "` + b.Dir + `/calls"; ` + exit})
}

func TestInstallWithoutSystemctlRefusesAndWritesNothing(t *testing.T) {
	b := box(t, app.Linux)
	err := helper.Install(b.Ctx)
	var ue *fail.UserError
	if !errors.As(err, &ue) || !strings.Contains(ue.Error(), "No systemd user session here") {
		t.Fatalf("err = %v", err)
	}
	if exists(filepath.Join(b.Home, ".config", "systemd")) {
		t.Fatal("systemd dir was written")
	}
}

func TestInstallWithoutUserSessionRefuses(t *testing.T) {
	b := box(t, app.Linux)
	recorder(b, "systemctl", `echo "Failed to connect to bus" >&2; exit 1`)
	err := helper.Install(b.Ctx)
	if err == nil || !strings.Contains(err.Error(), "Run this yourself, or from cron:\n  homebot tidy") {
		t.Fatalf("err = %v", err)
	}
	if got := calls(t, b); got != "systemctl --user show-environment\n" {
		t.Fatalf("stub systemctl calls = %q", got)
	}
	if exists(filepath.Join(b.Home, ".config", "systemd")) {
		t.Fatal("systemd dir was written")
	}
}

func TestInstallWithWorkingUserSession(t *testing.T) {
	b := box(t, app.Linux)
	recorder(b, "systemctl", "exit 0")
	if err := helper.Install(b.Ctx); err != nil {
		t.Fatal(err)
	}
	if !helper.Installed(b.Ctx) {
		t.Fatal("not installed")
	}
	want := "systemctl --user show-environment\nsystemctl --user daemon-reload\nsystemctl --user enable --now homebot-tidy.timer\n"
	if got := calls(t, b); got != want {
		t.Fatalf("systemctl calls:\n%s\nwant:\n%s", got, want)
	}
	dir := filepath.Join(b.Home, ".config", "systemd", "user")
	service, _ := os.ReadFile(filepath.Join(dir, "homebot-tidy.service"))
	if !strings.Contains(string(service), `ExecStart="`+exe+`" "tidy" "--notify" "--quiet" "--helper"`+"\n") {
		t.Fatalf("service:\n%s", service)
	}
}

func TestRecordedPathReadsBothUnitFormats(t *testing.T) {
	for _, sys := range []app.OS{app.Linux, app.MacOS} {
		t.Run(string(sys), func(t *testing.T) {
			b := box(t, sys)
			want := filepath.Join(b.Home, "local & bin") + ":/usr/bin:/bin"
			var file, text string
			if sys == app.Linux {
				file = filepath.Join(b.Home, ".config", "systemd", "user", "homebot-tidy.service")
				text, _ = helper.SystemdUnits(b.Ctx, exe, want)
			} else {
				file = filepath.Join(b.Home, "Library", "LaunchAgents", "sh.homebot.tidy.plist")
				text = helper.LaunchdPlist(b.Ctx, exe, want)
			}
			if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
			if got, ok := helper.RecordedPath(b.Ctx); !ok || got != want {
				t.Errorf("RecordedPath = %q, %v; want %q", got, ok, want)
			}
		})
	}
}

func TestInstallReportsSystemctlFailure(t *testing.T) {
	b := box(t, app.Linux)
	b.WithStubs(map[string]string{"systemctl": `[ "$2" = show-environment ] && exit 0; echo "boom" >&2; exit 1`})
	err := helper.Install(b.Ctx)
	if err == nil || !strings.Contains(err.Error(), "systemctl --user daemon-reload failed.") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestSystemdUnitsGolden(t *testing.T) {
	b := box(t, app.Linux)
	service, timer := helper.SystemdUnits(b.Ctx, exe, "/usr/bin:/bin")
	wantService := `[Unit]
Description=homebot: keep the home repo tidy and say what needs attention

[Service]
Type=oneshot
Environment="PATH=/usr/bin:/bin"
Environment="HOMEBOT_ROOT=` + b.Root + `"
Environment="XDG_CONFIG_HOME=` + b.Dir + `/xdg/config"
Environment="XDG_DATA_HOME=` + b.Dir + `/xdg/data"
Environment="XDG_STATE_HOME=` + b.Dir + `/xdg/state"
Environment="XDG_CACHE_HOME=` + b.Dir + `/xdg/cache"
ExecStart="` + exe + `" "tidy" "--notify" "--quiet" "--helper"
`
	wantTimer := `[Unit]
Description=Run homebot tidy every hour

[Timer]
OnBootSec=5min
OnUnitActiveSec=1h

[Install]
WantedBy=timers.target
`
	if service != wantService {
		t.Errorf("service:\n%s\nwant:\n%s", service, wantService)
	}
	if timer != wantTimer {
		t.Errorf("timer:\n%s\nwant:\n%s", timer, wantTimer)
	}
}

func TestUnitsCaptureUpdateProxyAndCertificateEnvironment(t *testing.T) {
	for _, sys := range []app.OS{app.Linux, app.MacOS} {
		t.Run(string(sys), func(t *testing.T) {
			b := box(t, sys)
			b.Ctx.Env = b.Ctx.Env.With(map[string]string{
				"HTTPS_PROXY":   "https://proxy.example/a%20 path?x=1&y=2",
				"no_proxy":      "localhost,127.0.0.1",
				"SSL_CERT_FILE": "/tmp/cert bundle.pem",
				"HTTP_PROXY":    "bad\ninjected=1",   // refused, not a new unit line
				"SSL_CERT_DIR":  "/tmp/certs\x01bad", // illegal XML control byte
			})
			if sys == app.Linux {
				service, _ := helper.SystemdUnits(b.Ctx, exe, "/usr/bin")
				for _, want := range []string{
					`Environment="HTTPS_PROXY=https://proxy.example/a%%20 path?x=1&y=2"`,
					`Environment="no_proxy=localhost,127.0.0.1"`,
					`Environment="SSL_CERT_FILE=/tmp/cert bundle.pem"`,
					`"--helper"`,
				} {
					if !strings.Contains(service, want) {
						t.Errorf("service missing %q: %s", want, service)
					}
				}
				if strings.Contains(service, "injected") || strings.Contains(service, "HTTP_PROXY=") || strings.Contains(service, "SSL_CERT_DIR=") {
					t.Errorf("unsafe environment entered service: %s", service)
				}
				return
			}
			plist := helper.LaunchdPlist(b.Ctx, exe, "/usr/bin")
			for _, want := range []string{
				`<key>HTTPS_PROXY</key>`,
				`<string>https://proxy.example/a%20 path?x=1&amp;y=2</string>`,
				`<key>no_proxy</key>`,
				`<key>SSL_CERT_FILE</key>`,
				`<string>--helper</string>`,
			} {
				if !strings.Contains(plist, want) {
					t.Errorf("plist missing %q: %s", want, plist)
				}
			}
			if strings.Contains(plist, "injected") || strings.Contains(plist, "HTTP_PROXY") || strings.Contains(plist, "SSL_CERT_DIR") {
				t.Errorf("unsafe environment entered plist: %s", plist)
			}
		})
	}
}

func TestInstallUsesStableHomebrewPath(t *testing.T) {
	for _, sys := range []app.OS{app.Linux, app.MacOS} {
		t.Run(string(sys), func(t *testing.T) {
			b := box(t, sys)
			prefix := filepath.Join(b.Dir, "brew prefix")
			versioned := filepath.Join(prefix, "Caskroom", "homebot", "0.1.0", "homebot")
			stable := filepath.Join(prefix, "bin", "homebot")
			if err := os.MkdirAll(filepath.Dir(versioned), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(versioned, []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(stable), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(versioned, stable); err != nil {
				t.Fatal(err)
			}
			b.Ctx.Exe = versioned
			if got := proc.SelfPath(b.Ctx); got != stable {
				t.Fatalf("SelfPath = %q, want %q", got, stable)
			}
			if sys == app.Linux {
				recorder(b, "systemctl", "exit 0")
			} else {
				recorder(b, "launchctl", "exit 0")
			}
			if err := helper.Install(b.Ctx); err != nil {
				t.Fatal(err)
			}
			var unit []byte
			var err error
			if sys == app.Linux {
				unit, err = os.ReadFile(filepath.Join(b.Home, ".config", "systemd", "user", "homebot-tidy.service"))
			} else {
				unit, err = os.ReadFile(filepath.Join(b.Home, "Library", "LaunchAgents", "sh.homebot.tidy.plist"))
			}
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(unit), stable) || strings.Contains(string(unit), versioned) {
				t.Errorf("unit uses versioned path instead of stable path:\n%s", unit)
			}
		})
	}
}

// The helper runs with the XDG_* folders setup saw, so it shares state,
// cache and lock with the shell and the CLI (final review home I2). Only
// the ones set are written, cleaned; with none set the helper uses the
// defaults, as the shell does.
func TestUnitsCarryOnlyTheXDGVariablesSet(t *testing.T) {
	for _, sys := range []app.OS{app.Linux, app.MacOS} {
		t.Run(string(sys), func(t *testing.T) {
			b := box(t, sys)
			b.Ctx.Env = b.Ctx.Env.With(map[string]string{
				"XDG_CONFIG_HOME": "",
				"XDG_DATA_HOME":   "",
				"XDG_CACHE_HOME":  "",
				"XDG_STATE_HOME":  "/srv/me/state/",
			})
			var text string
			if sys == app.Linux {
				text, _ = helper.SystemdUnits(b.Ctx, exe, "/usr/bin")
			} else {
				text = helper.LaunchdPlist(b.Ctx, exe, "/usr/bin")
			}
			for _, name := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
				if strings.Contains(text, name) {
					t.Errorf("%s written though unset:\n%s", name, text)
				}
			}
			want := `Environment="XDG_STATE_HOME=/srv/me/state"`
			if sys != app.Linux {
				want = "<key>XDG_STATE_HOME</key>\n    <string>/srv/me/state</string>"
			}
			if !strings.Contains(text, want) {
				t.Errorf("missing %q in\n%s", want, text)
			}
		})
	}
}

func TestUnitsQuoteXDGValues(t *testing.T) {
	b := box(t, app.Linux)
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"XDG_CACHE_HOME": `/x/100%"q"`})
	service, _ := helper.SystemdUnits(b.Ctx, exe, "/usr/bin")
	if want := `Environment="XDG_CACHE_HOME=/x/100%%\"q\""`; !strings.Contains(service, want) {
		t.Errorf("missing %q in\n%s", want, service)
	}
	m := box(t, app.MacOS)
	m.Ctx.Env = m.Ctx.Env.With(map[string]string{"XDG_CACHE_HOME": "/x/a&b<c"})
	if want := "<string>/x/a&amp;b&lt;c</string>"; !strings.Contains(helper.LaunchdPlist(m.Ctx, exe, "/usr/bin"), want) {
		t.Errorf("plist does not escape the XDG value")
	}
}

// A relative XDG_* is refused before anything is written: homebot ignores
// it and the helper could not use it.
func TestInstallRefusesARelativeXDGVariable(t *testing.T) {
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Run(name, func(t *testing.T) {
			b := box(t, app.Linux)
			recorder(b, "systemctl", "exit 0")
			b.Ctx.Env = b.Ctx.Env.With(map[string]string{name: "rel/dir"})
			err := helper.Install(b.Ctx)
			if err == nil || !strings.Contains(err.Error(), name+` is "rel/dir", which is not a full path`) {
				t.Fatalf("err = %v", err)
			}
			if helper.Installed(b.Ctx) {
				t.Error("installed anyway")
			}
			if exists(filepath.Join(b.Dir, "calls")) {
				t.Errorf("systemctl ran:\n%s", calls(t, b))
			}
		})
	}
}

func TestSystemdUnitsQuoteSpecialCharacters(t *testing.T) {
	b := box(t, app.Linux)
	service, _ := helper.SystemdUnits(b.Ctx, `/opt/my "bin"/100%/$x/homebot`, `/a\b%h$HOME`)
	for _, want := range []string{`Environment="PATH=/a\\b%%h$HOME"`, `ExecStart="/opt/my \"bin\"/100%%/$$x/homebot" "tidy"`} {
		if !strings.Contains(service, want) {
			t.Errorf("missing %q in\n%s", want, service)
		}
	}
}

func TestLaunchdPlistGolden(t *testing.T) {
	b := box(t, app.MacOS)
	got := helper.LaunchdPlist(b.Ctx, "/opt/homebrew/bin/homebot", "/usr/bin:/a&b")
	want := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>sh.homebot.tidy</string>
  <key>ProgramArguments</key>
  <array>
    <string>/opt/homebrew/bin/homebot</string>
    <string>tidy</string>
    <string>--notify</string>
    <string>--quiet</string>
    <string>--helper</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>/usr/bin:/a&amp;b</string>
    <key>HOMEBOT_ROOT</key>
    <string>` + b.Root + `</string>
    <key>XDG_CONFIG_HOME</key>
    <string>` + b.Dir + `/xdg/config</string>
    <key>XDG_DATA_HOME</key>
    <string>` + b.Dir + `/xdg/data</string>
    <key>XDG_STATE_HOME</key>
    <string>` + b.Dir + `/xdg/state</string>
    <key>XDG_CACHE_HOME</key>
    <string>` + b.Dir + `/xdg/cache</string>
  </dict>
  <key>StartInterval</key>
  <integer>3600</integer>
  <key>RunAtLoad</key>
  <true/>
  <key>StandardErrorPath</key>
  <string>` + b.Ctx.StateDir + `/agent.log</string>
</dict>
</plist>
`
	if got != want {
		t.Fatalf("plist:\n%s\nwant:\n%s", got, want)
	}
}

func TestInstallAndUninstallOnMacOS(t *testing.T) {
	b := box(t, app.MacOS)
	recorder(b, "launchctl", "exit 0")
	if err := helper.Install(b.Ctx); err != nil {
		t.Fatal(err)
	}
	if !helper.Installed(b.Ctx) {
		t.Fatal("not installed")
	}
	log := calls(t, b)
	if !strings.Contains(log, "launchctl bootout gui/") || !strings.Contains(log, "launchctl bootstrap gui/") {
		t.Fatalf("launchctl calls:\n%s", log)
	}
	if err := helper.Uninstall(b.Ctx); err != nil {
		t.Fatal(err)
	}
	if helper.Installed(b.Ctx) {
		t.Fatal("still installed")
	}
}

func TestInstallReportsLaunchctlFailure(t *testing.T) {
	b := box(t, app.MacOS)
	b.WithStubs(map[string]string{"launchctl": `[ "$1" = bootout ] && exit 0; echo nope >&2; exit 5`})
	err := helper.Install(b.Ctx)
	if err == nil || !strings.Contains(err.Error(), "launchctl bootstrap failed.") || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v", err)
	}
}

func TestUninstallOnLinuxRemovesFilesAndToleratesFailures(t *testing.T) {
	b := box(t, app.Linux)
	recorder(b, "systemctl", "exit 0")
	if err := helper.Install(b.Ctx); err != nil {
		t.Fatal(err)
	}
	b.WithStubs(map[string]string{"systemctl": "exit 1"})
	if err := helper.Uninstall(b.Ctx); err != nil {
		t.Fatal(err)
	}
	if helper.Installed(b.Ctx) {
		t.Fatal("still installed")
	}
	if err := helper.Uninstall(b.Ctx); err != nil {
		t.Fatalf("second uninstall: %v", err)
	}
}

func TestFiles(t *testing.T) {
	b := box(t, app.Linux)
	got := helper.Files(b.Ctx)
	if len(got) != 3 || !strings.HasSuffix(got[2], "timers.target.wants/homebot-tidy.timer") {
		t.Fatalf("linux files = %v", got)
	}
	m := box(t, app.MacOS)
	if got := helper.Files(m.Ctx); len(got) != 1 || !strings.HasSuffix(got[0], "Library/LaunchAgents/sh.homebot.tidy.plist") {
		t.Fatalf("macos files = %v", got)
	}
}

func TestStaleness(t *testing.T) {
	b := box(t, app.Linux)
	if _, ok := helper.LastRun(b.Ctx); ok || helper.Stale(b.Ctx) {
		t.Fatal("never ran must not be stale")
	}
	now := b.Ctx.Now()
	set := func(ago time.Duration) {
		if err := state.Update(b.Ctx, func(s *state.State) { s.LastTidy = now.Add(-ago).UnixMilli() }); err != nil {
			t.Fatal(err)
		}
	}
	set(5 * time.Hour)
	if helper.Stale(b.Ctx) {
		t.Fatal("5h is fresh")
	}
	set(7 * time.Hour)
	if !helper.Stale(b.Ctx) {
		t.Fatal("7h is stale")
	}
}

func TestNeedsLinger(t *testing.T) {
	cases := []struct {
		name string
		os   app.OS
		stub string
		env  map[string]string
		want bool
	}{
		{"headless without linger", app.Linux, `echo Linger=no`, nil, true},
		{"headless with linger", app.Linux, `echo Linger=yes`, nil, false},
		{"desktop session", app.Linux, `echo Linger=no`, map[string]string{"DISPLAY": ":0"}, false},
		{"loginctl fails", app.Linux, `exit 1`, nil, false},
		{"macOS", app.MacOS, `echo Linger=no`, nil, false},
		{"no loginctl", app.Linux, "", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := box(t, tc.os)
			if tc.stub != "" {
				b.WithStubs(map[string]string{"loginctl": tc.stub})
			}
			c := *b.Ctx
			c.Env = c.Env.With(tc.env)
			if got := helper.NeedsLinger(&c); got != tc.want {
				t.Fatalf("NeedsLinger = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestUnitsCarryTheUnresolvedPath(t *testing.T) {
	b := box(t, app.Linux)
	real := filepath.Join(b.Dir, "Cellar", "homebot", "1.2.3", "bin")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(real, "homebot")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(b.Bin, "homebot")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	b.Ctx.Exe = target
	recorder(b, "systemctl", "exit 0")
	if err := helper.Install(b.Ctx); err != nil {
		t.Fatal(err)
	}
	service, _ := os.ReadFile(filepath.Join(b.Home, ".config", "systemd", "user", "homebot-tidy.service"))
	if !strings.Contains(string(service), `ExecStart="`+link+`" "tidy"`) || strings.Contains(string(service), "Cellar") {
		t.Fatalf("service:\n%s", service)
	}
	if got := calls(t, b); !strings.Contains(got, "enable --now") {
		t.Fatalf("stub systemctl not used: %q", got)
	}
}

func TestUnitsFallBackToTheRunningBinary(t *testing.T) {
	b := box(t, app.Linux)
	recorder(b, "systemctl", "exit 0")
	if err := helper.Install(b.Ctx); err != nil {
		t.Fatal(err)
	}
	service, _ := os.ReadFile(filepath.Join(b.Home, ".config", "systemd", "user", "homebot-tidy.service"))
	if !strings.Contains(string(service), `ExecStart="`+exe+`"`) {
		t.Fatalf("service:\n%s", service)
	}
}

func TestMacOSPlistCarriesTheUnresolvedPath(t *testing.T) {
	b := box(t, app.MacOS)
	link := filepath.Join(b.Bin, "homebot")
	target := filepath.Join(b.Dir, "Caskroom", "homebot", "1.0", "homebot")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	recorder(b, "launchctl", "exit 0")
	if err := helper.Install(b.Ctx); err != nil {
		t.Fatal(err)
	}
	plist, _ := os.ReadFile(filepath.Join(b.Home, "Library", "LaunchAgents", "sh.homebot.tidy.plist"))
	if !strings.Contains(string(plist), "<string>"+link+"</string>") {
		t.Fatalf("plist:\n%s", plist)
	}
}

func TestUninstallRemovesTheEnableLinkEvenWhenDisableFails(t *testing.T) {
	b := box(t, app.Linux)
	recorder(b, "systemctl", "exit 0")
	if err := helper.Install(b.Ctx); err != nil {
		t.Fatal(err)
	}
	wants := filepath.Join(b.Home, ".config", "systemd", "user", "timers.target.wants")
	if err := os.MkdirAll(wants, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nowhere", filepath.Join(wants, "homebot-tidy.timer")); err != nil {
		t.Fatal(err)
	}
	b.WithStubs(map[string]string{"systemctl": "exit 1"})
	if err := helper.Uninstall(b.Ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(wants, "homebot-tidy.timer")); err == nil {
		t.Fatal("enable link left behind")
	}
}

// nameLinkExe lays out Homebrew's cask with the user's name: c.Exe is
// ~/.local/bin/zork -> <p>/bin/homebot -> <p>/Caskroom/homebot/<v>/homebot,
// as macOS's os.Executable gives it when the user types zork, and homebot
// is not on the Context's PATH. It returns the stable <p>/bin/homebot and
// the Caskroom folder (naming plan D4).
func nameLinkExe(t *testing.T, b *testutil.Box) (stable, caskroom string) {
	t.Helper()
	prefix := filepath.Join(b.Dir, "opt", "homebrew")
	caskroom = filepath.Join(prefix, "Caskroom")
	versioned := filepath.Join(caskroom, "homebot", "1.0", "homebot")
	stable = filepath.Join(prefix, "bin", "homebot")
	zork := filepath.Join(b.Home, ".local", "bin", "zork")
	for _, d := range []string{filepath.Dir(versioned), filepath.Dir(stable), filepath.Dir(zork)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(versioned, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(versioned, stable); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(stable, zork); err != nil {
		t.Fatal(err)
	}
	b.Ctx.Exe = zork
	return stable, caskroom
}

func TestUnitsRecordTheStablePathWhenRunThroughTheName(t *testing.T) {
	b := box(t, app.Linux)
	stable, caskroom := nameLinkExe(t, b)
	recorder(b, "systemctl", "exit 0")
	if err := helper.Install(b.Ctx); err != nil {
		t.Fatal(err)
	}
	service, _ := os.ReadFile(filepath.Join(b.Home, ".config", "systemd", "user", "homebot-tidy.service"))
	if !strings.Contains(string(service), `ExecStart="`+stable+`" "tidy"`) || strings.Contains(string(service), "zork") || strings.Contains(string(service), caskroom) {
		t.Fatalf("service:\n%s", service)
	}
}

func TestMacOSPlistRecordsTheStablePathWhenRunThroughTheName(t *testing.T) {
	b := box(t, app.MacOS)
	stable, caskroom := nameLinkExe(t, b)
	recorder(b, "launchctl", "exit 0")
	if err := helper.Install(b.Ctx); err != nil {
		t.Fatal(err)
	}
	plist, _ := os.ReadFile(filepath.Join(b.Home, "Library", "LaunchAgents", "sh.homebot.tidy.plist"))
	if !strings.Contains(string(plist), "<string>"+stable+"</string>") || strings.Contains(string(plist), "zork") || strings.Contains(string(plist), caskroom) {
		t.Fatalf("plist:\n%s", plist)
	}
}

// The stored unit and plist descriptions stay neutral even when this machine
// has a working recorded name and terminal output would say zork.
func TestUnitsKeepNeutralTextWithAnEffectiveName(t *testing.T) {
	for _, sys := range []app.OS{app.Linux, app.MacOS} {
		t.Run(string(sys), func(t *testing.T) {
			b := box(t, sys)
			stable, _ := nameLinkExe(t, b)
			if err := selfname.Adopt(b.Ctx, "zork"); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(b.Home, ".local", "bin")
			path := bin + string(filepath.ListSeparator) + b.Bin
			b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": path})
			if got := selfname.Effective(b.Ctx); got != "zork" {
				t.Fatalf("effective name = %q, want zork", got)
			}
			var unit string
			if sys == app.Linux {
				service, timer := helper.SystemdUnits(b.Ctx, stable, path)
				unit = service + timer
			} else {
				unit = helper.LaunchdPlist(b.Ctx, stable, path)
			}
			if sys == app.Linux && !strings.Contains(unit, "homebot: keep the home repo tidy") {
				t.Errorf("service description changed with machine name:\n%s", unit)
			}
			if strings.Contains(unit, "zork") || !strings.Contains(unit, stable) {
				t.Errorf("stored unit contains the name or loses the stable binary:\n%s", unit)
			}
		})
	}
}
