package remote_test

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/remote"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// fx is a sandbox with a bare repository to push to.
type fx struct {
	*testutil.Box
	tb   testing.TB
	bare string
}

// CreateRepository sends the catalog's description to the forge. This test
// cannot detect the description being voiced, and no test of this path can:
// remote.repo_description has no persona flag and no {bot} slot, so no
// persona can reword it, whatever the code calls. What guards that is
// msg.TestStoredOnlyIDsStayUnvoiced, which fails if the id gains either.
func TestCreateRepositorySendsTheCatalogDescription(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	b.WithStubs(map[string]string{"gh": `printf '%s\n' "$@" > "${0%/*}/gh-args"
echo '{"ssh_url":"git@example.test:user/home.git","clone_url":"https://example.test/user/home.git"}'`})
	if _, err := remote.CreateRepository(b.Ctx, remote.Forges[0], "home"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(b.Bin, "gh-args"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); !strings.Contains(got, "description=Dotfiles, kept by homebot\n") {
		t.Errorf("gh arguments = %q", got)
	}
}

func newFx(t *testing.T) *fx {
	t.Helper()
	f := &fx{Box: testutil.Sandbox(t, testutil.Options{Git: true}), tb: t}
	f.bare = filepath.Join(f.Dir, "remote.git")
	f.GitIn(f.Dir, "init", "-q", "--bare", "-b", "main", f.bare)
	return f
}

func (f *fx) commit() {
	f.tb.Helper()
	f.Files(map[string]string{"home/.home/file": "x"})
	f.GitIn(f.Root, "add", "-A")
	f.GitIn(f.Root, "commit", "-q", "-m", "first")
}

func (f *fx) origin() string {
	return strings.TrimSpace(f.GitIn(f.Root, "config", "--get", "remote.origin.url"))
}

func (f *fx) remotes() string { return strings.TrimSpace(f.GitIn(f.Root, "remote")) }

func (f *fx) pushedTo(repo string) bool {
	return strings.TrimSpace(f.GitIn(repo, "branch", "--list", "main")) != ""
}

// talk is a scripted person: the answers it gives, the questions it was
// asked and the lines it was shown, without styles.
type talk struct {
	replies []string
	asked   []string
	lines   []string
}

func (tk *talk) io(interactive bool) remote.IO {
	return remote.IO{
		Interactive: interactive,
		Ask: func(q string) string {
			tk.asked = append(tk.asked, q)
			if len(tk.replies) == 0 {
				return ""
			}
			r := tk.replies[0]
			tk.replies = tk.replies[1:]
			return r
		},
		Log: func(line string) { tk.lines = append(tk.lines, strings.Split(ansi.Strip(line), "\n")...) },
	}
}

func (tk *talk) text() string { return strings.Join(tk.lines, "\n") }

func (tk *talk) last(n int) []string { return tk.lines[len(tk.lines)-n:] }

func (f *fx) run(url string, tk *talk, interactive bool, opts remote.Options) error {
	f.tb.Helper()
	return remote.Run(f.Ctx, url, tk.io(interactive), opts)
}

func (f *fx) mustRun(url string, tk *talk) {
	f.tb.Helper()
	if err := f.run(url, tk, true, remote.Options{}); err != nil {
		f.tb.Fatal(err)
	}
}

// forges is a gh and a glab that answer `auth status` per logged, and `api`
// with JSON naming the given repositories. Their arguments land in gh-args
// and glab-args next to them.
func (f *fx) forges(ghLogged, glabLogged bool, ssh, https string) {
	exit := func(ok bool) string {
		if ok {
			return "0"
		}
		return "1"
	}
	f.WithStubs(map[string]string{
		"gh": `if [ "$1" = auth ]; then exit ` + exit(ghLogged) + ` ; fi
printf '%s\n' "$@" > "${0%/*}/gh-args"
echo '{"ssh_url":"` + ssh + `","clone_url":"` + https + `"}'`,
		"glab": `if [ "$1" = auth ]; then exit ` + exit(glabLogged) + ` ; fi
printf '%s\n' "$@" > "${0%/*}/glab-args"
echo '{"ssh_url_to_repo":"` + ssh + `","http_url_to_repo":"` + https + `"}'`,
	})
}

func (f *fx) read(name string) string {
	b, err := os.ReadFile(filepath.Join(f.Bin, name))
	if err != nil {
		return ""
	}
	return string(b)
}

func eq[T comparable](t *testing.T, got, want T, what string) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func eqLines(t *testing.T, got, want []string, what string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n%s\nwant:\n%s", what, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestSSHAddressesForHTTPSRemotes(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/alice/dotfiles.git":     "git@github.com:alice/dotfiles.git",
		"https://github.com/alice/dotfiles":         "git@github.com:alice/dotfiles.git",
		"https://me@gitlab.com/group/sub/home.git/": "git@gitlab.com:group/sub/home.git",
		"https://github.com/alice/dotfiles/":        "git@github.com:alice/dotfiles.git",
		"git@github.com:alice/dotfiles.git":         "",
		"/srv/git/home.git":                         "",
		"http://github.com/alice/dotfiles.git":      "",
		"https://github.com":                        "",
	} {
		eq(t, remote.SSHAddress(in), want, "SSHAddress("+in+")")
	}
}

