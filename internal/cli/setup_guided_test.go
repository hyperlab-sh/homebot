package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// guidedSandbox is a machine with no home repo: a sandbox whose ~/.home is
// gone and whose Context looks for one the way the binary does. gitleaks,
// systemctl and homebot are stubs that work; env is set over the
// sandbox's environment (a "" value removes the variable).
func guidedSandbox(t *testing.T, env map[string]string) *testutil.Box {
	t.Helper()
	sb := testutil.Sandbox(t, testutil.Options{})
	if err := os.RemoveAll(sb.Root); err != nil {
		t.Fatal(err)
	}
	vars := testutil.SandboxEnv(sb.Dir)
	vars["HOMEBOT_TEST_PASSPHRASE"] = testutil.TestPassphrase
	vars["HOMEBOT_TEST_SCRYPT_WORK_FACTOR"] = "10"
	for k, v := range env {
		if v == "" {
			delete(vars, k)
		} else {
			vars[k] = v
		}
	}
	c, err := app.New(app.Options{
		Env:    app.NewEnv(vars),
		Cwd:    sb.Dir,
		OS:     app.Linux,
		Host:   testutil.TestHost,
		Stdout: sb.Stdout,
		Stderr: sb.Stderr,
	})
	if err != nil {
		t.Fatal(err)
	}
	sb.Ctx = c
	sb.WithStubs(map[string]string{"homebot": "exit 0", "systemctl": "exit 0", "gitleaks": "exit 0"})
	installTesterPersona(t, c)
	return sb
}

// installTesterPersona gives the sandbox a local persona named tester.
// setup --persona refuses a persona that does not exist, so the tests that
// pass one need one to pass.
func installTesterPersona(t *testing.T, c *app.Context) {
	t.Helper()
	dir := filepath.Join(c.ConfigDir, "personas")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const tester = "schema = 1\nid = \"tester\"\nname = \"Tester\"\nemoji = \"🤖\"\ntagline = \"for tests\"\nextends = \"neatpik\"\n"
	if err := os.WriteFile(filepath.Join(dir, "tester.toml"), []byte(tester), 0o644); err != nil {
		t.Fatal(err)
	}
}

// answering makes stdin a terminal that answers with lines, then closes.
// tty false makes it a pipe.
func answering(sb *testutil.Box, tty bool, lines ...string) {
	sb.Ctx.StdinIsTerminal = &tty
	input := ""
	if len(lines) > 0 {
		input = strings.Join(lines, "\n") + "\n"
	}
	sb.Ctx.Stdin = strings.NewReader(input)
	sb.Stdout.Reset()
	sb.Stderr.Reset()
}

// fresh is a new Context for the same sandbox, as the next run of homebot
// is: it looks for the repo again.
func fresh(t *testing.T, sb *testutil.Box) {
	t.Helper()
	c, err := app.New(app.Options{
		Env:    sb.Ctx.Env,
		Cwd:    sb.Dir,
		OS:     app.Linux,
		Host:   testutil.TestHost,
		Stdout: sb.Stdout,
		Stderr: sb.Stderr,
	})
	if err != nil {
		t.Fatal(err)
	}
	sb.Ctx = c
}

func wantContains(t *testing.T, what, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s lacks %q:\n%s", what, w, got)
		}
	}
}

func wantLacks(t *testing.T, what, got string, nots ...string) {
	t.Helper()
	for _, n := range nots {
		if strings.Contains(got, n) {
			t.Errorf("%s has %q:\n%s", what, n, got)
		}
	}
}

