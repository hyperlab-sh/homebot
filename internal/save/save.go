// Package save commits the home repo's changes and keeps the branch level
// with its remote: seal secrets, stage what is tracked, commit, then fetch,
// fast-forward or rebase, and push. It ports src/save.ts.
package save

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/exports"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/gitx"
	"github.com/hyperlab-sh/homebot/internal/guard"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/secrets"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// Options says how Save words and records the commit.
type Options struct {
	Message string // empty: generated subject
	Auto    bool   // autosave subject; sets state.LastAutosave
}

// Result is what Save did.
type Result struct {
	Committed bool
	Files     []string
	// Sealed counts the secrets sealed; SealedPaths names them, relative to
	// private/.
	Sealed      int
	SealedPaths []string
}

// maxListed is how many files a generated commit body lists before "...and
// N more".
const maxListed = 50

// Save seals secrets, stages what is tracked, and commits it. New untracked
// files are left for triage: only tracked changes, sealed secrets and export
// snapshots are committed without asking. It writes private/.gitignore when
// private/ has none (secrets.EnsurePrivateGitignore). Before committing it runs the
// guard's checks itself (guard.Check), whether or not the pre-commit hook is
// installed. A commit the guard refuses, here or in the hook, is recorded as
// blocked state and returned as an error.
func Save(c *app.Context, o Options) (Result, error) {
	l := c.Layout()
	var res Result
	// private/.gitignore keeps plaintext out of what is staged below. A home
	// whose private/ never got one (made by hand, or before keys init and
	// pack --private wrote it) gets homebot's. A link or a file at private
	// is the guard's to refuse; nothing is written through it.
	if fi, err := os.Lstat(l.Private); err == nil && fi.IsDir() {
		if err := secrets.EnsurePrivateGitignore(c); err != nil {
			return res, err
		}
	}
	if _, err := os.Stat(l.Recipients); err == nil {
		sealed, err := secrets.Seal(c)
		res.SealedPaths = sealed.Sealed
		res.Sealed = len(sealed.Sealed)
		if err != nil {
			return res, err
		}
	}

	add := func(args ...string) error {
		r := gitx.Run(c, c.Root, append([]string{"add"}, args...)...)
		if r.Code != 0 {
			return fmt.Errorf("%w%s", fail.Say(msg.SaveErrorGitAdd()), fail.Failure("", r.Stderr, ""))
		}
		return nil
	}
	// The first save has nothing tracked yet, so it takes everything not
	// ignored, except the work folders an export may have left behind (they
	// hold copies of exports, and the person is told to delete them). The
	// second pattern is the folders' contents: a glob that names a folder
	// does not exclude the files in it.
	var err error
	if gitx.HasCommits(c, c.Root) {
		err = add("-u", "--", ".")
	} else {
		work := ":(exclude,glob)**/" + exports.WorkPattern
		err = add("-A", "--", ".", work, work+"/**")
	}
	if err != nil {
		return res, err
	}
	// private/.gitignore lets only *.age (and docs) through.
	if _, serr := os.Stat(l.Private); serr == nil {
		if err := add("--", "private"); err != nil {
			return res, err
		}
	}
	var outs []string
	for _, e := range exports.ExportsFor(c) {
		if p := exports.ExportOutPath(c, e); exists(p) {
			outs = append(outs, p)
		}
	}
	if len(outs) > 0 {
		if err := add(append([]string{"--"}, outs...)...); err != nil {
			return res, err
		}
	}

	r := gitx.Run(c, c.Root, "diff", "--cached", "--name-only", "-z")
	if r.Code != 0 {
		// Not "Nothing new to commit.": what is staged is unknown.
		if err := c.Context().Err(); err != nil {
			return res, err
		}
		return res, fmt.Errorf("%w%s", fail.Say(msg.GitErrorFailed("diff --cached")), fail.Failure("", r.Stderr, "git"))
	}
	for _, f := range strings.Split(r.Stdout, "\x00") {
		if f != "" {
			res.Files = append(res.Files, f)
		}
	}
	if len(res.Files) > 0 {
		// The guard's checks run here too, so a commit never depends on the
		// pre-commit hook being installed (D14).
		if out := guard.Check(c); out != "" {
			reason, details, headline := refusal(c, out)
			return res, block(c, reason, details, headline)
		}
		args := []string{"commit", "-q", "-m"}
		if o.Message != "" {
			args = append(args, o.Message)
		} else {
			subject, body := commitMessage(c, res.Files, o.Auto)
			args = append(args, subject, "-m", body)
		}
		if r := gitx.Run(c, c.Root, args...); r.Code != 0 {
			reason, details, headline := refusal(c, r.Stderr+r.Stdout)
			return res, block(c, reason, details, headline)
		}
		res.Committed = true
	}

	err = state.Update(c, func(s *state.State) {
		s.Blocked = nil
		if o.Auto {
			s.LastAutosave = c.Now().UnixMilli()
		}
	})
	return res, err
}

// block records a refused commit for the inbox and returns it as the error
// the user sees. Only a refused commit blocks; other failures are plain
// errors.
func block(c *app.Context, reason string, details []string, headline msg.Text) error {
	_, rest, more := strings.Cut(reason, "\n")
	if more {
		rest = "\n" + rest
	}
	for _, d := range details {
		rest += "\n" + d
	}
	uerr := fmt.Errorf("%w%s", fail.Say(headline), rest)
	err := state.Update(c, func(s *state.State) {
		s.Blocked = &state.Blocked{Reason: reason, At: c.Now().UnixMilli()}
	})
	return errors.Join(uerr, err)
}

// commitMessage is the generated subject and body of a commit of files. The
// text is stored in git history, so it always uses the neutral voice.
func commitMessage(c *app.Context, files []string, auto bool) (subject, body string) {
	if auto {
		subject = voice.Record(c, msg.SaveCommitAutosave(c.Host))
	} else {
		subject = voice.Record(c, msg.SaveCommitManual(c.Host))
	}
	var shown []string
	for _, f := range files[:min(len(files), maxListed)] {
		shown = append(shown, voice.Record(c, msg.SaveCommitBodyItem(f)))
	}
	if len(files) > maxListed {
		shown = append(shown, voice.Record(c, msg.SaveCommitBodyMore(len(files)-maxListed)))
	}
	body = voice.Record(c, msg.SaveCommitBody(len(files), c.Host, string(c.OS), strings.Join(shown, "\n")))
	return subject, body
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
