package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/hyperlab-sh/homebot"
	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"0.1.0", "0.1.1", true}, {"0.1.0-rc.1", "0.1.0", true},
		{"0.1.0-rc.2", "0.1.0-rc.10", true}, {"0.99.99", "1.0.0", true},
		{"v1.0.0+build.1", "1.0.0+build.2", false}, {"1.0.0", "v1.0.0-rc.1", false},
		{"1.0.0-alpha", "1.0.0-alpha.1", true}, {"1.0.0-1", "1.0.0-alpha", true},
	} {
		got, err := Newer(tc.a, tc.b)
		if err != nil || got != tc.want {
			t.Errorf("Newer(%q,%q) = %v,%v; want %v", tc.a, tc.b, got, err, tc.want)
		}
	}
	for _, bad := range []string{"dev", "1.2", "01.2.3", "1.2.3-01", "v0.0.0-20261013120000-abcdef123456", "v0.1.1-0.20261013120000-abcdef123456"} {
		if _, err := Newer(bad, "1.0.0"); !errors.Is(err, ErrNotARelease) {
			t.Errorf("Newer(%q) error = %v", bad, err)
		}
	}
}

func TestLatestDoesNotFollowRedirect(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		location string
		want     string
	}{
		{"release", 302, "/releases/tag/v1.2.3", "1.2.3"},
		{"short semver", 302, "/releases/tag/v1.2", ""},
		{"foreign redirect", 302, "https://example.invalid/releases/tag/v1.2.3", ""},
		{"loop", 302, "/releases/latest", ""},
		{"missing", 404, "", ""}, {"page", 200, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/releases/latest" || !strings.HasPrefix(r.UserAgent(), "homebot/") {
					t.Errorf("request: %s %q", r.URL.Path, r.UserAgent())
				}
				if tc.location != "" {
					w.Header().Set("Location", tc.location)
				}
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			b := testutil.Sandbox(t, testutil.Options{})
			b.Ctx.Env = b.Ctx.Env.With(map[string]string{testReleasesURL: srv.URL})
			got, err := Latest(b.Ctx)
			if got.Version != tc.want || (tc.want == "" && err == nil) || (tc.want != "" && err != nil) {
				t.Errorf("Latest = %+v, %v", got, err)
			}
			if calls != 1 {
				t.Errorf("calls = %d, want 1", calls)
			}
		})
	}
}

func TestLatestHonorsShortContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	b := testutil.Sandbox(t, testutil.Options{})
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{testReleasesURL: srv.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := Latest(b.Ctx.WithContext(ctx)); err == nil {
		t.Fatal("hung latest request succeeded")
	}
}