func TestSetupCommandUsesWorkingName(t *testing.T) {
	b := nameBox(t)
	b.Ctx.Env = b.Ctx.Env.With(map[string]string{
		"PATH": filepath.Join(b.Home, ".local", "bin") + ":" + b.Bin,
	})
	if code, _, stderr := runName(t, b, "zork"); code != 0 {
		t.Fatalf("name: %d %s", code, stderr)
	}
	fresh(t, b)
	for _, command := range []struct {
		name string
		text msg.Text
	}{
		{"setup", msg.SetupHintSetupCmd()},
		{"unpack", msg.SetupHintUnpackCmd()},
		{"triage", msg.SetupHintTriageCmd()},
	} {
		if got := setupCommand(b.Ctx, command.text); !strings.Contains(got, "zork "+command.name) {
			t.Errorf("%s: %q", command.name, got)
		}
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// treeHash is every path under dir with its mode, link target or content.
func treeHash(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		h.Write([]byte(rel + "\x00" + info.Mode().String() + "\x00" + info.ModTime().String() + "\x00"))
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			h.Write([]byte(target))
		case info.Mode().IsRegular():
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			h.Write(b)
		}
		h.Write([]byte{0})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestSetupWithoutARepoOrATerminalCreatesNothing(t *testing.T) {
	sb := guidedSandbox(t, nil)
	answering(sb, false)
	if code := execute(sb.Ctx, "setup"); code != 1 {
		t.Fatalf("exit %d, want 1; stdout:\n%s", code, sb.Stdout)
	}
	want := "homebot: No home repo yet, and setup starts or clones one only in a terminal. Run it in one:\n  homebot setup\nOr clone yours by hand:\n  git clone <url> ~/.home\n"
	if sb.Stderr.String() != want {
		t.Errorf("stderr:\n%s\nwant:\n%s", sb.Stderr, want)
	}
	if sb.Stdout.Len() != 0 {
		t.Errorf("stdout:\n%s", sb.Stdout)
	}
	for _, rel := range []string{"home/.home", "home/.config/systemd", "xdg/data/homebot"} {
		if exists(filepath.Join(sb.Dir, rel)) {
			t.Errorf("setup created %s", rel)
		}
	}
}

// An old dotfiles folder at ~/.home is never touched, terminal or not.
func TestSetupLeavesAnOccupiedHomeFolderAlone(t *testing.T) {
	for _, tc := range []struct {
		name string
		tty  bool
		file bool
	}{{"no terminal", false, false}, {"terminal", true, false}, {"a file there", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			sb := guidedSandbox(t, nil)
			if tc.file {
				sb.Files(map[string]string{"home/.home": "not a folder\n"})
			} else {
				sb.Files(map[string]string{"home/.home/vimrc": "set nu\n", "home/.home/zsh/aliases": "alias l=ls\n"})
				if err := os.Symlink("vimrc", filepath.Join(sb.Home, ".home", "link")); err != nil {
					t.Fatal(err)
				}
			}
			before := treeHash(t, sb.Home)
			answering(sb, tc.tty, "n", "y", "y", "y", "y")
			if code := execute(sb.Ctx, "setup"); code != 1 {
				t.Fatalf("exit %d, want 1; stdout:\n%s", code, sb.Stdout)
			}
			want := "homebot: ~/.home is there but holds no homebot.yml, so it isn't a home repo and setup won't touch it. Move it aside, or set HOMEBOT_ROOT to another folder, then run homebot setup again:\n  mv ~/.home ~/.home.old\n"
			if sb.Stderr.String() != want {
				t.Errorf("stderr:\n%s\nwant:\n%s", sb.Stderr, want)
			}
			if sb.Stdout.Len() != 0 {
				t.Errorf("stdout (nothing should be asked):\n%s", sb.Stdout)
			}
			if after := treeHash(t, sb.Home); after != before {
				t.Error("the home folder changed")
			}
		})
	}
}

// An empty ~/.home is not in the way: setup starts the repo in it.
func TestSetupStartsARepoInAnEmptyHomeFolder(t *testing.T) {
	sb := guidedSandbox(t, nil)
	if err := os.MkdirAll(filepath.Join(sb.Home, ".home"), 0o755); err != nil {
		t.Fatal(err)
	}
	answering(sb, true, "n", "n")
	if code := execute(sb.Ctx, "setup"); code != 0 {
		t.Fatalf("exit %d; stderr:\n%s", code, sb.Stderr)
	}
	if !exists(filepath.Join(sb.Home, ".home", "homebot.yml")) {
		t.Errorf("no homebot.yml; stdout:\n%s", sb.Stdout)
	}
}

// The menu: Enter or q is not now, and changes nothing; an unknown key asks
// again; an empty URL goes back to the menu.
func TestSetupNotNowCreatesNothing(t *testing.T) {
	for _, answers := range [][]string{{""}, {"q"}, {"x", "Q"}, {"c", "", ""}, {"c"}, nil} {
		t.Run(strings.Join(answers, ","), func(t *testing.T) {
			sb := guidedSandbox(t, nil)
			answering(sb, true, answers...)
			if code := execute(sb.Ctx, "setup"); code != 0 {
				t.Fatalf("exit %d; stderr:\n%s", code, sb.Stderr)
			}
			out := sb.Stdout.String()
			wantContains(t, "stdout", out,
				"No home repo yet.\n\n  c      clone your home repo\n  n      start a new one in ~/.home\n  enter  not now\n> ",
				"Not now. When you're ready:\n  homebot setup\n")
			if answers != nil && answers[0] == "x" {
				wantContains(t, "stdout", out, "  pick one of the letters above\n")
			}
			// Ending the input (Ctrl-D), at the choice or at the address,
			// ends the prompt's line first; an answer has ended it already.
			switch {
			case answers == nil:
				wantContains(t, "stdout", out, "> \nNot now.")
			case len(answers) == 1 && answers[0] == "c":
				wantContains(t, "stdout", out, "owner/repo on GitHub: \nNot now.")
			default:
				wantContains(t, "stdout", out, "> Not now.")
			}
			if exists(filepath.Join(sb.Home, ".home")) || exists(filepath.Join(sb.Home, ".config", "systemd")) {
				t.Error("setup created something")
			}
		})
	}
}

// The new-repo path, start to end: keys, the shell line, triage, a remote,
// the first save pushed, the inbox. Then a second run that asks nothing.
// It returns the bare repo it pushed to.
func guidedNewRepo(t *testing.T) (*testutil.Box, string) {
	t.Helper()
	sb := guidedSandbox(t, map[string]string{"SHELL": "/usr/bin/zsh"})
	bare := filepath.Join(sb.Dir, "backup.git")
	sb.GitIn(sb.Dir, "init", "-q", "--bare", "-b", "main", bare)
	sb.Files(map[string]string{"home/.vimrc": "set nu\n"})
	answering(sb, true,
		"n",   // start a new one
		"y",   // create the keys
		"",    // keep the neutral command name
		"",    // add the line to ~/.zshrc (Enter is yes)
		"y",   // sort the home now
		"i",   // ~/.vimrc: ignore
		"t",   // ~/.zshrc: track
		"u",   // remote: a URL
		bare,  // the URL
		"yes", // commit and push
	)
	if code := execute(sb.Ctx, "setup"); code != 0 {
		t.Fatalf("exit %d; stderr:\n%s\nstdout:\n%s", code, sb.Stderr, sb.Stdout)
	}
	return sb, bare
}

func TestSetupStartsANewRepoAndGoesToTheInbox(t *testing.T) {
	sb, bare := guidedNewRepo(t)
	out := sb.Stdout.String()
	root := filepath.Join(sb.Home, ".home")
	wantContains(t, "stdout", out,
		"created ~/.home, a new home repo with a base profile\n",
		"Keep passwords and tokens in the repo, encrypted? This creates your keys. [Y/n] ",
		"installed commit guard (core.hooksPath)\n",
		"installed hourly systemd timer homebot-tidy.timer\n",
		"Add it to ~/.zshrc now? [Y/n] added the startup line to ~/.zshrc\n",
		"2 things in your home could be tracked. Sort them now, one key each? [Y/n] ",
		"Commit and push now? [Y/n] ",
	)
	if !strings.HasSuffix(out, "pushed 1 commit\n\n"+setupEmptyInbox) {
		t.Errorf("the run does not end in an empty inbox:\n%s", out)
	}
	// Keys were offered, so the keys hint is not printed after.
	wantLacks(t, "stdout", out, "Secrets aren't set up yet", "Not now")

	for rel, want := range map[string]string{
		"homebot.yml":                 "profiles:\n  default: base\n",
		"manifests/profiles/base.yml": "steps:\n  - unlock\n  - link\n",
		"autolink/common/zshrc":       "eval \"$(homebot shell zsh)\"\n",
	} {
		got, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q", rel, got, err, want)
		}
	}
	for _, rel := range []string{"private/recipients.txt", "private/identity.age"} {
		if !exists(filepath.Join(root, rel)) {
			t.Errorf("keys init did not write %s", rel)
		}
	}
	if target, err := os.Readlink(filepath.Join(sb.Home, ".zshrc")); err != nil || !strings.HasSuffix(target, ".home/autolink/common/zshrc") {
		t.Errorf("~/.zshrc -> %q, %v", target, err)
	}
	if got := strings.TrimSpace(sb.GitIn(root, "remote", "get-url", "origin")); got != bare {
		t.Errorf("origin = %q, want %q", got, bare)
	}
	if got := strings.TrimSpace(sb.GitIn(root, "status", "--porcelain")); got != "" {
		t.Errorf("uncommitted after the save:\n%s", got)
	}
	pushed := sb.GitIn(bare, "ls-tree", "-r", "--name-only", "main")
	wantContains(t, "the pushed tree", pushed, "homebot.yml\n", "manifests/profiles/base.yml\n", "autolink/common/zshrc\n", "private/recipients.txt\n")
	if strings.Contains(pushed, "vimrc") {
		t.Errorf("the ignored ~/.vimrc was pushed:\n%s", pushed)
	}

	// The second run finds everything done and asks nothing.
	fresh(t, sb)
	answering(sb, true)
	if code := execute(sb.Ctx, "setup"); code != 0 {
		t.Fatalf("second run: exit %d; stderr:\n%s", code, sb.Stderr)
	}
	second := sb.Stdout.String()
	if second != "Give your homebot a command name? For example zork (Enter keeps homebot): Commit guard already installed.\n"+setupHelperLine+"\n"+setupEmptyInbox {
		t.Errorf("second run stdout:\n%s", second)
	}
	wantLacks(t, "second run", second, "[Y/n]", "> ", "Not now", ".zshrc", "Secrets")
	if n := strings.Count(mustRead(t, filepath.Join(sb.Home, ".zshrc")), "homebot shell zsh"); n != 1 {
		t.Errorf("the startup line is in ~/.zshrc %d times", n)
	}
}

