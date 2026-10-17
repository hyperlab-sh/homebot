package inbox

import (
	"runtime/debug"
	"time"

	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/update"
)

// readBuildInfo is the build info updateItems classifies the running binary
// with; tests replace it to stand in for a go install build.
var readBuildInfo = debug.ReadBuildInfo

// updateItems only reads local state. Release requests are confined to the
// helper's update.Step and the user's explicit version --check command.
func updateItems(e *env) ([]Item, error) {
	if e.c.Config == nil || e.c.Config.Update == config.UpdateOff {
		return nil, nil
	}
	// A failed check (offline) only means the last answer is old, not wrong:
	// it keeps Latest and UpdateFailed, so the item stays until the running
	// version is at least Latest (S5). update.StateVersion decides that.
	ch := e.st.UpdateCheck
	if ch == nil || ch.Latest == "" {
		return nil, nil
	}
	info, _ := readBuildInfo()
	i := update.Detect(e.c, update.Build{Version: e.c.BuildVersion, Channel: e.c.BuildChannel}, info)
	if !update.StateVersion(i, ch) {
		return nil, nil
	}
	r := update.Release{Version: ch.Latest, Tag: "v" + ch.Latest, URL: "https://github.com/hyperlab-sh/homebot/releases/tag/v" + ch.Latest}
	command := update.ManualCommand(i, r)
	var reason msg.Text
	switch e.c.Config.Update {
	case config.UpdateNotify:
		reason = msg.UpdateItemAvailable(ch.Latest)
	case config.UpdateAuto:
		switch {
		case i.Method == update.Deb || i.Method == update.RPM:
			reason = msg.UpdateItemRoot(ch.Latest)
		case i.Method != update.Cask && i.Method != update.Script:
			reason = msg.UpdateItemManual(ch.Latest)
		case e.st.UpdateFailed != nil && e.st.UpdateFailed.Version == ch.Latest:
			failed := e.st.UpdateFailed
			switch failed.Reason {
			case "cosign":
				reason = msg.UpdateItemCosign(ch.Latest, update.CosignMin())
			case "verify":
				reason = msg.UpdateItemVerify(ch.Latest)
			case "timeout":
				reason = msg.UpdateItemTimeout(ch.Latest)
			case "broken":
				reason = msg.UpdateItemBroken(ch.Latest)
			default:
				reason = msg.UpdateItemFailed(ch.Latest, failed.Error)
			}
		case ch.FirstAttempt != 0 && e.now.UnixMilli()-ch.FirstAttempt >= (72*time.Hour).Milliseconds():
			reason = msg.UpdateItemTapLag(ch.Latest)
		default:
			return nil, nil // cask or script release is still soaking or retrying quietly.
		}
	default:
		return nil, nil
	}
	it := e.item("update-available", Todo, msg.UpdateItemTitle(), &Fix{Shell: command})
	it.Detail = e.plain(reason)
	return []Item{it}, nil
}