func TestTheSuggestedNameIsTheRepoFoldersWithoutTheDot(t *testing.T) {
	for in, want := range map[string]string{
		"/Users/x/.home":    "home",
		"/Users/x/dotfiles": "dotfiles",
		"/Users/x/..odd":    "odd",
		"/Users/x/.":        "dotfiles",
		"/":                 "dotfiles",
		"":                  "dotfiles",
	} {
		eq(t, remote.DefaultName(in), want, "DefaultName("+in+")")
	}
}

func TestHasSSHKeyLooksForTheThreeKeyNames(t *testing.T) {
	f := newFx(t)
	eq(t, remote.HasSSHKey(f.Home), false, "no .ssh")
	f.Files(map[string]string{"home/.ssh/id_ed25519.pub": "PUB", "home/.ssh/known_hosts": "x"})
	eq(t, remote.HasSSHKey(f.Home), false, "only a public key")
	for _, k := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		g := newFx(t)
		g.Files(map[string]string{"home/.ssh/" + k: "KEY"})
		eq(t, remote.HasSSHKey(g.Home), true, k)
	}
}

func TestAURLBecomesOriginAndTheCommitsArePushed(t *testing.T) {
	f := newFx(t)
	f.commit()
	tk := &talk{}
	f.mustRun(f.bare, tk)
	eq(t, f.origin(), f.bare, "origin")
	eq(t, f.pushedTo(f.bare), true, "pushed")
	eqLines(t, tk.lines, []string{"remote " + f.bare, "reachable " + f.bare, "pushed the repo is backed up"}, "lines")
}

func TestAURLReplacesTheOriginThereWas(t *testing.T) {
	f := newFx(t)
	f.commit()
	f.GitIn(f.Root, "remote", "add", "origin", "/nowhere.git")
	f.mustRun(f.bare, &talk{})
	eq(t, f.origin(), f.bare, "origin")
}

func TestNothingCommittedYetOriginIsSetNothingPushed(t *testing.T) {
	f := newFx(t)
	tk := &talk{}
	f.mustRun(f.bare, tk)
	eq(t, f.origin(), f.bare, "origin")
	eqLines(t, tk.last(2), []string{"Nothing committed yet. This commits and pushes:", "  homebot save"}, "last lines")
}

func TestAnOriginThatHasEverythingSaysSo(t *testing.T) {
	f := newFx(t)
	f.commit()
	f.mustRun(f.bare, &talk{})
	tk := &talk{}
	f.mustRun("", tk)
	eqLines(t, tk.lines, []string{"reachable " + f.bare, "Already backed up: the remote has every commit."}, "lines")
}