// setupEmptyInbox is the inbox with nothing in it.
const setupEmptyInbox = "homebot · testhost · nothing needs you. Home is in order.\n"

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The clone path on a second machine: clone the repo the first machine
// pushed, unpack, and move aside the real ~/.zshrc that is in the way.
func TestSetupClonesTheRepoOnASecondMachine(t *testing.T) {
	_, bare := guidedNewRepo(t)
	sb := guidedSandbox(t, map[string]string{"SHELL": "/usr/bin/zsh"})
	sb.Files(map[string]string{"home/.zshrc": "# my own\n"})
	answering(sb, true,
		"c",  // clone
		bare, // the URL
		"y",  // run the steps
		"y",  // move the real ~/.zshrc aside and link
		"",   // keep the neutral command name
	)
	if code := execute(sb.Ctx, "setup"); code != 0 {
		t.Fatalf("exit %d; stderr:\n%s\nstdout:\n%s", code, sb.Stderr, sb.Stdout)
	}
	out := sb.Stdout.String()
	wantContains(t, "stdout", out,
		"URL of your home repo, or owner/repo on GitHub: Cloning "+bare+" into ~/.home...\n",
		"Run these 2 steps? [Y/n] ",
		"1 file is in the way of links. Move them aside (nothing is deleted) and link? [Y/n] ",
		"homebot cleanup",
		"installed commit guard (core.hooksPath)\n",
	)
	wantLacks(t, "stdout", out, "Not now", "Sort them now", "Commit and push", "Add it to")
	target, err := os.Readlink(filepath.Join(sb.Home, ".zshrc"))
	if err != nil || !strings.HasSuffix(target, ".home/autolink/common/zshrc") {
		t.Errorf("~/.zshrc -> %q, %v", target, err)
	}
	backups, _ := filepath.Glob(filepath.Join(sb.Home, ".zshrc*"))
	found := false
	for _, b := range backups {
		if b != filepath.Join(sb.Home, ".zshrc") && mustRead(t, b) == "# my own\n" {
			found = true
		}
	}
	if !found {
		t.Errorf("the real ~/.zshrc was not kept as a backup: %v", backups)
	}
}

