// Package guard is the pre-commit guard: it seals secrets, refuses plaintext
// private files, oversized files and anything gitleaks flags, and installs the
// shim that git runs before every commit.
//
// The guard's stderr is a wire format that package save parses; the exact
// bytes are pinned in testdata/guard/refusal-*.txt. The producer writes the
// guard.headline line bare, then each problem as a guard.bullet line, then
// each further line of that problem prefixed with two spaces (the problem's
// own lines already carry a two-space indent, so they land at four).
package guard

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/gitx"
	"github.com/hyperlab-sh/homebot/internal/layout"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/secrets"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// privateAllowed are the plain files private/ may commit, directly under it:
// secrets.PrivateDocs, the names private/.gitignore lets through. The keys
// are lower case: names compare without regard to case, as git compares
// them when core.ignorecase is on (macOS).
var privateAllowed = func() map[string]bool {
	m := map[string]bool{}
	for _, name := range secrets.PrivateDocs {
		m[strings.ToLower(name)] = true
	}
	return m
}()

// problem is one reason to refuse the commit: a first line, then any
// further lines, each already indented two spaces.
type problem struct {
	text string
	// commands says the further lines are commands to run, printed in the
	// command style.
	commands bool
}

// PreCommit runs the pre-commit checks and prints refusals to stderr.
// Returns the process exit code (0 ok, 1 refused).
//
// It checks the repo git is committing in, found from the working
// directory git runs hooks in, never the home HOMEBOT_ROOT or the usual
// lookup would pick. The guard records its verdict (state.blocked) and
// seals secrets only when that repo is the home homebot manages: the state
// directory is shared, and another home's records are not this repo's.
func PreCommit(c *app.Context) int {
	own, err := useCommitRepo(c)
	if err != nil {
		sayError(c, err)
		return 1
	}
	problems := check(c, own)
	if own {
		err = state.Update(c, func(s *state.State) {
			if len(problems) == 0 {
				s.Blocked = nil
				return
			}
			first, _, _ := strings.Cut(problems[0].text, "\n")
			s.Blocked = &state.Blocked{Reason: first, At: c.Now().UnixMilli()}
		})
	}
	if err != nil {
		// The TypeScript threw here, so git refused the commit: a guard
		// that cannot record its verdict does not let the commit through.
		sayError(c, err)
	}
	if len(problems) > 0 {
		printRefusal(c, problems)
	}
	if err != nil || len(problems) > 0 {
		return 1
	}
	return 0
}

// useCommitRepo points c.Root and c.Config at the repo being committed:
// the top of the work tree around c.Cwd, which must hold homebot.yml. It
// reports whether that repo is the home homebot locates (HOMEBOT_ROOT, the
// working directory, ~/.home), the one whose state it may write.
//
// Run by hand outside any work tree, it checks the located home, as the
// TypeScript did. Run by git (GIT_INDEX_FILE is set) it never falls back.
func useCommitRepo(c *app.Context) (bool, error) {
	located := ""
	if c.LocateRoot() == nil {
		located = c.Root
	}
	r := gitx.Run(c, c.Cwd, "rev-parse", "--show-toplevel")
	if r.Code != 0 {
		if c.Env.Get("GIT_INDEX_FILE") != "" {
			return false, gitFailed(r, "rev-parse --show-toplevel")
		}
		if located == "" {
			return false, c.LocateRoot()
		}
		return true, nil
	}
	top := realPath(strings.TrimSuffix(r.Stdout, "\n"))
	if root, ok := app.FindRoot(top); !ok || root != top {
		return false, fail.Say(msg.GuardNotHome(top))
	}
	if located != "" && realPath(located) == top {
		c.Root = located
		return true, nil
	}
	cfg, err := config.LoadDebug(top, c.Debug)
	if err != nil {
		return false, err
	}
	c.Root, c.Config = top, cfg
	return false, nil
}

// realPath is path with symlinks resolved, or path when that fails.
func realPath(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}

