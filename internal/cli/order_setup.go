package cli

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/gitx"
	"github.com/hyperlab-sh/homebot/internal/guard"
	"github.com/hyperlab-sh/homebot/internal/helper"
	"github.com/hyperlab-sh/homebot/internal/linker"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/secrets"
	"github.com/hyperlab-sh/homebot/internal/selfname"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func init() { register(KeepingOrder, "setup", newSetupCmd) }

// setupMinGit is the oldest git setup accepts: the guard needs `git
// cat-file --batch-check -Z`, which arrived in 2.40.
const (
	setupMinGitMajor = 2
	setupMinGitMinor = 40
	setupMinGit      = "2.40"
)

func newSetupCmd(c *app.Context) *cobra.Command {
	var remove bool
	var name, persona string
	cmd := &cobra.Command{
		Use:  "setup",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return setupRun(c, remove, setupOptions{
				name: name, persona: persona,
				hasName: cmd.Flags().Changed("name"), hasPersona: cmd.Flags().Changed("persona"),
			})
		},
	}
	cmd.Flags().BoolVar(&remove, "remove", false, "")
	cmd.Flags().StringVar(&name, "name", "", "")
	cmd.Flags().StringVar(&persona, "persona", "", "")
	return cmd
}

type setupOptions struct {
	name, persona       string
	hasName, hasPersona bool
}

func (o setupOptions) validate(c *app.Context, remove bool) error {
	if remove && (o.hasName || o.hasPersona) {
		return fail.User(voice.Plain(c, msg.SetupErrorRemoveFlags()))
	}
	if o.hasName && !config.ValidName(o.name) {
		return fail.User(voice.Plain(c, msg.SetupErrorNameSyntax(o.name)))
	}
	if o.hasPersona && !config.ValidPersonaID(o.persona) {
		return fail.User(voice.Plain(c, msg.SetupErrorPersonaSyntax(o.persona)))
	}
	// The spelling is right; the persona must also exist and load, as persona
	// use requires, or the next refresh would call the repo's persona broken.
	// Checked before anything is installed.
	if o.hasPersona {
		return personaUsable(c, o.persona)
	}
	return nil
}