// Declining every question changes only what the answers allow, and each
// "not now" says the command for later.
func TestSetupNewRepoWithEveryQuestionDeclined(t *testing.T) {
	sb := guidedSandbox(t, map[string]string{"SHELL": "/bin/zsh"})
	sb.Files(map[string]string{"home/.vimrc": "set nu\n"})
	answering(sb, true, "n", "n", "", "n", "n", "", "n")
	if code := execute(sb.Ctx, "setup"); code != 0 {
		t.Fatalf("exit %d; stderr:\n%s\nstdout:\n%s", code, sb.Stderr, sb.Stdout)
	}
	out := sb.Stdout.String()
	root := filepath.Join(sb.Home, ".home")
	wantContains(t, "stdout", out,
		"This creates your keys. [Y/n] Not now. When you're ready:\n  homebot keys init\n",
		"Give your homebot a command name? For example zork (Enter keeps homebot): installed commit guard",
		"Add it to ~/.zshrc now? [Y/n] ",
		"Sort them now, one key each? [Y/n] Not now. When you're ready:\n  homebot triage\n",
		"Commit now? [Y/n] Not now. When you're ready:\n  homebot save\n",
	)
	if exists(filepath.Join(sb.Home, ".zshrc")) {
		t.Error("~/.zshrc was written")
	}
	if exists(filepath.Join(root, "private", "recipients.txt")) {
		t.Error("keys were created")
	}
	if strings.TrimSpace(sb.GitIn(root, "remote")) != "" {
		t.Error("a remote was set")
	}
	if r := sb.GitIn(root, "rev-list", "--all"); strings.TrimSpace(r) != "" {
		t.Errorf("commits were made: %s", r)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if got := strings.Join(names, " "); got != ".git homebot.yml manifests" {
		t.Errorf("the new repo holds %s", got)
	}
}

// Declining the unpack declines its links too: the files in their way are
// not offered to be moved aside. Ending the input (Ctrl-D) at that question
// is no, not Enter's yes.
func TestSetupCloneDeclinedLeavesHomeAsItIs(t *testing.T) {
	_, bare := guidedNewRepo(t)
	for _, tc := range []struct {
		name    string
		answers []string
		said    string
	}{
		{"no", []string{"c", bare, "n", "n", "n"}, "Run these 2 steps? [Y/n] Not now."},
		{"input ended", []string{"c", bare}, "Run these 2 steps? [Y/n] \nNot now."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sb := guidedSandbox(t, map[string]string{"SHELL": "/bin/zsh"})
			sb.Files(map[string]string{"home/.zshrc": "# my own\n"})
			answering(sb, true, tc.answers...)
			if code := execute(sb.Ctx, "setup"); code != 0 {
				t.Fatalf("exit %d; stderr:\n%s\nstdout:\n%s", code, sb.Stderr, sb.Stdout)
			}
			out := sb.Stdout.String()
			wantContains(t, "stdout", out, tc.said+" When you're ready:\n  homebot unpack\n")
			wantLacks(t, "stdout", out, "Move them aside", "link --backup", "Unpacked")
			if got := mustRead(t, filepath.Join(sb.Home, ".zshrc")); got != "# my own\n" {
				t.Errorf("~/.zshrc = %q", got)
			}
		})
	}
}

// Ending the input (Ctrl-D) at the first yes-or-no question of a new repo
// answers no to it and to every one after: no keys, no .zshrc line, no
// triage, no commit.
func TestSetupNewRepoWhenTheInputEnds(t *testing.T) {
	sb := guidedSandbox(t, map[string]string{"SHELL": "/bin/zsh"})
	sb.Files(map[string]string{"home/.vimrc": "set nu\n"})
	answering(sb, true, "n")
	if code := execute(sb.Ctx, "setup"); code != 0 {
		t.Fatalf("exit %d; stderr:\n%s\nstdout:\n%s", code, sb.Stderr, sb.Stdout)
	}
	out := sb.Stdout.String()
	root := filepath.Join(sb.Home, ".home")
	wantContains(t, "stdout", out,
		"This creates your keys. [Y/n] \nNot now. When you're ready:\n  homebot keys init\n",
		"Give your homebot a command name? For example zork (Enter keeps homebot): installed commit guard",
		"Add it to ~/.zshrc now? [Y/n] \n",
		"Sort them now, one key each? [Y/n] \nNot now. When you're ready:\n  homebot triage\n",
	)
	wantLacks(t, "stdout", out, "added the startup line")
	if exists(filepath.Join(sb.Home, ".zshrc")) {
		t.Error("~/.zshrc was written")
	}
	if exists(filepath.Join(root, "private", "recipients.txt")) {
		t.Error("keys were created")
	}
	if r := sb.GitIn(root, "rev-list", "--all"); strings.TrimSpace(r) != "" {
		t.Errorf("commits were made: %s", r)
	}
	if got := strings.TrimSpace(sb.GitIn(root, "status", "--porcelain")); got != "A  homebot.yml\nA  manifests/profiles/base.yml" {
		t.Errorf("the new repo's status:\n%s", got)
	}
}

// cloneStub makes git a stub whose clone logs its address and fails with
// what fails says ('$1' is the address); everything else is the real git.
// git runs on the terminal, so what it says lands on stderr as it says it.
// It returns the log.
func cloneStub(sb *testutil.Box, fails string) string {
	log := filepath.Join(sb.Dir, "clones.log")
	sb.WithStubs(map[string]string{"git": `for a; do if [ "$a" = clone ]; then
  shift $(($# - 2)); echo "$1" >> "` + log + `"
  echo "` + fails + `" >&2; exit 128
fi; done
exec /usr/bin/git "$@"`})
	return log
}

// owner/repo is GitHub: ssh first when ~/.ssh has a key, then https; https
// alone without one. Something by that name here is a local path, as typed.
// git is a stub; nothing touches the network.
func TestSetupClonesOwnerRepoFromGitHub(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   bool
		local string
		typed string
		tries []string
	}{
		{"with a key", true, "", "octo/dots", []string{"git@github.com:octo/dots.git", "https://github.com/octo/dots.git"}},
		{"without a key", false, "", "octo/dots", []string{"https://github.com/octo/dots.git"}},
		{"https with a key", true, "", "https://example.com/octo/dots.git", []string{"https://example.com/octo/dots.git", "git@example.com:octo/dots.git"}},
		{"ssh as typed", true, "", "git@example.com:octo/dots.git", []string{"git@example.com:octo/dots.git"}},
		{"a folder here", true, "backups/home/README", "backups/home", []string{"backups/home"}},
		{"a file here", false, "backups/home", "backups/home", []string{"backups/home"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sb := guidedSandbox(t, nil)
			log := cloneStub(sb, "fatal: repository '$1' not found")
			if tc.key {
				sb.Files(map[string]string{"home/.ssh/id_ed25519": "key\n"})
			}
			if tc.local != "" {
				sb.Files(map[string]string{tc.local: "x\n"})
			}
			answering(sb, true, "c", tc.typed)
			if code := execute(sb.Ctx, "setup"); code != 1 {
				t.Fatalf("exit %d, want 1; stdout:\n%s", code, sb.Stdout)
			}
			got := strings.Fields(mustRead(t, log))
			if strings.Join(got, " ") != strings.Join(tc.tries, " ") {
				t.Errorf("cloned %q, want %q", got, tc.tries)
			}
			var want strings.Builder
			for _, u := range tc.tries {
				want.WriteString("fatal: repository '" + u + "' not found\n")
			}
			last := tc.tries[len(tc.tries)-1]
			want.WriteString("homebot: Couldn't clone " + last + ". Check the address and that you can reach it, then run homebot setup again.\n")
			if sb.Stderr.String() != want.String() {
				t.Errorf("stderr:\n%s\nwant:\n%s", sb.Stderr, want.String())
			}
			for _, u := range tc.tries {
				wantContains(t, "stdout", sb.Stdout.String(), "Cloning "+u+" into ~/.home...\n")
			}
			if len(tc.tries) > 1 {
				wantContains(t, "stdout", sb.Stdout.String(), "Cloning "+tc.tries[0]+" into ~/.home...\nCouldn't clone "+tc.tries[0]+".\nCloning ")
			}
			if exists(filepath.Join(sb.Home, ".home")) {
				t.Error("~/.home was created")
			}
		})
	}
}

