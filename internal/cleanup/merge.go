package cleanup

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// Picker lets the user pick lines in dir, a scratch git repository: HEAD
// holds the repo file, the working tree holds the backup, and whatever is
// staged when the picker returns becomes the repo file. file is relative to
// dir.
type Picker func(dir, file string) error

// pickerCommand is lazygit when it is installed (line by line), else
// git add -p (block by block).
func pickerCommand(c *app.Context, file string) []string {
	if proc.HasCommand(c, "lazygit") {
		return []string{"lazygit"}
	}
	return []string{"git", "add", "-p", "--", file}
}

// hasLazygit reports whether merges open lazygit.
func hasLazygit(c *app.Context) bool { return pickerCommand(c, "")[0] == "lazygit" }

// TerminalPicker runs lazygit or git add -p on the terminal, after a line
// saying which keys do what, with git pointed at the scratch repo only. How
// the tool exits does not matter: what is staged counts.
func TerminalPicker(c *app.Context) Picker {
	return func(dir, file string) error {
		argv := pickerCommand(c, file)
		keys := msg.CleanupPickerGit()
		if argv[0] == "lazygit" {
			keys = msg.CleanupPickerLazygit()
		}
		ui.Println(c, ui.Styles(c).Muted.Render(voice.Render(c, keys)))
		in, out, errOut := ui.Attached(c)
		_, err := runTool(c, scratchEnv(c.Env, dir), dir, argv, in, out, errOut)
		return err
	}
}

// repoGitVars are the variables that point git at a repository, or at
// settings that beat the scratch repo's own: each would send what the merge
// does (its git config writes among it) somewhere else, or change what it
// reads. GIT_CONFIG_KEY_n and GIT_CONFIG_VALUE_n go too; see scratchEnv.
var repoGitVars = map[string]bool{
	"GIT_DIR":                          true,
	"GIT_WORK_TREE":                    true,
	"GIT_INDEX_FILE":                   true,
	"GIT_COMMON_DIR":                   true,
	"GIT_OBJECT_DIRECTORY":             true,
	"GIT_ALTERNATE_OBJECT_DIRECTORIES": true,
	"GIT_NAMESPACE":                    true,
	"GIT_CEILING_DIRECTORIES":          true,
	"GIT_TEMPLATE_DIR":                 true,
	"GIT_DISCOVERY_ACROSS_FILESYSTEM":  true,
	"GIT_PREFIX":                       true,
	"GIT_SHALLOW_FILE":                 true,
	"GIT_REPLACE_REF_BASE":             true,
	"GIT_NO_REPLACE_OBJECTS":           true,
	"GIT_ATTR_SOURCE":                  true,
	"GIT_CONFIG":                       true,
	"GIT_CONFIG_PARAMETERS":            true,
	"GIT_CONFIG_COUNT":                 true,
}

// scratchEnv is env with git pointed at the scratch repo in dir and nowhere
// else: every repoGitVars variable and GIT_CONFIG_KEY_n/GIT_CONFIG_VALUE_n
// pair the user exported is dropped, then GIT_DIR, GIT_WORK_TREE and
// GIT_INDEX_FILE are set to the scratch repo. The global config
// (GIT_CONFIG_GLOBAL) stays; the scratch repo's local settings beat it.
func scratchEnv(env app.Env, dir string) app.Env {
	var drop []string
	for _, kv := range env.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if repoGitVars[k] || strings.HasPrefix(k, "GIT_CONFIG_KEY_") || strings.HasPrefix(k, "GIT_CONFIG_VALUE_") {
			drop = append(drop, k)
		}
	}
	gitDir := filepath.Join(dir, ".git")
	return env.Without(drop...).With(map[string]string{
		"GIT_DIR":        gitDir,
		"GIT_WORK_TREE":  dir,
		"GIT_INDEX_FILE": filepath.Join(gitDir, "index"),
		// core.attributesFile=/dev/null replaces the user's attributes file
		// only; this stops git reading the system one ($(prefix)/etc/gitattributes).
		"GIT_ATTR_NOSYSTEM": "1",
	})
}

// scratchPrefix starts the name of every scratch repo in the cache folder.
const scratchPrefix = "cleanup-merge-"

// scratchMaxAge is how old a scratch repo left behind (by a kill mid-merge)
// must be before the next merge removes it.
const scratchMaxAge = 24 * time.Hour

// sweepScratch removes scratch repos older than scratchMaxAge; a merge
// running elsewhere right now is never that old.
func sweepScratch(c *app.Context) {
	old, _ := filepath.Glob(filepath.Join(c.CacheDir, scratchPrefix+"*"))
	for _, dir := range old {
		if st, err := os.Lstat(dir); err == nil && st.IsDir() && c.Now().Sub(st.ModTime()) > scratchMaxAge {
			_ = os.RemoveAll(dir)
		}
	}
}

