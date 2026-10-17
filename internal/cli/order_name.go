package cli

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/helper"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/selfname"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
	"github.com/hyperlab-sh/homebot/internal/yamledit"
)

func init() { register(KeepingOrder, "name", newNameCmd) }

func newNameCmd(c *app.Context) *cobra.Command {
	var off bool
	cmd := &cobra.Command{
		Use:  "name [word]",
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error { return nameRun(c, args, off) },
	}
	cmd.Flags().BoolVar(&off, "off", false, "")
	return cmd
}

func nameRun(c *app.Context, args []string, off bool) error {
	if err := c.LocateRoot(); err != nil {
		return err
	}
	if len(args) > 1 || off && len(args) != 0 {
		return fail.User(voice.Plain(c, msg.CLIUsageName()))
	}
	cfg, err := config.Load(c.Root)
	if err != nil {
		return err
	}
	if len(args) == 0 && !off {
		return nameShow(c, cfg.Name)
	}
	name := ""
	if !off && args[0] != "homebot" {
		name = args[0]
	}
	if name != "" {
		taken, err := nameTaken(c)
		if err != nil {
			return err
		}
		v := selfname.Check(c, name, taken)
		if !v.OK() {
			return nameRefusal(c, name, v.Reason, v.Path)
		}
		oldState := selfname.Status(c)
		if err := changeName(c, cfg.Name, name); err != nil {
			return err
		}
		if cfg.Name != "" && cfg.Name != name && oldState.HasRecord && oldState.Rec.Name == cfg.Name && oldState.Rec.Made {
			ui.Println(c, voice.Render(c, msg.NameStatusOldStops(cfg.Name)))
		}
		if v.InWorkTree {
			ui.Println(c, voice.Render(c, msg.NameWarningInWorkTree()))
		}
		if !helper.PathHasLocalBin(c.Env.Get("PATH"), c) {
			ui.Println(c, voice.Render(c, msg.NameHintPath(name)))
		}
		ui.Println(c, voice.Render(c, msg.NameStatusSet(name)))
		refreshInbox(c)
		return nil
	}
	if err := changeName(c, cfg.Name, ""); err != nil {
		return err
	}
	ui.Println(c, voice.Render(c, msg.NameStatusOff()))
	refreshInbox(c)
	return nil
}

func nameShow(c *app.Context, configured string) error {
	st := selfname.Status(c)
	if configured == "" {
		configured = "homebot"
	}
	ui.Println(c, voice.Render(c, msg.NameStatusConfigured(configured)))
	ui.Println(c, voice.Render(c, msg.NameStatusEffective(st.Name)))
	if configured == "homebot" {
		if st.Name != "homebot" {
			ui.Println(c, voice.Render(c, msg.NameWhyConfigOff()))
		}
		return nil
	}
	if st.Name != "homebot" && st.Name != configured {
		ui.Println(c, voice.Render(c, msg.NameWhyRecordedOther(st.Name, configured)))
		return nil
	}
	switch st.Why {
	case selfname.LinkBroken:
		ui.Println(c, voice.Render(c, msg.NameWhyLinkBroken(st.Path)))
	case selfname.NotOnPath:
		ui.Println(c, voice.Render(c, msg.NameWhyNotOnPath()))
	case selfname.Shadowed:
		ui.Println(c, voice.Render(c, msg.NameWhyShadowed(st.Path)))
	case selfname.BadRecord:
		ui.Println(c, voice.Render(c, msg.NameWhyBadRecord(st.Path)))
	case selfname.NoRecord:
		ui.Println(c, voice.Render(c, msg.NameWhyNoRecord()))
	}
	return nil
}