// A clone that fails to sign in says how to sign in, never to check the
// address: gh auth login for GitHub when gh is there, else an ssh key.
func TestSetupSaysHowToSignInWhenACloneIsRefused(t *testing.T) {
	const https = "fatal: could not read Username for 'https://github.com': terminal prompts disabled"
	const ssh = "git@github.com: Permission denied (publickey)."
	for _, tc := range []struct {
		name  string
		gh    bool
		key   bool
		typed string
		fails string
		want  string
	}{
		{"gh there", true, false, "octo/dots", https,
			"homebot: git couldn't sign in to GitHub to clone https://github.com/octo/dots.git. Sign in with the GitHub CLI, which sets git up to use that login, then run homebot setup again:\n  gh auth login\n"},
		{"no gh", false, false, "octo/dots", https,
			"homebot: git couldn't sign in to github.com to clone https://github.com/octo/dots.git. Add an ssh key to your account there, then run homebot setup again.\n"},
		{"ssh refused", false, true, "git@github.com:octo/dots.git", ssh,
			"homebot: git couldn't sign in to github.com to clone git@github.com:octo/dots.git. Add an ssh key to your account there, then run homebot setup again.\n"},
		{"another server", true, false, "https://git.example.com/octo/dots.git", "remote: HTTP Basic: Access denied\nfatal: Authentication failed for 'https://git.example.com/octo/dots.git/'",
			"homebot: git couldn't sign in to git.example.com to clone https://git.example.com/octo/dots.git. Add an ssh key to your account there, then run homebot setup again.\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sb := guidedSandbox(t, nil)
			cloneStub(sb, tc.fails)
			if tc.gh {
				sb.WithStubs(map[string]string{"gh": "exit 0"})
			}
			if tc.key {
				sb.Files(map[string]string{"home/.ssh/id_ed25519": "key\n"})
			}
			// Only the stubs: no gh from the system can be found.
			sb.Ctx.Env = sb.Ctx.Env.With(map[string]string{"PATH": sb.Bin})
			answering(sb, true, "c", tc.typed)
			if code := execute(sb.Ctx, "setup"); code != 1 {
				t.Fatalf("exit %d, want 1; stdout:\n%s", code, sb.Stdout)
			}
			stderr := sb.Stderr.String()
			if !strings.HasSuffix(stderr, tc.want) {
				t.Errorf("stderr:\n%s\nwant it to end with:\n%s", stderr, tc.want)
			}
			wantLacks(t, "stderr", stderr, "Check the address")
		})
	}
}

// A relative HOMEBOT_ROOT is refused before anything is created: the repo
// would be found only from the folder setup ran in.
func TestSetupRefusesARelativeHomebotRoot(t *testing.T) {
	sb := guidedSandbox(t, map[string]string{"HOMEBOT_ROOT": "dots"})
	answering(sb, true, "n", "y", "y")
	if code := execute(sb.Ctx, "setup"); code != 1 {
		t.Fatalf("exit %d, want 1; stdout:\n%s", code, sb.Stdout)
	}
	want := "homebot: HOMEBOT_ROOT is \"dots\", which is not a full path, so homebot would find the repo only from this folder. Set it to a full path, or unset it, then run homebot setup again.\n"
	if sb.Stderr.String() != want {
		t.Errorf("stderr:\n%s\nwant:\n%s", sb.Stderr, want)
	}
	if sb.Stdout.Len() != 0 {
		t.Errorf("stdout:\n%s", sb.Stdout)
	}
	for _, rel := range []string{"dots", "home/.home", "home/.config/systemd"} {
		if exists(filepath.Join(sb.Dir, rel)) {
			t.Errorf("setup created %s", rel)
		}
	}
}

// An absolute HOMEBOT_ROOT with something in it that is not a home repo is
// left alone, as ~/.home is, and the move command names it.
func TestSetupLeavesAnOccupiedHomebotRootAlone(t *testing.T) {
	root := "srv/old dots"
	sb := guidedSandbox(t, nil)
	dir := filepath.Join(sb.Dir, root)
	sb.Ctx.Env = sb.Ctx.Env.With(map[string]string{"HOMEBOT_ROOT": dir})
	sb.Files(map[string]string{root + "/vimrc": "set nu\n"})
	before := treeHash(t, dir)
	answering(sb, true, "n", "y")
	if code := execute(sb.Ctx, "setup"); code != 1 {
		t.Fatalf("exit %d, want 1; stdout:\n%s", code, sb.Stdout)
	}
	want := "homebot: " + dir + " is there but holds no homebot.yml, so it isn't a home repo and setup won't touch it. Move it aside, or set HOMEBOT_ROOT to another folder, then run homebot setup again:\n  mv '" + dir + "' '" + dir + ".old'\n"
	if sb.Stderr.String() != want {
		t.Errorf("stderr:\n%s\nwant:\n%s", sb.Stderr, want)
	}
	if after := treeHash(t, dir); after != before {
		t.Error("$HOMEBOT_ROOT changed")
	}
}

