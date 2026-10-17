// Package remote is `homebot remote`: where the repo is backed up. Without a
// remote it offers to create a private repository with gh or glab (whichever
// is logged in) or takes a URL; with one, it checks that git can reach it,
// offers the ssh address when https has no login, and pushes what isn't
// there yet.
package remote

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/gitx"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/save"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// IO is how Run talks to a person. Ask shows a question and returns the
// trimmed answer; Log prints one line, already styled for the terminal.
type IO struct {
	Ask func(question string) string
	Log func(line string)
	// Interactive is false without a terminal: then nothing is asked, only
	// explained.
	Interactive bool
}

// Forge is a hosting service homebot can create a repository on.
type Forge struct {
	Name string // GitHub or GitLab
	CLI  string // gh or glab
	Key  string // the letter that picks it
}

// Forges are the services homebot knows, in the order they are offered.
var Forges = []Forge{
	{Name: "GitHub", CLI: "gh", Key: "g"},
	{Name: "GitLab", CLI: "glab", Key: "l"},
}

// Reach is whether git can talk to origin, and what git said when it cannot.
type Reach struct {
	OK    bool
	Error string
}

// Options lets a test replace the one check that needs a network.
type Options struct {
	// Reach replaces ReachOrigin.
	Reach func(c *app.Context) Reach
}

// Address is the two ways to clone a repository a forge created.
type Address struct{ SSH, HTTPS string }

// installedForges are the forges whose command line tool is on PATH.
func installedForges(c *app.Context) []Forge {
	var out []Forge
	for _, f := range Forges {
		if proc.HasCommand(c, f.CLI) {
			out = append(out, f)
		}
	}
	return out
}

func loggedIn(c *app.Context, f Forge) bool {
	return proc.Run(c, []string{f.CLI, "auth", "status"}, proc.Options{}).Code == 0
}

// HasSSHKey reports whether ~/.ssh holds a private key git can push with.
func HasSSHKey(home string) bool {
	for _, k := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		if _, err := os.Stat(filepath.Join(home, ".ssh", k)); err == nil {
			return true
		}
	}
	return false
}

var httpsURL = regexp.MustCompile(`^https://(?:[^@/]+@)?([^/:]+)/(.+?)(?:\.git)?/?$`)

// SSHAddress is https://host/owner/repo(.git) as git@host:owner/repo.git, and
// "" for anything else.
func SSHAddress(url string) string {
	m := httpsURL.FindStringSubmatch(url)
	if m == nil {
		return ""
	}
	return "git@" + m[1] + ":" + m[2] + ".git"
}

// DefaultName is the name offered for a new repository: the repo folder's,
// without its dot.
func DefaultName(root string) string {
	base := filepath.Base(root)
	if base == "/" || base == "." {
		base = ""
	}
	if name := strings.TrimLeft(base, "."); name != "" {
		return name
	}
	return "dotfiles"
}

// CreateRepository makes a private repository under the logged-in account.
func CreateRepository(c *app.Context, f Forge, name string) (Address, error) {
	description := voice.Record(c, msg.RemoteRepoDescription())
	argv := []string{"gh", "api", "-X", "POST", "user/repos", "-f", "name=" + name, "-F", "private=true", "-f", "description=" + description}
	if f.CLI == "glab" {
		argv = []string{"glab", "api", "-X", "POST", "projects", "-f", "name=" + name, "-f", "visibility=private", "-f", "description=" + description}
	}
	r := proc.Run(c, argv, proc.Options{})
	if r.Code != 0 {
		said := r.Stderr
		if said == "" {
			said = r.Stdout
		}
		return Address{}, fail.User(fail.Failure(voice.Plain(c, msg.RemoteErrorCreate(f.CLI, name)), said, f.CLI))
	}
	var body map[string]any
	if json.Unmarshal([]byte(r.Stdout), &body) != nil {
		body = nil
	}
	ssh, ok1 := firstOf(body, "ssh_url", "ssh_url_to_repo").(string)
	https, ok2 := firstOf(body, "clone_url", "http_url_to_repo").(string)
	if !ok1 || !ok2 {
		return Address{}, fail.User(voice.Plain(c, msg.RemoteErrorNoAddress(f.CLI, name)))
	}
	return Address{SSH: ssh, HTTPS: https}, nil
}

// firstOf is body[a], or body[b] when a is missing or null: JavaScript's ??.
func firstOf(body map[string]any, a, b string) any {
	if v := body[a]; v != nil {
		return v
	}
	return body[b]
}

// ReachOrigin asks whether git can talk to origin. It never stops to ask for
// an https username.
func ReachOrigin(c *app.Context) Reach {
	r := gitx.RunNet(c, c.Root, proc.Options{}, "ls-remote", "--heads", "origin")
	return Reach{OK: r.Code == 0, Error: strings.TrimSpace(r.Stderr)}
}

// noLogin matches the errors https gives when nobody is logged in, as opposed
// to a typo or no network.
var noLogin = regexp.MustCompile(`(?i)could not read Username|terminal prompts disabled|Authentication failed|returned error: 40[13]`)