func TestAnOriginTheRemoteHasMovedPastIsPulled(t *testing.T) {
	f := newFx(t)
	f.commit()
	f.mustRun(f.bare, &talk{})
	other := filepath.Join(f.Dir, "other")
	f.GitIn(f.Dir, "clone", "-q", f.bare, other)
	f.Files(map[string]string{"other/new.txt": "x"})
	f.GitIn(other, "add", "-A")
	f.GitIn(other, "commit", "-q", "-m", "from other")
	f.GitIn(other, "push", "-q")
	tk := &talk{}
	f.mustRun("", tk)
	eqLines(t, tk.lines, []string{"reachable " + f.bare, "pulled 1 commit", "Already backed up: the remote has every commit."}, "lines")
}

func TestPullingWaitsWhenThereAreUncommittedChanges(t *testing.T) {
	f := newFx(t)
	f.commit()
	f.mustRun(f.bare, &talk{})
	other := filepath.Join(f.Dir, "other")
	f.GitIn(f.Dir, "clone", "-q", f.bare, other)
	f.Files(map[string]string{"other/new.txt": "x", "home/.home/file": "edited"})
	f.GitIn(other, "add", "-A")
	f.GitIn(other, "commit", "-q", "-m", "from other")
	f.GitIn(other, "push", "-q")
	tk := &talk{}
	f.mustRun("", tk)
	eqLines(t, tk.last(2), []string{"The remote has new commits. Commit or discard the changes here, then:", "  homebot save"}, "last lines")
	eq(t, strings.Contains(tk.text(), "pulled"), false, "pulled")
}

func TestARemoteNotCalledOriginSaysHowToRenameIt(t *testing.T) {
	f := newFx(t)
	f.GitIn(f.Root, "remote", "add", "upstream", f.bare)
	f.GitIn(f.Root, "remote", "add", "zed", f.bare)
	tk := &talk{}
	f.mustRun("", tk)
	eqLines(t, tk.lines, []string{
		"This repo has a remote named upstream, zed, but homebot pushes to the one named origin. Rename it:",
		"  git -C ~/.home remote rename upstream origin",
	}, "lines")
	eq(t, f.remotes(), "upstream\nzed", "remotes")
}

func TestWithoutATerminalItExplainsAndAsksNothing(t *testing.T) {
	f := newFx(t)
	f.forges(true, false, f.bare, f.bare)
	tk := &talk{replies: []string{"g"}}
	if err := f.run("", tk, false, remote.Options{}); err != nil {
		t.Fatal(err)
	}
	eq(t, len(tk.asked), 0, "questions asked")
	eqLines(t, tk.lines, []string{
		"Nothing is backed up yet: the repo has no remote.",
		"Run this in a terminal to create a private GitHub repository for it:",
		"  homebot remote",
		"Or use a repository you already have:",
		"  homebot remote <url>",
	}, "lines")
	eq(t, f.remotes(), "", "remotes")
}

func TestWithoutATerminalBothForgesAreNamed(t *testing.T) {
	f := newFx(t)
	f.forges(true, true, f.bare, f.bare)
	tk := &talk{}
	if err := f.run("", tk, false, remote.Options{}); err != nil {
		t.Fatal(err)
	}
	eq(t, tk.lines[1], "Run this in a terminal to create a private GitHub or GitLab repository for it:", "line 1")
}

func TestWithoutATerminalAndWithoutAForgeOnlyTheURLWayIsExplained(t *testing.T) {
	f := newFx(t)
	f.forges(false, false, f.bare, f.bare)
	tk := &talk{}
	if err := f.run("", tk, false, remote.Options{}); err != nil {
		t.Fatal(err)
	}
	eqLines(t, tk.lines, []string{
		"Nothing is backed up yet: the repo has no remote.",
		"Use a repository you already have:",
		"  homebot remote <url>",
	}, "lines")
}