// nameReason is the plain reason fragment for a refused name, in the active
// voice and without a sentence around it: "a program with that name is on
// PATH". A reason with no catalog text is its own code, and known is false.
func nameReason(c *app.Context, reason selfname.Reason) (text string, known bool) {
	var why msg.Text
	switch reason {
	case selfname.Syntax:
		why = msg.NameReasonSyntax()
	case selfname.Reserved:
		why = msg.NameReasonReserved()
	case selfname.Shell:
		why = msg.NameReasonShell()
	case selfname.Tool:
		why = msg.NameReasonTool()
	case selfname.OnPath:
		why = msg.NameReasonOnPath()
	case selfname.InRepo:
		why = msg.NameReasonInRepo()
	case selfname.Foreign:
		why = msg.NameReasonForeign()
	case selfname.SyncedBin:
		why = msg.NameReasonSyncedBin()
	case selfname.TempBlocked:
		why = msg.NameReasonTempBlocked()
	case selfname.NoHomebot:
		why = msg.NameReasonNoHomebot()
	default:
		return string(reason), false
	}
	return voice.Plain(c, why), true
}

// nameRefusal is the whole voiced refusal, "Cannot use x: why (path)", for a
// command that fails. Where the refusal sits inside another sentence, as in
// the setup warning, pass nameReason instead: this one carries its own
// persona line and punctuation.
func nameRefusal(c *app.Context, name string, reason selfname.Reason, path string) error {
	detail, known := nameReason(c, reason)
	if !known {
		return fail.User(detail)
	}
	if path == "" {
		return fail.User(voice.Plain(c, msg.NameErrorRefusedNoPath(name, detail)))
	}
	return fail.User(voice.Plain(c, msg.NameErrorRefused(name, detail, path)))
}

// changeName edits only the name scalar, preserving the rest of homebot.yml.
// Validation runs before the edit. If the link operation refuses, the old
// config bytes are restored; a completed link still has its own D4 record.
func changeName(c *app.Context, old, name string) error {
	return changeNameLink(c, old, name, true)
}

// changeNameLink is shared with setup, which installs the helper only after
// the name and PATH choices are complete.
func changeNameLink(c *app.Context, old, name string, updateHelper bool) error {
	file := filepath.Join(c.Root, config.FileName)
	before, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	after := before
	if name == "" {
		after, err = yamledit.DeleteScalar(before, []string{"name"})
	} else if name != old {
		after, err = yamledit.SetScalar(before, []string{"name"}, name)
	}
	if err != nil {
		return err
	}
	if string(after) != string(before) {
		if _, err := config.Parse(after, file); err != nil {
			return err
		}
		if err := fsx.WriteFileAtomic(file, after, info.Mode().Perm()); err != nil {
			return err
		}
	}
	if name == "" {
		err = selfname.Unlink(c)
	} else {
		err = selfname.Link(c, name)
	}
	if err != nil {
		// A fault after the new link is recorded is resumable. Keep name:
		// aligned with that record so the next name or tidy can finish it.
		rec, has := selfname.Record(c)
		if string(after) != string(before) && (!has || rec.Name != name) {
			if rollbackErr := fsx.WriteFileAtomic(file, before, info.Mode().Perm()); rollbackErr != nil {
				return errors.Join(err, rollbackErr)
			}
		}
		var linkErr *selfname.Error
		if errors.As(err, &linkErr) {
			return nameRefusal(c, name, linkErr.Reason, linkErr.Path)
		}
		return err
	}
	if name != "" && updateHelper {
		return fixNamedHelperPath(c)
	}
	return nil
}

func fixNamedHelperPath(c *app.Context) error {
	if !helper.Installed(c) {
		return nil
	}
	bin := filepath.Join(c.Home, ".local", "bin")
	recorded, ok := helper.RecordedPath(c)
	if !ok || helper.PathHasLocalBin(recorded, c) {
		return nil
	}
	current := c.Env.Get("PATH")
	if !helper.PathHasLocalBin(current, c) && !helper.RCPutsLocalBinOnPath(c) {
		return nil
	}
	copy := *c
	if !helper.PathHasLocalBin(current, c) {
		current = bin + string(os.PathListSeparator) + current
	}
	copy.Env = c.Env.With(map[string]string{"PATH": current})
	return helper.Install(&copy)
}