// hasOrigin reports whether a remote named origin exists.
func hasOrigin(c *app.Context) bool {
	return gitx.Run(c, c.Root, "remote", "get-url", "origin").Code == 0
}

// originURL is origin's URL as the config holds it, before any insteadOf
// rewriting.
func originURL(c *app.Context) string {
	return strings.TrimSpace(gitx.Run(c, c.Root, "config", "--get", "remote.origin.url").Stdout)
}

func setOrigin(c *app.Context, url string) error {
	verb := "add"
	if hasOrigin(c) {
		verb = "set-url"
	}
	if r := gitx.Run(c, c.Root, "remote", verb, "origin", url); r.Code != 0 {
		return fail.User(fail.Failure(voice.Plain(c, msg.RemoteErrorSetURL(url)), r.Stderr, ""))
	}
	return nil
}

// run is one Run: the Context, where to talk, and the test options.
type run struct {
	c    *app.Context
	out  IO
	opts Options
}

// Run is `homebot remote [url]`.
func Run(c *app.Context, url string, out IO, opts Options) error {
	r := run{c: c, out: out, opts: opts}
	if url != "" {
		if err := setOrigin(c, url); err != nil {
			return err
		}
		r.log(r.lead(r.st().OK, msg.RemoteSuccessRemote(url)))
		return r.checkAndPush()
	}
	if hasOrigin(c) {
		return r.checkAndPush()
	}
	var others []string
	for _, n := range strings.Split(strings.TrimSpace(gitx.Run(c, c.Root, "remote").Stdout), "\n") {
		if n != "" {
			others = append(others, n)
		}
	}
	if len(others) > 0 {
		r.say(msg.RemoteHintRename(strings.Join(others, ", ")))
		r.command(msg.RemoteHintRenameCmd(ui.Tildify(c.Root, c.Home), others[0]))
		return nil
	}
	return r.setUp()
}

func (r run) st() ui.Palette  { return ui.Styles(r.c) }
func (r run) log(line string) { r.out.Log(line) }

// say logs t in the voice.
func (r run) say(t msg.Text) { r.log(voice.Render(r.c, t)) }

// styled logs t in style s.
func (r run) styled(s ui.Style, t msg.Text) { r.log(s.Render(voice.Render(r.c, t))) }

// command logs t, a command to run, styled as one.
func (r run) command(t msg.Text) {
	r.log(voice.Command(r.c, t))
}

func (r run) ask(t msg.Text) string { return strings.ToLower(r.out.Ask(voice.Plain(r.c, t))) }

// lead is t with its first word in style s, as "remote /x" and "pushed ..." are.
func (r run) lead(s ui.Style, t msg.Text) string {
	plain, rendered := voice.Plain(r.c, t), voice.Render(r.c, t)
	i := strings.IndexByte(plain, ' ')
	if i < 0 {
		return s.Render(rendered)
	}
	return s.Render(plain[:i]) + ansi.TruncateLeft(rendered, ansi.StringWidth(plain[:i]), "")
}

func (r run) setUp() error {
	c, st := r.c, r.st()
	installed := installedForges(c)
	var ready []Forge
	for _, f := range installed {
		if loggedIn(c, f) {
			ready = append(ready, f)
		}
	}
	r.styled(st.Accent, msg.RemoteHeadingNone())
	if !r.out.Interactive {
		if len(ready) > 0 {
			names := make([]string, len(ready))
			for i, f := range ready {
				names[i] = f.Name
			}
			r.say(msg.RemoteHintCreateInTerminal(strings.Join(names, " or ")))
			r.command(msg.RemoteHintCreateCmd())
			r.say(msg.RemoteHintOrExisting())
		} else {
			r.say(msg.RemoteHintExisting())
		}
		r.command(msg.RemoteHintURLCmd())
		return nil
	}
	r.log("")
	for _, f := range ready {
		r.say(msg.RemoteChoiceForge(f.Key, f.Name, f.CLI))
	}
	r.say(msg.RemoteChoiceURL())
	r.say(msg.RemoteChoiceLater())
	for _, f := range installed {
		if !containsForge(ready, f) {
			r.styled(st.Muted, msg.RemoteHintLogin(f.CLI, f.Name))
		}
	}
	for {
		key := r.ask(msg.RemotePromptChoice())
		if key == "" {
			r.say(msg.RemoteStatusNotNow())
			r.command(msg.RemoteHintCreateCmd())
			return nil
		}
		if key == "u" {
			url := r.out.Ask(voice.Plain(c, msg.RemotePromptURL()))
			if url == "" {
				continue
			}
			if err := setOrigin(c, url); err != nil {
				return err
			}
			r.log(r.lead(st.OK, msg.RemoteSuccessRemote(url)))
			return r.checkAndPush()
		}
		i := indexOfKey(ready, key)
		if i < 0 {
			r.styled(st.Muted, msg.RemoteHintPick())
			continue
		}
		forge := ready[i]
		suggested := DefaultName(c.Root)
		name := r.out.Ask(voice.Plain(c, msg.RemotePromptName(forge.Name, suggested)))
		if name == "" {
			name = suggested
		}
		if !validName.MatchString(name) {
			r.styled(st.Error, msg.RemoteErrorBadName(name))
			continue
		}
		address, err := CreateRepository(c, forge, name)
		if err != nil {
			return err
		}
		url := address.HTTPS
		if HasSSHKey(c.Home) {
			url = address.SSH
		}
		if err := setOrigin(c, url); err != nil {
			return err
		}
		r.log(r.lead(st.OK, msg.RemoteSuccessCreated(name, forge.Name)))
		r.log(r.lead(st.OK, msg.RemoteSuccessRemote(url)))
		if url == address.HTTPS {
			r.styled(st.Muted, msg.RemoteHintHTTPS(forge.CLI))
			r.command(msg.RemoteHintHTTPSCmd(forge.CLI))
		}
		return r.checkAndPush()
	}
}

