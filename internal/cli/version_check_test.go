package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/update"
)

func TestVersionCheckExplicitRequestAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		wantCode int
		want     string
	}{
		{"release", http.StatusFound, 0, "0.1.1"},
		{"no release", http.StatusNotFound, 1, "no release"},
		{"server failure", http.StatusInternalServerError, 1, "latest release"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/releases/latest" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				if tc.status == http.StatusFound {
					w.Header().Set("Location", "/releases/tag/v0.1.1")
				}
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			b := testutil.Sandbox(t, testutil.Options{})
			b.Ctx.Env = b.Ctx.Env.With(map[string]string{"HOMEBOT_TEST_RELEASES_URL": srv.URL})
			if code := Execute(t.Context(), b.Ctx, Build{Version: "0.1.0", Channel: "release"}, []string{"version"}); code != 0 || calls != 0 {
				t.Fatalf("plain version made request: code=%d calls=%d", code, calls)
			}
			b.Stdout.Reset()
			if code := Execute(t.Context(), b.Ctx, Build{Version: "0.1.0", Channel: "release"}, []string{"version", "--check"}); code != tc.wantCode || calls != 1 {
				t.Fatalf("check: code=%d calls=%d stdout=%q stderr=%q", code, calls, b.Stdout.String(), b.Stderr.String())
			}
			if !strings.Contains(b.Stdout.String()+b.Stderr.String(), tc.want) {
				t.Errorf("missing %q: stdout=%q stderr=%q", tc.want, b.Stdout.String(), b.Stderr.String())
			}
		})
	}
}

func TestVersionCheckBuildAndAutoStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/releases/tag/v0.1.1")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	for _, tc := range []struct {
		name  string
		build Build
		info  *debug.BuildInfo
		want  string
	}{
		{"development", Build{Version: "dev"}, nil, "development"},
		{"source", Build{Version: "0.1.0", Channel: "source"}, nil, "source"},
		{"go install", Build{Version: "0.1.0", Channel: "source"}, &debug.BuildInfo{Main: debug.Module{Sum: "h1:example"}}, "go install"},
		{"pseudo", Build{Version: "v0.0.0-20261014000000-abcdef123456", Channel: "release"}, nil, "development"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := testutil.Sandbox(t, testutil.Options{})
			b.Ctx.Env = b.Ctx.Env.With(map[string]string{"HOMEBOT_TEST_RELEASES_URL": srv.URL})
			if err := versionCheckWithInfo(b.Ctx, tc.build, tc.info); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.ToLower(b.Stdout.String()), tc.want) {
				t.Errorf("stdout=%q, want %q", b.Stdout.String(), tc.want)
			}
		})
	}

	b := testutil.Sandbox(t, testutil.Options{})
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"HOMEBOT_TEST_RELEASES_URL": srv.URL})
	b.Ctx.Config = &config.Config{Update: config.UpdateAuto}
	b.Ctx.Exe = filepath.Join(b.Dir, "Caskroom", "homebot", "0.1.0", "homebot")
	// Detection needs an existing executable, but version --check never runs it.
	if err := os.MkdirAll(filepath.Dir(b.Ctx.Exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b.Ctx.Exe, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := versionCheckWithInfo(b.Ctx, Build{Version: "0.1.0", Channel: "release"}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(b.Stdout.String()), "start its 48-hour soak") {
		t.Errorf("unseen release status: %q", b.Stdout.String())
	}
	b.Stdout.Reset()
	now := b.Ctx.Now()
	if err := state.Update(b.Ctx, func(s *state.State) {
		s.UpdateCheck = &state.UpdateCheck{At: now.UnixMilli(), Latest: "0.1.1", FirstSeen: now.Add(-time.Hour).UnixMilli()}
	}); err != nil {
		t.Fatal(err)
	}
	if err := versionCheckWithInfo(b.Ctx, Build{Version: "0.1.0", Channel: "release"}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(b.Stdout.String()), "waits until") {
		t.Errorf("auto status: %q", b.Stdout.String())
	}
}

func TestOnlyExplicitCheckOrHelperMayRequestRelease(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		args       []string
	}{
		{"plain version", "auto", []string{"version"}},
		{"ordinary tidy", "auto", []string{"tidy", "--notify", "--quiet"}},
		{"disabled helper", "off", []string{"tidy", "--notify", "--quiet", "--helper"}},
		{"unknown mode helper", "atuo", []string{"tidy", "--notify", "--quiet", "--helper"}},
		{"inbox", "notify", []string{"inbox"}},
		{"help", "auto", []string{"help", "version"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer srv.Close()
			b := testutil.Sandbox(t, testutil.Options{Config: testutil.BaseConfig + "update: " + tc.mode + "\n"})
			b.Ctx.Env = b.Ctx.Env.With(map[string]string{"HOMEBOT_TEST_RELEASES_URL": srv.URL})
			_ = Execute(t.Context(), b.Ctx, Build{Version: "0.1.0", Channel: "release"}, tc.args)
			if calls != 0 {
				t.Errorf("%v made %d release requests", tc.args, calls)
			}
		})
	}
}