func TestGCreatesAPrivateGitHubRepositoryNamedAfterTheFolderAndPushesOverHTTPSWithoutAnSSHKey(t *testing.T) {
	f := newFx(t)
	f.commit()
	https := filepath.Join(f.Dir, "https.git")
	f.GitIn(f.Dir, "init", "-q", "--bare", "-b", "main", https)
	f.forges(true, false, f.bare, https)
	tk := &talk{replies: []string{"g", ""}}
	f.mustRun("", tk)
	eqLines(t, strings.Split(strings.TrimSuffix(f.read("gh-args"), "\n"), "\n"), []string{
		"api", "-X", "POST", "user/repos",
		"-f", "name=home", "-F", "private=true", "-f", "description=Dotfiles, kept by homebot",
	}, "gh's arguments")
	eqLines(t, tk.asked, []string{">", "Name for the new private GitHub repository [home]:"}, "questions")
	out := tk.text()
	for _, want := range []string{
		"  glab is installed but not logged in. To offer GitLab too: glab auth login",
		"created home, a private GitHub repository",
		"There's no ssh key in ~/.ssh, so the remote uses https. To let git use gh's login:",
		"  gh auth setup-git",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	eq(t, f.origin(), https, "origin")
	eq(t, f.pushedTo(https), true, "pushed")
}

func TestWithAnSSHKeyTheNewRepositorysSSHAddressIsUsed(t *testing.T) {
	f := newFx(t)
	f.commit()
	f.Files(map[string]string{"home/.ssh/id_ed25519": "KEY"})
	f.forges(true, false, f.bare, "/not/this.git")
	tk := &talk{replies: []string{"g", "dots"}}
	f.mustRun("", tk)
	eq(t, f.origin(), f.bare, "origin")
	eq(t, f.pushedTo(f.bare), true, "pushed")
	eqLines(t, strings.Split(strings.TrimSuffix(f.read("gh-args"), "\n"), "\n"), []string{
		"api", "-X", "POST", "user/repos",
		"-f", "name=dots", "-F", "private=true", "-f", "description=Dotfiles, kept by homebot",
	}, "gh's arguments")
	eq(t, strings.Contains(tk.text(), "no ssh key"), false, "https hint")
}

func TestLCreatesItOnGitLab(t *testing.T) {
	f := newFx(t)
	f.commit()
	f.Files(map[string]string{"home/.ssh/id_rsa": "KEY"})
	f.forges(false, true, f.bare, "/not/this.git")
	tk := &talk{replies: []string{"g", "l", ""}}
	f.mustRun("", tk)
	if !strings.Contains(tk.text(), "  pick one of the letters above") {
		t.Errorf("no hint for the unknown letter:\n%s", tk.text())
	}
	eqLines(t, strings.Split(strings.TrimSuffix(f.read("glab-args"), "\n"), "\n"), []string{
		"api", "-X", "POST", "projects",
		"-f", "name=home", "-f", "visibility=private", "-f", "description=Dotfiles, kept by homebot",
	}, "glab's arguments")
	eq(t, f.origin(), f.bare, "origin")
}

func TestTheChoiceIsCaseInsensitiveAndTheMenuListsWhatIsReady(t *testing.T) {
	f := newFx(t)
	f.commit()
	f.Files(map[string]string{"home/.ssh/id_rsa": "KEY"})
	f.forges(true, true, f.bare, f.bare)
	tk := &talk{replies: []string{"G", ""}}
	f.mustRun("", tk)
	eqLines(t, tk.lines[1:6], []string{
		"",
		"  g      create a private GitHub repository (with gh)",
		"  l      create a private GitLab repository (with glab)",
		"  u      use the URL of a repository you already have",
		"  enter  not now",
	}, "menu")
}

func TestABadNameIsRefusedAndAskedAgain(t *testing.T) {
	f := newFx(t)
	f.commit()
	f.forges(true, false, f.bare, f.bare)
	tk := &talk{replies: []string{"g", "my home!", "g", "ok-name"}}
	f.mustRun("", tk)
	if !strings.Contains(tk.text(), `  "my home!" won't do: letters, digits, ".", "-" and "_" only`) {
		t.Errorf("no refusal:\n%s", tk.text())
	}
	eq(t, strings.Contains(f.read("gh-args"), "name=ok-name"), true, "name given to gh")
	eq(t, strings.Contains(f.read("gh-args"), "my home"), false, "bad name given to gh")
}

func TestUTakesAURLYouAlreadyHave(t *testing.T) {
	f := newFx(t)
	f.commit()
	f.forges(false, false, f.bare, f.bare)
	f.mustRun("", &talk{replies: []string{"u", f.bare}})
	eq(t, f.origin(), f.bare, "origin")
	eq(t, f.pushedTo(f.bare), true, "pushed")
}

func TestAnEmptyURLAsksForTheChoiceAgain(t *testing.T) {
	f := newFx(t)
	f.forges(false, false, f.bare, f.bare)
	tk := &talk{replies: []string{"u", "", ""}}
	f.mustRun("", tk)
	eqLines(t, tk.asked, []string{">", "URL:", ">"}, "questions")
	eq(t, f.remotes(), "", "remotes")
}

func TestEnterMeansNotNow(t *testing.T) {
	f := newFx(t)
	f.forges(true, true, f.bare, f.bare)
	tk := &talk{replies: []string{""}}
	f.mustRun("", tk)
	eqLines(t, tk.last(2), []string{"Not now. When you're ready:", "  homebot remote"}, "last lines")
	eq(t, f.read("gh-args"), "", "gh was run")
	eq(t, f.remotes(), "", "remotes")
}

func TestAFailedCreateIsAnErrorWithNothingSet(t *testing.T) {
	f := newFx(t)
	f.WithStubs(map[string]string{
		"gh":   `if [ "$1" = auth ]; then exit 0; fi` + "\n" + `echo "name already exists on this account" >&2; exit 1`,
		"glab": "exit 1",
	})
	err := f.run("", &talk{replies: []string{"g", ""}}, true, remote.Options{})
	if err == nil || err.Error() != "gh couldn't create home.\n  gh: name already exists on this account" {
		t.Errorf("err = %v", err)
	}
	eq(t, f.remotes(), "", "remotes")
}

func TestAFailedCreateWithOnlyStdoutQuotesStdout(t *testing.T) {
	f := newFx(t)
	f.WithStubs(map[string]string{
		"gh":   `if [ "$1" = auth ]; then exit 0; fi` + "\n" + `echo "quota exceeded"; exit 1`,
		"glab": "exit 1",
	})
	err := f.run("", &talk{replies: []string{"g", ""}}, true, remote.Options{})
	if err == nil || err.Error() != "gh couldn't create home.\n  gh: quota exceeded" {
		t.Errorf("err = %v", err)
	}
}

func TestACreateWithoutAnAddressIsAnErrorWithNothingSet(t *testing.T) {
	for name, body := range map[string]string{
		"no JSON":      `echo 'created'`,
		"no addresses": `echo '{"id":1}'`,
		"only ssh":     `echo '{"ssh_url":"git@x:y/z.git"}'`,
		"not a string": `echo '{"ssh_url":1,"clone_url":2}'`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFx(t)
			f.WithStubs(map[string]string{
				"gh":   `if [ "$1" = auth ]; then exit 0; fi` + "\n" + body,
				"glab": "exit 1",
			})
			err := f.run("", &talk{replies: []string{"g", "dots"}}, true, remote.Options{})
			want := "gh created dots but didn't say its address. Add it with:\n  homebot remote <url>"
			if err == nil || err.Error() != want {
				t.Errorf("err = %v, want %q", err, want)
			}
			eq(t, f.remotes(), "", "remotes")
		})
	}
}

