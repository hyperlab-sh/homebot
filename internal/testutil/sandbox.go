package testutil

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/proc"
)

// Options shape a sandbox.
type Options struct {
	// Config is the homebot.yml text; BaseConfig when empty.
	Config string
	// OS is the Context's OS; linux when empty.
	OS app.OS
	// Git makes the home repo a git repository on branch main.
	Git bool
	// BehindLink builds the sandbox in a folder reached through a symlink,
	// as $HOME is when it lives on another volume, and as every sandbox is
	// on macOS (its temp dir is under /var, a link to /private/var). Dir
	// and every path under it name the link; a program's working directory
	// and git's top level, as the kernel reports them, name the folder it
	// points at. Off macOS, this is how a test meets that case.
	BehindLink bool
	// RepoBehindLink makes the home repo, home/.home, a symlink to a folder
	// elsewhere in $WORK, as it is when ~/.home points at another volume
	// while $HOME itself does not. Root names the link; the folder it
	// points at is outside Home.
	RepoBehindLink bool
	// RepoLinkedFromOutside puts the home repo inside $HOME, at
	// home/dotfiles, and makes Root a link to it from outside, $WORK/srv/dotfiles,
	// as HOMEBOT_ROOT=/srv/dotfiles is when /srv/dotfiles links to ~/dotfiles.
	RepoLinkedFromOutside bool
}

// Box is a sandbox: a temp $WORK holding a home with a home repo in
// home/.home, and a Context that sees only that.
type Box struct {
	t testing.TB
	// Dir is $WORK.
	Dir string
	// Home is $WORK/home.
	Home string
	// Root is the home repo, $WORK/home/.home (see Options for the others).
	Root string
	// Bin is $WORK/bin, first on the Context's PATH.
	Bin string
	Ctx *app.Context
	// Stdout and Stderr collect what the Context's writers receive.
	Stdout *bytes.Buffer
	Stderr *bytes.Buffer
}

// Sandbox builds a sandbox under t.TempDir(), laid out as the TypeScript
// tests' sandbox() was: autolink/{common,linux,macos}, private/{common,linux}
// with its .gitignore, manifests/profiles and homebot.yml. The Context uses
// the environment of SandboxEnv, host testhost and the fixed test clock.
func Sandbox(t testing.TB, opts Options) *Box {
	t.Helper()
	work := t.TempDir()
	if opts.BehindLink {
		real, link := filepath.Join(work, "real"), filepath.Join(work, "link")
		if err := os.Mkdir(real, 0o755); err != nil {
			t.Fatalf("testutil: %v", err)
		}
		if err := os.Symlink(real, link); err != nil {
			t.Fatalf("testutil: %v", err)
		}
		work = link
	}
	b := &Box{
		t:      t,
		Dir:    work,
		Home:   filepath.Join(work, "home"),
		Root:   filepath.Join(work, "home", ".home"),
		Bin:    filepath.Join(work, "bin"),
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
	}
	if opts.RepoLinkedFromOutside {
		real := filepath.Join(b.Home, "dotfiles")
		b.Root = filepath.Join(work, "srv", "dotfiles")
		b.mkdir(real)
		b.mkdir(filepath.Dir(b.Root))
		if err := os.Symlink(real, b.Root); err != nil {
			t.Fatalf("testutil: %v", err)
		}
	}
	if opts.RepoBehindLink {
		real := filepath.Join(work, "elsewhere", "home-repo")
		b.mkdir(real)
		b.mkdir(b.Home)
		if err := os.Symlink(real, b.Root); err != nil {
			t.Fatalf("testutil: %v", err)
		}
	}
	for _, d := range []string{"autolink/common", "autolink/linux", "autolink/macos", "private/common", "private/linux", "manifests/profiles"} {
		b.mkdir(filepath.Join(b.Root, d))
	}
	b.mkdir(b.Bin)
	config := opts.Config
	if config == "" {
		config = BaseConfig
	}
	repoRel, err := filepath.Rel(b.Dir, b.Root)
	if err != nil {
		t.Fatalf("testutil: %v", err)
	}
	b.Files(map[string]string{
		filepath.Join(repoRel, "homebot.yml"):        config,
		filepath.Join(repoRel, "private/.gitignore"): PrivateGitignore,
		"gitconfig": GitConfig,
	})

	osName := opts.OS
	if osName == "" {
		osName = app.Linux
	}
	ctx, err := app.New(app.Options{
		Env:    app.NewEnv(SandboxEnv(work)),
		Cwd:    work,
		Root:   b.Root,
		OS:     osName,
		Host:   TestHost,
		Stdout: b.Stdout,
		Stderr: b.Stderr,
	})
	if err != nil {
		t.Fatalf("testutil: building the sandbox Context: %v", err)
	}
	b.Ctx = ctx

	if opts.Git {
		b.GitIn(b.Root, "init", "-q", "-b", "main")
		b.GitIn(b.Root, "config", "user.email", "test@example.com")
		b.GitIn(b.Root, "config", "user.name", "Test")
		b.GitIn(b.Root, "config", "commit.gpgsign", "false")
	}
	return b
}

// Files writes files given by paths relative to $WORK, creating their
// directories: {"home/.home/autolink/common/zshrc": "..."}.
func (b *Box) Files(files map[string]string) {
	b.t.Helper()
	for rel, content := range files {
		path := filepath.Join(b.Dir, filepath.FromSlash(rel))
		b.mkdir(filepath.Dir(path))
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			b.t.Fatalf("testutil: %v", err)
		}
	}
}

// GitIn runs git -C dir with args in the sandbox's environment and returns
// its stdout. A failing git fails the test.
func (b *Box) GitIn(dir string, args ...string) string {
	b.t.Helper()
	r := proc.Run(b.Ctx, append([]string{"git", "-C", dir}, args...), proc.Options{})
	if r.Code != 0 {
		b.t.Fatalf("git %s: exit %d: %s", strings.Join(args, " "), r.Code, r.Stderr)
	}
	return r.Stdout
}

// WithStubs writes each script as an executable named after its key into
// $WORK/bin, first on the Context's PATH, and returns that directory. The
// body runs under #!/bin/sh. Stubs stay for the rest of the test; writing a
// name again replaces its stub.
func (b *Box) WithStubs(stubs map[string]string) string {
	b.t.Helper()
	for name, body := range stubs {
		path := filepath.Join(b.Bin, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			b.t.Fatalf("testutil: %v", err)
		}
		// WriteFile keeps the mode of an existing file; a stub must be executable.
		if err := os.Chmod(path, 0o755); err != nil {
			b.t.Fatalf("testutil: %v", err)
		}
	}
	return b.Bin
}

func (b *Box) mkdir(dir string) {
	b.t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		b.t.Fatalf("testutil: %v", err)
	}
}
