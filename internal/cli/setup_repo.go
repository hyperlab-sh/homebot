package cli

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/gitx"
	"github.com/hyperlab-sh/homebot/internal/linker"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/profile"
	"github.com/hyperlab-sh/homebot/internal/remote"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/unpack"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// setupRepo says where the home repo setup works on came from.
type setupRepo int

const (
	// setupFound is a home repo that was there before setup ran.
	setupFound setupRepo = iota
	// setupCreated is one setup just started.
	setupCreated
	// setupCloned is one setup just cloned.
	setupCloned
	// setupNone is no repo: the person said not now.
	setupNone
)

// setupNewConfig and setupNewProfile are the only files a new home repo
// starts with: a default profile, base, that unlocks secrets and links.
const (
	setupNewConfig  = "profiles:\n  default: base\n"
	setupNewProfile = "steps:\n  - unlock\n  - link\n"
)

// setupFindRepo locates the home repo, or, when there is none, offers to
// clone one or start one on a terminal. Without a terminal it creates
// nothing and says how. A folder in the way that is not a home repo is
// never touched, terminal or not.
func setupFindRepo(c *app.Context, opts setupOptions) (setupRepo, error) {
	err := c.LocateRoot()
	if err == nil {
		return setupFound, nil
	}
	if !isNoRoot(err) {
		return setupNone, err
	}
	// A relative HOMEBOT_ROOT names a different folder from every other
	// folder: the repo would be found only from this one.
	if dir := c.Env.Get("HOMEBOT_ROOT"); dir != "" && !filepath.IsAbs(dir) {
		return setupNone, fail.User(voice.Plain(c, msg.SetupErrorRootRelative(dir)))
	}
	target := setupTarget(c)
	shown := ui.Tildify(target, c.Home)
	if setupTaken(target) {
		mv := "mv " + setupShellPath(c, target) + " " + setupShellPath(c, target+".old")
		return setupNone, fail.User(voice.Plain(c, msg.SetupErrorHomeTaken(shown, mv)))
	}
	if !ui.Interactive(c) {
		return setupNone, fail.User(voice.Plain(c, msg.SetupErrorNoTerminal(shown)))
	}
	st := ui.Styles(c)
	ui.Println(c, st.Accent.Render(voice.Render(c, msg.SetupStatusNoRepo())))
	ui.Println(c, "")
	ui.Println(c, voice.Render(c, msg.SetupChoiceClone()))
	ui.Println(c, voice.Render(c, msg.SetupChoiceNew(shown)))
	ui.Println(c, voice.Render(c, msg.RemoteChoiceLater()))
	// Ending the input (Ctrl-D), at the choice or at the address, is not
	// now; the newline after it keeps "Not now" off the prompt's line.
	notNow := func(ended bool) (setupRepo, error) {
		if ended {
			ui.Println(c, "")
		}
		flags := ""
		if opts.hasPersona {
			flags += " --persona=" + opts.persona
		}
		if opts.hasName {
			flags += " --name=" + opts.name
		}
		command := msg.SetupHintSetupCmd()
		if flags != "" {
			command = msg.SetupHintRunLater(flags)
		}
		setupLater(c, setupCommand(c, command))
		return setupNone, nil
	}
	for {
		choice, ok := ui.AskOrEnd(c, voice.Plain(c, msg.RemotePromptChoice()))
		if !ok {
			return notNow(true)
		}
		switch strings.ToLower(choice) {
		case "", "q":
			return notNow(false)
		case "n":
			return setupCreated, setupCreate(c, target)
		case "c":
			url, ok := ui.AskOrEnd(c, voice.Plain(c, msg.SetupPromptURL()))
			if !ok {
				return notNow(true)
			}
			if url == "" {
				continue
			}
			return setupCloned, setupClone(c, url, target)
		default:
			ui.Println(c, st.Muted.Render(voice.Render(c, msg.RemoteHintPick())))
		}
	}
}

// setupTarget is where a new or cloned home repo goes: $HOMEBOT_ROOT, as
// homebot looks for it there first, else ~/.home. setupFindRepo has
// already refused a relative HOMEBOT_ROOT.
func setupTarget(c *app.Context) string {
	dir := c.Env.Get("HOMEBOT_ROOT")
	if dir == "" {
		return filepath.Join(c.Home, ".home")
	}
	return filepath.Clean(dir)
}

// setupTaken says whether something other than a home repo is at path: a
// file, or a folder with anything in it. LocateRoot already found no
// homebot.yml there. A folder setup cannot read counts as taken.
func setupTaken(path string) bool {
	st, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil || !st.IsDir() {
		return true
	}
	entries, err := os.ReadDir(path)
	return err != nil || len(entries) > 0
}

// setupShellPath is path as a shell word: ~/ and the quoted rest under
// $HOME, so the tilde still expands, else the quoted path.
func setupShellPath(c *app.Context, path string) string {
	if rel, ok := strings.CutPrefix(path, c.Home+"/"); ok {
		return "~/" + ui.ShellQuote(rel)
	}
	return ui.ShellQuote(path)
}

