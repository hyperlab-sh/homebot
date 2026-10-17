package actions_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/actions"
	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// forge is a machine with vendor repos in both formats, distro sources, and
// some that must not travel (commented out, disabled).
var forge = map[string]string{
	"etc/os-release":                                   "NAME=\"Pop!_OS\"\nVERSION_CODENAME=noble\nUBUNTU_CODENAME=noble\n",
	"etc/apt/sources.list":                             "# Pop uses deb822 files in sources.list.d\n",
	"etc/apt/sources.list.d/docker.list":               "deb [arch=amd64 signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu noble stable\n",
	"etc/apt/keyrings/docker.gpg":                      "DOCKER-KEY",
	"etc/apt/sources.list.d/wezterm.list":              "deb [signed-by=/usr/share/keyrings/wezterm-fury.gpg] https://apt.fury.io/wez/ * *\n",
	"usr/share/keyrings/wezterm-fury.gpg":              "WEZTERM-KEY",
	"etc/apt/sources.list.d/1password.list":            "# deb [signed-by=/usr/share/keyrings/1password-archive-keyring.gpg] https://downloads.1password.com/linux/debian/amd64 stable main\n",
	"etc/apt/sources.list.d/1password.sources":         "Types: deb\nURIs: https://downloads.1password.com/linux/debian/amd64\nSuites: stable\nComponents: main\nSigned-By: /usr/share/keyrings/1password-archive-keyring.gpg\n",
	"usr/share/keyrings/1password-archive-keyring.gpg": "1PASSWORD-KEY",
	"etc/apt/sources.list.d/inline.sources":            "Types: deb\nURIs: https://inline.example.com/apt\nSuites: stable\nComponents: main\nSigned-By:\n -----BEGIN PGP PUBLIC KEY BLOCK-----\n .\n mQINBF\n -----END PGP PUBLIC KEY BLOCK-----\n",
	"etc/apt/sources.list.d/system.sources":            "X-Repolib-Name: Pop_OS System Sources\nEnabled: yes\nTypes: deb deb-src\nURIs: http://apt.pop-os.org/ubuntu\nSuites: noble noble-security\nComponents: main\n",
	"usr/share/keyrings/ubuntu-archive-keyring.gpg":    "UBUNTU-KEY",
	"etc/apt/sources.list.d/old.sources":               "Enabled: no\nTypes: deb\nURIs: https://old.example.org/repo\nSuites: stable\nComponents: main\n",
}

var exported = []string{
	"1password.sources",
	"docker.list",
	"inline.sources",
	"keyrings/1password-archive-keyring.gpg",
	"keyrings/docker.gpg",
	"keyrings/wezterm-fury.gpg",
	"origin",
	"wezterm.list",
}

type rig struct {
	t   *testing.T
	box *testutil.Box
}

// newRig builds a sandbox whose package managers are tripwires: running any
// of apt-get, apt-cache, dpkg-query or sudo that a test did not stub itself
// fails the test. A test replaces the ones it means with box.WithStubs.
func newRig(t *testing.T) *rig {
	return newRigWith(t, testutil.Options{Git: true})
}

// newLinkedRig is newRig with the sandbox behind a symlink, as a home on
// another volume is, and every sandbox on macOS: what a script sees must
// name the repo as homebot does, not as the kernel resolves it.
func newLinkedRig(t *testing.T) *rig {
	return newRigWith(t, testutil.Options{Git: true, BehindLink: true})
}

func newRigWith(t *testing.T, opts testutil.Options) *rig {
	r := &rig{t: t, box: testutil.Sandbox(t, opts)}
	tripped := filepath.Join(r.box.Dir, "tripwire.log")
	stubs := map[string]string{}
	for _, name := range []string{"apt-get", "apt-cache", "dpkg-query", "sudo"} {
		stubs[name] = `echo "` + name + ` $*" >> '` + tripped + `'; exit 99`
	}
	r.box.WithStubs(stubs)
	t.Cleanup(func() {
		if b, err := os.ReadFile(tripped); err == nil {
			t.Errorf("a test ran an unstubbed package manager:\n%s", b)
		}
	})
	return r
}

func (r *rig) path(rel string) string { return filepath.Join(r.box.Root, rel) }