// mergeBackup takes the lines the user picks from the backup into the repo
// file. The backup stays; Review asks about it again, now with what is left.
// took is false when nothing was taken. The scratch repo lives under the
// cache folder and is removed afterwards, also when the picker fails. Every
// git call in it, and the picker, sees only the scratch repo, and its local
// settings beat the user's: no signing, hooks, line-ending or attribute
// conversion, or ignore rules.
func mergeBackup(c *app.Context, b Backup, pick Picker) (done msg.Text, took bool, err error) {
	dest := repoFileFor(c, b)
	if dest == "" {
		return msg.Text{}, false, fail.User(voice.Plain(c, msg.CleanupErrorMergeNotRepo(ui.Tildify(b.Target, c.Home))))
	}
	if err := os.MkdirAll(c.CacheDir, 0o755); err != nil {
		return msg.Text{}, false, err
	}
	sweepScratch(c)
	dir, err := os.MkdirTemp(c.CacheDir, scratchPrefix)
	if err != nil {
		return msg.Text{}, false, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	// A copy of c whose environment sees only the scratch repo: proc.Run
	// can set variables over c's but not unset them.
	scratch := c.WithContext(c.Context())
	scratch.Env = scratchEnv(c.Env, dir)
	git := func(args ...string) (string, error) {
		r := proc.Run(scratch, append([]string{"git"}, args...), proc.Options{Dir: dir})
		if r.Code != 0 {
			return "", fail.User(fail.Failure(voice.Plain(c, msg.CleanupErrorScratchGit(args[0])), r.Stderr, ""))
		}
		return r.Stdout, nil
	}
	// No template: one with info/attributes would beat every setting below.
	// Local settings beat the user's: a fixed author in case the picker
	// commits, no signing, no hooks or fsmonitor, the bytes as they are,
	// nothing ignored.
	if _, err := git("init", "-q", "--template="); err != nil {
		return msg.Text{}, false, err
	}
	name, email := scratchAuthor(c)
	for _, kv := range [][2]string{
		{"user.name", name},
		{"user.email", email},
		{"commit.gpgsign", "false"},
		{"core.hooksPath", "/dev/null"},
		{"core.fsmonitor", "false"},
		{"core.autocrlf", "false"},
		{"core.excludesFile", "/dev/null"},
		{"core.attributesFile", "/dev/null"},
	} {
		if _, err := git("config", kv[0], kv[1]); err != nil {
			return msg.Text{}, false, err
		}
	}
	file := scratchName(filepath.Base(b.Target))
	before, err := os.ReadFile(dest)
	if err != nil {
		return msg.Text{}, false, err
	}
	if err := os.WriteFile(filepath.Join(dir, file), before, 0o644); err != nil {
		return msg.Text{}, false, err
	}
	if _, err := git("add", "-f", "--", file); err != nil {
		return msg.Text{}, false, err
	}
	if _, err := git("commit", "-q", "-m", voice.Record(c, msg.CleanupScratchCommit())); err != nil {
		return msg.Text{}, false, err
	}
	if err := copyFile(b.Path, filepath.Join(dir, file)); err != nil {
		return msg.Text{}, false, err
	}
	if err := pick(dir, file); err != nil {
		return msg.Text{}, false, err
	}
	after, err := git("show", ":"+file)
	if err != nil {
		return msg.Text{}, false, err
	}
	if after == string(before) {
		return msg.Text{}, false, nil
	}
	if err := os.WriteFile(dest, []byte(after), 0o644); err != nil {
		return msg.Text{}, false, err
	}
	return msg.CleanupSuccessMerged(repoRel(c, dest)), true, nil
}

// scratchSpecial are the names git gives meaning to in a repository's root.
var scratchSpecial = []string{".git", ".gitattributes", ".gitignore", ".gitmodules", ".mailmap"}

// scratchName is the name the scratch copy of a file called name has in the
// scratch repo. Git gives some names meaning inside a repository: a
// .gitattributes there would apply to the scratch repo itself (a CRLF
// `* text eol=lf` one would be stored as LF), .gitignore, .gitmodules and
// .mailmap are read as settings, and a file called .git is the repository.
// Those lose their leading dot (gitattributes, mailmap, ...), which git gives
// no meaning and the picker still shows recognizably, as the repo stores them.
// The others keep their name. Names compare case-insensitively
// (strings.EqualFold): the cache folder may be on a volume that folds case,
// where .GitAttributes is .gitattributes.
func scratchName(name string) string {
	for _, special := range scratchSpecial {
		if strings.EqualFold(name, special) {
			return strings.TrimPrefix(name, ".")
		}
	}
	return name
}

// scratchAuthor splits cleanup.scratch.author, "name <email>".
func scratchAuthor(c *app.Context) (name, email string) {
	author := voice.Record(c, msg.CleanupScratchAuthor())
	name, email, _ = strings.Cut(author, " <")
	return name, strings.TrimSuffix(email, ">")
}