// check seals what changed (when seal is set), then lists everything
// wrong with what is staged, in the TypeScript's order: sealing,
// plaintext, size, gitleaks.
func check(c *app.Context, seal bool) []problem {
	var problems []problem
	if seal {
		if p, ok := sealAndStage(c); !ok {
			problems = append(problems, p)
		}
	}

	changes, err := staged(c)
	if err != nil {
		problems = append(problems, problem{text: voice.Record(c, msg.GuardCheckFailed(err.Error()))})
	}
	var sealed []gitx.RawChange
	// plain are the plaintext files found, and where their problems sit in
	// problems: their wording waits on git check-ignore, asked once for all.
	var plain []string
	var plainAt []int
	for _, ch := range changes {
		// Without regard to case: on a case-insensitive file system git
		// stores Private/token as typed, and it is the same directory.
		lower := strings.ToLower(ch.Path)
		// Anything but a folder at private itself (a folder never shows up
		// here, only the files in it) would stand in for the whole
		// directory: a gitlink, a symlink or a file. Committing it drops
		// every sealed file from the repo.
		if lower == "private" {
			text := msg.GuardPrivateNotFolder(ch.Path)
			if ch.NewMode == gitlinkMode {
				text = msg.GuardPrivateGitlink(ch.Path)
			}
			problems = append(problems, problem{text: voice.Record(c, text), commands: true})
			continue
		}
		if !strings.HasPrefix(lower, "private/") {
			continue
		}
		if ch.NewMode == gitlinkMode {
			problems = append(problems, problem{text: voice.Record(c, msg.GuardPrivateGitlink(ch.Path)), commands: true})
			continue
		}
		// private/'s own plain files, only directly under it: the same names
		// deeper down are plaintext like any other (the TypeScript allowed
		// them at any depth).
		// Both compare without regard to case, as git does on macOS, where
		// private/.gitignore's !*.age lets x.AGE through: the guard then
		// checks it is sealed rather than calling it plaintext.
		if path.Dir(lower) == "private" && privateAllowed[path.Base(lower)] {
			continue
		}
		if strings.HasSuffix(lower, ".age") {
			sealed = append(sealed, ch)
			continue
		}
		plain = append(plain, ch.Path)
		plainAt = append(plainAt, len(problems))
		problems = append(problems, problem{commands: true})
	}
	if len(plain) > 0 {
		via, err := letThrough(c, plain)
		if err != nil {
			problems = append(problems, problem{text: voice.Record(c, msg.GuardCheckFailed(err.Error()))})
		}
		for i, p := range plain {
			text := msg.GuardPlaintext(p)
			if g, ok := via[p]; ok {
				text = msg.GuardPlaintextNotIgnored(p, g)
			}
			problems[plainAt[i]].text = voice.Record(c, text)
		}
	}
	if p, err := allSealedDeleted(c); err != nil {
		problems = append(problems, problem{text: voice.Record(c, msg.GuardCheckFailed(err.Error()))})
	} else if p != nil {
		problems = append(problems, *p)
	}
	notSealed, err := unsealed(c, sealed)
	if err != nil {
		problems = append(problems, problem{text: voice.Record(c, msg.GuardCheckFailed(err.Error()))})
	}
	problems = append(problems, notSealed...)

	tooBig, err := oversized(c, changes)
	if err != nil {
		problems = append(problems, problem{text: voice.Record(c, msg.GuardCheckFailed(err.Error()))})
	}
	problems = append(problems, tooBig...)

	if p, ok := scan(c); !ok {
		problems = append(problems, p)
	}
	return problems
}

// letThrough says, for each of paths (plaintext files staged under
// private/) that git does not ignore, which .gitignore is to blame: the one
// whose negated pattern lets it through, or private/.gitignore when no
// pattern matches it. Such a file comes back with every git add -- private,
// so the refusal says to fix that file. A path git ignores was force-added
// (or tracked before it was ignored) and is left out. The index is not
// consulted (--no-index), so a tracked file is judged by the patterns alone.
func letThrough(c *app.Context, paths []string) (map[string]string, error) {
	r := gitx.RunIn(c, c.Root, strings.Join(paths, "\x00")+"\x00", "check-ignore", "--no-index", "--verbose", "--non-matching", "-z", "--stdin")
	// 0: some paths are ignored, 1: none are.
	if r.Code != 0 && r.Code != 1 {
		return nil, gitFailed(r, "check-ignore")
	}
	via := map[string]string{}
	fields := strings.Split(r.Stdout, "\x00")
	for i := 0; i+3 < len(fields); i += 4 {
		source, pattern, p := fields[i], fields[i+2], fields[i+3]
		switch {
		case source == "":
			via[p] = layout.Private + "/" + layout.Gitignore
		case strings.HasPrefix(pattern, "!"):
			via[p] = source
		}
	}
	return via, nil
}

// allSealedDeleted is a problem when the staged commit deletes every .age
// file under private/ that HEAD has and leaves none in the index: private/
// was moved, emptied, unmounted or replaced by a link, and the commit would
// take every sealed secret out of the repo for every other machine.
func allSealedDeleted(c *app.Context) (*problem, error) {
	r := gitx.Run(c, c.Root, "diff", "--cached", "--name-only", "-z", "--no-renames", "--diff-filter=D")
	if r.Code != 0 {
		return nil, gitFailed(r, "diff --cached")
	}
	deleted := countSealed(r.Stdout)
	if deleted == 0 {
		return nil, nil
	}
	r = gitx.Run(c, c.Root, "ls-files", "--cached", "-z")
	if r.Code != 0 {
		return nil, gitFailed(r, "ls-files")
	}
	if countSealed(r.Stdout) > 0 {
		return nil, nil
	}
	return &problem{text: voice.Record(c, msg.GuardAllSealedDeleted(deleted)), commands: true}, nil
}

