package triage

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/pack"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// Answer is what the person decided about one candidate.
type Answer string

const (
	Track   Answer = "track"    // pack into common, or git add in the repo
	TrackOS Answer = "track-os" // pack into this OS's layer; home only
	Ignore  Answer = "ignore"   // never ask again
	Snooze  Answer = "snooze"   // ask again in 7 days
	Skip    Answer = "skip"     // next, decide nothing
	Quit    Answer = "quit"     // stop asking
)

// snoozeDays is how long a snoozed candidate stays quiet.
const snoozeDays = 7

// bigFolder is the size above which a folder is called probably app data.
const bigFolder = 10 * 1024 * 1024

// Describe is what triage shows next to a candidate's key: "gone", the size
// of a file, or "folder, <size>", "over <size>" when the walk stopped early.
func Describe(c *app.Context, cand Candidate) string {
	st, err := os.Lstat(cand.Abs)
	if err != nil {
		return voice.Plain(c, msg.TriageDescribeGone())
	}
	if !st.IsDir() {
		return voice.Plain(c, msg.TriageDescribeFile(voice.Plain(c, ui.HumanSize(st.Size()))))
	}
	bytes, capped := fsx.TreeSize(cand.Abs, fsx.TreeSizeLimit)
	size := voice.Plain(c, ui.HumanSize(bytes))
	if capped {
		size = voice.Plain(c, msg.TriageDescribeOver(size))
	}
	if bytes > bigFolder {
		return voice.Plain(c, msg.TriageDescribeFolderBig(size))
	}
	return voice.Plain(c, msg.TriageDescribeFolder(size))
}

// Decide carries out answer for cand and says what it did; done is false
// when there was nothing to do (Skip, Quit). The line prompt and
// the full-screen picker both decide through it.
//
// Track packs a home candidate into common (TrackOS: this OS's layer) and
// stages a repo one. Ignore adds a home candidate to decisions.yml and a
// repo one to the repo's .gitignore. Snooze hides either for 7 days.
func Decide(c *app.Context, cand Candidate, answer Answer) (said msg.Text, done bool, err error) {
	switch answer {
	case Track, TrackOS:
		if cand.Kind == Repo {
			failed := func(stderr string) error {
				return fmt.Errorf("%w%s", fail.Say(msg.TriageErrorGitAdd(cand.Key)), fail.Failure("", stderr, ""))
			}
			if err := pack.Stage(c, cand.Key, failed); err != nil {
				return msg.Text{}, false, err
			}
			return msg.TriageSuccessStaged(cand.Key), true, nil
		}
		layer := "common"
		if answer == TrackOS {
			layer = string(c.OS)
		}
		res, err := pack.Pack(c, cand.Abs, pack.Options{Layer: layer})
		rel, relErr := filepath.Rel(c.Root, res.To)
		if relErr != nil {
			rel = res.To
		}
		if err != nil && res.To != "" {
			// It moved and failed after (staging, linking back): say where
			// it went, as homebot pack does.
			moved := voice.Record(c, msg.PackStatusMoved(ui.Tildify(res.From, c.Home), rel))
			return msg.Text{}, false, fmt.Errorf("%w\n%s", err, moved)
		}
		if err != nil {
			return msg.Text{}, false, err
		}
		return msg.TriageSuccessPacked(cand.Key, rel), true, nil
	case Ignore:
		if cand.Kind == Repo {
			if strings.ContainsAny(cand.Key, "\n\r") {
				return msg.Text{}, false, fail.User(voice.Plain(c, msg.TriageErrorLineBreak(strconv.Quote(cand.Key))))
			}
			if err := gitignore(filepath.Join(c.Root, ".gitignore"), "/"+gitignoreEscape(cand.Key)); err != nil {
				return msg.Text{}, false, err
			}
			return msg.TriageSuccessGitignored(cand.Key), true, nil
		}
		if err := AddIgnore(c, cand.Key); err != nil {
			return msg.Text{}, false, err
		}
		return msg.TriageSuccessIgnored(cand.Key), true, nil
	case Snooze:
		if err := state.Snooze(c, cand.Key, snoozeDays); err != nil {
			return msg.Text{}, false, err
		}
		return msg.TriageSuccessSnoozed(cand.Key), true, nil
	}
	return msg.Text{}, false, nil
}

// gitignore appends line to the .gitignore at path, creating it. A file
// that does not end in a newline gets one first, so the line stays its own;
// the TypeScript glued it to the last line.
func gitignore(path, line string) error {
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var add strings.Builder
	if len(old) > 0 && !bytes.HasSuffix(old, []byte("\n")) {
		add.WriteString("\n")
	}
	add.WriteString(line + "\n")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(add.String()); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// gitignoreEscape writes path so a .gitignore line matches it and nothing
// else: * ? [ and \ are escaped, and so is a trailing space, which git
// would otherwise drop.
func gitignoreEscape(path string) string {
	var b strings.Builder
	for _, r := range path {
		if strings.ContainsRune(`*?[\`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	out := b.String()
	if trimmed := strings.TrimRight(out, " "); trimmed != out {
		out = trimmed + strings.Repeat(`\ `, len(out)-len(trimmed))
	}
	return out
}
