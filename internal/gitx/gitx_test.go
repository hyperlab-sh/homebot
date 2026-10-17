package gitx_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/hyperlab-sh/homebot/internal/gitx"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func commitAll(b *testutil.Box, dir, msg string) {
	b.GitIn(dir, "add", "-A")
	b.GitIn(dir, "commit", "-q", "-m", msg)
}

func TestIsRepoAndHasCommits(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	if gitx.IsRepo(b.Ctx, b.Root) {
		t.Error("a plain directory is not a repo")
	}
	b2 := testutil.Sandbox(t, testutil.Options{Git: true})
	if !gitx.IsRepo(b2.Ctx, b2.Root) {
		t.Error("expected a repo")
	}
	if gitx.HasCommits(b2.Ctx, b2.Root) {
		t.Error("a fresh repo has no commits")
	}
	b2.Files(map[string]string{"home/.home/f": "x"})
	commitAll(b2, b2.Root, "init")
	if !gitx.HasCommits(b2.Ctx, b2.Root) {
		t.Error("expected commits")
	}
}

func TestRunDoesNotQuotePaths(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{Git: true})
	b.Files(map[string]string{"home/.home/café.txt": "x"})
	b.GitIn(b.Root, "add", "-A")
	if out := gitx.Run(b.Ctx, b.Root, "diff", "--cached", "--name-only").Stdout; !strings.Contains("\n"+out, "\ncafé.txt\n") {
		t.Errorf("out = %q", out)
	}
}

func TestStatusAndStaged(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{Git: true})
	r := b.Root
	b.Files(map[string]string{
		"home/.home/plain":          "1\n",
		"home/.home/with space.txt": "2\n",
		"home/.home/café.txt":       "3\n",
		"home/.home/old\nname":      "a long enough body that rename detection is sure about it\nline2\nline3\n",
		"home/.home/gone":           "bye\n",
	})
	commitAll(b, r, "base")

	st, err := gitx.Status(b.Ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch != "main" || st.Upstream != "" || st.Ahead != 0 || st.Behind != 0 || len(st.Changed) != 0 {
		t.Fatalf("clean repo: %+v", st)
	}

	b.Files(map[string]string{
		"home/.home/plain":          "changed\n",
		"home/.home/with space.txt": "changed\n",
		"home/.home/café.txt":       "changed\n",
		"home/.home/untracked":      "u\n",
	})
	b.GitIn(r, "mv", "old\nname", "new name é")
	b.GitIn(r, "rm", "-q", "gone")
	b.Files(map[string]string{"home/.home/added": "new\n"})
	b.GitIn(r, "add", "added")

	st, err = gitx.Status(b.Ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"added", "café.txt", "gone", "new name é", "plain", "with space.txt"}
	got := append([]string(nil), st.Changed...)
	slices.Sort(got)
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("Changed (-want +got):\n%s", d)
	}
	for _, e := range st.Entries {
		if e.Kind == gitx.EntryRenamed && (e.Path != "new name é" || e.OrigPath != "old\nname") {
			t.Errorf("rename entry = %+v", e)
		}
	}

	// Staged: additions and renames, not the deletion; unstaged edits are not staged.
	staged, err := gitx.StagedFiles(b.Ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(staged)
	if d := cmp.Diff([]string{"added", "new name é"}, staged); d != "" {
		t.Errorf("StagedFiles (-want +got):\n%s", d)
	}
	changes, err := gitx.StagedChanges(b.Ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range changes {
		if ch.Status == "R" && ch.OrigPath != "old\nname" {
			t.Errorf("rename = %+v", ch)
		}
	}
}

func TestStatusAgainstAnUpstream(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{Git: true})
	r := b.Root
	remote := filepath.Join(b.Dir, "remote.git")
	b.GitIn(b.Dir, "init", "-q", "--bare", "-b", "main", remote)
	b.Files(map[string]string{"home/.home/f": "1"})
	commitAll(b, r, "one")
	b.GitIn(r, "remote", "add", "origin", remote)
	b.GitIn(r, "push", "-q", "-u", "origin", "main")

	// a second clone pushes two commits; the first makes one of its own
	other := filepath.Join(b.Dir, "other")
	b.GitIn(b.Dir, "clone", "-q", remote, other)
	for _, n := range []string{"a", "b"} {
		b.Files(map[string]string{"other/" + n: n})
		commitAll(b, other, n)
	}
	b.GitIn(other, "push", "-q")
	b.Files(map[string]string{"home/.home/mine": "m"})
	commitAll(b, r, "mine")
	b.GitIn(r, "fetch", "-q")

	st, err := gitx.Status(b.Ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if st.Upstream != "origin/main" || st.Ahead != 1 || st.Behind != 2 {
		t.Errorf("status = %+v", st)
	}

	refs, err := gitx.LsRemote(b.Ctx, r, proc.Options{}, "origin", "refs/heads/*")
	if err != nil || len(refs) != 1 || refs[0].Name != "refs/heads/main" || len(refs[0].ID) != 40 {
		t.Errorf("refs=%+v err=%v", refs, err)
	}
	if _, err := gitx.LsRemote(b.Ctx, r, proc.Options{}, filepath.Join(b.Dir, "no-such-remote")); err == nil {
		t.Error("expected an error for a missing remote")
	}
}

func TestStatusDetachedAndNotARepo(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{Git: true})
	b.Files(map[string]string{"home/.home/f": "1"})
	commitAll(b, b.Root, "one")
	b.GitIn(b.Root, "checkout", "-q", "--detach")
	st, err := gitx.Status(b.Ctx, b.Root)
	if err != nil || st.Branch != "" {
		t.Errorf("detached: %+v %v", st, err)
	}
	plain := testutil.Sandbox(t, testutil.Options{})
	if _, err := gitx.Status(plain.Ctx, plain.Root); err == nil {
		t.Error("a directory outside a repo must fail")
	}
}