func TestGitLabsAddressesAreTheOnesWithTheLongerNames(t *testing.T) {
	f := newFx(t)
	f.commit()
	f.WithStubs(map[string]string{
		"gh": "exit 1",
		"glab": `if [ "$1" = auth ]; then exit 0; fi
echo '{"ssh_url_to_repo":"` + f.bare + `","http_url_to_repo":"/not/this.git"}'`,
	})
	f.Files(map[string]string{"home/.ssh/id_rsa": "KEY"})
	f.mustRun("", &talk{replies: []string{"l", ""}})
	eq(t, f.origin(), f.bare, "origin")
}

func TestARemoteGitCannotSetIsAnError(t *testing.T) {
	f := newFx(t)
	// A remote name git refuses cannot be reached through origin; a broken
	// repo config makes `git remote add` fail.
	if err := os.WriteFile(filepath.Join(f.Root, ".git", "config"), []byte("[core\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := f.run(f.bare, &talk{}, true, remote.Options{})
	if err == nil || !strings.HasPrefix(err.Error(), "Couldn't set the remote to "+f.bare+".") {
		t.Errorf("err = %v", err)
	}
}

func TestAnHTTPSOriginNobodyIsLoggedFor(t *testing.T) {
	const noLogin = "fatal: could not read Username for 'https://github.com': terminal prompts disabled"
	const https = "https://github.com/me/home.git"
	const ssh = "git@github.com:me/home.git"

	setup := func(t *testing.T) (*fx, remote.Options) {
		f := newFx(t)
		f.commit()
		f.GitIn(f.Root, "remote", "add", "origin", https)
		// the ssh address leads to the local bare repo, so nothing leaves the machine
		f.GitIn(f.Root, "config", "url."+f.bare+".insteadOf", ssh)
		return f, remote.Options{Reach: func(*app.Context) remote.Reach {
			if f.origin() == https {
				return remote.Reach{Error: noLogin}
			}
			return remote.Reach{OK: true}
		}}
	}

	t.Run("with an ssh key, it offers the ssh address and pushes there", func(t *testing.T) {
		f, opts := setup(t)
		f.Files(map[string]string{"home/.ssh/id_rsa": "KEY"})
		tk := &talk{replies: []string{""}}
		if err := f.run("", tk, true, opts); err != nil {
			t.Fatal(err)
		}
		eqLines(t, tk.lines[:6], []string{
			"Can't reach the remote: git has no login for https here.",
			"  " + https,
			"  git: could not read Username for 'https://github.com': terminal prompts disabled",
			"",
			"You have an ssh key, so the same repository can be reached over ssh:",
			"  " + ssh,
		}, "first lines")
		eqLines(t, tk.asked, []string{"Switch to it? [Y/n]"}, "questions")
		eqLines(t, tk.lines[6:8], []string{"remote " + ssh, "reachable " + ssh}, "after the switch")
		eq(t, f.origin(), ssh, "origin")
		eq(t, f.pushedTo(f.bare), true, "pushed")
	})

	for _, yes := range []string{"y", "Y", "yes", "YES"} {
		t.Run("answering "+yes+" switches", func(t *testing.T) {
			f, opts := setup(t)
			f.Files(map[string]string{"home/.ssh/id_rsa": "KEY"})
			if err := f.run("", &talk{replies: []string{yes}}, true, opts); err != nil {
				t.Fatal(err)
			}
			eq(t, f.origin(), ssh, "origin")
		})
	}

	for _, no := range []string{"n", "no", "later"} {
		t.Run("answering "+no+" keeps https and says how to switch later", func(t *testing.T) {
			f, opts := setup(t)
			f.Files(map[string]string{"home/.ssh/id_rsa": "KEY"})
			tk := &talk{replies: []string{no}}
			if err := f.run("", tk, true, opts); err != nil {
				t.Fatal(err)
			}
			eq(t, f.origin(), https, "origin")
			eqLines(t, tk.last(2), []string{"Kept https. To switch later:", "  homebot remote " + ssh}, "last lines")
			eq(t, f.pushedTo(f.bare), false, "pushed")
		})
	}

	t.Run("without a terminal, it names the ssh command and changes nothing", func(t *testing.T) {
		f, opts := setup(t)
		f.Files(map[string]string{"home/.ssh/id_rsa": "KEY"})
		tk := &talk{replies: []string{"y"}}
		if err := f.run("", tk, false, opts); err != nil {
			t.Fatal(err)
		}
		eq(t, len(tk.asked), 0, "questions asked")
		eqLines(t, tk.last(4), []string{
			"You have an ssh key, so the same repository can be reached over ssh:",
			"  " + ssh,
			"To switch:",
			"  homebot remote " + ssh,
		}, "last lines")
		eq(t, f.origin(), https, "origin")
	})

	t.Run("without an ssh key, it says how to log in", func(t *testing.T) {
		f, opts := setup(t)
		tk := &talk{}
		if err := f.run("", tk, true, opts); err != nil {
			t.Fatal(err)
		}
		eq(t, len(tk.asked), 0, "questions asked")
		eqLines(t, tk.last(4), []string{
			"Log git in:",
			"  gh auth setup-git",
			"Or add an ssh key to ~/.ssh and switch to the ssh address:",
			"  homebot remote " + ssh,
		}, "last lines")
	})

	t.Run("other failures (a typo, no network) get no offer", func(t *testing.T) {
		f, _ := setup(t)
		f.Files(map[string]string{"home/.ssh/id_rsa": "KEY"})
		tk := &talk{}
		opts := remote.Options{Reach: func(*app.Context) remote.Reach {
			return remote.Reach{Error: "fatal: unable to access: Could not resolve host: github.com"}
		}}
		if err := f.run("", tk, true, opts); err != nil {
			t.Fatal(err)
		}
		eq(t, len(tk.asked), 0, "questions asked")
		eqLines(t, tk.lines, []string{"Can't reach the remote.", "  " + https, "  git: unable to access: Could not resolve host: github.com"}, "lines")
	})

	t.Run("an ssh address that does not work either is reported, and nothing is pushed", func(t *testing.T) {
		f, _ := setup(t)
		f.Files(map[string]string{"home/.ssh/id_rsa": "KEY"})
		tk := &talk{replies: []string{"y"}}
		opts := remote.Options{Reach: func(*app.Context) remote.Reach {
			if f.origin() == https {
				return remote.Reach{Error: noLogin}
			}
			return remote.Reach{Error: "git@github.com: Permission denied (publickey)."}
		}}
		if err := f.run("", tk, true, opts); err != nil {
			t.Fatal(err)
		}
		eqLines(t, tk.last(3), []string{"Can't reach it over ssh either.", "  " + ssh, "  git: git@github.com: Permission denied (publickey)."}, "last lines")
		eq(t, f.origin(), ssh, "origin")
		eq(t, f.pushedTo(f.bare), false, "pushed")
	})

	t.Run("every no-login wording counts", func(t *testing.T) {
		for _, said := range []string{
			"fatal: could not read Username for 'https://github.com'",
			"fatal: Authentication failed for 'https://github.com/me/home.git/'",
			"fatal: unable to access 'https://x/': The requested URL returned error: 403",
			"remote: error\nfatal: unable to access: The requested URL returned error: 401",
			"fatal: TERMINAL PROMPTS DISABLED",
		} {
			f, _ := setup(t)
			tk := &talk{}
			opts := remote.Options{Reach: func(*app.Context) remote.Reach { return remote.Reach{Error: said} }}
			if err := f.run("", tk, true, opts); err != nil {
				t.Fatal(err)
			}
			eq(t, tk.lines[0], "Can't reach the remote: git has no login for https here.", "headline for "+said)
		}
	})

	t.Run("a GitLab host is logged in with glab, and an unknown host with neither", func(t *testing.T) {
		for host, want := range map[string][]string{
			"https://gitlab.com/me/home.git": {"Log git in:", "  glab auth login", "Or add an ssh key to ~/.ssh and switch to the ssh address:", "  homebot remote git@gitlab.com:me/home.git"},
			"https://git.example.org/me/home.git": {
				"Log git in to that host, or add an ssh key to ~/.ssh and switch to the ssh address:",
				"  homebot remote git@git.example.org:me/home.git",
			},
		} {
			f := newFx(t)
			f.commit()
			f.GitIn(f.Root, "remote", "add", "origin", host)
			tk := &talk{}
			opts := remote.Options{Reach: func(*app.Context) remote.Reach { return remote.Reach{Error: noLogin} }}
			if err := f.run("", tk, true, opts); err != nil {
				t.Fatal(err)
			}
			eqLines(t, tk.last(len(want)), want, host)
		}
	})

	t.Run("a non-https origin that cannot be reached gets no offer either", func(t *testing.T) {
		f := newFx(t)
		f.commit()
		f.GitIn(f.Root, "remote", "add", "origin", "git@github.com:me/home.git")
		tk := &talk{}
		opts := remote.Options{Reach: func(*app.Context) remote.Reach { return remote.Reach{Error: noLogin} }}
		if err := f.run("", tk, true, opts); err != nil {
			t.Fatal(err)
		}
		eq(t, len(tk.lines), 3, "lines")
	})
}

// The reach check must tell git never to prompt: with a git that records how
// it was called, the check fails if ReachOrigin stops going through
// gitx.RunNet. (The test below it uses the real git, which finds nothing to
// ask for at a host that hangs up, so it cannot see the guard go.)
func TestTheReachCheckTellsGitNeverToPrompt(t *testing.T) {
	f := newFx(t)
	f.WithStubs(map[string]string{"git": `{ printf '%s\n' "$@"; printf 'GIT_TERMINAL_PROMPT=%s\n' "${GIT_TERMINAL_PROMPT-unset}"; } > "${0%/*}/git-call"
echo "fatal: no" >&2
exit 128`})
	got := remote.ReachOrigin(f.Ctx)
	eq(t, got, remote.Reach{Error: "fatal: no"}, "reach")
	eqLines(t, strings.Split(strings.TrimSuffix(f.read("git-call"), "\n"), "\n"), []string{
		"--no-optional-locks", "-c", "core.quotePath=false", "ls-remote", "--heads", "origin",
		"GIT_TERMINAL_PROMPT=0",
	}, "git's arguments and prompt setting")
}

func TestTheRealReachCheckNeverWaitsForAUsername(t *testing.T) {
	// A real listener that hangs up on every connection stands in for an
	// unreachable host. A closed port would do on most machines, but under
	// WSL mirrored networking the SYN to a closed loopback port is dropped
	// instead of refused, and git then waits for the kernel's connect timeout.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	url := "https://" + ln.Addr().String() + "/nothing.git"

	f := newFx(t)
	f.GitIn(f.Root, "remote", "add", "origin", url)
	started := time.Now()
	tk := &talk{}
	f.mustRun("", tk)
	if d := time.Since(started); d > 10*time.Second {
		t.Errorf("took %v", d)
	}
	eqLines(t, tk.lines[:2], []string{"Can't reach the remote.", "  " + url}, "first lines")
}

func TestAnUnreachablePathShowsGitsLastTwoLines(t *testing.T) {
	f := newFx(t)
	f.commit()
	tk := &talk{}
	missing := filepath.Join(f.Dir, "missing.git")
	f.mustRun(missing, tk)
	eqLines(t, tk.lines, []string{
		"remote " + missing,
		"Can't reach the remote.",
		"  " + missing,
		"  git: Please make sure you have the correct access rights",
		"  git: and the repository exists.",
	}, "lines")
	eq(t, f.origin(), missing, "origin")
}