// setupCreate starts a home repo at target: git init on main, homebot.yml
// naming the base profile, and that profile. Nothing else is written; the
// two files are staged so triage does not ask about them. It then offers
// to create the keys.
func setupCreate(c *app.Context, target string) error {
	if err := os.MkdirAll(target, 0o777); err != nil {
		return err
	}
	if r := gitx.Run(c, target, "init", "-q", "-b", "main"); r.Code != 0 {
		return fail.User(fail.Failure(voice.Plain(c, msg.GitErrorFailed("init")), r.Stderr, "git"))
	}
	files := []struct{ rel, body string }{
		{"homebot.yml", setupNewConfig},
		{filepath.Join("manifests", "profiles", "base.yml"), setupNewProfile},
	}
	for _, f := range files {
		if err := setupWriteNew(filepath.Join(target, f.rel), f.body); err != nil {
			return err
		}
	}
	if r := gitx.Run(c, target, "add", "--", "homebot.yml", "manifests"); r.Code != 0 {
		return fail.User(fail.Failure(voice.Plain(c, msg.GitErrorFailed("add")), r.Stderr, "git"))
	}
	if err := c.LocateRoot(); err != nil {
		return err
	}
	if err := setupRecordUnpack(c); err != nil {
		return err
	}
	st := ui.Styles(c)
	ui.Println(c, secretsStyleLead(c, st.OK, msg.SetupSuccessCreated(ui.Tildify(target, c.Home)), " "))
	if setupYes(c, msg.SetupPromptKeys()) {
		return runKeysInit(c)
	}
	setupLater(c, st.Command.Render(voice.Plain(c, msg.SetupHintKeysCmd())))
	ui.Println(c, "") // the command stands apart from the guard line after it
	return nil
}

// setupRecordUnpack records the new repo's profile as unpacked here, as
// homebot unpack does after its last step. In a repo this new both steps
// have nothing to do (no secrets to unlock, nothing to link), so running
// them would only print that; without the record the inbox would ask for
// an unpack that changes nothing.
func setupRecordUnpack(c *app.Context) error {
	name, ok := profile.DefaultProfile(c)
	if !ok {
		return nil
	}
	steps, err := profile.ResolveFor(c, name)
	if err != nil {
		return err
	}
	return unpack.Record(c, name, steps)
}