var validName = regexp.MustCompile(`^[\w.-]+$`)

func containsForge(fs []Forge, f Forge) bool {
	for _, x := range fs {
		if x == f {
			return true
		}
	}
	return false
}

func indexOfKey(fs []Forge, key string) int {
	for i, f := range fs {
		if f.Key == key {
			return i
		}
	}
	return -1
}

// httpsLogin is the command that logs git in over https for url's host, when
// one is known.
func httpsLogin(url string) (msg.Text, bool) {
	var host string
	if m := httpsURL.FindStringSubmatch(url); m != nil {
		host = m[1]
	}
	switch {
	case host == "github.com":
		return msg.RemoteLoginGh(), true
	case strings.Contains(host, "gitlab"):
		return msg.RemoteLoginGlab(), true
	}
	return msg.Text{}, false
}

func (r run) unreachable(headline msg.Text, url, said string) {
	st := r.st()
	r.styled(st.Error, headline)
	r.say(msg.RemoteStatusURL(url))
	for _, line := range fail.ToolSaid(said, 2) {
		r.styled(st.Muted, msg.RemoteStatusGitSaid(line))
	}
}

func (r run) reach() Reach {
	if r.opts.Reach != nil {
		return r.opts.Reach(r.c)
	}
	return ReachOrigin(r.c)
}

func (r run) checkAndPush() error {
	c, st := r.c, r.st()
	url := originURL(c)
	if reach := r.reach(); !reach.OK {
		ssh := SSHAddress(url)
		login := noLogin.MatchString(reach.Error)
		headline := msg.RemoteErrorUnreachable()
		if login {
			headline = msg.RemoteErrorUnreachableLogin()
		}
		r.unreachable(headline, url, reach.Error)
		if ssh == "" || !login {
			return nil
		}
		r.log("")
		if !HasSSHKey(c.Home) {
			if cmd, ok := httpsLogin(url); ok {
				r.say(msg.RemoteHintLogIn())
				r.command(cmd)
				r.say(msg.RemoteHintOrSSH())
			} else {
				r.say(msg.RemoteHintLogInOrSSH())
			}
			r.command(msg.RemoteHintSSHCmd(ssh))
			return nil
		}
		r.say(msg.RemoteHintHaveKey())
		r.say(msg.RemoteStatusSSHURL(ssh))
		if !r.out.Interactive {
			r.say(msg.RemoteHintToSwitch())
			r.command(msg.RemoteHintSSHCmd(ssh))
			return nil
		}
		if a := r.ask(msg.RemotePromptSwitch()); a != "" && a != "y" && a != "yes" {
			r.say(msg.RemoteStatusKeptHTTPS())
			r.command(msg.RemoteHintSSHCmd(ssh))
			return nil
		}
		if err := setOrigin(c, ssh); err != nil {
			return err
		}
		if again := r.reach(); !again.OK {
			r.unreachable(msg.RemoteErrorUnreachableSSH(), ssh, again.Error)
			return nil
		}
		r.log(r.lead(st.OK, msg.RemoteSuccessRemote(ssh)))
	}
	r.log(r.lead(st.OK, msg.RemoteSuccessReachable(originURL(c))))
	if !gitx.HasCommits(c, c.Root) {
		r.say(msg.RemoteHintFirstSave())
		r.command(msg.RemoteHintSaveCmd())
		return nil
	}
	s, err := save.Sync(c, save.SyncOptions{Push: true})
	if err != nil {
		return err
	}
	if s.Pulled > 0 {
		r.log(r.lead(st.OK, msg.SaveSuccessPulled(s.Pulled)))
	}
	switch {
	case s.Waiting:
		r.styled(st.Todo, msg.RemoteWarningWaiting())
		r.command(msg.RemoteHintSaveCmd())
	case s.Pushed > 0:
		r.log(r.lead(st.OK, msg.RemoteSuccessPushed()))
	default:
		r.say(msg.RemoteStatusUpToDate())
	}
	return nil
}