// latestServer answers the latest-release redirect with 0.1.1.
func latestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/releases/tag/v0.1.1")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// Detection needs an existing executable, but version --check never runs it.
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho binary\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// caskBox is a sandbox whose Context runs from a Homebrew Caskroom path.
func caskBox(t *testing.T, server string, mode config.UpdateMode) *testutil.Box {
	t.Helper()
	b := testutil.Sandbox(t, testutil.Options{})
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"HOMEBOT_TEST_RELEASES_URL": server})
	b.Ctx.Config = &config.Config{Update: mode}
	b.Ctx.Exe = filepath.Join(b.Dir, "brew", "Caskroom", "homebot", "0.1.0", "homebot")
	writeExecutable(t, b.Ctx.Exe)
	return b
}

// scriptBox is a sandbox whose Context runs the binary an install receipt
// names. Its PATH holds only the sandbox bin, so a test decides whether a
// cosign exists.
func scriptBox(t *testing.T, server string, mode config.UpdateMode) *testutil.Box {
	t.Helper()
	b := testutil.Sandbox(t, testutil.Options{})
	b.Ctx.Config = &config.Config{Update: mode}
	scriptInstall(t, b, server)
	return b
}

// scriptInstall makes b's Context a script install (an executable and the
// receipt that names it) with a PATH of only the sandbox bin.
func scriptInstall(t *testing.T, b *testutil.Box, server string) {
	t.Helper()
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"HOMEBOT_TEST_RELEASES_URL": server, "PATH": b.Bin})
	b.Ctx.Exe = filepath.Join(b.Dir, "local", "bin", "homebot")
	writeExecutable(t, b.Ctx.Exe)
	if err := os.MkdirAll(b.Ctx.DataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	receipt := "method=script\nbin=" + b.Ctx.Exe + "\nversion=0.1.0\ninstalled=1\n"
	if err := os.WriteFile(filepath.Join(b.Ctx.DataDir, "install-receipt"), []byte(receipt), 0o600); err != nil {
		t.Fatal(err)
	}
}