func TestCatFileCheck(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{Git: true})
	b.Files(map[string]string{"home/.home/a b": "12345", "home/.home/n\nl": "x"})
	commitAll(b, b.Root, "one")
	got, err := gitx.CatFileCheck(b.Ctx, b.Root, []string{"HEAD:a b", "HEAD:nope\nnope", "HEAD:n\nl", "HEAD:", "HEAD:a missing\nb"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Missing || got[0].Type != "blob" || got[0].Size != 5 || len(got[0].ID) != 40 {
		t.Errorf("a b = %+v", got[0])
	}
	if !got[1].Missing {
		t.Errorf("nope = %+v", got[1])
	}
	if got[2].Missing || got[2].Size != 1 {
		t.Errorf("n\\nl = %+v", got[2])
	}
	if !got[4].Missing {
		t.Errorf("a missing\\nb = %+v", got[4])
	}
	if got[3].Type != "tree" {
		t.Errorf("tree = %+v", got[3])
	}
	if o, err := gitx.CatFileCheck(b.Ctx, b.Root, nil); o != nil || err != nil {
		t.Errorf("no specs: %v %v", o, err)
	}
}

func TestRunNetNeverPrompts(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{Git: true})
	b.WithStubs(map[string]string{"git": `echo "$GIT_TERMINAL_PROMPT"`})
	if out := gitx.RunNet(b.Ctx, b.Root, proc.Options{}, "ls-remote").Stdout; out != "0\n" {
		t.Errorf("RunNet: GIT_TERMINAL_PROMPT = %q", out)
	}
	if out := gitx.Run(b.Ctx, b.Root, "status").Stdout; out != "\n" {
		t.Errorf("Run: GIT_TERMINAL_PROMPT = %q", out)
	}
}

func sh(b *testutil.Box, cmd string, env map[string]string) proc.Result {
	return proc.Run(b.Ctx, []string{"env", "-u", "GIT_CONFIG_GLOBAL", "bash", "-c", cmd}, proc.Options{Env: env})
}

