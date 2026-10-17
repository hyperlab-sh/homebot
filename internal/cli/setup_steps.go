package cli

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/gitx"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/remote"
	"github.com/hyperlab-sh/homebot/internal/shell"
	"github.com/hyperlab-sh/homebot/internal/triage"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// setupZshLine is what setup appends to .zshrc: the line the shell hint
// shows, without its indent. It names the stable homebot binary, not the
// effective name, as the shell integration does.
func setupZshLine(c *app.Context) string {
	return strings.TrimSpace(voice.Plain(c, msg.SetupHintZshrcCmd()))
}

// setupShell sees to the shell integration. A login shell other than zsh
// gets one line saying it is zsh-only. Otherwise, when no zsh startup file
// loads it yet, setup shows the line to add and, when the login shell is
// zsh and a person is there, offers to append it to .zshrc.
func setupShell(c *app.Context) error {
	st := ui.Styles(c)
	name := filepath.Base(c.Env.Get("SHELL"))
	if name != "." && name != "/" && name != "zsh" {
		ui.Println(c, st.Muted.Render(voice.Render(c, msg.SetupStatusZshOnly(name))))
		return nil
	}
	if shell.Integrated(c) {
		return nil
	}
	ui.Println(c, "")
	ui.Println(c, voice.Render(c, msg.SetupHintZshrc()))
	ui.Println(c, st.Command.Render(voice.Plain(c, msg.SetupHintZshrcCmd())))
	if name != "zsh" || !ui.Interactive(c) {
		return nil
	}
	file := filepath.Join(c.Home, ".zshrc")
	if dir := c.Env.Get("ZDOTDIR"); dir != "" {
		file = filepath.Join(dir, ".zshrc")
	}
	shown := ui.Tildify(file, c.Home)
	rel, link, ok := setupZshrcWritable(c, file)
	if !ok {
		if link != "" {
			ui.Println(c, voice.Render(c, msg.SetupStatusZshrcLinked(shown, setupTildify(c, link))))
		}
		return nil
	}
	if !setupYes(c, msg.SetupPromptZshrc(shown)) {
		return nil
	}
	if err := setupAppendLine(file, setupZshLine(c)); err != nil {
		return err
	}
	ui.Println(c, secretsStyleLead(c, st.OK, msg.SetupSuccessZshrc(shown), " "))
	if rel != "" {
		ui.Println(c, voice.Render(c, msg.SetupStatusZshrcInRepo(rel)))
	}
	return nil
}

// setupZshrcWritable says whether setup may append to the .zshrc at file:
// when there is none, when it is a regular file, or when it is a link that
// resolves to a regular file inside the home repo, whose path in the repo
// is rel. Any other link (out of the repo, into an old dotfiles folder,
// dangling, a loop) is never written through: link is where it points, for
// the person to edit. Anything else there, a folder say, is left alone
// with nothing to add.
func setupZshrcWritable(c *app.Context, file string) (rel, link string, ok bool) {
	info, err := os.Lstat(file)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", "", true
	case err != nil:
		return "", "", false
	case info.Mode().IsRegular():
		return "", "", true
	case info.Mode()&fs.ModeSymlink == 0:
		return "", "", false
	}
	real, err := filepath.EvalSymlinks(file)
	if err != nil {
		raw, rerr := os.Readlink(file)
		if rerr != nil {
			return "", file, false
		}
		if !filepath.IsAbs(raw) {
			raw = filepath.Join(filepath.Dir(file), raw)
		}
		return "", filepath.Clean(raw), false
	}
	if st, err := os.Stat(real); err != nil || !st.Mode().IsRegular() {
		return "", real, false
	}
	rel, inside := setupInRepo(c, real)
	if !inside {
		return "", real, false
	}
	return rel, "", true
}

// setupTildify writes path as ~/... when it is under the home folder, even
// when path is a resolved one (symlinks followed) and c.Home is not: on
// macOS a home under /var is /private/var once resolved.
func setupTildify(c *app.Context, path string) string {
	if shown := ui.Tildify(path, c.Home); shown != path {
		return shown
	}
	if real, err := filepath.EvalSymlinks(c.Home); err == nil && real != c.Home {
		return ui.Tildify(path, real)
	}
	return path
}

// setupInRepo is path relative to the home repo, when it is inside it.
func setupInRepo(c *app.Context, path string) (string, bool) {
	roots := []string{c.Root}
	if real, err := filepath.EvalSymlinks(c.Root); err == nil && real != c.Root {
		roots = append(roots, real)
	}
	for _, root := range roots {
		if path != root && app.IsInside(path, root) {
			return strings.TrimPrefix(path, root+"/"), true
		}
	}
	return "", false
}

// setupAppendLine appends line to the file at path, through a symlink
// setupZshrcWritable allowed, creating the file when there is none. A file that does not end in a
// newline gets one first, so the line stays its own.
func setupAppendLine(path, line string) error {
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	add := line + "\n"
	if len(old) > 0 && !bytes.HasSuffix(old, []byte("\n")) {
		add = "\n" + add
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(add); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// setupTriage offers to sort what triage would ask about, in a new repo.
func setupTriage(c *app.Context) error {
	items, err := triage.Candidates(c)
	if err != nil || len(items) == 0 {
		return err
	}
	if !setupYes(c, msg.SetupPromptTriage(len(items))) {
		setupLater(c, setupCommand(c, msg.SetupHintTriageCmd()))
		return nil
	}
	return triage.Run(c)
}

// setupRemote runs homebot remote's questions when the repo has no remote
// at all. A repo with one, origin or another, is left as it is.
func setupRemote(c *app.Context) error {
	if strings.TrimSpace(gitx.Run(c, c.Root, "remote").Stdout) != "" {
		return nil
	}
	ui.Println(c, "")
	return remote.Run(c, "", remote.IO{
		Ask:         func(q string) string { return ui.Ask(c, q) },
		Log:         func(line string) { ui.Println(c, line) },
		Interactive: true,
	}, remote.Options{})
}

// setupSave offers homebot save when the repo has uncommitted changes:
// before the first commit, anything in it; after, changed tracked files.
// setupRun calls it only when those changes are setup's own: in a repo it
// made or cloned, or one that had none before it ran. It pushes only when
// there is a remote. Without a name and email git
// would refuse the commit, so it is not offered; the identity hint above
// already said how to set them.
func setupSave(c *app.Context) error {
	if who := gitx.Identity(c, c.Root); who.Name == "" || who.Email == "" {
		return nil
	}
	if !setupHasChanges(c) {
		return nil
	}
	question := msg.SetupPromptSaveLocal()
	if strings.TrimSpace(gitx.Run(c, c.Root, "remote").Stdout) != "" {
		question = msg.SetupPromptSave()
	}
	ui.Println(c, "")
	if !setupYes(c, question) {
		setupLater(c, setupCommand(c, msg.RemoteHintSaveCmd()))
		return nil
	}
	return runSave(c, saveFlags{})
}

// setupHasChanges says whether save has something to commit.
func setupHasChanges(c *app.Context) bool {
	if !gitx.HasCommits(c, c.Root) {
		return strings.TrimSpace(gitx.Run(c, c.Root, "status", "--porcelain").Stdout) != ""
	}
	s, err := gitx.Status(c, c.Root)
	return err == nil && len(s.Changed) > 0
}