// setupRun takes the person from nothing to an empty inbox: it checks the
// tools, finds, clones or starts the home repo, installs the commit guard
// and the background helper, and offers the shell line, triage, a remote
// and a commit of what it changed. Steps already done are skipped, so
// running it again is safe. It ends on the inbox, which lists what is
// left. Without a terminal it asks nothing and creates no repo. --remove
// takes the helper away and leaves the rest, with or without a repo.
func setupRun(c *app.Context, remove bool, opts setupOptions) error {
	if err := opts.validate(c, remove); err != nil {
		return err
	}
	if remove {
		// The helper's files are in $HOME, not the repo: --remove works
		// without one, say after the repo was deleted.
		nameErr := selfname.Unlink(c)
		helperErr := helper.Uninstall(c)
		if err := errors.Join(nameErr, helperErr); err != nil {
			return err
		}
		ui.Println(c, voice.Render(c, msg.SetupStatusRemoved()))
		return nil
	}
	if err := setupCheckGit(c); err != nil {
		return err
	}
	// Before anything is installed: a relative XDG_* would give the helper
	// other folders than the shell's.
	if err := helper.CheckEnv(c); err != nil {
		return err
	}
	if err := setupCheckTools(c); err != nil {
		return err
	}
	repo, err := setupFindRepo(c, opts)
	if err != nil || repo == setupNone {
		return err
	}
	// Changes in a repo that was already here are the person's own work:
	// setup never offers to commit them. In a repo it just made or cloned,
	// every change is setup's.
	ownChanges := repo != setupFound || !gitx.IsRepo(c, c.Root) || !setupHasChanges(c)
	naming, err := setupChooseNaming(c, opts)
	if err != nil {
		return err
	}
	// Earlier setup messages may have cached homebot as the effective name.
	// A new Context observes the link we just made for all later output.
	c = c.Fork(nil)
	c, pathAdded, err := setupNamePath(c, naming.name)
	if err != nil {
		return err
	}
	st := ui.Styles(c)
	if gitx.IsRepo(c, c.Root) {
		changed, err := guard.InstallHooks(c)
		if err != nil {
			return err
		}
		if changed {
			ui.Println(c, secretsStyleLead(c, st.OK, msg.SetupSuccessGuard(), " "))
		} else {
			ui.Println(c, voice.Render(c, msg.SetupStatusGuardPresent()))
		}
	} else {
		ui.Println(c, st.Todo.Render(voice.Render(c, msg.SetupWarningNoRepo(ui.Tildify(c.Root, c.Home)))))
	}
	if err := setupEnsureIdentity(c); err != nil {
		return err
	}
	if err := helper.Install(c); err != nil {
		return err
	}
	what := msg.HelperDescriptionSystemd()
	if c.OS != app.Linux {
		what = msg.HelperDescriptionLaunchd()
	}
	ui.Println(c, secretsStyleLead(c, st.OK, msg.SetupSuccessHelper(voice.Plain(c, what)), " "))
	// The helper keeps the XDG_* folders setup saw (final review home I2).
	ui.Println(c, st.Muted.Render(voice.Render(c, msg.SetupHintXdg())))
	// A new repo was just offered the keys; one found or cloned gets the hint.
	if repo != setupCreated && !secrets.KeysExist(c) {
		ui.Println(c, "")
		ui.Println(c, voice.Render(c, msg.SetupHintKeys()))
		ui.Println(c, st.Command.Render(voice.Plain(c, msg.SetupHintKeysCmd())))
	}
	if err := setupShell(c); err != nil {
		return err
	}
	if naming.linkMade || pathAdded || naming.blocked != "" {
		ui.Println(c, "")
		ui.Println(c, voice.Render(c, msg.SetupStatusChangedHere()))
		if naming.linkMade {
			ui.Println(c, voice.Render(c, msg.SetupStatusNameLink(naming.name)))
		}
		if pathAdded {
			ui.Println(c, voice.Render(c, msg.SetupStatusPathLine()))
		}
		if naming.blocked != "" {
			ui.Println(c, voice.Render(c, msg.SetupStatusNameBlocked(naming.blocked)))
		}
	}
	if naming.nameEdited && repo == setupFound && !ownChanges {
		ui.Println(c, voice.Render(c, msg.SetupStatusNameUncommitted()))
	}
	if naming.name != "" && helper.ShellWillFindLocalBin(c) {
		ui.Println(c, voice.Render(c, msg.SetupStatusOpenShell(naming.name)))
	}
	if ui.Interactive(c) && gitx.IsRepo(c, c.Root) {
		if repo == setupCreated {
			if err := setupTriage(c); err != nil {
				return err
			}
		}
		if err := setupRemote(c); err != nil {
			return err
		}
		if ownChanges {
			if err := setupSave(c); err != nil {
				return err
			}
		}
	}
	// What is left, terminal or not: the steps setup did not do are inbox
	// items, each with its command.
	ui.Println(c, "")
	return runInbox(c, false)
}

var setupGitVersion = regexp.MustCompile(`^git version ((\d+)\.(\d+)\S*)`)

// setupParseGitVersion reads the version from `git --version`, whose output
// can carry a vendor suffix ("git version 2.39.5 (Apple Git-154)") or extra
// components ("git version 2.43.0.windows.1"). version is the first word
// after "git version"; ok is false for any other text.
func setupParseGitVersion(out string) (version string, major, minor int, ok bool) {
	m := setupGitVersion.FindStringSubmatch(strings.TrimSpace(out))
	if m == nil {
		return "", 0, 0, false
	}
	major, err1 := strconv.Atoi(m[2])
	minor, err2 := strconv.Atoi(m[3])
	return m[1], major, minor, err1 == nil && err2 == nil
}