func writeBinary(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 0.1.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDetectAndPlan(t *testing.T) {
	for _, layout := range []string{"Caskroom", "Cellar"} {
		t.Run(layout, func(t *testing.T) {
			b := testutil.Sandbox(t, testutil.Options{})
			// Detect reports symlink-resolved paths (macOS /var -> /private/var).
			dir, err := filepath.EvalSymlinks(b.Dir)
			if err != nil {
				t.Fatal(err)
			}
			prefix := filepath.Join(dir, "brew prefix")
			binary := filepath.Join(prefix, layout, "homebot", "0.1.0")
			if layout == "Cellar" {
				binary = filepath.Join(binary, "bin")
			}
			binary = filepath.Join(binary, "homebot")
			writeBinary(t, binary)
			b.Ctx.Exe = binary
			i := Detect(b.Ctx, Build{Version: "0.1.0", Channel: "release"}, nil)
			want := Cask
			if layout == "Cellar" {
				want = Formula
			}
			if i.Method != want || i.Prefix != prefix {
				t.Fatalf("Detect = %+v", i)
			}
			u := Plan(i, Release{Version: "0.1.1", Tag: "v0.1.1"})
			if len(u.Argv) < 3 || u.Argv[0] != filepath.Join(prefix, "bin", "brew") {
				t.Errorf("Plan = %+v", u)
			}
		})
	}
	b := testutil.Sandbox(t, testutil.Options{})
	b.Ctx.Exe = filepath.Join(b.Bin, "homebot")
	writeBinary(t, b.Ctx.Exe)
	for _, tc := range []struct {
		build Build
		info  *debug.BuildInfo
		want  Method
	}{
		{Build{Version: "0.1.0"}, &debug.BuildInfo{Main: debug.Module{Sum: "h1:test"}}, Go},
		{Build{Version: "0.1.0"}, &debug.BuildInfo{}, Source},
		{Build{Version: "0.1.0", Channel: "release"}, nil, Unknown},
	} {
		if got := Detect(b.Ctx, tc.build, tc.info); got.Method != tc.want {
			t.Errorf("Detect = %s, want %s", got.Method, tc.want)
		}
	}
	for _, tc := range []struct {
		method Method
		want   string
	}{{Deb, "homebot_0.1.1_amd64.deb"}, {RPM, "homebot-0.1.1-1.x86_64.rpm"}} {
		u := Plan(Install{Method: tc.method, Arch: "x64"}, Release{Version: "0.1.1", Tag: "v0.1.1"})
		if !strings.Contains(u.Text, tc.want) {
			t.Errorf("Plan(%s) = %q", tc.method, u.Text)
		}
	}
}

func TestReceiptResolvesBothPaths(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	real := filepath.Join(b.Dir, "actual", "homebot")
	link := filepath.Join(b.Dir, "link", "homebot")
	writeBinary(t, real)
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	b.Ctx.Exe = real
	if err := os.MkdirAll(b.Ctx.DataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(b.Ctx.DataDir, "install-receipt"), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("method=script\nbin=" + link + "\nversion=0.0.9\ninstalled=1\n")
	if got := Detect(b.Ctx, Build{Version: "0.1.0", Channel: "release"}, nil); got.Method != Script {
		t.Errorf("matching stale-version receipt: %+v", got)
	}
	write("method=script\nbin=/missing/homebot\nversion=0.1.0\ninstalled=1\n")
	if got := Detect(b.Ctx, Build{Version: "0.1.0", Channel: "release"}, nil); got.Method != Unknown {
		t.Errorf("stale receipt: %+v", got)
	}
	write("method=script\nbin=relative/homebot\n")
	if got := Detect(b.Ctx, Build{Version: "0.1.0", Channel: "release"}, nil); got.Method != Unknown {
		t.Errorf("malformed receipt: %+v", got)
	}
	write("method=script\nbin=" + link + "\nversion=invalid\ninstalled=1\n")
	if got := Detect(b.Ctx, Build{Version: "0.1.0", Channel: "release"}, nil); got.Method != Unknown {
		t.Errorf("invalid version receipt: %+v", got)
	}
}

func TestDetectPackageOwners(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{OS: app.Linux})
	bin := filepath.Join(b.Dir, "system", "homebot")
	writeBinary(t, bin)
	b.Ctx.Exe = bin
	before := systemBinary
	systemBinary = bin
	t.Cleanup(func() { systemBinary = before })
	stub := func(name, text string) {
		t.Helper()
		writeBinary(t, filepath.Join(b.Bin, name))
		if err := os.WriteFile(filepath.Join(b.Bin, name), []byte("#!/bin/sh\n"+text), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": b.Bin})
	stub("dpkg-query", "echo 'homebot: "+bin+"'\n")
	if got := Detect(b.Ctx, Build{Version: "0.1.0", Channel: "release"}, nil); got.Method != Deb {
		t.Errorf("deb: %+v", got)
	}
	stub("dpkg-query", "exit 1\n")
	stub("rpm", "echo 'homebot-0.1.0-1'\n")
	if got := Detect(b.Ctx, Build{Version: "0.1.0", Channel: "release"}, nil); got.Method != RPM {
		t.Errorf("rpm: %+v", got)
	}
	stub("rpm", "exit 1\n")
	if got := Detect(b.Ctx, Build{Version: "0.1.0", Channel: "release"}, nil); got.Method != Unknown {
		t.Errorf("unowned: %+v", got)
	}
}

func TestDetectPackageOwnerThroughSymlinkedSystemPath(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{OS: app.Linux})
	real := filepath.Join(b.Dir, "usr-real", "homebot")
	writeBinary(t, real)
	link := filepath.Join(b.Dir, "usr-link", "homebot")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	before := systemBinary
	systemBinary = link // configured path is a symlink; the running exe is the real file
	t.Cleanup(func() { systemBinary = before })
	b.Ctx.Exe = real
	writeBinary(t, filepath.Join(b.Bin, "dpkg-query"))
	if err := os.WriteFile(filepath.Join(b.Bin, "dpkg-query"), []byte("#!/bin/sh\necho 'homebot: x'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"PATH": b.Bin})
	if got := Detect(b.Ctx, Build{Version: "0.1.0", Channel: "release"}, nil); got.Method != Deb {
		t.Errorf("symlinked system path: %+v", got)
	}
	systemBinary = filepath.Join(b.Dir, "does-not-exist")
	if got := Detect(b.Ctx, Build{Version: "0.1.0", Channel: "release"}, nil); got.Method != Unknown {
		t.Errorf("unresolvable system path matched: %+v", got)
	}
}

func TestRunUnattendedRefusesDowngradesAndUnknownCurrent(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	marker := filepath.Join(b.Dir, "ran")
	before := InstallScript
	InstallScript = []byte("#!/bin/sh\n: > '" + marker + "'\n")
	t.Cleanup(func() { InstallScript = before })
	brew := filepath.Join(b.Dir, "brew")
	if err := os.WriteFile(brew, []byte("#!/bin/sh\n: > '"+marker+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name            string
		method          Method
		current, target string
		want            error
	}{
		{"script older", Script, "0.2.0", "0.1.9", ErrNotNewer},
		{"script equal", Script, "0.2.0", "0.2.0", ErrNotNewer},
		{"script release below prerelease-free current", Script, "1.0.0", "1.0.0-rc.1", ErrNotNewer},
		{"cask older", Cask, "0.2.0", "0.1.9", ErrNotNewer},
		{"script empty current", Script, "", "0.2.0", ErrNotARelease},
		{"script dev current", Script, "dev", "0.2.0", ErrNotARelease},
		{"cask invalid target", Cask, "0.1.0", "latest", ErrNotARelease},
	} {
		t.Run(tc.name, func(t *testing.T) {
			argv := []string{"/bin/sh", "-s", "--"}
			if tc.method == Cask {
				argv = []string{brew}
			}
			u := Upgrade{Method: tc.method, Version: tc.target, Current: tc.current, Binary: filepath.Join(b.Dir, "homebot"), Argv: argv}
			if err := RunUnattended(context.Background(), b.Ctx, u); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if _, err := os.Stat(marker); err == nil {
				t.Fatal("installer child ran despite refusal")
			}
		})
	}
}

func TestRunUnattendedRefusesManualMethods(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{OS: app.Linux})
	for _, method := range []Method{Deb, RPM, Go, Formula, Unknown, Source} {
		err := RunUnattended(context.Background(), b.Ctx, Upgrade{Method: method})
		want := ErrManual
		if method == Deb || method == RPM {
			want = ErrNeedsRoot
		}
		if !errors.Is(err, want) {
			t.Errorf("method %s: %v", method, err)
		}
	}
}

func TestScriptManualCommandKeepsInstallerDownloadHTTPSOnly(t *testing.T) {
	command := ManualCommand(Install{Method: Script, Binary: "/tmp/bin dir/homebot"}, Release{Version: "0.1.1", Tag: "v0.1.1"})
	for _, required := range []string{"curl --proto '=https' --tlsv1.2 -fsSL https://github.com/hyperlab-sh/homebot/releases/download/v0.1.1/install.sh | /bin/sh -s --", "--version v0.1.1", "--bin-dir '/tmp/bin dir'", "--require-signature"} {
		if !strings.Contains(command, required) {
			t.Errorf("manual command %q missing %q", command, required)
		}
	}
}

func TestRunUnattendedBrewAllowlistAndPostcheck(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	version := filepath.Join(b.Dir, "version")
	binary := filepath.Join(b.Dir, "homebot")
	brew := filepath.Join(b.Dir, "brew")
	log := filepath.Join(b.Dir, "brew.log")
	if err := os.WriteFile(version, []byte("0.1.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(binary, "#!/bin/sh\n[ \"$HOMEBOT_PLAIN\" = 1 ] || echo voiced\n/bin/cat '"+version+"'\n")
	write(brew, "#!/bin/sh\nprintf '%s\\n' \"$*\" \"${HOMEBREW_NO_INSTALL_CLEANUP:-}\" \"${HOMEBREW_NO_AUTO_UPDATE:-}\" \"${SECRET_X:-}\" \"${HTTPS_PROXY:-}\" > '"+log+"'\necho 0.1.1 > '"+version+"'\n")
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{"SECRET_X": "do-not-leak", "HOMEBREW_NO_AUTO_UPDATE": "1", "HTTPS_PROXY": "https://proxy.example"})
	u := Upgrade{Method: Cask, Version: "0.1.1", Current: "0.1.0", Binary: binary, Argv: []string{brew, "upgrade", "--cask", "hyperlab-sh/tap/homebot"}}
	if err := RunUnattended(context.Background(), b.Ctx, u); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "upgrade --cask hyperlab-sh/tap/homebot\n1\n\n\nhttps://proxy.example\n" {
		t.Errorf("brew argv/env = %q", got)
	}
	if err := os.WriteFile(version, []byte("0.1.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	write(brew, "#!/bin/sh\nexit 0\n")
	if err := RunUnattended(context.Background(), b.Ctx, u); !errors.Is(err, ErrNotYetAvailable) {
		t.Errorf("old binary after brew = %v", err)
	}
	if err := os.WriteFile(version, []byte("0.1.2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var found string
	u.Found = &found
	if err := RunUnattended(context.Background(), b.Ctx, u); err != nil || found != "0.1.2" {
		t.Errorf("newer binary after brew = %q, %v", found, err)
	}
}

func TestRunUnattendedScriptExitCodes(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	u := Upgrade{Method: Script, Version: "0.1.1", Current: "0.1.0", Binary: filepath.Join(b.Dir, "homebot"), Argv: []string{"/bin/sh", "-s", "--", "--version", "v0.1.1", "--require-signature"}}
	previous := InstallScript
	t.Cleanup(func() { InstallScript = previous })
	for code, want := range map[int]error{1: ErrInstall, 2: ErrPlatform, 3: ErrDownload, 4: ErrCosign, 5: ErrVerify, 6: ErrSmoke, 7: ErrRefused} {
		InstallScript = []byte("#!/bin/sh\necho first-line >&2\nexit " + string(rune('0'+code)) + "\n")
		if err := RunUnattended(context.Background(), b.Ctx, u); !errors.Is(err, want) || !strings.Contains(err.Error(), "first-line") {
			t.Errorf("code %d = %v", code, err)
		}
	}
	InstallScript = []byte("#!/bin/sh\n  COSIGN_MIN=2.4.0\n")
	if got := CosignMin(); got != "2.4.0" {
		t.Errorf("CosignMin = %q", got)
	}
}

func TestRunUnattendedPostcheckTimeout(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	bin := filepath.Join(b.Dir, "homebot")
	upgrade := filepath.Join(b.Dir, "upgrade")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ntrap '' TERM\nwhile :; do /bin/sleep 1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(upgrade, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	beforeScript, beforeGrace := InstallScript, termGrace
	InstallScript = []byte("#!/bin/sh\nexit 0\n")
	termGrace = 50 * time.Millisecond
	t.Cleanup(func() { InstallScript, termGrace = beforeScript, beforeGrace })
	for _, method := range []Method{Script, Cask} {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		err := RunUnattended(ctx, b.Ctx, Upgrade{Method: method, Version: "0.1.1", Current: "0.1.0", Binary: bin, Argv: []string{upgrade}})
		cancel()
		if !errors.Is(err, ErrTimeout) {
			t.Errorf("%s post-check = %v, want timeout", method, err)
		}
		if method == Cask && !errors.Is(err, ErrBrokenInstall) {
			t.Errorf("%s post-check = %v, want broken install", method, err)
		}
	}
}

func TestRunUnattendedScriptArgumentsAndPostcheck(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	args := filepath.Join(b.Dir, "args")
	bin := filepath.Join(b.Dir, "homebot")
	writeBinary(t, bin)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n[ \"$HOMEBOT_PLAIN\" = 1 ] || exit 2\necho 0.1.1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := InstallScript
	InstallScript = []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" > '" + args + "'\n")
	t.Cleanup(func() { InstallScript = before })
	u := Upgrade{Method: Script, Version: "0.1.1", Current: "0.1.0", Binary: bin, BaseURL: "http://127.0.0.1:1234", Argv: []string{"/bin/sh", "-s", "--", "--version", "v0.1.1", "--bin-dir", b.Bin, "--require-signature", "--max-time", "60"}}
	if err := RunUnattended(context.Background(), b.Ctx, u); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(args)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--version v0.1.1", "--require-signature", "--max-time 60", "--base-url http://127.0.0.1:1234"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("installer args %q missing %q", data, want)
		}
	}
}

// releaseFiles is a fake release of version for linux/amd64, as the embedded
// installer downloads it: the archive holding binary, its checksums.txt and the
// signature bundle, keyed by request path.
func releaseFiles(t *testing.T, version string, binary []byte) map[string][]byte {
	t.Helper()
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "homebot", Mode: 0o755, Size: int64(len(binary))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	asset := "homebot_" + version + "_linux_amd64.tar.gz"
	dir := "/releases/download/v" + version + "/"
	return map[string][]byte{
		dir + "checksums.txt":               []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(archive.Bytes()), asset)),
		dir + "checksums.txt.sigstore.json": []byte("{}\n"),
		dir + asset:                         archive.Bytes(),
	}
}

func TestEmbeddedInstallerVerifiesAndWritesReceipt(t *testing.T) {
	source, err := os.ReadFile("../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(homebot.InstallScript, source) || !bytes.Equal(InstallScript, source) {
		t.Fatal("updater is not running the root installer bytes")
	}
	if got := CosignMin(); got != "2.4.3" {
		t.Fatalf("embedded cosign minimum = %q, want 2.4.3", got)
	}

	b := testutil.Sandbox(t, testutil.Options{})
	b.WithStubs(map[string]string{
		"uname": `case "$1" in -s) echo Linux ;; -m) echo x86_64 ;; *) exit 2 ;; esac`,
		"cosign": `case "$1" in
  version) echo 'GitVersion: v2.4.3' ;;
  verify-blob)
    [ "$#" -eq 9 ] && [ "$2" = --new-bundle-format ] &&
    [ "$3" = --certificate-identity ] &&
    [ "$4" = 'https://github.com/hyperlab-sh/homebot/.github/workflows/release.yml@refs/tags/v0.0.2' ] &&
    [ "$5" = --certificate-oidc-issuer ] &&
    [ "$6" = https://token.actions.githubusercontent.com ] &&
    [ "$7" = --bundle ] && [ -s "$8" ] && [ -s "$9" ] || exit 2 ;;
  *) exit 2 ;;
esac`,
	})
	bin := filepath.Join(b.Bin, "homebot")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 0.0.1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	newBinary := []byte("#!/bin/sh\n[ \"${HOMEBOT_PLAIN:-}\" = 1 ] || exit 2\ncase \"${1:-}\" in --version|version) echo 0.0.2 ;; *) exit 2 ;; esac\n")
	files := releaseFiles(t, "0.0.2", newBinary)
	requests := make(map[string]int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, ok := files[r.URL.Path]
		if !ok {
			t.Errorf("unexpected installer request %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		requests[r.URL.Path]++
		_, _ = w.Write(data)
	}))
	defer srv.Close()
	u := Upgrade{
		Method: Script, Version: "0.0.2", Current: "0.0.1", Binary: bin, BaseURL: srv.URL,
		Argv: []string{"/bin/sh", "-s", "--", "--version", "v0.0.2", "--bin-dir", b.Bin, "--require-signature", "--max-time", "60"},
	}
	if err := RunUnattended(context.Background(), b.Ctx, u); err != nil {
		t.Fatal(err)
	}
	for path := range files {
		if requests[path] != 1 {
			t.Errorf("request %q made %d times, want once", path, requests[path])
		}
	}
	installed, err := os.ReadFile(bin)
	if err != nil || !bytes.Equal(installed, newBinary) {
		t.Errorf("installed binary differs from verified archive: %v", err)
	}
	receipt, err := os.ReadFile(filepath.Join(b.Dir, "xdg", "data", "homebot", "install-receipt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(receipt), "method=script\nbin="+bin+"\nversion=0.0.2\n") {
		t.Errorf("unexpected receipt: %s", receipt)
	}
	b.Ctx.Exe = bin
	if got := Detect(b.Ctx, Build{Version: "0.0.2", Channel: "release"}, nil); got.Method != Script || got.Binary != bin {
		t.Errorf("installed binary classification = %+v", got)
	}
}

func TestRunChildKillsProcessGroupAtDeadline(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	marker := filepath.Join(b.Dir, "child-survived")
	before := termGrace
	termGrace = 50 * time.Millisecond
	t.Cleanup(func() { termGrace = before })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _, timedOut, _ := runChild(ctx, []string{"/bin/sh", "-c", "( /bin/sleep 1; echo survived > '" + marker + "' ) & trap '' TERM; while :; do /bin/sleep 1; done"}, nil, childEnv(b.Ctx, false, false))
	if !timedOut {
		t.Fatal("process group did not hit the deadline")
	}
	time.Sleep(1200 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("child continued after group cancellation")
	}
}

func TestRunUnattendedReportsBrokenBrewAfterTimeout(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	brew := filepath.Join(b.Dir, "brew")
	if err := os.WriteFile(brew, []byte("#!/bin/sh\ntrap '' TERM\nwhile :; do /bin/sleep 1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := termGrace
	termGrace = 50 * time.Millisecond
	t.Cleanup(func() { termGrace = before })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := RunUnattended(ctx, b.Ctx, Upgrade{Method: Cask, Version: "0.1.1", Current: "0.1.0", Binary: filepath.Join(b.Dir, "missing-homebot"), Argv: []string{brew, "upgrade", "--cask", "hyperlab-sh/tap/homebot"}})
	if !errors.Is(err, ErrTimeout) || !errors.Is(err, ErrBrokenInstall) {
		t.Errorf("killed brew = %v", err)
	}
}
