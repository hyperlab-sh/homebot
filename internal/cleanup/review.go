package cleanup

import (
	"os"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// Answer is what the user wants done with one backup.
type Answer string

// The answers a Chooser gives.
const (
	Delete Answer = "delete" // remove the backup
	Keep   Answer = "keep"   // leave it for next time
	Use    Answer = "use"    // copy it into the repo file, then remove it
	Merge  Answer = "merge"  // take some of its lines into the repo file
	Quit   Answer = "quit"   // stop; it and the rest stay
)

// Question is what a Chooser is asked about one backup.
type Question struct {
	Index int // 1-based
	Total int
	// Diff is backupDiff's text, colored; "" when the backup is the same as
	// what is linked there now.
	Diff string
	// CanUse is true when the target is a file in the repo: only then are
	// Use and Merge offered.
	CanUse bool
}

// Chooser asks what to do with one backup.
type Chooser func(b Backup, q Question) (Answer, error)

// Result counts what a review did.
type Result struct {
	Deleted int
	Used    int
	// Merged counts merges that took something; the backup itself is then
	// deleted or kept like any other.
	Merged int
	Kept   int
}

// Review asks about each backup in turn and prints what each answer did.
// It stops at Quit; the rest stay untouched and count as kept. After a
// merge the same backup comes back, with the diff of what is left. A
// Chooser error, or the command stopped during a merge, ends the review: the
// rest count as kept.
func Review(c *app.Context, backups []Backup, choose Chooser, pick Picker) (Result, error) {
	var r Result
	st := ui.Styles(c)
	for i, b := range backups {
		var answer Answer
		for {
			q := Question{Index: i + 1, Total: len(backups), Diff: backupDiff(c, b, true), CanUse: repoFileFor(c, b) != ""}
			var err error
			if answer, err = choose(b, q); err != nil {
				r.Kept += len(backups) - i
				return r, err
			}
			if answer != Merge {
				break
			}
			done, took, err := mergeBackup(c, b, pick)
			if ctxErr := c.Context().Err(); err != nil && ctxErr != nil {
				// Stopped mid-merge (Ctrl-C in git add -p): no error line,
				// the review ends as a chooser error would end it.
				r.Kept += len(backups) - i
				return r, ctxErr
			}
			switch {
			case err != nil:
				ui.Println(c, st.Error.Render(err.Error()))
			case took:
				ui.Println(c, st.OK.Render(voice.Render(c, done)))
				r.Merged++
			default:
				ui.Println(c, st.Muted.Render(voice.Render(c, msg.CleanupStatusNothingTaken())))
			}
		}
		switch answer {
		case Quit:
			r.Kept += len(backups) - i
			return r, nil
		case Delete, Use:
		default:
			r.Kept++
			continue
		}
		done, err := applyAnswer(c, b, answer)
		if err != nil {
			ui.Println(c, st.Error.Render(err.Error()))
			r.Kept++
			continue
		}
		ui.Println(c, st.OK.Render(voice.Render(c, done)))
		if answer == Delete {
			r.Deleted++
		} else {
			r.Used++
		}
	}
	return r, nil
}

// applyAnswer deletes the backup or copies it into the repo file, and says
// what it did. Only Delete and Use do anything.
func applyAnswer(c *app.Context, b Backup, answer Answer) (msg.Text, error) {
	switch answer {
	case Delete:
		if err := os.RemoveAll(b.Path); err != nil {
			return msg.Text{}, err
		}
		return msg.CleanupSuccessDeleted(ui.Tildify(b.Path, c.Home)), nil
	case Use:
		dest := repoFileFor(c, b)
		if dest == "" {
			return msg.Text{}, fail.User(voice.Plain(c, msg.CleanupErrorUseNotRepo(ui.Tildify(b.Target, c.Home))))
		}
		if err := copyFile(b.Path, dest); err != nil {
			return msg.Text{}, err
		}
		if err := os.Remove(b.Path); err != nil && !os.IsNotExist(err) {
			return msg.Text{}, err
		}
		return msg.CleanupSuccessUsed(repoRel(c, dest)), nil
	}
	return msg.Text{}, nil
}

// copyFile writes src's bytes and mode over dst, as Node's copyFileSync
// does. src may be a link to a file.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, info.Mode().Perm()); err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode().Perm())
}