// A clone without homebot.yml stays where it is, and setup stops.
func TestSetupStopsOnACloneThatIsNotAHomeRepo(t *testing.T) {
	sb := guidedSandbox(t, nil)
	src := filepath.Join(sb.Dir, "other")
	sb.Files(map[string]string{"other/README": "hi\n"})
	sb.GitIn(src, "init", "-q", "-b", "main")
	sb.GitIn(src, "add", ".")
	sb.GitIn(src, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "x")
	answering(sb, true, "c", src)
	if code := execute(sb.Ctx, "setup"); code != 1 {
		t.Fatalf("exit %d, want 1; stdout:\n%s", code, sb.Stdout)
	}
	want := "homebot: " + src + " has no homebot.yml, so it isn't a home repo. It is in ~/.home now: move it aside, then run homebot setup again.\n"
	if sb.Stderr.String() != want {
		t.Errorf("stderr:\n%s\nwant:\n%s", sb.Stderr, want)
	}
	if !exists(filepath.Join(sb.Home, ".home", "README")) {
		t.Error("the clone was removed")
	}
}

// A missing tool stops setup before it changes anything, unless brew is
// there, a person is, and they say yes.
func TestSetupChecksForGitleaks(t *testing.T) {
	const missing = "homebot: Missing: gitleaks. The commit guard needs it to check every commit. Install it, then run homebot setup again:\n  brew install gitleaks\n"
	brew := func(code string) string {
		return `echo "$@" >> "$HOME/../brew.log"
printf '#!/bin/sh\nexit 0\n' > "$HOME/../bin/gitleaks"; chmod +x "$HOME/../bin/gitleaks"
exit ` + code
	}
	for _, tc := range []struct {
		name    string
		brew    string
		tty     bool
		answers []string
		code    int
		stderr  string
		ran     string
	}{
		{"no brew", "", true, []string{"y"}, 1, missing, ""},
		{"no terminal", brew("0"), false, nil, 1, missing, ""},
		{"declined", brew("0"), true, []string{"n"}, 1, missing, ""},
		{"input ended", brew("0"), true, nil, 1, missing, ""},
		{"installed", brew("0"), true, []string{"y"}, 0, "", "install gitleaks\n"},
		{"brew failed", "echo \"$@\" >> \"$HOME/../brew.log\"; exit 1", true, []string{""}, 1, "homebot: brew install gitleaks failed. Install gitleaks yourself, then run homebot setup again.\n", "install gitleaks\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sb := guidedSandbox(t, nil)
			if err := os.Remove(filepath.Join(sb.Bin, "gitleaks")); err != nil {
				t.Fatal(err)
			}
			if tc.brew != "" {
				sb.WithStubs(map[string]string{"brew": tc.brew})
			}
			answering(sb, tc.tty, tc.answers...)
			if code := execute(sb.Ctx, "setup"); code != tc.code {
				t.Fatalf("exit %d, want %d; stderr:\n%s", code, tc.code, sb.Stderr)
			}
			if sb.Stderr.String() != tc.stderr {
				t.Errorf("stderr:\n%s\nwant:\n%s", sb.Stderr, tc.stderr)
			}
			ran, _ := os.ReadFile(filepath.Join(sb.Dir, "brew.log"))
			if string(ran) != tc.ran {
				t.Errorf("brew ran %q, want %q", ran, tc.ran)
			}
			asked := strings.Contains(sb.Stdout.String(), "Missing: gitleaks. Install now with brew install gitleaks? [Y/n] ")
			if asked != (tc.brew != "" && tc.tty) {
				t.Errorf("asked = %v; stdout:\n%s", asked, sb.Stdout)
			}
			if tc.code != 0 && exists(filepath.Join(sb.Home, ".config", "systemd")) {
				t.Error("setup went on after the tools check")
			}
		})
	}
}

// age is not a tool setup needs: homebot seals with the Go age library, so
// a machine without the age binary is set up all the same.
func TestSetupDoesNotNeedTheAgeBinary(t *testing.T) {
	sb := setupSandbox(t, "")
	// Only the stubs are on PATH, so no age binary can be found.
	sb.WithStubs(map[string]string{"git": `exec /usr/bin/git "$@"`})
	sb.Ctx.Env = sb.Ctx.Env.With(map[string]string{"PATH": sb.Bin})
	answering(sb, false)
	out := setupRunOK(t, sb)
	wantContains(t, "stdout", out, "installed commit guard (core.hooksPath)\n")
	wantLacks(t, "output", out+sb.Stderr.String(), "Missing:", "brew install")
}

// Another login shell gets one line; the .zshrc hint and question are
// zsh's.
func TestSetupOnBashSaysTheShellLineIsZshOnly(t *testing.T) {
	sb := setupSandbox(t, "")
	sb.Ctx.Env = sb.Ctx.Env.With(map[string]string{"SHELL": "/bin/bash"})
	answering(sb, false)
	want := setupGuardLine + setupHelperLine + setupKeysHint + "Your shell is bash. The startup line and the prompt count are zsh-only for now.\n"
	if out := beforeInbox(t, setupRunOK(t, sb)); out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
}

// Without a terminal the line is shown, never written.
func TestSetupOnZshWithoutATerminalOnlyShowsTheLine(t *testing.T) {
	sb := setupSandbox(t, "")
	sb.Ctx.Env = sb.Ctx.Env.With(map[string]string{"SHELL": "/bin/zsh"})
	answering(sb, false)
	want := setupGuardLine + setupHelperLine + setupKeysHint + setupZshHint
	if out := beforeInbox(t, setupRunOK(t, sb)); out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
	if exists(filepath.Join(sb.Home, ".zshrc")) {
		t.Error("~/.zshrc was written")
	}
}