// countSealed counts the .age files under private/, in any case, in a
// NUL-separated list of repo paths.
func countSealed(list string) int {
	n := 0
	for _, p := range strings.Split(list, "\x00") {
		lower := strings.ToLower(p)
		if strings.HasPrefix(lower, "private/") && strings.HasSuffix(lower, ".age") {
			n++
		}
	}
	return n
}

// ageHeaders begin every age file: the binary format and the armored one.
var ageHeaders = []string{"age-encryption.org/v1\n", "-----BEGIN AGE ENCRYPTED FILE-----"}

// unsealed lists a problem for every staged .age file under private/ whose
// staged content is not age ciphertext. The TypeScript trusted the name.
func unsealed(c *app.Context, changes []gitx.RawChange) ([]problem, error) {
	var problems []problem
	for _, ch := range changes {
		r := gitx.Run(c, c.Root, "cat-file", "blob", ch.NewID)
		if r.Code != 0 {
			return problems, gitFailed(r, "cat-file blob")
		}
		if !isAge(r.Stdout) {
			problems = append(problems, problem{text: voice.Record(c, msg.GuardNotSealed(ch.Path)), commands: true})
		}
	}
	return problems, nil
}

func isAge(content string) bool {
	if strings.HasPrefix(content, ageHeaders[0]) {
		return true
	}
	return strings.HasPrefix(strings.TrimLeft(content, " \t\r\n"), ageHeaders[1])
}

// sealAndStage seals new and changed secrets and stages their .age files,
// when the repo has a public key to seal to.
func sealAndStage(c *app.Context) (problem, bool) {
	if _, err := os.Stat(c.Layout().Recipients); err != nil {
		return problem{}, true
	}
	res, err := secrets.Seal(c)
	if len(res.Sealed) > 0 {
		args := []string{"add", "--"}
		for _, rel := range res.Sealed {
			args = append(args, "private/"+rel+".age")
		}
		if r := gitx.Run(c, c.Root, args...); r.Code != 0 && err == nil {
			err = gitFailed(r, "add")
		}
	}
	if err != nil {
		return problem{text: voice.Record(c, msg.GuardSealFailed(err.Error()))}, false
	}
	return problem{}, true
}

// staged lists the staged additions, copies, changes, renames and type
// changes. The TypeScript left type changes out, so a regular file staged
// over a committed symlink in private/ went unchecked; here it is checked.
// The object ids are full (--no-abbrev): the TypeScript sized blobs by
// abbreviated ids, which git cat-file can find ambiguous in a big repo.
func staged(c *app.Context) ([]gitx.RawChange, error) {
	r := gitx.Run(c, c.Root, "diff", "--cached", "--raw", "-z", "--no-abbrev", "--diff-filter=ACMRT")
	if r.Code != 0 {
		return nil, gitFailed(r, "diff --cached")
	}
	return gitx.ParseRaw(r.Stdout), nil
}

// Check runs the guard's checks on what is staged in c.Root, as the
// pre-commit hook does, and returns what the hook would print, without
// styles: "" when the commit may go ahead. It does not seal (save has
// sealed already) and records nothing in state: save records a refusal
// itself. save runs it before every commit, so a commit never depends on
// the hook being installed (D14).
func Check(c *app.Context) string {
	problems := check(c, false)
	if len(problems) == 0 {
		return ""
	}
	plain := func(s string) string { return s }
	return strings.Join(refusalLines(c, problems, plain, plain), "\n") + "\n"
}

// printRefusal writes the headline and each problem to stderr in the wire
// format package save parses.
func printRefusal(c *app.Context, problems []problem) {
	st := ui.ErrStyles(c)
	for _, line := range refusalLines(c, problems, st.Alarm.Render, st.Command.Render) {
		ui.Eprintln(c, line)
	}
}

// refusalLines are the lines of a refusal in the wire format: the headline
// in the alarm style, then each problem, its commands in the command style.
func refusalLines(c *app.Context, problems []problem, alarm, command func(string) string) []string {
	lines := []string{alarm(voice.Record(c, msg.GuardHeadline()))}
	for _, p := range problems {
		first, rest, more := strings.Cut(p.text, "\n")
		lines = append(lines, voice.Record(c, msg.GuardBullet(first)))
		if !more {
			continue
		}
		for _, line := range strings.Split(rest, "\n") {
			if p.commands {
				cmd := strings.TrimLeft(line, " ")
				line = line[:len(line)-len(cmd)] + command(cmd)
			}
			lines = append(lines, "  "+line)
		}
	}
	return lines
}

// sayError prints err as the CLI prints a failed command's error: its first
// line after "homebot: " in the error style, then the rest.
func sayError(c *app.Context, err error) {
	first, rest, more := strings.Cut(err.Error(), "\n")
	ui.Eprintln(c, ui.ErrStyles(c).Error.Render(voice.Plain(c, msg.CLIErrorPrefix(first))))
	if more {
		ui.Eprintln(c, rest)
	}
}
