package guard

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/secrets"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// newRepo is git-flow.test.ts's sandbox: a home repo with one commit, and a
// gitleaks that finds nothing, so only the check under test speaks.
func newRepo(t *testing.T, config string) *testutil.Box {
	t.Helper()
	b := testutil.Sandbox(t, testutil.Options{Git: true, Config: config})
	b.Files(map[string]string{"home/.home/README.md": "home\n"})
	b.GitIn(b.Root, "add", "-A")
	b.GitIn(b.Root, "commit", "-qm", "init")
	b.WithStubs(map[string]string{"gitleaks": "exit 0"})
	// git runs hooks from the top of the work tree being committed.
	b.Ctx.Cwd = b.Root
	return b
}

// preCommit runs the guard and returns its exit code and what it printed
// to stderr, which is all it prints.
func preCommit(t *testing.T, b *testutil.Box) (int, string) {
	t.Helper()
	b.Stderr.Reset()
	b.Stdout.Reset()
	code := PreCommit(b.Ctx)
	if b.Stdout.Len() != 0 {
		t.Errorf("PreCommit printed to stdout: %q", b.Stdout.String())
	}
	return code, b.Stderr.String()
}

func mustRefuse(t *testing.T, b *testutil.Box) string {
	t.Helper()
	code, out := preCommit(t, b)
	if code != 1 {
		t.Fatalf("PreCommit = %d, want 1 (refused); stderr:\n%s", code, out)
	}
	return out
}

func mustPass(t *testing.T, b *testutil.Box) {
	t.Helper()
	if code, out := preCommit(t, b); code != 0 || out != "" {
		t.Fatalf("PreCommit = %d with stderr %q, want 0 and nothing", code, out)
	}
}