func setCheck(t *testing.T, b *testutil.Box, check *state.UpdateCheck, failed *state.UpdateFailed) {
	t.Helper()
	if err := state.Update(b.Ctx, func(s *state.State) { s.UpdateCheck, s.UpdateFailed = check, failed }); err != nil {
		t.Fatal(err)
	}
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func outputLacks(t *testing.T, out string, banned ...string) {
	t.Helper()
	for _, word := range banned {
		if strings.Contains(strings.ToLower(out), strings.ToLower(word)) {
			t.Errorf("output must not contain %q:\n%s", word, out)
		}
	}
}

// A build that already runs the latest release (or a newer one) has nothing to
// upgrade to. It must say so, and print neither an upgrade command nor
// anything about automatic updates, whatever the update key and the state say.
func TestVersionCheckUpToDateOffersNothing(t *testing.T) {
	srv := latestServer(t)
	for _, tc := range []struct {
		name, method string
		box          func(*testing.T, config.UpdateMode) (*testutil.Box, Build, *debug.BuildInfo)
	}{
		{"cask", "cask", func(t *testing.T, m config.UpdateMode) (*testutil.Box, Build, *debug.BuildInfo) {
			return caskBox(t, srv.URL, m), Build{Channel: "release"}, nil
		}},
		{"script", "script", func(t *testing.T, m config.UpdateMode) (*testutil.Box, Build, *debug.BuildInfo) {
			return scriptBox(t, srv.URL, m), Build{Channel: "release"}, nil
		}},
		{"go install", "go", func(t *testing.T, m config.UpdateMode) (*testutil.Box, Build, *debug.BuildInfo) {
			b := testutil.Sandbox(t, testutil.Options{})
			b.Ctx.Env = b.Ctx.Env.With(map[string]string{"HOMEBOT_TEST_RELEASES_URL": srv.URL})
			b.Ctx.Config = &config.Config{Update: m}
			return b, Build{Channel: "source"}, &debug.BuildInfo{Main: debug.Module{Sum: "h1:example"}}
		}},
	} {
		for _, running := range []string{"0.1.1", "0.2.0"} {
			for _, mode := range []config.UpdateMode{config.UpdateOff, config.UpdateNotify, config.UpdateAuto} {
				t.Run(tc.name+"/"+running+"/"+string(mode), func(t *testing.T) {
					b, build, info := tc.box(t, mode)
					build.Version = running
					now := b.Ctx.Now()
					// The state a soaked release leaves: it would be eligible if it were newer.
					setCheck(t, b, &state.UpdateCheck{At: now.Add(-25 * time.Hour).UnixMilli(), Latest: "0.1.1", FirstSeen: now.Add(-72 * time.Hour).UnixMilli()}, nil)
					if err := versionCheckWithInfo(b.Ctx, build, info); err != nil {
						t.Fatal(err)
					}
					out := b.Stdout.String()
					for _, want := range []string{"Current: " + running, "Latest: 0.1.1", "Install method: " + tc.method, "up to date"} {
						if !strings.Contains(out, want) {
							t.Errorf("missing %q:\n%s", want, out)
						}
					}
					outputLacks(t, out, "Upgrade:", "automatic", "eligible", "soak", "helper")
				})
			}
		}
	}
}

// Every install method, as the S3 table lists them: the command it prints, and
// what update: auto adds. deb and rpm cannot be detected from this package, so
// the report is driven with the Install that Detect would return.
func TestVersionCheckReportsEachInstallMethod(t *testing.T) {
	release := update.Release{Version: "0.1.1", Tag: "v0.1.1", URL: "https://github.com/hyperlab-sh/homebot/releases/tag/v0.1.1"}
	for _, tc := range []struct {
		name    string
		install update.Install
		build   Build
		command string
		manual  bool // update: auto says the install cannot update itself
	}{
		{"cask", update.Install{Method: update.Cask, Version: "0.1.0", Prefix: "/opt/homebrew"}, Build{Channel: "release"},
			"/opt/homebrew/bin/brew upgrade --cask hyperlab-sh/tap/homebot", false},
		{"formula", update.Install{Method: update.Formula, Version: "0.1.0", Prefix: "/opt/homebrew"}, Build{Channel: "release"},
			"/opt/homebrew/bin/brew upgrade homebot", true},
		{"script", update.Install{Method: update.Script, Version: "0.1.0", Binary: "/home/me/.local/bin/homebot"}, Build{Channel: "release"},
			"curl --proto '=https' --tlsv1.2 -fsSL https://github.com/hyperlab-sh/homebot/releases/download/v0.1.1/install.sh | /bin/sh -s -- --version v0.1.1 --bin-dir /home/me/.local/bin --require-signature", false},
		{"deb", update.Install{Method: update.Deb, Version: "0.1.0", Arch: "x64"}, Build{Channel: "release"},
			"curl -LO https://github.com/hyperlab-sh/homebot/releases/download/v0.1.1/homebot_0.1.1_amd64.deb && sudo apt install ./homebot_0.1.1_amd64.deb", true},
		{"rpm", update.Install{Method: update.RPM, Version: "0.1.0", Arch: "arm64"}, Build{Channel: "release"},
			"curl -LO https://github.com/hyperlab-sh/homebot/releases/download/v0.1.1/homebot-0.1.1-1.aarch64.rpm && sudo dnf install ./homebot-0.1.1-1.aarch64.rpm", true},
		{"go", update.Install{Method: update.Go, Version: "0.1.0"}, Build{Channel: "source"},
			"go install github.com/hyperlab-sh/homebot/cmd/homebot@v0.1.1", true},
		{"unknown", update.Install{Method: update.Unknown, Version: "0.1.0"}, Build{Channel: "release"},
			release.URL, true},
	} {
		for _, mode := range []config.UpdateMode{config.UpdateOff, config.UpdateNotify, config.UpdateAuto} {
			t.Run(tc.name+"/"+string(mode), func(t *testing.T) {
				b := testutil.Sandbox(t, testutil.Options{})
				b.Ctx.Config = &config.Config{Update: mode}
				build := tc.build
				build.Version = "0.1.0"
				if err := versionReport(b.Ctx, build, tc.install, release); err != nil {
					t.Fatal(err)
				}
				out := b.Stdout.String()
				for _, want := range []string{"Current: 0.1.0", "Latest: 0.1.1", "Install method: " + tc.name, "Upgrade: " + tc.command + "\n"} {
					if !strings.Contains(out, want) {
						t.Errorf("missing %q:\n%s", want, out)
					}
				}
				outputLacks(t, out, "up to date", "eligible")
				unavailable := strings.Contains(out, "unavailable for this install method")
				if unavailable != (mode == config.UpdateAuto && tc.manual) {
					t.Errorf("mode %s: unavailable = %v, want %v:\n%s", mode, unavailable, mode == config.UpdateAuto && tc.manual, out)
				}
				if mode != config.UpdateAuto {
					outputLacks(t, out, "automatic")
				}
			})
		}
	}
}

// The "eligible" line is the helper's real gate for a pending release on a
// cask install: the 48-hour wait, the day between attempts, a recorded
// failure, and the next check the helper is due for.
func TestVersionCheckAutoGateFollowsHelperState(t *testing.T) {
	srv := latestServer(t)
	ago := func(b *testutil.Box, d time.Duration) int64 { return b.Ctx.Now().Add(-d).UnixMilli() }
	at := func(ms int64) string { return stamp(time.UnixMilli(ms)) }
	for _, tc := range []struct {
		name   string
		check  func(*testutil.Box) *state.UpdateCheck
		failed func(*testutil.Box) *state.UpdateFailed
		want   func(*testutil.Box) []string
		banned []string
	}{
		{"nothing recorded", func(*testutil.Box) *state.UpdateCheck { return nil }, nil,
			func(*testutil.Box) []string { return []string{"48-hour soak"} }, []string{"eligible"}},
		{"another release recorded", func(b *testutil.Box) *state.UpdateCheck {
			return &state.UpdateCheck{At: ago(b, time.Hour), Latest: "0.1.0", FirstSeen: ago(b, 72*time.Hour)}
		}, nil, func(*testutil.Box) []string { return []string{"48-hour soak"} }, []string{"eligible"}},
		{"soaking", func(b *testutil.Box) *state.UpdateCheck {
			return &state.UpdateCheck{At: ago(b, time.Hour), Latest: "0.1.1", FirstSeen: ago(b, time.Hour)}
		}, nil, func(b *testutil.Box) []string {
			return []string{"waits until " + at(ago(b, time.Hour)+(48*time.Hour).Milliseconds()) + "."}
		}, []string{"eligible"}},
		{"soaked, last check long ago", func(b *testutil.Box) *state.UpdateCheck {
			return &state.UpdateCheck{At: ago(b, 25*time.Hour), Latest: "0.1.1", FirstSeen: ago(b, 72*time.Hour)}
		}, nil, func(*testutil.Box) []string { return []string{"eligible on the next helper run."} }, nil},
		{"soaked, checked an hour ago", func(b *testutil.Box) *state.UpdateCheck {
			return &state.UpdateCheck{At: ago(b, time.Hour), Latest: "0.1.1", FirstSeen: ago(b, 72*time.Hour)}
		}, nil, func(b *testutil.Box) []string {
			return []string{"eligible on the first helper run after " + at(ago(b, time.Hour)+(24*time.Hour).Milliseconds()) + "."}
		}, []string{"next helper run"}},
		{"soaked, last check failed 7 hours ago", func(b *testutil.Box) *state.UpdateCheck {
			return &state.UpdateCheck{At: ago(b, 7*time.Hour), Latest: "0.1.1", FirstSeen: ago(b, 72*time.Hour), Failed: true}
		}, nil, func(*testutil.Box) []string { return []string{"eligible on the next helper run."} }, nil},
		{"soaked, last check failed 5 hours ago", func(b *testutil.Box) *state.UpdateCheck {
			return &state.UpdateCheck{At: ago(b, 5*time.Hour), Latest: "0.1.1", FirstSeen: ago(b, 72*time.Hour), Failed: true}
		}, nil, func(b *testutil.Box) []string {
			return []string{"eligible on the first helper run after " + at(ago(b, 5*time.Hour)+(6*time.Hour).Milliseconds()) + "."}
		}, []string{"next helper run"}},
		{"attempt an hour ago left the old version (tap lag)", func(b *testutil.Box) *state.UpdateCheck {
			return &state.UpdateCheck{At: ago(b, time.Hour), Latest: "0.1.1", FirstSeen: ago(b, 72*time.Hour), FirstAttempt: ago(b, time.Hour), LastAttempt: ago(b, time.Hour)}
		}, nil, func(b *testutil.Box) []string {
			return []string{"left this version in place", "tries again after " + at(ago(b, time.Hour)+(24*time.Hour).Milliseconds()) + "."}
		}, []string{"eligible"}},
		{"attempt failed an hour ago", func(b *testutil.Box) *state.UpdateCheck {
			return &state.UpdateCheck{At: ago(b, time.Hour), Latest: "0.1.1", FirstSeen: ago(b, 72*time.Hour), LastAttempt: ago(b, time.Hour)}
		}, func(b *testutil.Box) *state.UpdateFailed {
			return &state.UpdateFailed{Version: "0.1.1", At: ago(b, time.Hour), Reason: "verify", Error: "release verification failed: bad signature"}
		}, func(b *testutil.Box) []string {
			return []string{"update to 0.1.1 failed (release verification failed: bad signature)", "tries again after " + at(ago(b, time.Hour)+(24*time.Hour).Milliseconds()) + "."}
		}, []string{"eligible", "next helper run"}},
		{"failure older than the day gap is still a failure", func(b *testutil.Box) *state.UpdateCheck {
			return &state.UpdateCheck{At: ago(b, 30*time.Hour), Latest: "0.1.1", FirstSeen: ago(b, 100*time.Hour), LastAttempt: ago(b, 30*time.Hour)}
		}, func(b *testutil.Box) *state.UpdateFailed {
			return &state.UpdateFailed{Version: "0.1.1", At: ago(b, 30*time.Hour), Reason: "timeout", Error: "upgrade timed out"}
		}, func(b *testutil.Box) []string {
			return []string{"update to 0.1.1 failed (upgrade timed out)"}
		}, []string{"eligible"}},
		{"failure recorded without an error text", func(b *testutil.Box) *state.UpdateCheck {
			return &state.UpdateCheck{At: ago(b, time.Hour), Latest: "0.1.1", FirstSeen: ago(b, 72*time.Hour), LastAttempt: ago(b, time.Hour)}
		}, func(b *testutil.Box) *state.UpdateFailed {
			return &state.UpdateFailed{Version: "0.1.1", At: ago(b, time.Hour), Reason: "upgrade"}
		}, func(*testutil.Box) []string { return []string{"failed (upgrade)"} }, []string{"eligible"}},
		{"failure of another release does not count", func(b *testutil.Box) *state.UpdateCheck {
			return &state.UpdateCheck{At: ago(b, 25*time.Hour), Latest: "0.1.1", FirstSeen: ago(b, 72*time.Hour)}
		}, func(b *testutil.Box) *state.UpdateFailed {
			return &state.UpdateFailed{Version: "0.1.0", At: ago(b, 25*time.Hour), Reason: "verify", Error: "old news"}
		}, func(*testutil.Box) []string { return []string{"eligible on the next helper run."} }, []string{"failed", "old news"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := caskBox(t, srv.URL, config.UpdateAuto)
			var failed *state.UpdateFailed
			if tc.failed != nil {
				failed = tc.failed(b)
			}
			setCheck(t, b, tc.check(b), failed)
			if err := versionCheckWithInfo(b.Ctx, Build{Version: "0.1.0", Channel: "release"}, nil); err != nil {
				t.Fatal(err)
			}
			out := b.Stdout.String()
			for _, want := range tc.want(b) {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q:\n%s", want, out)
				}
			}
			if !strings.Contains(out, "Upgrade: ") || !strings.Contains(out, "Install method: cask") {
				t.Errorf("a pending release must show the method and its command:\n%s", out)
			}
			outputLacks(t, out, tc.banned...)
			outputLacks(t, out, "up to date")
		})
	}
}