// setupWriteNew writes body to a file that must not exist yet, making its
// folder. It never replaces a file.
func setupWriteNew(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(body); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// setupShortURL is owner/repo, the short form of a GitHub repository.
var setupShortURL = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// setupCloneURLs are the addresses setup tries, in order, for what the
// person typed. owner/repo is GitHub: over ssh first when ~/.ssh has a key,
// then https. An https address with a key also gets its ssh form after it,
// as homebot remote offers. Anything else is tried as typed, and so is an
// owner/repo that is a folder or file here (backups/home, say): a local
// path wins over GitHub.
func setupCloneURLs(c *app.Context, typed string) []string {
	hasKey := remote.HasSSHKey(c.Home)
	if setupIsShortURL(c, typed) {
		repo := strings.TrimSuffix(typed, ".git")
		https := "https://github.com/" + repo + ".git"
		if hasKey {
			return []string{"git@github.com:" + repo + ".git", https}
		}
		return []string{https}
	}
	if ssh := remote.SSHAddress(typed); ssh != "" && hasKey {
		return []string{typed, ssh}
	}
	return []string{typed}
}

// setupIsShortURL says whether typed is GitHub's owner/repo: two names
// joined by one slash, not starting with a dot, and nothing by that path in
// the current folder.
func setupIsShortURL(c *app.Context, typed string) bool {
	if !setupShortURL.MatchString(typed) || strings.HasPrefix(typed, ".") {
		return false
	}
	_, err := os.Lstat(filepath.Join(c.Cwd, typed))
	return errors.Is(err, fs.ErrNotExist)
}

// setupClone clones typed into target, trying each of its addresses, then
// offers to unpack the profile for this machine and, once it has run, to
// move aside what is in the way of its links. Declining the unpack declines
// its links too. A clone without homebot.yml is left where it is
// and stops setup.
func setupClone(c *app.Context, typed, target string) error {
	st := ui.Styles(c)
	shown := ui.Tildify(target, c.Home)
	urls := setupCloneURLs(c, typed)
	var cloned string
	var said bytes.Buffer
	for i, url := range urls {
		ui.Println(c, voice.Render(c, msg.SetupStatusCloning(url, shown)))
		code, err := setupCloneOnce(c, url, target, &said)
		if err != nil {
			return err
		}
		if code == 0 {
			cloned = url
			break
		}
		if i == len(urls)-1 {
			return setupCloneFailed(c, url, said.String())
		}
		ui.Println(c, st.Todo.Render(voice.Render(c, msg.SetupWarningCloneFailed(url))))
	}
	if _, err := os.Stat(filepath.Join(target, "homebot.yml")); err != nil {
		return fail.User(voice.Plain(c, msg.SetupErrorNotAHomeRepo(cloned, shown)))
	}
	if err := c.LocateRoot(); err != nil {
		return err
	}
	ran, err := setupUnpack(c)
	if err != nil || !ran {
		return err
	}
	return setupBackup(c)
}

// setupCloneOnce runs git clone on the terminal: git can ask for a user
// name and password, and ssh about a host it has not seen, as on the
// command line (no GIT_TERMINAL_PROMPT=0). What git says goes to the
// person as it says it, and into said too, so a failure can be told apart.
// It returns git's exit code; the error is set only when git could not run.
func setupCloneOnce(c *app.Context, url, target string, said *bytes.Buffer) (int, error) {
	in, out, errw := setupAttached(c)
	return proc.RunAttached(c, []string{"git", "clone", "-q", "--", url, target}, proc.AttachedOptions{
		Dir: c.Cwd, Env: c.Env, Stdin: in, Stdout: out, Stderr: io.MultiWriter(errw, said),
	})
}

// setupAttached is the terminal for a program setup hands it to (git,
// brew). A stdin that is not a file stays with homebot: os/exec would copy
// it into the program ahead of its reads, and the answers to setup's later
// questions with it.
func setupAttached(c *app.Context) (io.Reader, io.Writer, io.Writer) {
	in, out, errw := ui.Attached(c)
	if _, ok := in.(*os.File); !ok {
		in = nil
	}
	return in, out, errw
}

// setupAuthSigns are what git and ssh print when the clone failed for want
// of a login rather than a wrong address.
var setupAuthSigns = []string{
	"terminal prompts disabled",
	"could not read username",
	"could not read password",
	"authentication failed",
	"permission denied (publickey",
	"invalid username or password",
	"password authentication is not supported",
}

// setupCloneFailed is the error of a clone whose last try failed. A
// failure to sign in, on any try, says how to sign in: gh auth login for
// GitHub when gh is there, else an ssh key on that server. Anything else
// says to check the address. git has already said why, on the terminal.
func setupCloneFailed(c *app.Context, url, said string) error {
	lower := strings.ToLower(said)
	auth := false
	for _, sign := range setupAuthSigns {
		if strings.Contains(lower, sign) {
			auth = true
			break
		}
	}
	if !auth {
		return fail.User(voice.Plain(c, msg.SetupErrorCloneFailed(url)))
	}
	host := setupURLHost(url)
	if host == "github.com" && proc.HasCommand(c, "gh") {
		return fail.User(voice.Plain(c, msg.SetupErrorCloneAuthGh(url)))
	}
	if host == "" {
		host = url
	}
	return fail.User(voice.Plain(c, msg.SetupErrorCloneAuthKey(url, host)))
}

// setupURLHost is the server of a git address: the host of
// https://host/... or ssh://user@host/..., and of user@host:path. A local
// path has none.
func setupURLHost(url string) string {
	if _, rest, ok := strings.Cut(url, "://"); ok {
		host, _, _ := strings.Cut(rest, "/")
		if _, h, ok := strings.Cut(host, "@"); ok {
			host = h
		}
		host, _, _ = strings.Cut(host, ":")
		return host
	}
	if user, rest, ok := strings.Cut(url, "@"); ok && !strings.Contains(user, "/") {
		host, _, _ := strings.Cut(rest, ":")
		return host
	}
	return ""
}

// setupUnpack shows the steps of this machine's profile and runs them when
// the person agrees, and says whether they ran. No profile for this machine
// is said, and setup goes on.
func setupUnpack(c *app.Context) (bool, error) {
	st := ui.Styles(c)
	name, ok := profile.DefaultProfile(c)
	if !ok {
		ui.Println(c, st.Todo.Render(voice.Render(c, msg.SetupWarningNoProfile(c.Host))))
		ui.Println(c, setupCommand(c, msg.SetupHintUnpackCmd()))
		return false, nil
	}
	steps, err := profile.ResolveFor(c, name)
	if err != nil {
		return false, err
	}
	if err := unpack.Run(c, name, unpack.Options{DryRun: true}); err != nil {
		return false, err
	}
	if !setupYes(c, msg.SetupPromptUnpack(len(steps))) {
		setupLater(c, setupCommand(c, msg.SetupHintUnpackCmd()))
		return false, nil
	}
	return true, unpack.Run(c, name, unpack.Options{})
}

// setupBackup offers homebot link --backup when real files stand where the
// repo's links go.
func setupBackup(c *app.Context) error {
	plan, err := linker.PlanLinks(c)
	if err != nil {
		return err
	}
	n := len(plan.Only(linker.Conflict))
	if n == 0 {
		return nil
	}
	if !setupYes(c, msg.SetupPromptBackup(n)) {
		setupLater(c, setupCommand(c, msg.LinkHintBackupCmd()))
		return nil
	}
	return runLink(c, linkFlags{backup: true})
}
