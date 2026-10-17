package cli

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/selfname"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
	"github.com/hyperlab-sh/homebot/internal/yamledit"
)

// setupSetPersona edits only persona:, preserving the rest of the repo's
// config. The explicit flag wins on both interactive and unattended runs.
func setupSetPersona(c *app.Context, persona string) error {
	file := filepath.Join(c.Root, config.FileName)
	before, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	after, err := yamledit.SetScalar(before, []string{"persona"}, persona)
	if err != nil {
		return err
	}
	parsed, err := config.Parse(after, file)
	if err != nil {
		return err
	}
	if string(after) != string(before) {
		info, err := os.Stat(file)
		if err != nil {
			return err
		}
		if err := fsx.WriteFileAtomic(file, after, info.Mode().Perm()); err != nil {
			return err
		}
	}
	c.Config = parsed
	return nil
}

type setupNamingResult struct {
	name       string
	blocked    string
	linkMade   bool
	nameEdited bool
}

// setupChooseNaming runs after the repo is known, before guard/helper
// installation. The repo's name is shared across machines; a blocked one
// stays in homebot.yml and gets an inbox item, but no link on this machine.
func setupChooseNaming(c *app.Context, opts setupOptions) (setupNamingResult, error) {
	if opts.hasPersona {
		if c.Config.Persona != opts.persona {
			if err := setupSetPersona(c, opts.persona); err != nil {
				return setupNamingResult{}, err
			}
		}
		ui.Println(c, voice.Render(c, msg.SetupStatusPersona(opts.persona)))
	} else if c.Config.Persona != "" {
		ui.Println(c, voice.Render(c, msg.SetupStatusPersona(c.Config.Persona)))
	}

	stored := c.Config.Name
	want := stored
	chosen := false
	if opts.hasName && opts.name != stored {
		if stored != "" {
			if !ui.Interactive(c) {
				ui.Println(c, voice.Render(c, msg.SetupStatusRepoNameWins(stored, opts.name)))
			} else {
				answer, ok := ui.AskOrEnd(c, voice.Plain(c, msg.SetupPromptChangeName(stored, opts.name)))
				if ok && (strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes")) {
					want, chosen = opts.name, true
				}
			}
		} else {
			want, chosen = opts.name, true
		}
	} else if stored == "" && !opts.hasName && ui.Interactive(c) {
		for {
			answer, ok := ui.AskOrEnd(c, voice.Plain(c, msg.SetupPromptName()))
			if !ok || answer == "" || answer == "homebot" {
				break
			}
			if !config.ValidName(answer) {
				ui.Println(c, voice.Render(c, msg.SetupWarningNameInvalid(answer)))
				continue
			}
			taken, err := nameTaken(c)
			if err != nil {
				return setupNamingResult{}, err
			}
			if verdict := selfname.Check(c, answer, taken); !verdict.OK() {
				why, _ := nameReason(c, verdict.Reason)
				ui.Println(c, voice.Render(c, msg.SetupWarningNameBlocked(answer, why)))
				continue
			}
			want, chosen = answer, true
			break
		}
	}
	if want == "homebot" {
		want = ""
	}
	if want == "" {
		if chosen && stored != "" {
			if err := changeNameLink(c, stored, "", false); err != nil {
				return setupNamingResult{}, err
			}
			c.Config.Name = ""
		}
		return setupNamingResult{nameEdited: chosen && stored != ""}, nil
	}
	if !chosen && stored != "" {
		ui.Println(c, voice.Render(c, msg.SetupStatusRepoName(stored)))
	}
	taken, err := nameTaken(c)
	if err != nil {
		return setupNamingResult{}, err
	}
	verdict := selfname.Check(c, want, taken)
	if !verdict.OK() {
		why, _ := nameReason(c, verdict.Reason)
		ui.Println(c, voice.Render(c, msg.SetupWarningNameBlocked(want, why)))
		return setupNamingResult{blocked: want}, nil
	}
	prior, hadPrior := selfname.Record(c)
	if chosen && want != stored {
		if err := changeNameLink(c, stored, want, false); err != nil {
			return setupNamingResult{}, err
		}
		c.Config.Name = want
	} else if err := selfname.Link(c, want); err != nil {
		return setupNamingResult{}, err
	}
	return setupNamingResult{
		name: want, linkMade: verdict.Existing == selfname.Absent || hadPrior && (prior.Name != want || prior.Pending),
		nameEdited: chosen && want != stored,
	}, nil
}