// ts: "git identity"
func TestIdentity(t *testing.T) {
	setup := func(t *testing.T) (*testutil.Box, string) {
		b := testutil.Sandbox(t, testutil.Options{Git: true})
		b.Files(map[string]string{"gitconfig": ""})
		b.GitIn(b.Root, "config", "--unset", "user.name")
		b.GitIn(b.Root, "config", "--unset", "user.email")
		return b, filepath.Join(b.Dir, "gitconfig")
	}
	t.Run("a new machine has none", func(t *testing.T) {
		b, _ := setup(t)
		if got := gitx.Identity(b.Ctx, b.Root); got != (gitx.Author{}) {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("the global config counts", func(t *testing.T) {
		b, global := setup(t)
		must(t, os.WriteFile(global, []byte("[user]\n\tname = Ada\n\temail = ada@example.com\n"), 0o644))
		if got := gitx.Identity(b.Ctx, b.Root); got != (gitx.Author{Name: "Ada", Email: "ada@example.com"}) {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("a name alone is half an identity", func(t *testing.T) {
		b, global := setup(t)
		must(t, os.WriteFile(global, []byte("[user]\n\tname = Ada\n"), 0o644))
		if got := gitx.Identity(b.Ctx, b.Root); got != (gitx.Author{Name: "Ada"}) {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("git's environment variables count", func(t *testing.T) {
		b, _ := setup(t)
		b.Ctx.Env = b.Ctx.Env.With(map[string]string{"GIT_AUTHOR_NAME": "Ada", "EMAIL": "ada@example.com"})
		if got := gitx.Identity(b.Ctx, b.Root); got != (gitx.Author{Name: "Ada", Email: "ada@example.com"}) {
			t.Errorf("got %+v", got)
		}
		b.Ctx.Env = b.Ctx.Env.With(map[string]string{"GIT_AUTHOR_EMAIL": "author@example.com"})
		if got := gitx.Identity(b.Ctx, b.Root); got.Email != "author@example.com" {
			t.Errorf("GIT_AUTHOR_EMAIL must win over EMAIL: %+v", got)
		}
	})
	t.Run("the repo's own config counts, and beats the environment", func(t *testing.T) {
		b, _ := setup(t)
		b.Ctx.Env = b.Ctx.Env.With(map[string]string{"GIT_AUTHOR_NAME": "Env"})
		b.GitIn(b.Root, "config", "user.name", "Repo")
		b.GitIn(b.Root, "config", "user.email", "repo@example.com")
		if got := gitx.Identity(b.Ctx, b.Root); got != (gitx.Author{Name: "Repo", Email: "repo@example.com"}) {
			t.Errorf("got %+v", got)
		}
	})
}

// ts: "missingSigningKey"
func TestMissingSigningKey(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{Git: true})
	key := filepath.Join(b.Home, ".ssh", "id_rsa")
	sign := func(key, format string) {
		b.GitIn(b.Root, "config", "commit.gpgsign", "true")
		b.GitIn(b.Root, "config", "gpg.format", format)
		b.GitIn(b.Root, "config", "user.signingkey", key)
	}
	t.Run("empty while signing is off", func(t *testing.T) {
		b.GitIn(b.Root, "config", "user.signingkey", filepath.Join(b.Home, ".ssh", "none"))
		if got := gitx.MissingSigningKey(b.Ctx, b.Root); got != "" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("the key path when signing is on and the file is missing", func(t *testing.T) {
		sign(key, "ssh")
		if got := gitx.MissingSigningKey(b.Ctx, b.Root); got != key {
			t.Errorf("got %q want %q", got, key)
		}
	})
	t.Run("a ~ path is expanded", func(t *testing.T) {
		sign("~/.ssh/id_rsa", "ssh")
		if got := gitx.MissingSigningKey(b.Ctx, b.Root); got != key {
			t.Errorf("got %q want %q", got, key)
		}
	})
	t.Run("empty once the key is there", func(t *testing.T) {
		b.Files(map[string]string{"home/.ssh/id_rsa": "KEY"})
		sign(key, "ssh")
		if got := gitx.MissingSigningKey(b.Ctx, b.Root); got != "" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("inline keys and gpg signing are not its business", func(t *testing.T) {
		for _, tc := range []struct{ key, format string }{
			{"ssh-ed25519 AAAAC3Nza me@example.com", "ssh"},
			{"key::ssh-rsa AAAAB3", "ssh"},
			{"ecdsa-sha2-nistp256 AAAA", "ssh"},
			{"sk-ssh-ed25519@openssh.com AAAA", "ssh"},
			{"ABCDEF0123456789", "openpgp"},
		} {
			sign(tc.key, tc.format)
			if got := gitx.MissingSigningKey(b.Ctx, b.Root); got != "" {
				t.Errorf("%q (%s): got %q", tc.key, tc.format, got)
			}
		}
	})
	t.Run("signing on without a key", func(t *testing.T) {
		sign("x", "ssh")
		b.GitIn(b.Root, "config", "--unset", "user.signingkey")
		if got := gitx.MissingSigningKey(b.Ctx, b.Root); got != "" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("the fix text turns signing off per machine", func(t *testing.T) {
		if fix := voice.Plain(b.Ctx, msg.GitSigningOff()); !strings.Contains(fix, "--file ~/.config/git/config.local") {
			t.Error(fix)
		}
	})
}

// ts: "a ~/.gitconfig beside the repo's ~/.config/git/config"
func TestShadowingGitconfig(t *testing.T) {
	setup := func(t *testing.T) *testutil.Box {
		return testutil.Sandbox(t, testutil.Options{Git: true})
	}
	fromRepo := func(b *testutil.Box) {
		b.Files(map[string]string{"home/.home/autolink/common/config/(git)/config": "[user]\n\tname = Repo\n"})
		must(t, os.MkdirAll(filepath.Join(b.Home, ".config/git"), 0o755))
		must(t, os.Symlink(filepath.Join(b.Root, "autolink/common/config/(git)/config"), filepath.Join(b.Home, ".config/git/config")))
	}
	gitconfig := filepath.Join

	t.Run("a real ~/.gitconfig is flagged", func(t *testing.T) {
		b := setup(t)
		fromRepo(b)
		b.Files(map[string]string{"home/.gitconfig": "[user]\n\temail = old@example.com\n"})
		if got := gitx.ShadowingGitconfig(b.Ctx); got != gitconfig(b.Home, ".gitconfig") {
			t.Errorf("got %q", got)
		}
	})
	t.Run("nothing to say without a ~/.gitconfig", func(t *testing.T) {
		b := setup(t)
		fromRepo(b)
		if got := gitx.ShadowingGitconfig(b.Ctx); got != "" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("nothing to say when ~/.config/git/config isn't from the repo", func(t *testing.T) {
		b := setup(t)
		b.Files(map[string]string{"home/.config/git/config": "[user]\n", "home/.gitconfig": "[user]\n"})
		if got := gitx.ShadowingGitconfig(b.Ctx); got != "" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("nothing to say when ~/.gitconfig links into the repo too", func(t *testing.T) {
		b := setup(t)
		fromRepo(b)
		b.Files(map[string]string{"home/.home/autolink/common/gitconfig": "[core]\n"})
		must(t, os.Symlink(filepath.Join(b.Root, "autolink/common/gitconfig"), gitconfig(b.Home, ".gitconfig")))
		if got := gitx.ShadowingGitconfig(b.Ctx); got != "" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("a dangling ~/.gitconfig symlink still shadows", func(t *testing.T) {
		b := setup(t)
		fromRepo(b)
		must(t, os.Symlink(filepath.Join(b.Dir, "nowhere"), gitconfig(b.Home, ".gitconfig")))
		if got := gitx.ShadowingGitconfig(b.Ctx); got != gitconfig(b.Home, ".gitconfig") {
			t.Errorf("got %q", got)
		}
	})
	t.Run("the fix leaves a backup that homebot cleanup finds", func(t *testing.T) {
		b := setup(t)
		fromRepo(b)
		b.Files(map[string]string{"home/.gitconfig": "[user]\n\temail = old@example.com\n"})
		r := sh(b, voice.Plain(b.Ctx, msg.GitGitconfigAside()), map[string]string{"GIT_CONFIG_NOSYSTEM": "1"})
		if r.Code != 0 || r.Stderr != "" {
			t.Fatalf("%+v", r)
		}
		if _, err := os.Lstat(gitconfig(b.Home, ".gitconfig")); !os.IsNotExist(err) {
			t.Error("~/.gitconfig must be gone")
		}
		if got := gitx.ShadowingGitconfig(b.Ctx); got != "" {
			t.Errorf("got %q", got)
		}
		matches, _ := filepath.Glob(gitconfig(b.Home, ".gitconfig.homebot-backup-[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]-[0-9][0-9][0-9][0-9][0-9][0-9]"))
		if len(matches) != 1 {
			t.Fatalf("backups = %v", matches)
		}
		if data, _ := os.ReadFile(matches[0]); !strings.Contains(string(data), "old@example.com") {
			t.Errorf("backup = %q", data)
		}
	})
	t.Run("the identity fix writes ~/.config/git/config, which git reads as the global config", func(t *testing.T) {
		b := setup(t)
		noXDG := map[string]string{"XDG_CONFIG_HOME": ""}
		r := sh(b, voice.Plain(b.Ctx, msg.GitIdentityFix()), noXDG)
		if r.Code != 0 || r.Stderr != "" {
			t.Fatalf("%+v", r)
		}
		if _, err := os.Lstat(gitconfig(b.Home, ".gitconfig")); !os.IsNotExist(err) {
			t.Error("~/.gitconfig must not exist")
		}
		data, _ := os.ReadFile(gitconfig(b.Home, ".config/git/config"))
		if !strings.Contains(string(data), "you@example.com") {
			t.Errorf("config = %q", data)
		}
		if r := sh(b, "git config --global --get user.email", noXDG); strings.TrimSpace(r.Stdout) != "you@example.com" {
			t.Errorf("%+v", r)
		}
	})
	t.Run("so does signing off, into config.local", func(t *testing.T) {
		b := setup(t)
		r := sh(b, "mkdir -p ~/.config/git && "+voice.Plain(b.Ctx, msg.GitSigningOff()), nil)
		if r.Code != 0 || r.Stderr != "" {
			t.Fatalf("%+v", r)
		}
		data, _ := os.ReadFile(gitconfig(b.Home, ".config/git/config.local"))
		if !strings.Contains(string(data), "gpgsign = false") {
			t.Errorf("config.local = %q", data)
		}
		if _, err := os.Lstat(gitconfig(b.Home, ".gitconfig.local")); !os.IsNotExist(err) {
			t.Error("~/.gitconfig.local must not exist")
		}
	})
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestGitFailuresAreErrors(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	if _, err := gitx.StagedFiles(b.Ctx, b.Root); err == nil || !strings.HasPrefix(err.Error(), "git diff --cached failed.") {
		t.Errorf("StagedFiles err = %v", err)
	}
	if _, err := gitx.UntrackedFiles(b.Ctx, b.Root, "."); err == nil {
		t.Error("UntrackedFiles must fail outside a repo")
	}
	g := testutil.Sandbox(t, testutil.Options{Git: true})
	g.Files(map[string]string{"home/.home/dir/a": "1", "home/.home/lone": "1", "home/.home/sp ace": "1"})
	got, err := gitx.UntrackedFiles(g.Ctx, g.Root, "dir")
	if err != nil || !slices.Equal(got, []string{"dir"}) {
		t.Errorf("got %v err %v", got, err)
	}
}

func TestStatusGoneUpstream(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{Git: true})
	r := b.Root
	remote := filepath.Join(b.Dir, "remote.git")
	b.GitIn(b.Dir, "init", "-q", "--bare", "-b", "main", remote)
	b.Files(map[string]string{"home/.home/f": "1"})
	commitAll(b, r, "one")
	b.GitIn(r, "remote", "add", "origin", remote)
	b.GitIn(r, "push", "-q", "-u", "origin", "main")
	b.GitIn(r, "checkout", "-q", "-b", "topic")
	b.GitIn(r, "push", "-q", "-u", "origin", "topic")
	b.GitIn(remote, "branch", "-q", "-D", "topic")
	b.GitIn(r, "fetch", "-q", "-p")
	st, err := gitx.Status(b.Ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch != "topic" || st.Upstream != "" || st.Ahead != 0 || st.Behind != 0 {
		t.Errorf("status = %+v", st)
	}
}

func TestStatusRemoteWithoutTracking(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{Git: true})
	remote := filepath.Join(b.Dir, "remote.git")
	b.GitIn(b.Dir, "init", "-q", "--bare", "-b", "main", remote)
	b.Files(map[string]string{"home/.home/f": "1"})
	commitAll(b, b.Root, "one")
	b.GitIn(b.Root, "remote", "add", "origin", remote)
	st, err := gitx.Status(b.Ctx, b.Root)
	if err != nil || st.Branch != "main" || st.Upstream != "" {
		t.Errorf("status = %+v err %v", st, err)
	}
}

func TestStatusInitialCommit(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{Git: true})
	b.Files(map[string]string{"home/.home/new file": "1", "home/.home/b": "2"})
	b.GitIn(b.Root, "add", "new file", "b")
	st, err := gitx.Status(b.Ctx, b.Root)
	if err != nil {
		t.Fatal(err)
	}
	got := append([]string(nil), st.Changed...)
	slices.Sort(got)
	if st.Branch != "main" || st.Upstream != "" || !slices.Equal(got, []string{"b", "new file"}) {
		t.Errorf("status = %+v", st)
	}
	staged, err := gitx.StagedFiles(b.Ctx, b.Root)
	if err != nil || len(staged) != 2 {
		t.Errorf("staged = %v err %v", staged, err)
	}
}

func TestRunNetTakesATimeout(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{Git: true})
	b.WithStubs(map[string]string{"git": "sleep 5"})
	r := gitx.RunNet(b.Ctx, b.Root, proc.Options{Timeout: 100 * time.Millisecond}, "fetch")
	if !r.TimedOut {
		t.Errorf("result = %+v", r)
	}
}

// staleIndex commits a file, then changes its mtime without changing its
// content, so the index's stat cache is out of date. A plain git status
// refreshes the cache and rewrites the index; a read-only one must not.
func staleIndex(t *testing.T) (b *testutil.Box, index string) {
	t.Helper()
	b = testutil.Sandbox(t, testutil.Options{Git: true})
	b.Files(map[string]string{"home/.home/f": "same\n"})
	commitAll(b, b.Root, "base")
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(b.Root, "f"), later, later); err != nil {
		t.Fatal(err)
	}
	return b, filepath.Join(b.Root, ".git", "index")
}

func TestStatusTakesNoOptionalLocks(t *testing.T) {
	b, index := staleIndex(t)
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}

	// Sanity: this setup does make a plain git status rewrite the index.
	plain := proc.Run(b.Ctx, []string{"git", "-C", b.Root, "status", "--porcelain"}, proc.Options{})
	if plain.Code != 0 {
		t.Fatal(plain.Stderr)
	}
	refreshed, _ := os.ReadFile(index)
	if string(refreshed) == string(before) {
		t.Fatal("setup: a plain git status did not refresh the index, so this test proves nothing")
	}

	// Put the stale index back and read through gitx.
	if err := os.WriteFile(index, before, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Status(b.Ctx, b.Root); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(index)
	if string(after) != string(before) {
		t.Error("gitx.Status refreshed the index, which takes index.lock")
	}
	if _, err := os.Stat(index + ".lock"); !os.IsNotExist(err) {
		t.Errorf("index.lock left behind: %v", err)
	}
}

func TestReadOnlyCallsLeaveAConcurrentIndexLockAlone(t *testing.T) {
	b, index := staleIndex(t)
	// A concurrent autosave holds the lock.
	if err := os.WriteFile(index+".lock", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(index)
	for _, args := range [][]string{
		{"status", "--porcelain"},
		{"diff", "--name-only"},
		{"diff", "--cached", "--name-only"},
		{"ls-files", "-m"},
	} {
		if r := gitx.Run(b.Ctx, b.Root, args...); r.Code != 0 || strings.Contains(r.Stderr, "index.lock") {
			t.Errorf("git %v: code %d, stderr %q", args, r.Code, r.Stderr)
		}
	}
	if _, err := gitx.Status(b.Ctx, b.Root); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(index)
	if string(after) != string(before) {
		t.Error("a read-only call rewrote the index")
	}
	if _, err := os.Stat(index + ".lock"); err != nil {
		t.Errorf("the other process's lock is gone: %v", err)
	}
}

func TestWritesStillTakeTheirLock(t *testing.T) {
	// GIT_OPTIONAL_LOCKS=0 must not stop git add from locking: it is
	// required there, and a held lock makes it fail loudly.
	b, index := staleIndex(t)
	if err := os.WriteFile(index+".lock", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	b.Files(map[string]string{"home/.home/g": "new\n"})
	r := gitx.Run(b.Ctx, b.Root, "add", "-A")
	if r.Code == 0 || !strings.Contains(r.Stderr, "index.lock") {
		t.Errorf("git add with a held lock: code %d, stderr %q", r.Code, r.Stderr)
	}
}