func put(t *testing.T, base string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(base, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func walkFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, rel)
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

// result is how a script action ended: its exit code and what it printed.
type result struct {
	code int
	out  string
}

// run runs the action as the CLI does, with extra environment variables, and
// captures stdout and stderr together.
func (r *rig) run(name string, args []string, env map[string]string) result {
	r.t.Helper()
	c := *r.box.Ctx
	c.Env = c.Env.With(env)
	r.box.Stdout.Reset()
	r.box.Stderr.Reset()
	err := actions.Run(&c, name, args)
	res := result{out: r.box.Stdout.String() + r.box.Stderr.String()}
	var exit *fail.Exit
	switch {
	case err == nil:
	case errors.As(err, &exit):
		res.code = exit.Code
	default:
		res.code = 1
		res.out += err.Error()
	}
	return res
}

// debianOnly skips t off Linux: apt and apt-repos are Debian's actions,
// written for its bash. They use mapfile (bash 4), and macOS's /bin/bash is
// 3.2.
func debianOnly(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("apt and apt-repos are Debian-only actions; they need bash 4 (mapfile), and macOS's /bin/bash is 3.2")
	}
}

// The actions a Mac runs work in the bash it has: the sandbox's PATH ends
// in /usr/bin:/bin, so on macOS this is /bin/bash 3.2, with no mapfile,
// associative arrays or ${x,,}.
func TestMacActionsRunUnderTheSystemBash(t *testing.T) {
	mac := func(t *testing.T) *rig {
		r := newRigWith(t, testutil.Options{Git: true, OS: app.MacOS})
		version, _, _ := strings.Cut(proc.Run(r.box.Ctx, []string{"bash", "--version"}, proc.Options{}).Stdout, "\n")
		t.Log(version)
		// CI does not run with -v, so the log above is never seen on a green
		// run: on a Mac, require the system bash 3.2 or these stop proving
		// what they claim (a runner image, or a new pathDirs entry, could
		// put a newer bash first).
		if runtime.GOOS == "darwin" && !strings.HasPrefix(version, "GNU bash, version 3.") {
			t.Fatalf("bash --version starts %q, want it to start \"GNU bash, version 3.\": the Mac actions must run under the system bash 3.2", version)
		}
		return r
	}
	t.Run("brew installs each Brewfile and stops at a missing one", func(t *testing.T) {
		r := mac(t)
		log := filepath.Join(r.box.Dir, "brew.log")
		r.box.Files(map[string]string{
			"home/.home/manifests/macos/Brewfile":      "brew 'jq'\n",
			"home/.home/manifests/macos/Work.Brewfile": "cask 'zed'\n",
		})
		r.box.WithStubs(map[string]string{"brew": `echo "brew $*" >> "$LOG"`})
		env := map[string]string{"LOG": log}
		if res := r.run("brew", nil, env); res.code != 0 || res.out != "" {
			t.Errorf("no Brewfiles: exit %d: %q", res.code, res.out)
		}
		if res := r.run("brew", []string{"macos/Brewfile", "macos/Work.Brewfile"}, env); res.code != 0 {
			t.Fatalf("exit %d: %q", res.code, res.out)
		}
		if got, want := read(t, log), "brew bundle install --file=macos/Brewfile\nbrew bundle install --file=macos/Work.Brewfile\n"; got != want {
			t.Errorf("log %q, want %q", got, want)
		}
		if res := r.run("brew", []string{"macos/Nope"}, env); res.code != 1 || !strings.Contains(res.out, "Brewfile not found: manifests/macos/Nope") {
			t.Errorf("missing Brewfile: exit %d: %q", res.code, res.out)
		}
	})
	t.Run("fonts copies a folder's fonts flat into ~/Library/Fonts", func(t *testing.T) {
		r := mac(t)
		r.box.Files(map[string]string{
			"home/.home/furniture/fonts/Mono/Mono-Regular.ttf":   "ttf",
			"home/.home/furniture/fonts/Mono/static/Mono-It.otf": "otf",
			"home/.home/furniture/fonts/Mono/OFL.txt":            "license",
		})
		res := r.run("fonts", []string{"Mono"}, nil)
		if res.code != 0 || res.out != "fonts: Mono (2 files)\n" {
			t.Fatalf("exit %d: %q", res.code, res.out)
		}
		if got := walkFiles(t, filepath.Join(r.box.Home, "Library/Fonts")); !slices.Equal(got, []string{"Mono-It.otf", "Mono-Regular.ttf"}) {
			t.Errorf("~/Library/Fonts holds %q", got)
		}
		if res := r.run("fonts", []string{"Gone"}, nil); res.code != 1 || !strings.Contains(res.out, "font folder not found: furniture/fonts/Gone") {
			t.Errorf("missing folder: exit %d: %q", res.code, res.out)
		}
	})
}