// A .zshrc that links into the repo is appended to through the link, and
// setup says the repo file changed. $ZDOTDIR's .zshrc is the one used when
// ZDOTDIR is set.
func TestSetupAppendsThroughALinkIntoTheRepo(t *testing.T) {
	sb := setupSandbox(t, "")
	zdot := filepath.Join(sb.Home, ".config", "zsh")
	sb.Ctx.Env = sb.Ctx.Env.With(map[string]string{"SHELL": "/bin/zsh", "ZDOTDIR": zdot})
	sb.Files(map[string]string{"home/.home/autolink/common/.config/zsh/.zshrc": "export A=1"})
	if err := os.MkdirAll(zdot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(sb.Root, "autolink/common/.config/zsh/.zshrc"), filepath.Join(zdot, ".zshrc")); err != nil {
		t.Fatal(err)
	}
	answering(sb, true, "", "y", "", "n") // neutral name, the line yes, a remote not now, commit no
	out := setupRunOK(t, sb)
	wantContains(t, "stdout", out,
		"Add it to ~/.config/zsh/.zshrc now? [Y/n] added the startup line to ~/.config/zsh/.zshrc\nThat file links into your home repo, so autolink/common/.config/zsh/.zshrc changed there.\n")
	if got := mustRead(t, filepath.Join(sb.Root, "autolink/common/.config/zsh/.zshrc")); got != "export A=1\neval \"$(homebot shell zsh)\"\n" {
		t.Errorf("the repo's .zshrc = %q", got)
	}
	if exists(filepath.Join(sb.Home, ".zshrc")) {
		t.Error("~/.zshrc was written")
	}
}

// A repo that already has a remote is not asked about one; nothing to
// commit asks nothing.
func TestSetupSkipsTheRemoteAndSaveWhenDone(t *testing.T) {
	sb := setupSandbox(t, "")
	testutil.TestKeys(t, sb.Root)
	sb.Files(map[string]string{"home/.zshrc": "eval \"$(homebot shell zsh)\"\n"})
	sb.GitIn(sb.Root, "add", "-A")
	sb.GitIn(sb.Root, "commit", "-q", "-m", "first")
	sb.GitIn(sb.Root, "remote", "add", "backup", filepath.Join(sb.Dir, "nowhere.git"))
	answering(sb, true, "")
	out := setupRunOK(t, sb)
	if !strings.Contains(out, setupGuardLine+setupHelperLine+"\n") {
		t.Errorf("stdout:\n%s", out)
	}
	wantLacks(t, "stdout", out, "[Y/n]", "No backup", "Not now")
}

// A remote that cannot be reached does not stop setup's commit: save
// commits what setup changed (here the .zshrc line, through a link into the
// repo), says the remote is out of reach, and setup goes on to the inbox.
func TestSetupCommitsWhenTheRemoteIsOutOfReach(t *testing.T) {
	sb := setupSandbox(t, "")
	sb.Ctx.Env = sb.Ctx.Env.With(map[string]string{"SHELL": "/bin/zsh"})
	testutil.TestKeys(t, sb.Root)
	sb.Files(map[string]string{"home/.home/autolink/common/zshrc": "export A=1\n"})
	sb.GitIn(sb.Root, "add", "-A")
	sb.GitIn(sb.Root, "commit", "-q", "-m", "first")
	sb.GitIn(sb.Root, "remote", "add", "origin", filepath.Join(sb.Dir, "nowhere.git"))
	if err := os.Symlink(filepath.Join(sb.Root, "autolink/common/zshrc"), filepath.Join(sb.Home, ".zshrc")); err != nil {
		t.Fatal(err)
	}
	answering(sb, true, "", "y", "y") // neutral name, the line, then commit and push
	out := setupRunOK(t, sb)
	wantContains(t, "stdout", out, "Commit and push now? [Y/n] committed ")
	if got := strings.TrimSpace(sb.GitIn(sb.Root, "rev-list", "--count", "HEAD")); got != "2" {
		t.Errorf("%s commits, want 2", got)
	}
	wantContains(t, "stdout", out, "Couldn't reach the remote, so nothing was pushed.", "\nhomebot · testhost · ")
}

// A rerun on a repo with the person's own uncommitted work never offers to
// commit and push it, even with Enter as the answer to everything: HEAD and
// the remote stay where they were, and the inbox lists the change.
func TestSetupNeverCommitsThePersonsOwnWork(t *testing.T) {
	sb, bare := guidedNewRepo(t)
	root := filepath.Join(sb.Home, ".home")
	sb.Files(map[string]string{"home/.home/manifests/profiles/base.yml": "steps:\n  - unlock\n  - link\n# half-done\n"})
	head := sb.GitIn(root, "rev-parse", "HEAD")
	pushed := sb.GitIn(bare, "rev-parse", "main")
	fresh(t, sb)
	answering(sb, true, "", "", "", "")
	if code := execute(sb.Ctx, "setup"); code != 0 {
		t.Fatalf("exit %d; stderr:\n%s", code, sb.Stderr)
	}
	out := sb.Stdout.String()
	wantLacks(t, "stdout", out, "[Y/n]", "Commit and push", "Commit now", "committed 1", "pushed 1")
	wantContains(t, "stdout", out, "1 uncommitted change")
	if got := sb.GitIn(root, "rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD moved: %s -> %s", head, got)
	}
	if got := sb.GitIn(bare, "rev-parse", "main"); got != pushed {
		t.Errorf("the remote moved: %s -> %s", pushed, got)
	}
	if got := sb.GitIn(root, "status", "--porcelain"); got != " M manifests/profiles/base.yml\n" {
		t.Errorf("status:\n%s", got)
	}
}

// A repo that already had the person's own changes gets no offer to commit,
// even when setup changes it too (here the .zshrc line, through a link into
// the repo): the two cannot be told apart in one commit. HEAD stays, and the
// inbox lists both.
func TestSetupDoesNotOfferToCommitWhenTheRepoWasAlreadyDirty(t *testing.T) {
	sb := setupSandbox(t, "")
	sb.Ctx.Env = sb.Ctx.Env.With(map[string]string{"SHELL": "/bin/zsh"})
	testutil.TestKeys(t, sb.Root)
	sb.Files(map[string]string{
		"home/.home/autolink/common/zshrc": "export A=1\n",
		"home/.home/autolink/common/vimrc": "set nu\n",
	})
	sb.GitIn(sb.Root, "add", "-A")
	sb.GitIn(sb.Root, "commit", "-q", "-m", "first")
	sb.GitIn(sb.Root, "remote", "add", "origin", filepath.Join(sb.Dir, "nowhere.git"))
	if err := os.Symlink(filepath.Join(sb.Root, "autolink/common/zshrc"), filepath.Join(sb.Home, ".zshrc")); err != nil {
		t.Fatal(err)
	}
	sb.Files(map[string]string{"home/.home/autolink/common/vimrc": "set nu\n# half-done\n"})
	head := sb.GitIn(sb.Root, "rev-parse", "HEAD")
	answering(sb, true, "y", "", "", "")
	out := setupRunOK(t, sb)
	wantContains(t, "stdout", out,
		"added the startup line to ~/.zshrc\nThat file links into your home repo, so autolink/common/zshrc changed there.\n",
		"2 uncommitted changes")
	wantLacks(t, "stdout", out, "Commit and push", "Commit now", "\ncommitted ", "] committed ")
	if got := sb.GitIn(sb.Root, "rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD moved: %s -> %s", head, got)
	}
	if got := sb.GitIn(sb.Root, "status", "--porcelain"); got != " M autolink/common/vimrc\n M autolink/common/zshrc\n" {
		t.Errorf("status:\n%s", got)
	}
}