// update: auto on a script install needs a cosign the helper can run. Without
// one the report says so, instead of a soak or an eligibility the helper will
// not honour; under notify there is no such line.
func TestVersionCheckAutoScriptNeedsUsableCosign(t *testing.T) {
	srv := latestServer(t)
	min := update.CosignMin()
	soaked := func(b *testutil.Box) {
		now := b.Ctx.Now()
		setCheck(t, b, &state.UpdateCheck{At: now.Add(-25 * time.Hour).UnixMilli(), Latest: "0.1.1", FirstSeen: now.Add(-72 * time.Hour).UnixMilli()}, nil)
	}
	for _, tc := range []struct {
		name   string
		mode   config.UpdateMode
		cosign string // stub body; empty means no cosign on the PATH
		want   []string
		banned []string
	}{
		{"cosign missing", config.UpdateAuto, "", []string{"need cosign " + min + " or newer", "rerun homebot setup"}, []string{"eligible", "soak", "waits"}},
		{"cosign below the minimum", config.UpdateAuto, "echo 'GitVersion: v2.4.2'", []string{"need cosign " + min + " or newer"}, []string{"eligible", "soak", "waits"}},
		{"cosign that cannot say its version", config.UpdateAuto, "exit 1", []string{"need cosign " + min + " or newer"}, []string{"eligible"}},
		{"cosign at the minimum", config.UpdateAuto, "echo 'GitVersion: v" + min + "'", []string{"eligible on the next helper run."}, []string{"need cosign"}},
		{"cosign above the minimum", config.UpdateAuto, "echo 'GitVersion: v9.0.0'", []string{"eligible on the next helper run."}, []string{"need cosign"}},
		{"notify never mentions cosign", config.UpdateNotify, "", nil, []string{"need cosign", "automatic", "eligible"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := scriptBox(t, srv.URL, tc.mode)
			if tc.cosign != "" {
				b.WithStubs(map[string]string{"cosign": tc.cosign})
			}
			soaked(b)
			if err := versionCheckWithInfo(b.Ctx, Build{Version: "0.1.0", Channel: "release"}, nil); err != nil {
				t.Fatal(err)
			}
			out := b.Stdout.String()
			for _, want := range append(tc.want, "Install method: script", "Upgrade: curl --proto '=https'") {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q:\n%s", want, out)
				}
			}
			outputLacks(t, out, tc.banned...)
		})
	}
}