func blocked(t *testing.T, b *testutil.Box) *state.Blocked {
	t.Helper()
	s, err := state.Load(b.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	return s.Blocked
}

func sparse(t *testing.T, b *testutil.Box, rel string, size int64) {
	t.Helper()
	p := filepath.Join(b.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// git-flow.test.ts "pre-commit guard" > "passes clean commits".
func TestPreCommitPassesCleanCommits(t *testing.T) {
	b := newRepo(t, "")
	b.Files(map[string]string{"home/.home/README.md": "changed\n"})
	b.GitIn(b.Root, "add", "README.md")
	mustPass(t, b)
	if got := blocked(t, b); got != nil {
		t.Errorf("blocked = %+v after a clean run", got)
	}
}

// "seals changed secrets and stages the .age files".
func TestPreCommitSealsChangedSecretsAndStagesTheAgeFiles(t *testing.T) {
	b := newRepo(t, "")
	testutil.TestKeys(t, b.Root)
	b.Files(map[string]string{"home/.home/private/common/netrc": "machine x login y password z"})
	mustPass(t, b)
	staged := b.GitIn(b.Root, "diff", "--cached", "--name-only")
	if !strings.Contains(staged, "private/common/netrc.age\n") {
		t.Errorf("staged %q lacks private/common/netrc.age", staged)
	}
	if strings.Contains(staged, "private/common/netrc\n") {
		t.Errorf("staged %q holds the plaintext", staged)
	}
}

// "refuses plaintext under private/, even when force-added".
func TestPreCommitRefusesPlaintextUnderPrivate(t *testing.T) {
	b := newRepo(t, "")
	b.Files(map[string]string{"home/.home/private/common/token": "abc"})
	b.GitIn(b.Root, "add", "-f", "private/common/token")
	out := mustRefuse(t, b)
	want := "homebot refused this commit:\n" +
		"  - private/common/token is plaintext. Only *.age files may be committed from private/. Unstage it:\n" +
		"    git rm --cached private/common/token\n"
	if out != want {
		t.Errorf("stderr:\n%s\nwant:\n%s", out, want)
	}
	got := blocked(t, b)
	if got == nil || got.Reason != "private/common/token is plaintext. Only *.age files may be committed from private/. Unstage it:" {
		t.Errorf("blocked = %+v, want the first line of the problem", got)
	}
	if got != nil && got.At != testutil.TestNow {
		t.Errorf("blocked.at = %d, want the clock's %d", got.At, testutil.TestNow)
	}
}

// Sealed files and private/'s own plain files pass; only plaintext secrets
// are refused.
func TestPreCommitAllowsAgeFilesAndPrivatesOwnFiles(t *testing.T) {
	b := newRepo(t, "")
	testutil.TestKeys(t, b.Root)
	b.Files(map[string]string{
		"home/.home/private/README.md":          "what lives here\n",
		"home/.home/private/.gitkeep":           "",
		"home/.home/private/.gitignore":         "*\n",
		"home/.home/private/common/sealed.age":  "age-encryption.org/v1\n",
		"home/.home/autolink/common/private/ok": "not under the repo's private/\n",
	})
	b.GitIn(b.Root, "add", "-f", "private", "autolink")
	mustPass(t, b)
}

// A plaintext file renamed into private/ and one copied there are refused
// by their new path; a regular file staged over a committed symlink (a type
// change, which the TypeScript skipped) is refused too.
func TestPreCommitRefusesPlaintextArrivingByRenameCopyOrTypeChange(t *testing.T) {
	b := newRepo(t, "")
	b.Files(map[string]string{"home/.home/notes": "secret-ish\n"})
	if err := os.Symlink("../notes", filepath.Join(b.Root, "private", "link")); err != nil {
		t.Fatal(err)
	}
	b.GitIn(b.Root, "add", "-f", "notes", "private/link")
	b.GitIn(b.Root, "commit", "-qm", "two")

	b.GitIn(b.Root, "mv", "-f", "notes", "private/common/notes")
	if err := os.Remove(filepath.Join(b.Root, "private", "link")); err != nil {
		t.Fatal(err)
	}
	b.Files(map[string]string{"home/.home/private/link": "token\n"})
	b.GitIn(b.Root, "add", "-f", "private/link")
	out := mustRefuse(t, b)
	for _, want := range []string{"  - private/common/notes is plaintext.", "  - private/link is plaintext."} {
		if !strings.Contains(out, want) {
			t.Errorf("stderr lacks %q:\n%s", want, out)
		}
	}
}

// "refuses files over the size limit".
func TestPreCommitRefusesFilesOverTheSizeLimit(t *testing.T) {
	b := newRepo(t, "limits:\n  maxFileMB: 1\n")
	sparse(t, b, "furniture/big.bin", 2<<20)
	b.GitIn(b.Root, "add", "homebot.yml", "furniture/big.bin")
	changes, err := staged(b.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, sizes, err := stagedSizes(b.Ctx, changes)
	if err != nil {
		t.Fatal(err)
	}
	if sizes["furniture/big.bin"] != 2<<20 {
		t.Errorf("stagedSizes[furniture/big.bin] = %d, want %d", sizes["furniture/big.bin"], 2<<20)
	}
	if out := mustRefuse(t, b); !strings.Contains(out, "furniture/big.bin is 2.0 MB (limit 1 MB)") {
		t.Errorf("stderr:\n%s", out)
	}
}

// A file of exactly the limit is not over it.
func TestPreCommitAllowsAFileOfExactlyTheLimit(t *testing.T) {
	b := newRepo(t, "limits:\n  maxFileMB: 1\n")
	sparse(t, b, "furniture/big.bin", 1<<20)
	b.GitIn(b.Root, "add", "furniture/big.bin")
	mustPass(t, b)
}

// repos-identity.test.ts "size limit message". The TypeScript said "just
// over N MB (limit N MB)" where the rounded size equalled the limit; Go shows
// two decimals there (decision D2, a deliberate difference).
func TestSizeLimitMessage(t *testing.T) {
	cases := []struct {
		name   string
		config string
		bytes  int64
		want   string
	}{
		// "a file that rounds to the limit says just over it", reworded.
		{"rounds to the limit of 10", "limits:\n  maxFileMB: 10\n", 10<<20 + 300_000, "furniture/big.bin is 10.29 MB (limit 10 MB)"},
		{"rounds to the limit of 1", "limits:\n  maxFileMB: 1\n", 1<<20 + 20_000, "furniture/big.bin is 1.02 MB (limit 1 MB)"},
		// "a clearly bigger file shows its size".
		{"clearly bigger", "limits:\n  maxFileMB: 1\n", 3 << 20, "furniture/big.bin is 3.0 MB (limit 1 MB)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newRepo(t, tc.config)
			sparse(t, b, "furniture/big.bin", tc.bytes)
			b.GitIn(b.Root, "add", "homebot.yml", "furniture/big.bin")
			out := mustRefuse(t, b)
			if !strings.Contains(out, tc.want) {
				t.Errorf("stderr:\n%s\nwant it to hold %q", out, tc.want)
			}
			if strings.Contains(out, "just over") {
				t.Errorf("stderr still says just over:\n%s", out)
			}
		})
	}
}

func TestOverSize(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	const mb = 1 << 20
	cases := []struct {
		bytes   int64
		limitMB float64
		want    string
	}{
		{1_100_000, 1, "1.05 MB"},             // hook-pre-commit.txtar's big.bin
		{mb + 1, 1, "1.01 MB"},                // two decimals round to the limit: round up
		{mb + mb/200, 1, "1.01 MB"},           // 1.00499...: half up gives 1.00, so up
		{50*mb + mb/5, 50, "50.20 MB"},        // whole MB from 10 up
		{60 * mb, 50, "60 MB"},                // clearly over: format.size
		{3 * mb, 1, "3.0 MB"},                 // one decimal under 10
		{mb*3/2 + 1, 1.5, "1.51 MB"},          // a fractional limit
		{600_000, 0.5, "586 KB"},              // over a limit under 1 MB: not in MB
		{2*mb - 1, 1.99, "2.0 MB"},            // rounds above the limit
		{1<<30 + 1, 1024, "1.0 GB"},           // GB is never reworded
		{mb*1023 + mb/2 + 1, 1023, "1024 MB"}, // rounds above a limit near a GB
	}
	for _, tc := range cases {
		if got := overSize(b.Ctx, tc.bytes, tc.limitMB); got != tc.want {
			t.Errorf("overSize(%d, %v) = %q, want %q", tc.bytes, tc.limitMB, got, tc.want)
		}
	}
}

// A fractional limit is written as JavaScript writes the number.
func TestSizeLimitMessageWithAFractionalLimit(t *testing.T) {
	b := newRepo(t, "limits:\n  maxFileMB: 0.5\n")
	sparse(t, b, "big.bin", 600_000)
	b.GitIn(b.Root, "add", "big.bin")
	if out := mustRefuse(t, b); !strings.Contains(out, "  - big.bin is 586 KB (limit 0.5 MB). Unstage it and add it to .gitignore.\n") {
		t.Errorf("stderr:\n%s", out)
	}
}

// stagedSizes skips gitlinks (submodule commits, mode 160000): they have no
// blob. The gitlink here names a commit of the repo itself, so sizing it
// would find an object and refuse it under a tiny limit.
func TestStagedSizesSkipsGitlinks(t *testing.T) {
	b := newRepo(t, "limits:\n  maxFileMB: 0.00001\n")
	head := strings.TrimSpace(b.GitIn(b.Root, "rev-parse", "HEAD"))
	b.GitIn(b.Root, "update-index", "--add", "--cacheinfo", "160000,"+head+",vendor/sub")
	changes, err := staged(b.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].NewMode != gitlinkMode || changes[0].NewID != head {
		t.Fatalf("staged = %+v, want the gitlink with mode 160000 and id %s", changes, head)
	}
	paths, sizes, err := stagedSizes(b.Ctx, changes)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 || len(sizes) != 0 {
		t.Errorf("stagedSizes = %v %v, want nothing", paths, sizes)
	}
	mustPass(t, b)
}

// Each staged blob is sized by its staged id, not by the file on disk.
func TestStagedSizesUseTheStagedBlob(t *testing.T) {
	b := newRepo(t, "")
	b.Files(map[string]string{"home/.home/a": "12345", "home/.home/dir/b c": "1"})
	b.GitIn(b.Root, "add", "a", "dir/b c")
	b.Files(map[string]string{"home/.home/a": "longer on disk than staged"})
	changes, err := staged(b.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	paths, sizes, err := stagedSizes(b.Ctx, changes)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(paths, "|") != "a|dir/b c" || sizes["a"] != 5 || sizes["dir/b c"] != 1 {
		t.Errorf("stagedSizes = %q %v", paths, sizes)
	}
}

// "a later clean run clears the refusal".
func TestALaterCleanRunClearsTheRefusal(t *testing.T) {
	b := newRepo(t, "")
	b.Files(map[string]string{"home/.home/private/common/token": "abc"})
	b.GitIn(b.Root, "add", "-f", "private/common/token")
	mustRefuse(t, b)
	if blocked(t, b) == nil {
		t.Fatal("no blocked state after a refusal")
	}
	b.GitIn(b.Root, "rm", "-q", "--cached", "private/common/token")
	mustPass(t, b)
	if got := blocked(t, b); got != nil {
		t.Errorf("blocked = %+v after a clean run", got)
	}
}

// A guard that cannot record its verdict refuses the commit, as the
// TypeScript's throw did.
func TestPreCommitRefusesWhenStateCannotBeWritten(t *testing.T) {
	b := newRepo(t, "")
	if err := os.MkdirAll(filepath.Dir(b.Ctx.StateDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b.Ctx.StateDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := preCommit(t, b)
	if code != 1 || !strings.HasPrefix(out, "homebot: ") {
		t.Errorf("PreCommit = %d with stderr %q, want 1 and a homebot: error", code, out)
	}
}

// A git that cannot list the staged files refuses the commit; the
// TypeScript read empty output and let it through unchecked.
func TestPreCommitRefusesWhenTheStagedFilesCannotBeListed(t *testing.T) {
	b := newRepo(t, "")
	if err := os.WriteFile(filepath.Join(b.Root, ".git", "index"), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := mustRefuse(t, b)
	if !strings.Contains(out, "  - Could not check the staged files: git diff --cached failed.") {
		t.Errorf("stderr:\n%s", out)
	}
}

// A secret that cannot be sealed refuses the commit.
func TestPreCommitRefusesWhenSealingFails(t *testing.T) {
	b := newRepo(t, "")
	b.Files(map[string]string{
		"home/.home/private/recipients.txt": "not a recipient\n",
		"home/.home/private/common/netrc":   "machine x",
	})
	out := mustRefuse(t, b)
	if !strings.Contains(out, "  - Could not seal secrets: ") {
		t.Errorf("stderr:\n%s", out)
	}
}

// The refusal is the wire format package save parses: the bytes pinned in
// testdata/guard (Task 13 parses the same files).
func TestRefusalMatchesTheWireFormat(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "guard", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	t.Run("refusal-gitleaks", func(t *testing.T) {
		b := newRepo(t, "")
		b.Files(map[string]string{"home/.home/autolink/common/zshrc": "token = x\n"})
		b.GitIn(b.Root, "add", "-A")
		b.WithStubs(map[string]string{"gitleaks": "printf 'Finding:     token = REDACTED\\nFile:        autolink/common/zshrc\\n'; exit 99"})
		if out, want := mustRefuse(t, b), read("refusal-gitleaks.txt"); out != want {
			t.Errorf("stderr:\n%q\nwant:\n%q", out, want)
		}
	})

	t.Run("refusal-two", func(t *testing.T) {
		b := newRepo(t, "")
		b.Files(map[string]string{"home/.home/private/a": "plain\n"})
		sparse(t, b, "big.bin", 60<<20)
		b.GitIn(b.Root, "add", "-f", "private/a", "big.bin")
		if out, want := mustRefuse(t, b), read("refusal-two.txt"); out != want {
			t.Errorf("stderr:\n%q\nwant:\n%q", out, want)
		}
	})
}

// With colour on, the headline is in the alarm style and a problem's
// command in the command style; the bullets and indents stay plain, so the
// wire format is the same once the styles are stripped.
func TestRefusalStyles(t *testing.T) {
	b := newRepo(t, "")
	b.Files(map[string]string{"home/.home/private/common/token": "abc"})
	b.GitIn(b.Root, "add", "-f", "private/common/token")
	env := map[string]string{}
	for k, v := range testutil.SandboxEnv(b.Dir) {
		env[k] = v
	}
	delete(env, "NO_COLOR")
	env["CLICOLOR_FORCE"] = "1"
	env["COLORFGBG"] = "15;0"
	c, err := app.New(app.Options{Env: app.NewEnv(env), Cwd: b.Root, Root: b.Root, Host: "testhost", Stderr: b.Stderr})
	if err != nil {
		t.Fatal(err)
	}
	if code := PreCommit(c); code != 1 {
		t.Fatalf("PreCommit = %d", code)
	}
	out := b.Stderr.String()
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("no styles in %q", out)
	}
	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(lines[0], "\x1b[") || !strings.HasPrefix(lines[1], "  - private/common/token") || !strings.HasPrefix(lines[2], "    \x1b[") {
		t.Errorf("styled refusal:\n%q", out)
	}
}

// git-flow.test.ts "gitleaks compatibility", with a stub gitleaks.
func TestGitleaksCompatibility(t *testing.T) {
	stage := func(t *testing.T) *testutil.Box {
		b := newRepo(t, "")
		b.Files(map[string]string{"home/.home/autolink/common/x": "x\n"})
		b.GitIn(b.Root, "add", "-A")
		return b
	}
	gitless := `if [ "$1" = git ]; then echo 'unknown command "git" for "gitleaks"' >&2; exit 1; fi` + "\n"

	t.Run("a leak (exit 99) is reported as a leak", func(t *testing.T) {
		b := stage(t)
		b.WithStubs(map[string]string{"gitleaks": `echo "Finding: redacted" ; exit 99`})
		out := mustRefuse(t, b)
		for _, want := range []string{"gitleaks found something", "    Finding: redacted\n"} {
			if !strings.Contains(out, want) {
				t.Errorf("stderr lacks %q:\n%s", want, out)
			}
		}
		if got := blocked(t, b); got == nil || !strings.HasPrefix(got.Reason, "gitleaks found something that looks like a secret.") {
			t.Errorf("blocked = %+v", got)
		}
	})

	t.Run("asks gitleaks for a distinct leak exit code and keeps --redact", func(t *testing.T) {
		b := stage(t)
		bin := b.WithStubs(map[string]string{"gitleaks": `echo "$@" > "$0.args"; pwd -P > "$0.cwd"; exit 0`})
		mustPass(t, b)
		args := readFile(t, filepath.Join(bin, "gitleaks.args"))
		want := "git --pre-commit --staged --redact --exit-code 99 --no-banner --log-level warn " + b.Root + "\n"
		if args != want {
			t.Errorf("gitleaks args %q, want %q", args, want)
		}
		// The folder itself, links resolved on both sides: macOS's temp dir
		// is under /var, a link to /private/var.
		repo, err := filepath.EvalSymlinks(b.Root)
		if err != nil {
			t.Fatal(err)
		}
		if cwd := readFile(t, filepath.Join(bin, "gitleaks.cwd")); cwd != repo+"\n" {
			t.Errorf("gitleaks ran in %q, want the repo, %s", cwd, repo)
		}
	})

	t.Run("a gitleaks that cannot run refuses the commit honestly (fail closed)", func(t *testing.T) {
		b := stage(t)
		b.WithStubs(map[string]string{"gitleaks": `echo "boom: broken config" >&2; echo "more" >&2; exit 1`})
		out := mustRefuse(t, b)
		if !strings.Contains(out, "  - gitleaks failed to run: boom: broken config\n") {
			t.Errorf("stderr:\n%s", out)
		}
		if strings.Contains(out, "looks like a secret") || strings.Contains(out, "more") {
			t.Errorf("stderr:\n%s", out)
		}
	})

	t.Run("a gitleaks that fails silently names its exit code", func(t *testing.T) {
		b := stage(t)
		b.WithStubs(map[string]string{"gitleaks": `exit 2`})
		if out := mustRefuse(t, b); !strings.Contains(out, "  - gitleaks failed to run: exit code 2\n") {
			t.Errorf("stderr:\n%s", out)
		}
	})

	t.Run("a leak exit code without output is still a leak", func(t *testing.T) {
		b := stage(t)
		b.WithStubs(map[string]string{"gitleaks": `exit 99`})
		if out := mustRefuse(t, b); !strings.Contains(out, "  - gitleaks found something that looks like a secret.") {
			t.Errorf("stderr:\n%s", out)
		}
	})

	t.Run("falls back to `protect --staged` when there is no `git` subcommand", func(t *testing.T) {
		b := stage(t)
		bin := b.WithStubs(map[string]string{"gitleaks": gitless + `echo "$@" > "$0.args"; exit 0`})
		mustPass(t, b)
		args := readFile(t, filepath.Join(bin, "gitleaks.args"))
		want := "protect --staged --redact --exit-code 99 --no-banner --log-level warn --source " + b.Root + "\n"
		if args != want {
			t.Errorf("gitleaks args %q, want %q", args, want)
		}
	})

	t.Run("the fallback still tells leaks from failures", func(t *testing.T) {
		b := stage(t)
		b.WithStubs(map[string]string{"gitleaks": gitless + `exit 99`})
		if out := mustRefuse(t, b); !strings.Contains(out, "gitleaks found something") {
			t.Errorf("stderr:\n%s", out)
		}
		b.WithStubs(map[string]string{"gitleaks": gitless + `echo "fallback broke" >&2; exit 1`})
		if out := mustRefuse(t, b); !strings.Contains(out, "  - gitleaks failed to run: fallback broke\n") {
			t.Errorf("stderr:\n%s", out)
		}
	})

	t.Run("a run that passes never falls back, whatever it prints", func(t *testing.T) {
		b := stage(t)
		bin := b.WithStubs(map[string]string{"gitleaks": `echo "$1" >> "$0.calls"; echo 'unknown command "git"' >&2; exit 0`})
		mustPass(t, b)
		if calls := readFile(t, filepath.Join(bin, "gitleaks.calls")); calls != "git\n" {
			t.Errorf("gitleaks calls %q: a run that passed must not fall back", calls)
		}
	})

	t.Run("a leak exit code is a leak, never a fallback, whatever it prints", func(t *testing.T) {
		b := stage(t)
		bin := b.WithStubs(map[string]string{"gitleaks": `echo "$1" >> "$0.calls"; [ "$1" = git ] || exit 0; echo 'Finding: unknown command "git"' >&2; exit 99`})
		if out := mustRefuse(t, b); !strings.Contains(out, "gitleaks found something") {
			t.Errorf("stderr:\n%s", out)
		}
		if calls := readFile(t, filepath.Join(bin, "gitleaks.calls")); calls != "git\n" {
			t.Errorf("gitleaks calls %q: a leak must not fall back", calls)
		}
	})

	t.Run("no gitleaks refuses the commit and says how to install it", func(t *testing.T) {
		b := newRepo(t, "")
		if err := os.Remove(filepath.Join(b.Bin, "gitleaks")); err != nil {
			t.Fatal(err)
		}
		if proc.HasCommand(b.Ctx, "gitleaks") {
			t.Skip("the system's PATH has a gitleaks")
		}
		out := mustRefuse(t, b)
		want := "  - gitleaks is not installed, so this commit can't be checked for leaked secrets. Install it:\n    brew install gitleaks\n"
		if !strings.Contains(out, want) {
			t.Errorf("stderr:\n%s\nwant it to hold:\n%s", out, want)
		}
	})
}

// "refuses what gitleaks flags, with the secret redacted", with the real
// gitleaks, through its git subcommand and through the protect fallback.
func TestRealGitleaks(t *testing.T) {
	real, err := exec.LookPath("gitleaks")
	if err != nil {
		t.Skip("gitleaks is not installed")
	}
	token := "ghp_" + "Ab1Cd2Ef3Gh4Ij5Kl6Mn7Op8Qr9St0Uv1Wx2"
	stage := func(t *testing.T) *testutil.Box {
		b := newRepo(t, "")
		b.Files(map[string]string{"home/.home/autolink/common/config/tool/config.toml": "token = \"" + token + "\"\n"})
		b.GitIn(b.Root, "add", "-A")
		return b
	}
	modes := []struct{ name, stub string }{
		{"git subcommand", `exec ` + real + ` "$@"`},
		{"protect fallback", `if [ "$1" = git ]; then echo 'unknown command "git" for "gitleaks"' >&2; exit 1; fi` + "\nexec " + real + ` "$@"`},
	}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			b := stage(t)
			b.WithStubs(map[string]string{"gitleaks": m.stub})
			out := mustRefuse(t, b)
			if !strings.Contains(out, "gitleaks found something") {
				t.Errorf("stderr:\n%s", out)
			}
			if strings.Contains(out, token) {
				t.Errorf("stderr shows the secret:\n%s", out)
			}
			if s := readFile(t, filepath.Join(b.Ctx.StateDir, "state.json")); strings.Contains(s, token) {
				t.Errorf("state.json holds the secret")
			}
		})
		t.Run(m.name+" passes a clean commit", func(t *testing.T) {
			b := newRepo(t, "")
			b.WithStubs(map[string]string{"gitleaks": m.stub})
			b.Files(map[string]string{"home/.home/autolink/common/zshrc": "export A=1\n"})
			b.GitIn(b.Root, "add", "-A")
			mustPass(t, b)
		})
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// otherHome makes a second home repo in the box, as HOMEBOT_ROOT might name.
func otherHome(t *testing.T, b *testutil.Box) string {
	t.Helper()
	b.Files(map[string]string{"other/homebot.yml": testutil.BaseConfig, "other/README.md": "other\n"})
	other := filepath.Join(b.Dir, "other")
	b.GitIn(other, "init", "-q", "-b", "main")
	b.GitIn(other, "add", "-A")
	b.GitIn(other, "commit", "-qm", "init")
	return other
}

// hookContext is a Context as homebot hook pre-commit gets one from git:
// Root unset, Cwd the top of the work tree, env with extra set.
func hookContext(t *testing.T, b *testutil.Box, cwd string, extra map[string]string) *app.Context {
	t.Helper()
	env := testutil.SandboxEnv(b.Dir)
	for k, v := range extra {
		env[k] = v
	}
	c, err := app.New(app.Options{Env: app.NewEnv(env), Cwd: cwd, Host: testutil.TestHost, Stdout: b.Stdout, Stderr: b.Stderr})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func setBlocked(t *testing.T, b *testutil.Box, reason string) {
	t.Helper()
	if err := state.Update(b.Ctx, func(s *state.State) { s.Blocked = &state.Blocked{Reason: reason, At: 1} }); err != nil {
		t.Fatal(err)
	}
}

// The guard checks the repo git is committing in, not the home
// HOMEBOT_ROOT names: the TypeScript checked that other home's index and
// let the plaintext through. The other home's blocked state is left alone.
func TestPreCommitChecksTheRepoGitIsCommittingIn(t *testing.T) {
	b := newRepo(t, "")
	other := otherHome(t, b)
	setBlocked(t, b, "the other home's refusal")
	b.Files(map[string]string{"home/.home/private/common/token": "abc"})
	b.GitIn(b.Root, "add", "-f", "private/common/token")

	c := hookContext(t, b, b.Root, map[string]string{"HOMEBOT_ROOT": other})
	b.Stderr.Reset()
	if code := PreCommit(c); code != 1 {
		t.Fatalf("PreCommit = %d, want 1; stderr:\n%s", code, b.Stderr.String())
	}
	if out := b.Stderr.String(); !strings.Contains(out, "  - private/common/token is plaintext.") {
		t.Errorf("stderr:\n%s", out)
	}
	if got := blocked(t, b); got == nil || got.Reason != "the other home's refusal" {
		t.Errorf("blocked = %+v, want the other home's refusal untouched", got)
	}
}

// With HOMEBOT_ROOT naming the home being committed, the guard records
// its verdict as usual.
func TestPreCommitRecordsStateForItsOwnHome(t *testing.T) {
	b := newRepo(t, "")
	b.Files(map[string]string{"home/.home/private/common/token": "abc"})
	b.GitIn(b.Root, "add", "-f", "private/common/token")
	c := hookContext(t, b, b.Root, map[string]string{"HOMEBOT_ROOT": b.Root})
	if code := PreCommit(c); code != 1 {
		t.Fatalf("PreCommit = %d, want 1", code)
	}
	if got := blocked(t, b); got == nil || !strings.HasPrefix(got.Reason, "private/common/token is plaintext.") {
		t.Errorf("blocked = %+v", got)
	}
}

// A repo without homebot.yml is refused with one line, even when
// HOMEBOT_ROOT names a home.
func TestPreCommitRefusesARepoThatIsNotAHome(t *testing.T) {
	b := newRepo(t, "")
	plain := filepath.Join(b.Dir, "plain")
	b.Files(map[string]string{"plain/notes": "x\n"})
	b.GitIn(plain, "init", "-q", "-b", "main")
	b.GitIn(plain, "add", "-A")
	setBlocked(t, b, "untouched")
	c := hookContext(t, b, plain, map[string]string{"HOMEBOT_ROOT": b.Root})
	b.Stderr.Reset()
	code := PreCommit(c)
	want := "homebot: " + realPath(plain) + " has no homebot.yml, so this commit can't be checked. Refusing.\n"
	if code != 1 || b.Stderr.String() != want {
		t.Errorf("PreCommit = %d with stderr %q, want 1 and %q", code, b.Stderr.String(), want)
	}
	if got := blocked(t, b); got == nil || got.Reason != "untouched" {
		t.Errorf("blocked = %+v", got)
	}
}

// A git repo nested in a home (a submodule, a vendored clone) is not that
// home.
func TestPreCommitRefusesARepoNestedInAHome(t *testing.T) {
	b := newRepo(t, "")
	nested := filepath.Join(b.Root, "vendor", "lib")
	b.Files(map[string]string{"home/.home/vendor/lib/x": "x\n"})
	b.GitIn(nested, "init", "-q", "-b", "main")
	c := hookContext(t, b, nested, nil)
	b.Stderr.Reset()
	if code := PreCommit(c); code != 1 || !strings.Contains(b.Stderr.String(), realPath(nested)+" has no homebot.yml") {
		t.Errorf("PreCommit = %d with stderr %q", code, b.Stderr.String())
	}
}

// Run by hand outside any repo (as the parity recording does), the guard
// checks the home it locates, as the TypeScript did.
func TestPreCommitRunByHandOutsideARepoChecksTheLocatedHome(t *testing.T) {
	b := newRepo(t, "")
	b.Files(map[string]string{"home/.home/private/common/token": "abc"})
	b.GitIn(b.Root, "add", "-f", "private/common/token")
	c := hookContext(t, b, b.Dir, nil)
	b.Stderr.Reset()
	if code := PreCommit(c); code != 1 || !strings.Contains(b.Stderr.String(), "private/common/token is plaintext") {
		t.Errorf("PreCommit = %d with stderr %q", code, b.Stderr.String())
	}
}

// Run by git (GIT_INDEX_FILE set) where git can't name the work tree, the
// guard refuses rather than guess a home.
func TestPreCommitRefusesWhenGitCannotNameTheRepo(t *testing.T) {
	b := newRepo(t, "")
	c := hookContext(t, b, b.Dir, map[string]string{"GIT_INDEX_FILE": filepath.Join(b.Root, ".git", "index")})
	b.Stderr.Reset()
	if code := PreCommit(c); code != 1 || !strings.HasPrefix(b.Stderr.String(), "homebot: ") {
		t.Errorf("PreCommit = %d with stderr %q", code, b.Stderr.String())
	}
}

// private/ is matched without regard to case: on a case-insensitive file
// system git stores Private/token as typed, and it is the same directory.
func TestPreCommitRefusesPlaintextUnderPrivateInAnyCase(t *testing.T) {
	b := newRepo(t, "")
	b.Files(map[string]string{"home/.home/token-src": "password=1\n"})
	blob := strings.TrimSpace(b.GitIn(b.Root, "hash-object", "-w", "token-src"))
	for _, p := range []string{"Private/token", "PRIVATE/common/token"} {
		b.GitIn(b.Root, "update-index", "--add", "--cacheinfo", "100644,"+blob+","+p)
	}
	out := mustRefuse(t, b)
	for _, want := range []string{"  - Private/token is plaintext", "  - PRIVATE/common/token is plaintext"} {
		if !strings.Contains(out, want) {
			t.Errorf("stderr lacks %q:\n%s", want, out)
		}
	}
}

// A private/**.age file must hold age ciphertext: the staged blob is read,
// not the file on disk.
func TestPreCommitChecksThatAgeFilesAreSealed(t *testing.T) {
	const header = "age-encryption.org/v1\n-> X25519 abc\n"
	const armored = "-----BEGIN AGE ENCRYPTED FILE-----\nYWdlLWVuY3J5cHRpb24ub3JnL3YxCg==\n-----END AGE ENCRYPTED FILE-----\n"

	t.Run("plaintext named .age is refused", func(t *testing.T) {
		b := newRepo(t, "")
		b.Files(map[string]string{"home/.home/private/common/x.age": "password=hunter2\n"})
		b.GitIn(b.Root, "add", "-f", "private/common/x.age")
		want := "  - private/common/x.age is named like a sealed file but is not age-encrypted. Unstage it:\n    git rm --cached private/common/x.age\n"
		if out := mustRefuse(t, b); !strings.Contains(out, want) {
			t.Errorf("stderr:\n%s\nwant it to hold:\n%s", out, want)
		}
	})

	t.Run("the staged blob counts, not the file on disk", func(t *testing.T) {
		b := newRepo(t, "")
		b.Files(map[string]string{"home/.home/private/common/x.age": "password=hunter2\n"})
		b.GitIn(b.Root, "add", "-f", "private/common/x.age")
		b.Files(map[string]string{"home/.home/private/common/x.age": header})
		if out := mustRefuse(t, b); !strings.Contains(out, "x.age is named like a sealed file") {
			t.Errorf("stderr:\n%s", out)
		}
	})

	t.Run("an empty .age file is refused", func(t *testing.T) {
		b := newRepo(t, "")
		b.Files(map[string]string{"home/.home/private/common/x.age": ""})
		b.GitIn(b.Root, "add", "-f", "private/common/x.age")
		mustRefuse(t, b)
	})

	t.Run("binary and armored age files pass", func(t *testing.T) {
		b := newRepo(t, "")
		b.Files(map[string]string{
			"home/.home/private/common/a.age": header,
			"home/.home/private/linux/b.age":  armored,
			"home/.home/private/c.age":        "\n  " + armored,
		})
		b.GitIn(b.Root, "add", "-f", "private")
		mustPass(t, b)
	})

	t.Run("a real sealed file passes", func(t *testing.T) {
		b := newRepo(t, "")
		testutil.TestKeys(t, b.Root)
		b.GitIn(b.Root, "add", "-f", "private")
		mustPass(t, b)
	})

	t.Run("a .age file outside private/ is not checked", func(t *testing.T) {
		b := newRepo(t, "")
		b.Files(map[string]string{"home/.home/autolink/common/notes.age": "just text\n"})
		b.GitIn(b.Root, "add", "-A")
		mustPass(t, b)
	})
}

// private/'s own plain files are allowed only directly under it; the same
// names deeper down are plaintext, and so is a gitlink anywhere in
// private/. The TypeScript let both through.
func TestPreCommitAllowsPrivatesOwnFilesOnlyAtItsTop(t *testing.T) {
	b := newRepo(t, "")
	b.Files(map[string]string{
		"home/.home/private/a/README.md":          "nested\n",
		"home/.home/private/a/.gitignore":         "!*\n",
		"home/.home/private/a/b/.gitkeep":         "",
		"home/.home/private/linux/recipients.txt": "age1x\n",
	})
	b.GitIn(b.Root, "add", "-f", "private")
	head := strings.TrimSpace(b.GitIn(b.Root, "rev-parse", "HEAD"))
	b.GitIn(b.Root, "update-index", "--add", "--cacheinfo", "160000,"+head+",private/sub")
	out := mustRefuse(t, b)
	for _, want := range []string{
		"  - private/a/README.md is plaintext, and private/a/.gitignore does not ignore it,",
		"  - private/a/.gitignore is plaintext, and private/a/.gitignore does not ignore it,",
		"  - private/a/b/.gitkeep is plaintext, and private/a/.gitignore does not ignore it,",
		"  - private/linux/recipients.txt is plaintext.",
		"  - private/sub is a repository (a submodule or an embedded repo). Only *.age files may be committed from private/. Unstage it:\n    git rm --cached private/sub\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stderr lacks %q:\n%s", want, out)
		}
	}
}

// A gitlink outside private/ is not the guard's business.
func TestPreCommitAllowsAGitlinkOutsidePrivate(t *testing.T) {
	b := newRepo(t, "")
	head := strings.TrimSpace(b.GitIn(b.Root, "rev-parse", "HEAD"))
	b.GitIn(b.Root, "update-index", "--add", "--cacheinfo", "160000,"+head+",vendor/sub")
	mustPass(t, b)
}

// A gitlink at the path private itself, standing in for the whole
// directory, is refused too.
func TestPreCommitRefusesAGitlinkAtPrivate(t *testing.T) {
	for _, p := range []string{"private", "Private"} {
		t.Run(p, func(t *testing.T) {
			b := newRepo(t, "")
			head := strings.TrimSpace(b.GitIn(b.Root, "rev-parse", "HEAD"))
			b.GitIn(b.Root, "rm", "-rq", "--cached", "private")
			b.GitIn(b.Root, "update-index", "--add", "--cacheinfo", "160000,"+head+","+p)
			want := "  - " + p + " is a repository (a submodule or an embedded repo). Only *.age files may be committed from private/. Unstage it:\n    git rm --cached " + p + "\n"
			if out := mustRefuse(t, b); !strings.Contains(out, want) {
				t.Errorf("stderr:\n%s\nwant it to hold:\n%s", out, want)
			}
		})
	}
}

// sealedFile is content that passes the guard's age header check.
const sealedFile = "age-encryption.org/v1\n-> X25519 x\nbody\n"

// commitSealed commits sealed files at the given repo paths, spelled as
// given. On a case-insensitive file system (macOS) git's core.ignorecase
// would store Private/x.age as private/x.age, the case private/ already has
// in the index; with it off for the add, the index holds Private/ as a
// repo committed elsewhere can.
func commitSealed(t *testing.T, b *testutil.Box, rels ...string) {
	t.Helper()
	files := map[string]string{}
	for _, rel := range rels {
		files["home/.home/"+rel] = sealedFile
	}
	b.Files(files)
	b.GitIn(b.Root, append([]string{"-c", "core.ignorecase=false", "add", "--"}, rels...)...)
	b.GitIn(b.Root, "commit", "-qm", "sealed")
}

// F3: private/ moved to another volume and linked back. save stages the
// link and, since git sees nothing beyond a symlink, the deletion of every
// sealed file. The guard refuses both.
func TestPreCommitRefusesASymlinkAtPrivate(t *testing.T) {
	b := newRepo(t, "")
	commitSealed(t, b, "private/common/a.age", "private/b.age")
	vault := filepath.Join(b.Dir, "vault")
	if err := os.Rename(filepath.Join(b.Root, "private"), vault); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(vault, filepath.Join(b.Root, "private")); err != nil {
		t.Fatal(err)
	}
	// As save stages.
	b.GitIn(b.Root, "add", "-u", "--", ".")
	b.GitIn(b.Root, "add", "--", "private")
	if staged := b.GitIn(b.Root, "diff", "--cached", "--raw", "--", "private"); !strings.Contains(staged, " 120000 ") {
		t.Fatalf("the link is not staged:\n%s", staged)
	}
	out := mustRefuse(t, b)
	for _, want := range []string{
		"  - private is staged as a symlink or a file, not a folder: committing it would drop every sealed secret from the repo. Make private a real folder again, then unstage it:\n    git rm --cached private\n",
		"  - This commit deletes all 2 sealed files in private/ and leaves none: private/ is missing, moved or emptied. If that is a mistake, put it back and unstage the deletions with the first command. If you mean to remove your secrets from the repo, commit with git yourself, in the home repo, with the second:\n    git restore --staged private\n    git commit --no-verify -- private\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stderr lacks %q:\n%s", want, out)
		}
	}
}

// F3: a symlink or a plain file staged at private, in any case, is refused
// even when nothing sealed is deleted.
func TestPreCommitRefusesANonFolderAtPrivateInAnyCase(t *testing.T) {
	for _, tc := range []struct{ path, mode string }{
		{"private", "120000"},
		{"Private", "120000"},
		{"PRIVATE", "100644"},
		{"private", "100755"},
	} {
		t.Run(tc.path+"-"+tc.mode, func(t *testing.T) {
			b := newRepo(t, "")
			blob := strings.TrimSpace(b.GitIn(b.Root, "hash-object", "-w", "--", "README.md"))
			b.GitIn(b.Root, "rm", "-rq", "--cached", "private")
			b.GitIn(b.Root, "update-index", "--add", "--cacheinfo", tc.mode+","+blob+","+tc.path)
			want := "  - " + tc.path + " is staged as a symlink or a file, not a folder: committing it would drop every sealed secret from the repo. Make private a real folder again, then unstage it:\n    git rm --cached " + tc.path + "\n"
			if out := mustRefuse(t, b); !strings.Contains(out, want) {
				t.Errorf("stderr:\n%s\nwant it to hold:\n%s", out, want)
			}
		})
	}
}

// F3: a commit that deletes every sealed file under private/ and leaves none
// is refused, whatever the case of the folder; deleting some of them, or all
// while adding another, is not.
func TestPreCommitRefusesDeletingEverySealedFile(t *testing.T) {
	t.Run("all", func(t *testing.T) {
		b := newRepo(t, "")
		commitSealed(t, b, "private/common/a.age", "private/identity.age")
		b.GitIn(b.Root, "rm", "-q", "--cached", "private/common/a.age", "private/identity.age")
		out := mustRefuse(t, b)
		want := "  - This commit deletes all 2 sealed files in private/ and leaves none: private/ is missing, moved or emptied. If that is a mistake, put it back and unstage the deletions with the first command. If you mean to remove your secrets from the repo, commit with git yourself, in the home repo, with the second:\n    git restore --staged private\n    git commit --no-verify -- private\n"
		if !strings.Contains(out, want) {
			t.Errorf("stderr:\n%s\nwant it to hold:\n%s", out, want)
		}
	})
	// In another case than private/: the same folder on macOS.
	t.Run("the only one", func(t *testing.T) {
		b := newRepo(t, "")
		commitSealed(t, b, "Private/token.age")
		if got := b.GitIn(b.Root, "ls-files", "--", "Private"); got != "Private/token.age\n" {
			t.Fatalf("the index holds %q, want Private/token.age", got)
		}
		b.GitIn(b.Root, "rm", "-q", "--cached", "Private/token.age")
		if out := mustRefuse(t, b); !strings.Contains(out, "  - This commit deletes 1 sealed file in private/ and leaves none") {
			t.Errorf("stderr:\n%s", out)
		}
	})
	t.Run("some", func(t *testing.T) {
		b := newRepo(t, "")
		commitSealed(t, b, "private/common/a.age", "private/identity.age")
		b.GitIn(b.Root, "rm", "-q", "--cached", "private/common/a.age")
		mustPass(t, b)
	})
	t.Run("all, adding another", func(t *testing.T) {
		b := newRepo(t, "")
		commitSealed(t, b, "private/common/a.age")
		b.GitIn(b.Root, "rm", "-q", "--cached", "private/common/a.age")
		b.Files(map[string]string{"home/.home/private/linux/b.age": sealedFile})
		b.GitIn(b.Root, "add", "--", "private/linux/b.age")
		mustPass(t, b)
	})
	t.Run("plain files only", func(t *testing.T) {
		b := newRepo(t, "")
		b.GitIn(b.Root, "rm", "-q", "--cached", "private/.gitignore")
		mustPass(t, b)
	})
}

// D14: private/.gitignore, as homebot writes it, and the guard's allowlist
// agree. Everything git stages from private/ (as save stages it: git add
// -- private) is something the guard lets through, and everything the guard
// lets through, git stages. testutil's copy is the same file.
func TestPrivateGitignoreAgreesWithTheGuard(t *testing.T) {
	if testutil.PrivateGitignore != secrets.PrivateGitignore {
		t.Fatalf("testutil.PrivateGitignore = %q, want secrets.PrivateGitignore %q", testutil.PrivateGitignore, secrets.PrivateGitignore)
	}
	if len(privateAllowed) != len(secrets.PrivateDocs) {
		t.Fatalf("privateAllowed = %v, want secrets.PrivateDocs %v", privateAllowed, secrets.PrivateDocs)
	}
	b := newRepo(t, "")
	// The hook seals the plaintext it finds before it checks: it needs keys.
	testutil.TestKeys(t, b.Root)
	b.Files(map[string]string{"home/.home/private/.gitignore": secrets.PrivateGitignore})
	files := map[string]string{}
	allowed := []string{"private/.gitignore"}
	for _, name := range secrets.PrivateDocs {
		if !privateAllowed[strings.ToLower(name)] {
			t.Errorf("the guard does not allow private/%s", name)
		}
		if name != ".gitignore" {
			// TestKeys wrote recipients.txt.
			if name != "recipients.txt" {
				files["private/"+name] = "doc\n"
			}
			allowed = append(allowed, "private/"+name)
		}
		// Deeper down the same names are plaintext, and git must not take them.
		files["private/common/"+name] = "plain\n"
		files["private/linux/(ssh)/"+name] = "plain\n"
	}
	for _, rel := range []string{"private/common/netrc.age", "private/linux/(ssh)/config.age"} {
		files[rel] = sealedFile
		allowed = append(allowed, rel)
	}
	for _, rel := range []string{"private/token", "private/common/netrc", "private/linux/(ssh)/config", "private/notes.txt", "private/common/.age.txt"} {
		files[rel] = "plain\n"
	}
	for rel, content := range files {
		b.Files(map[string]string{"home/.home/" + rel: content})
	}
	b.GitIn(b.Root, "add", "--", "private")
	got := strings.Fields(b.GitIn(b.Root, "ls-files", "--cached", "--", "private"))
	allowed = append(allowed, "private/identity.age")
	slices.Sort(got)
	slices.Sort(allowed)
	if !slices.Equal(got, allowed) {
		t.Errorf("git stages %q from private/, want exactly what the guard allows %q", got, allowed)
	}
	if out := Check(b.Ctx); out != "" {
		t.Errorf("Check refuses what private/.gitignore lets git stage:\n%s", out)
	}
	mustPass(t, b)
}

// R1: a plaintext file that no pattern ignores, or that a .gitignore lets
// through with a negated pattern, gets past git add -- private on every
// save. The refusal names that .gitignore (private/.gitignore when nothing
// matches) and says to edit it, beside the git rm --cached advice. One
// force-added past a pattern that ignores it keeps the plain advice.
func TestPreCommitPlaintextNamesTheGitignoreThatLetItThrough(t *testing.T) {
	refusal := func(file, gitignore string) string {
		return "  - " + file + " is plaintext, and " + gitignore + " does not ignore it, so every save stages it again. " +
			"Only *.age files may be committed from private/: edit " + gitignore + " so it ignores the file, then unstage it:\n" +
			"    git rm --cached " + file + "\n"
	}
	t.Run("a negated pattern in private/.gitignore", func(t *testing.T) {
		b := newRepo(t, "")
		b.Files(map[string]string{
			"home/.home/private/.gitignore":   "*\n!*/\n!*.age\n!token\n",
			"home/.home/private/common/token": "abc",
			"home/.home/private/common/other": "abc",
		})
		b.GitIn(b.Root, "add", "--", "private")
		out := mustRefuse(t, b)
		want := "homebot refused this commit:\n" + refusal("private/common/token", "private/.gitignore")
		if out != want {
			t.Errorf("stderr:\n%s\nwant:\n%s", out, want)
		}
		got := blocked(t, b)
		if got == nil || !strings.HasPrefix(got.Reason, "private/common/token is plaintext, and private/.gitignore does not ignore it,") {
			t.Errorf("blocked = %+v", got)
		}
	})
	t.Run("a nested .gitignore", func(t *testing.T) {
		b := newRepo(t, "")
		b.Files(map[string]string{
			"home/.home/private/linux/.gitignore": "!netrc\n",
			"home/.home/private/linux/netrc":      "machine x password y\n",
		})
		b.GitIn(b.Root, "add", "--", "private")
		out := mustRefuse(t, b)
		// private/.gitignore ignores the nested .gitignore itself, so git
		// stages only netrc.
		want := "homebot refused this commit:\n" + refusal("private/linux/netrc", "private/linux/.gitignore")
		if out != want {
			t.Errorf("stderr:\n%s\nwant:\n%s", out, want)
		}
	})
	t.Run("no private/.gitignore", func(t *testing.T) {
		b := newRepo(t, "")
		if err := os.Remove(filepath.Join(b.Root, "private", ".gitignore")); err != nil {
			t.Fatal(err)
		}
		b.Files(map[string]string{"home/.home/private/common/token": "abc"})
		b.GitIn(b.Root, "add", "-A")
		if out := mustRefuse(t, b); !strings.Contains(out, refusal("private/common/token", "private/.gitignore")) {
			t.Errorf("stderr:\n%s", out)
		}
	})
	t.Run("force-added past private/.gitignore", func(t *testing.T) {
		b := newRepo(t, "")
		b.Files(map[string]string{"home/.home/private/common/token": "abc"})
		b.GitIn(b.Root, "add", "-f", "private/common/token")
		want := "  - private/common/token is plaintext. Only *.age files may be committed from private/. Unstage it:\n    git rm --cached private/common/token\n"
		if out := mustRefuse(t, b); !strings.HasSuffix(out, want) {
			t.Errorf("stderr:\n%s\nwant it to end with:\n%s", out, want)
		}
	})
}

// R1: the guard compares the .age suffix and private/'s own file names
// without regard to case, as git does when core.ignorecase is on (macOS):
// there private/.gitignore's !*.age lets x.AGE through. The age header
// check still applies to every .age file, whatever its case.
func TestPreCommitComparesAgeAndPrivatesOwnFilesInAnyCase(t *testing.T) {
	stage := func(t *testing.T, b *testutil.Box, files map[string]string) {
		t.Helper()
		for rel, content := range files {
			b.Files(map[string]string{"home/.home/src": content})
			blob := strings.TrimSpace(b.GitIn(b.Root, "hash-object", "-w", "src"))
			b.GitIn(b.Root, "update-index", "--add", "--cacheinfo", "100644,"+blob+","+rel)
		}
		if err := os.Remove(filepath.Join(b.Root, "src")); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("sealed files and own files pass", func(t *testing.T) {
		b := newRepo(t, "")
		stage(t, b, map[string]string{
			"private/common/x.AGE":   sealedFile,
			"private/linux/y.Age":    sealedFile,
			"private/README.MD":      "docs\n",
			"private/Recipients.TXT": "age1x\n",
			"private/.GitKeep":       "",
		})
		mustPass(t, b)
	})
	t.Run("an unsealed .AGE file is refused as not sealed", func(t *testing.T) {
		b := newRepo(t, "")
		stage(t, b, map[string]string{"private/common/x.AGE": "password=1\n"})
		want := "  - private/common/x.AGE is named like a sealed file but is not age-encrypted. Unstage it:\n    git rm --cached private/common/x.AGE\n"
		if out := mustRefuse(t, b); !strings.Contains(out, want) {
			t.Errorf("stderr:\n%s\nwant it to hold:\n%s", out, want)
		}
	})
	t.Run("own names deeper down are still plaintext", func(t *testing.T) {
		b := newRepo(t, "")
		stage(t, b, map[string]string{"private/common/README.MD": "x\n"})
		if out := mustRefuse(t, b); !strings.Contains(out, "  - private/common/README.MD is plaintext") {
			t.Errorf("stderr:\n%s", out)
		}
	})
}

// Check is the hook's verdict without the hook: the same lines, without
// styles, and no state written.
func TestCheckReturnsTheRefusalTheHookPrints(t *testing.T) {
	b := newRepo(t, "")
	if out := Check(b.Ctx); out != "" {
		t.Fatalf("Check = %q with nothing staged", out)
	}
	b.Files(map[string]string{"home/.home/private/common/token": "abc"})
	b.GitIn(b.Root, "add", "-f", "private/common/token")
	want := "homebot refused this commit:\n" +
		"  - private/common/token is plaintext. Only *.age files may be committed from private/. Unstage it:\n" +
		"    git rm --cached private/common/token\n"
	if got := Check(b.Ctx); got != want {
		t.Errorf("Check =\n%q\nwant\n%q", got, want)
	}
	if got := blocked(t, b); got != nil {
		t.Errorf("Check wrote blocked = %+v", got)
	}
	if b.Stderr.Len() != 0 || b.Stdout.Len() != 0 {
		t.Errorf("Check printed %q / %q", b.Stdout, b.Stderr)
	}
}