// setupGitTooOld says whether a git of this version is older than setupMinGit.
func setupGitTooOld(major, minor int) bool {
	return major < setupMinGitMajor || (major == setupMinGitMajor && minor < setupMinGitMinor)
}

// setupCheckGit stops setup before it installs anything when git is too old
// for the guard: on an older git the guard fails closed and every save is
// refused. With brew on PATH (Apple's git on a Mac) it gives the command
// that installs a newer one, and leaves running it to the person. Output that is not a version (a failing git, a build with its own
// banner) is not judged here; the steps that use git report their own
// failures.
func setupCheckGit(c *app.Context) error {
	r := proc.Run(c, []string{"git", "--version"}, proc.Options{})
	if r.Code != 0 {
		return nil
	}
	version, major, minor, ok := setupParseGitVersion(r.Stdout)
	if !ok || !setupGitTooOld(major, minor) {
		return nil
	}
	if proc.HasCommand(c, "brew") {
		return fail.User(voice.Plain(c, msg.SetupErrorGitTooOldBrew(version, setupMinGit)))
	}
	return fail.User(voice.Plain(c, msg.SetupErrorGitTooOld(version, setupMinGit)))
}

// setupEnsureIdentity makes sure git knows who commits. It asks for the
// missing name and email on a terminal and otherwise says how to set them.
func setupEnsureIdentity(c *app.Context) error {
	who := gitx.Identity(c, c.Root)
	if who.Name != "" && who.Email != "" {
		return nil
	}
	st := ui.Styles(c)
	fix := func() string { return ui.Command(c, "  "+voice.Plain(c, msg.GitIdentityFix())) }
	ui.Println(c, st.Todo.Render(voice.Render(c, msg.IdentityWarningUnknown())))
	if !ui.Interactive(c) {
		ui.Println(c, voice.Render(c, msg.IdentityHintSetOnce()))
		ui.Println(c, fix())
		return nil
	}
	name, email := who.Name, who.Email
	if name == "" {
		name = ui.Ask(c, voice.Plain(c, msg.IdentityPromptName()))
	}
	if email == "" {
		email = ui.Ask(c, voice.Plain(c, msg.IdentityPromptEmail()))
	}
	if name == "" || email == "" {
		ui.Println(c, voice.Render(c, msg.IdentityStatusSkipped()))
		ui.Println(c, fix())
		return nil
	}
	// Into ~/.config/git/config, which git reads with or without a ~/.gitconfig.
	file := filepath.Join(c.Home, ".config", "git", "config")
	global := c.Env.Get("GIT_CONFIG_GLOBAL") != ""
	where := []string{"--file", file}
	scope := "--file " + file
	if global {
		where, scope = []string{"--global"}, "--global"
	} else if err := os.MkdirAll(filepath.Dir(file), 0o777); err != nil {
		return err
	}
	for _, kv := range [][2]string{{"user.name", name}, {"user.email", email}} {
		r := gitx.Run(c, c.Root, append(append([]string{"config"}, where...), kv[0], kv[1])...)
		if r.Code != 0 {
			summary := voice.Plain(c, msg.IdentityErrorConfigFailed(scope, kv[0], ui.ShellQuote(kv[1])))
			return fail.User(fail.Failure(summary, r.Stderr, ""))
		}
	}
	config := "~/.config/git/config"
	if global {
		config = voice.Plain(c, msg.IdentityWhereGlobal())
	}
	ui.Println(c, secretsStyleLead(c, st.OK, msg.IdentitySuccessSaved(name, email, config), " "))
	if global {
		return nil
	}
	if _, owned, err := linker.OwnsLink(c, file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	} else if !owned {
		ui.Println(c, voice.Render(c, msg.IdentityHintTrack()))
		ui.Println(c, voice.Command(c, msg.IdentityHintTrackCmd()))
	}
	return nil
}
