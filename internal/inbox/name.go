package inbox

import (
	"os"
	"path/filepath"

	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/helper"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/selfname"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// nameItems inspects the configured name without changing a link or unit.
// A pulled name that is unsafe on this machine stays in homebot.yml; the
// helper leaves its previous machine-local link in place.
func nameItems(e *env) ([]Item, error) {
	cfg, err := config.LoadDebug(e.c.Root, e.c.Debug)
	if err != nil || cfg.Name == "" {
		return nil, err
	}
	name := cfg.Name
	fix := &Fix{Args: []string{"name", name}}
	var taken selfname.Taken
	if e.nameTaken != nil {
		taken, err = e.nameTaken(e.c)
		if err != nil {
			return nil, err
		}
	}
	v := selfname.Check(e.c, name, taken)
	rec, hasRecord := selfname.Record(e.c)
	if v.Reason == selfname.OnPath && hasRecord && rec.Name == name {
		level := Todo
		title := msg.InboxNameShadowedOtherTitle(name)
		if selfname.Status(e.c).Name == name {
			level = Urgent // our link is the program a script would run
			title = msg.InboxNameShadowedOursTitle(name)
		}
		it := e.item("name_shadowed", level, title, fix)
		it.Detail = e.plain(msg.InboxNameShadowedDetail(v.Path))
		return append([]Item{it}, helperPathItem(e, name, rec)...), nil
	}
	if !v.OK() {
		return []Item{blockedNameItem(e, name, v.Reason, v.Path)}, nil
	}
	if path := selfname.TempBlockedPath(e.c, name); path != "" {
		return []Item{blockedNameItem(e, name, selfname.TempBlocked, path)}, nil
	}
	if fi, err := os.Stat(helper.LocalBin(e.c)); err != nil || !fi.IsDir() {
		return []Item{e.item("name_no_bin", Todo, msg.InboxNameNoBinTitle(name), fix)}, nil
	}
	return helperPathItem(e, name, rec), nil
}

func blockedNameItem(e *env, name string, reason selfname.Reason, path string) Item {
	it := e.item("name_blocked", Todo, msg.InboxNameBlockedTitle(name), &Fix{Args: []string{"name", name}})
	why := voice.Plain(e.c, nameReason(reason))
	if path == "" {
		it.Detail = e.plain(msg.NameErrorRefusedNoPath(name, why))
	} else {
		it.Detail = e.plain(msg.NameErrorRefused(name, why, path))
	}
	return it
}

func nameReason(reason selfname.Reason) msg.Text {
	switch reason {
	case selfname.Syntax:
		return msg.NameReasonSyntax()
	case selfname.Reserved:
		return msg.NameReasonReserved()
	case selfname.Shell:
		return msg.NameReasonShell()
	case selfname.Tool:
		return msg.NameReasonTool()
	case selfname.OnPath:
		return msg.NameReasonOnPath()
	case selfname.InRepo:
		return msg.NameReasonInRepo()
	case selfname.Foreign:
		return msg.NameReasonForeign()
	case selfname.SyncedBin:
		return msg.NameReasonSyncedBin()
	case selfname.TempBlocked:
		return msg.NameReasonTempBlocked()
	case selfname.NoHomebot:
		return msg.NameReasonNoHomebot()
	}
	return msg.NameReasonForeign()
}

func helperPathItem(e *env, name string, rec selfname.Rec) []Item {
	if rec.Name != name || !helper.Installed(e.c) {
		return nil
	}
	if _, err := os.Lstat(filepath.Join(helper.LocalBin(e.c), name)); err != nil {
		return nil
	}
	recorded, ok := helper.RecordedPath(e.c)
	if !ok || helper.PathHasLocalBin(recorded, e.c) || !helper.ShellWillFindLocalBin(e.c) {
		return nil
	}
	return []Item{e.item("name_helper_path", Todo, msg.InboxNameHelperPathTitle(name), &Fix{Args: []string{"name", name}})}
}