// A .zshrc that is a folder is left alone: setup shows the line and asks
// nothing, and says nothing about a link.
func TestSetupLeavesAZshrcFolderAlone(t *testing.T) {
	sb := setupSandbox(t, "")
	sb.Ctx.Env = sb.Ctx.Env.With(map[string]string{"SHELL": "/bin/zsh"})
	sb.Files(map[string]string{"home/.zshrc/keep": "x\n"})
	before := treeHash(t, filepath.Join(sb.Home, ".zshrc"))
	answering(sb, true, "", "", "", "")
	out := setupRunOK(t, sb)
	wantContains(t, "stdout", out, setupZshHint)
	wantLacks(t, "stdout", out, "Add it to", "added the startup line", "is a link to")
	if after := treeHash(t, filepath.Join(sb.Home, ".zshrc")); after != before {
		t.Error("the ~/.zshrc folder changed")
	}
}

// A .zshrc that links into an old dotfiles folder at ~/.home, with the new
// home repo in $HOMEBOT_ROOT, is never written through: setup says where the
// link goes and asks nothing about it.
func TestSetupLeavesAZshrcLinkedIntoAnOldHomeFolderAlone(t *testing.T) {
	sb := guidedSandbox(t, nil)
	repo := filepath.Join(sb.Dir, "elsewhere")
	sb.Ctx.Env = sb.Ctx.Env.With(map[string]string{"SHELL": "/bin/zsh", "HOMEBOT_ROOT": repo})
	sb.Files(map[string]string{"home/.home/zshrc": "# old dotfiles\n"})
	if err := os.Symlink(filepath.Join(sb.Home, ".home", "zshrc"), filepath.Join(sb.Home, ".zshrc")); err != nil {
		t.Fatal(err)
	}
	before := treeHash(t, filepath.Join(sb.Home, ".home"))
	answering(sb, true, "n", "n", "", "", "", "")
	if code := execute(sb.Ctx, "setup"); code != 0 {
		t.Fatalf("exit %d; stderr:\n%s\nstdout:\n%s", code, sb.Stderr, sb.Stdout)
	}
	out := sb.Stdout.String()
	wantContains(t, "stdout", out,
		"start a new one in "+repo+"\n",
		"created "+repo+", a new home repo",
		"~/.zshrc is a link to ~/.home/zshrc, and setup writes only through a link to a file in your home repo. Add the line there yourself.\n",
	)
	wantLacks(t, "stdout", out, "Add it to", "added the startup line")
	if after := treeHash(t, filepath.Join(sb.Home, ".home")); after != before {
		t.Errorf("the old ~/.home changed; zshrc = %q", mustRead(t, filepath.Join(sb.Home, ".home", "zshrc")))
	}
	if !exists(filepath.Join(repo, "homebot.yml")) {
		t.Error("no repo in $HOMEBOT_ROOT")
	}
}

// A .zshrc link that is dangling, or that leads out of the repo, is never
// written through, and nothing is created at its end.
func TestSetupLeavesAZshrcLinkOutOfTheRepoAlone(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target string
		body   string
	}{
		{"dangling", "some/dir/zshrc", ""},
		{"to a plain file", "dotfiles/zshrc", "export B=2\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sb := setupSandbox(t, "")
			sb.Ctx.Env = sb.Ctx.Env.With(map[string]string{"SHELL": "/bin/zsh"})
			target := filepath.Join(sb.Dir, tc.target)
			if tc.body != "" {
				sb.Files(map[string]string{tc.target: tc.body})
			}
			if err := os.Symlink(target, filepath.Join(sb.Home, ".zshrc")); err != nil {
				t.Fatal(err)
			}
			answering(sb, true, "", "", "", "")
			out := setupRunOK(t, sb)
			// Setup names the file a link resolves to; a dangling link has none,
			// so it names the path the link holds. On macOS the sandbox is
			// under /var, which resolves to /private/var.
			shown := target
			if tc.body != "" {
				var err error
				if shown, err = filepath.EvalSymlinks(target); err != nil {
					t.Fatal(err)
				}
			}
			wantContains(t, "stdout", out, setupZshHint+"~/.zshrc is a link to "+shown+", and setup writes only")
			wantLacks(t, "stdout", out, "Add it to", "added the startup line")
			if tc.body == "" {
				if exists(filepath.Join(sb.Dir, "some")) {
					t.Error("setup created the dangling link's folder")
				}
			} else if got := mustRead(t, target); got != tc.body {
				t.Errorf("the linked file = %q", got)
			}
		})
	}
}

// With $SHELL unset on a terminal, setup shows the line and asks nothing:
// it cannot tell that zsh is the shell.
func TestSetupWithoutSHELLOnlyShowsTheLine(t *testing.T) {
	sb := setupSandbox(t, "")
	answering(sb, true, "", "", "", "")
	out := setupRunOK(t, sb)
	wantContains(t, "stdout", out, setupZshHint)
	wantLacks(t, "stdout", out, "Add it to", "zsh-only")
	if exists(filepath.Join(sb.Home, ".zshrc")) {
		t.Error("~/.zshrc was written")
	}
}