func TestAptReposExport(t *testing.T) {
	debianOnly(t)
	t.Run("copies third-party repos and their keys, nothing from the distro", func(t *testing.T) {
		r := newRig(t)
		sys := filepath.Join(r.box.Dir, "forge")
		put(t, sys, forge)
		res := r.run("apt-repos", []string{"--export", r.path("manifests/linux/apt-repos")}, map[string]string{"HOMEBOT_APT_ROOT": sys})
		if res.code != 0 {
			t.Fatalf("exit %d: %s", res.code, res.out)
		}
		out := r.path("manifests/linux/apt-repos")
		if got := walkFiles(t, out); !slices.Equal(got, exported) {
			t.Errorf("exported %v, want %v", got, exported)
		}
		if got := read(t, filepath.Join(out, "origin")); got != "noble\n" {
			t.Errorf("origin = %q", got)
		}
		if got := read(t, filepath.Join(out, "keyrings/docker.gpg")); got != "DOCKER-KEY" {
			t.Errorf("docker key = %q", got)
		}
		if got := read(t, filepath.Join(out, "docker.list")); got != forge["etc/apt/sources.list.d/docker.list"] {
			t.Errorf("docker.list = %q", got)
		}
	})

	t.Run("an empty machine exports only the origin", func(t *testing.T) {
		r := newRig(t)
		sys := filepath.Join(r.box.Dir, "bare")
		put(t, sys, map[string]string{"etc/os-release": "VERSION_CODENAME=trixie\n"})
		if err := os.MkdirAll(filepath.Join(sys, "etc/apt/sources.list.d"), 0o755); err != nil {
			t.Fatal(err)
		}
		res := r.run("apt-repos", []string{"--export", r.path("out")}, map[string]string{"HOMEBOT_APT_ROOT": sys})
		if res.code != 0 {
			t.Fatalf("exit %d: %s", res.code, res.out)
		}
		if got := walkFiles(t, r.path("out")); !slices.Equal(got, []string{"origin"}) {
			t.Errorf("exported %v", got)
		}
	})

	t.Run("--export without a folder is a usage error", func(t *testing.T) {
		r := newRig(t)
		res := r.run("apt-repos", []string{"--export"}, nil)
		if res.code != 2 || !strings.Contains(res.out, "usage") {
			t.Errorf("got exit %d, %q", res.code, res.out)
		}
	})
}

