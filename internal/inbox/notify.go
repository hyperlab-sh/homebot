package inbox

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// notifyAgain is how long the same urgent items stay quiet after a notification.
const notifyAgain = 24 * time.Hour

// Notify sends a desktop notification about the urgent items when they
// changed, and again once a day while they stay. It records what it sent in
// state.json, also when the notification could not be shown, and forgets it
// once nothing is urgent. It reports whether a notification was shown.
func Notify(c *app.Context, items []Item) (bool, error) {
	var ids []string
	for _, it := range items {
		if it.Level == Urgent {
			ids = append(ids, it.ID)
		}
	}
	slices.Sort(ids)
	key := strings.Join(ids, ",")
	st, err := state.Load(c)
	if err != nil {
		return false, err
	}
	last := st.Notify
	if key == "" {
		if last != nil {
			return false, state.Update(c, func(s *state.State) { s.Notify = nil })
		}
		return false, nil
	}
	now := c.Now().UnixMilli()
	if last != nil && last.Key == key && now-last.At < notifyAgain.Milliseconds() {
		return false, nil
	}
	title, body := NotificationText(c, items)
	sent := sendNotification(c, title, body)
	err = state.Update(c, func(s *state.State) { s.Notify = &state.Notify{Key: key, At: now} })
	return sent, err
}

// NotifyUpdate announces a completed unattended upgrade or a broken cask
// installation. Neither message is persisted as an update failure.
func NotifyUpdate(c *app.Context, version, reinstall string) bool {
	if reinstall != "" {
		return sendNotification(c, voice.Nudge(c, msg.UpdateNotifyTitle(), ""), voice.Nudge(c, msg.UpdateNotifyBroken(reinstall), ""))
	}
	return sendNotification(c, voice.Nudge(c, msg.UpdateNotifyTitle(), ""), voice.Nudge(c, msg.UpdateNotifySuccess(version), ""))
}

// NotificationText is the title and body of the notification about items'
// urgent ones: the first one's title, and how many more there are. Both are
// empty when none is urgent. Notify sends them; persona preview shows them
// without sending anything. They speak with the nudge's voice, so a broken
// persona in homebot.yml gives the neutral notification (see backgroundCtx).
func NotificationText(c *app.Context, items []Item) (title, body string) {
	c = backgroundCtx(c)
	var urgent []Item
	for _, it := range items {
		if it.Level == Urgent {
			urgent = append(urgent, it)
		}
	}
	if len(urgent) == 0 {
		return "", ""
	}
	more := ""
	if len(urgent) > 1 {
		more = voice.Nudge(c, msg.InboxNotifyMore(len(urgent)-1), "")
	}
	emoji := ""
	if p := persona.NudgeVoice(c); p != nil {
		emoji = p.Emoji
	}
	title = notificationTitle(emoji, voice.Nudge(c, msg.InboxNotifyTitle(), ""))
	body = msg.StripUnsafe(voice.Nudge(c, msg.InboxNotifyBody(urgent[0].Title, more), ""))
	return title, body
}

func notificationTitle(emoji, text string) string {
	text = strings.ReplaceAll(msg.StripUnsafe(text), "\n", " ")
	if msg.ValidEmoji(emoji) {
		return emoji + " " + text
	}
	return text
}

// sendNotification shows title and body as a desktop notification:
// osascript on macOS, notify-send or else gdbus on Linux. It reports whether
// that worked.
func sendNotification(c *app.Context, title, body string) bool {
	run := func(argv ...string) bool { return proc.Run(c, argv, proc.Options{}).Code == 0 }
	if c.OS == app.MacOS {
		return run("osascript", "-e", "display notification "+jsString(body)+" with title "+jsString(title))
	}
	name := voice.Nudge(c, msg.InboxNotifyAppName(), "")
	if proc.HasCommand(c, "notify-send") {
		return run("notify-send", "--app-name="+name, title, escapeMarkup(body))
	}
	if proc.HasCommand(c, "gdbus") {
		return run("gdbus", "call", "--session",
			"--dest", "org.freedesktop.Notifications",
			"--object-path", "/org/freedesktop/Notifications",
			"--method", "org.freedesktop.Notifications.Notify",
			gvariantString(name), "0", gvariantString(""), gvariantString(title), gvariantString(escapeMarkup(body)),
			"[]", "{}", "10000")
	}
	return false
}

func escapeMarkup(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// gvariantString quotes s as a GVariant text-format string, which is what
// gdbus call parses each argument as. Unquoted, gdbus reads a quote in the
// text as the end of the string and a backslash as an escape, so a persona's
// "it's" or "C:\" would fail the call or change the message. Inside double
// quotes only the backslash, the double quote and control characters need an
// escape; a newline is \n and any other control character is \uXXXX.
func gvariantString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\' || r == '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// jsString quotes s as JSON.stringify does, which AppleScript reads as a
// string: <, > and & stay as they are.
func jsString(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	return strings.TrimSuffix(b.String(), "\n")
}