func TestAptReposInstall(t *testing.T) {
	debianOnly(t)
	type setup struct {
		*rig
		log string
	}
	stubs := map[string]string{"apt-get": `echo "apt-get $*" >> "$LOG"`, "sudo": `echo "sudo $*" >> "$LOG"; exit 1`}
	prepare := func(t *testing.T) *setup {
		s := &setup{rig: newRig(t)}
		s.log = filepath.Join(s.box.Dir, "apt.log")
		s.box.WithStubs(stubs)
		sys := filepath.Join(s.box.Dir, "forge")
		put(t, sys, forge)
		res := s.run("apt-repos", []string{"--export", s.path("manifests/linux/apt-repos")}, map[string]string{"HOMEBOT_APT_ROOT": sys})
		if res.code != 0 {
			t.Fatalf("export: exit %d: %s", res.code, res.out)
		}
		return s
	}
	target := func(s *setup, codename string, files map[string]string) string {
		root := filepath.Join(s.box.Dir, "target-"+codename)
		all := map[string]string{"etc/os-release": "VERSION_CODENAME=" + codename + "\n"}
		for k, v := range files {
			all[k] = v
		}
		put(t, root, all)
		if err := os.MkdirAll(filepath.Join(root, "etc/apt/sources.list.d"), 0o755); err != nil {
			t.Fatal(err)
		}
		return root
	}
	install := func(s *setup, root string, args ...string) result {
		if args == nil {
			args = []string{"linux/apt-repos"}
		}
		return s.run("apt-repos", args, map[string]string{"HOMEBOT_APT_ROOT": root, "LOG": s.log})
	}
	calls := func(s *setup) string {
		b, _ := os.ReadFile(s.log)
		return string(b)
	}

	t.Run("adds every repo and key to a machine of the same release, then updates once", func(t *testing.T) {
		s := prepare(t)
		tgt := target(s, "noble", nil)
		res := install(s, tgt)
		if res.code != 0 {
			t.Fatalf("exit %d: %s", res.code, res.out)
		}
		want := []string{"1password.sources", "docker.list", "inline.sources", "wezterm.list"}
		if got := walkFiles(t, filepath.Join(tgt, "etc/apt/sources.list.d")); !slices.Equal(got, want) {
			t.Errorf("sources %v, want %v", got, want)
		}
		if got := read(t, filepath.Join(tgt, "etc/apt/keyrings/docker.gpg")); got != "DOCKER-KEY" {
			t.Errorf("docker key %q", got)
		}
		if got := read(t, filepath.Join(tgt, "usr/share/keyrings/wezterm-fury.gpg")); got != "WEZTERM-KEY" {
			t.Errorf("wezterm key %q", got)
		}
		if got := read(t, filepath.Join(tgt, "usr/share/keyrings/1password-archive-keyring.gpg")); got != "1PASSWORD-KEY" {
			t.Errorf("1password key %q", got)
		}
		if !strings.Contains(res.out, "apt-repos: added docker.list") {
			t.Errorf("output %q", res.out)
		}
		if got := calls(s); got != "apt-get update\n" {
			t.Errorf("calls %q", got)
		}
	})

	t.Run("a second run adds nothing and doesn't update", func(t *testing.T) {
		s := prepare(t)
		tgt := target(s, "noble", nil)
		install(s, tgt)
		if err := os.WriteFile(s.log, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		res := install(s, tgt)
		if res.code != 0 || !strings.Contains(res.out, "4 already set up") || strings.Contains(res.out, "added") {
			t.Errorf("exit %d: %q", res.code, res.out)
		}
		if got := calls(s); got != "" {
			t.Errorf("calls %q", got)
		}
	})

	t.Run("on another release, repos naming the old codename are skipped and said so", func(t *testing.T) {
		s := prepare(t)
		tgt := target(s, "trixie", nil)
		res := install(s, tgt)
		if res.code != 0 {
			t.Fatalf("exit %d: %s", res.code, res.out)
		}
		if !strings.Contains(res.out, "skipped docker.list: it is for noble and this machine runs trixie") {
			t.Errorf("output %q", res.out)
		}
		want := []string{"1password.sources", "inline.sources", "wezterm.list"}
		if got := walkFiles(t, filepath.Join(tgt, "etc/apt/sources.list.d")); !slices.Equal(got, want) {
			t.Errorf("sources %v, want %v", got, want)
		}
		if exists(filepath.Join(tgt, "etc/apt/keyrings/docker.gpg")) {
			t.Error("docker key installed for a skipped repo")
		}
	})

	t.Run("a repo already configured under another name is left alone", func(t *testing.T) {
		s := prepare(t)
		tgt := target(s, "noble", map[string]string{
			"etc/apt/sources.list.d/docker.sources": "Types: deb\nURIs: https://download.docker.com/linux/ubuntu/\nSuites: noble\nComponents: stable\n",
		})
		install(s, tgt)
		if exists(filepath.Join(tgt, "etc/apt/sources.list.d/docker.list")) {
			t.Error("docker.list added beside docker.sources")
		}
		if !exists(filepath.Join(tgt, "etc/apt/sources.list.d/wezterm.list")) {
			t.Error("wezterm.list not added")
		}
	})

	t.Run("a commented-out line doesn't count as configured", func(t *testing.T) {
		s := prepare(t)
		tgt := target(s, "noble", map[string]string{"etc/apt/sources.list": "# deb https://download.docker.com/linux/ubuntu noble stable\n"})
		install(s, tgt)
		if !exists(filepath.Join(tgt, "etc/apt/sources.list.d/docker.list")) {
			t.Error("docker.list not added")
		}
	})

	t.Run("a key already on the machine is kept, a missing one skips only its repo", func(t *testing.T) {
		s := prepare(t)
		for _, k := range []string{"keyrings/docker.gpg", "keyrings/wezterm-fury.gpg"} {
			if err := os.Remove(s.path("manifests/linux/apt-repos/" + k)); err != nil {
				t.Fatal(err)
			}
		}
		tgt := target(s, "noble", map[string]string{"usr/share/keyrings/wezterm-fury.gpg": "LOCAL-KEY"})
		res := install(s, tgt)
		if res.code != 0 {
			t.Fatalf("exit %d: %s", res.code, res.out)
		}
		if !strings.Contains(res.out, "skipped docker.list: its key docker.gpg is not in linux/apt-repos/keyrings") {
			t.Errorf("output %q", res.out)
		}
		if exists(filepath.Join(tgt, "etc/apt/sources.list.d/docker.list")) {
			t.Error("docker.list added without its key")
		}
		if !exists(filepath.Join(tgt, "etc/apt/sources.list.d/wezterm.list")) {
			t.Error("wezterm.list not added")
		}
		if got := read(t, filepath.Join(tgt, "usr/share/keyrings/wezterm-fury.gpg")); got != "LOCAL-KEY" {
			t.Errorf("wezterm key %q", got)
		}
	})

	t.Run("a missing folder fails with its name", func(t *testing.T) {
		s := prepare(t)
		res := install(s, target(s, "noble", nil), "linux/nope")
		if res.code != 1 || !strings.Contains(res.out, "folder not found: manifests/linux/nope") {
			t.Errorf("exit %d: %q", res.code, res.out)
		}
	})

	t.Run("no folder is a usage error", func(t *testing.T) {
		s := prepare(t)
		res := install(s, target(s, "noble", nil), []string{}...)
		if res.code != 2 {
			t.Errorf("exit %d: %q", res.code, res.out)
		}
	})
}

func TestAptSkipsUnavailablePackages(t *testing.T) {
	debianOnly(t)
	t.Run("installs the rest and names the skipped ones", func(t *testing.T) {
		r := newRig(t)
		log := filepath.Join(r.box.Dir, "apt.log")
		r.box.Files(map[string]string{"home/.home/manifests/linux/apt.txt": "jq\npop-desktop\nlibfoo:i386\ndocker-ce\n"})
		r.box.WithStubs(map[string]string{
			"apt-get":    `echo "apt-get $*" >> "$LOG"`,
			"dpkg-query": "exit 1",
			"apt-cache":  `printf "jq\nlibfoo\nzsh\n"`,
			"sudo":       `exec env "$@"`,
		})
		res := r.run("apt", []string{"linux/apt.txt"}, map[string]string{"LOG": log})
		if res.code != 0 {
			t.Fatalf("exit %d: %s", res.code, res.out)
		}
		for _, want := range []string{
			"apt: skipped 2 that no repository here offers: pop-desktop docker-ce",
			"apt: installing 2 of 4 packages",
		} {
			if !strings.Contains(res.out, want) {
				t.Errorf("output lacks %q: %q", want, res.out)
			}
		}
		if got := read(t, log); !strings.Contains(got, "apt-get install -y jq libfoo:i386") {
			t.Errorf("log %q", got)
		}
	})

	t.Run("nothing available means no install call", func(t *testing.T) {
		r := newRig(t)
		log := filepath.Join(r.box.Dir, "apt.log")
		r.box.Files(map[string]string{"home/.home/manifests/linux/apt.txt": "pop-desktop\n"})
		r.box.WithStubs(map[string]string{
			"apt-get": `echo "apt-get $*" >> "$LOG"`, "dpkg-query": "exit 1", "apt-cache": "true", "sudo": `exec env "$@"`,
		})
		res := r.run("apt", []string{"linux/apt.txt"}, map[string]string{"LOG": log})
		if res.code != 0 {
			t.Fatalf("exit %d: %s", res.code, res.out)
		}
		if got := read(t, log); got != "apt-get update\n" {
			t.Errorf("log %q", got)
		}
	})
}

func TestAptAction(t *testing.T) {
	debianOnly(t)
	run := func(t *testing.T) string {
		r := newRig(t)
		r.box.Files(map[string]string{"home/.home/manifests/linux/apt.txt": "jq\n"})
		r.box.WithStubs(map[string]string{
			"apt-get":    `echo "apt-get $DEBIAN_FRONTEND $@" >> "$LOG"`,
			"dpkg-query": "exit 1",
			"apt-cache":  `printf "jq\n"`,
			"sudo":       `echo "sudo $@" >> "$LOG"; exec env "$@"`,
		})
		log := filepath.Join(r.box.Dir, "apt.log")
		res := r.run("apt", []string{"linux/apt.txt"}, map[string]string{"LOG": log})
		if res.code != 0 {
			t.Fatalf("exit %d: %s", res.code, res.out)
		}
		return read(t, log)
	}

	t.Run("non-root goes through sudo with a noninteractive debconf", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("runs as root")
		}
		out := run(t)
		for _, want := range []string{"sudo DEBIAN_FRONTEND=noninteractive apt-get install -y jq", "apt-get noninteractive install -y jq"} {
			if !strings.Contains(out, want) {
				t.Errorf("log lacks %q: %q", want, out)
			}
		}
	})

	t.Run("root never calls sudo", func(t *testing.T) {
		if os.Geteuid() != 0 {
			t.Skip("runs as a user")
		}
		out := run(t)
		if strings.Contains(out, "sudo") || !strings.Contains(out, "apt-get noninteractive install -y jq") {
			t.Errorf("log %q", out)
		}
	})
}
