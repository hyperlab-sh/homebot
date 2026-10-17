package inbox

import (
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// shownItems is how many items the inbox lists without --all.
const shownItems = 5

// doneShown is how many journal entries "Done for you" lists at most.
const doneShown = 6

// Render lays out the inbox as homebot prints it: the header, the items
// (the first five unless all), what was done since the person last looked
// (done, from the journal), and the hint to run homebot next.
func Render(c *app.Context, items []Item, done []state.Entry, all bool) string {
	st := ui.Styles(c)
	say := func(t msg.Text) string { return voice.Render(c, t) }
	prefix := say(msg.InboxHeaderPrefix(c.Host))
	if p := persona.Speaking(c); p != nil && msg.ValidEmoji(p.Emoji) {
		prefix = p.Emoji + " " + prefix
	}
	var lines []string
	if len(items) == 0 {
		lines = append(lines, st.OK.Render(prefix+say(msg.InboxHeaderClear())))
	} else {
		head := st.Accent.Render(prefix + say(msg.InboxHeaderCount(len(items))))
		if u := countUrgent(items); u > 0 {
			head += st.Urgent.Render(say(msg.InboxHeaderUrgent(u)))
		}
		lines = append(lines, head, "")
		shown := items
		if !all && len(shown) > shownItems {
			shown = shown[:shownItems]
		}
		width := ui.Columns(c)
		for i, it := range shown {
			mark := st.Todo.Render(voice.Plain(c, msg.InboxMarkTodo()))
			if it.Level == Urgent {
				mark = st.Urgent.Render(voice.Plain(c, msg.InboxMarkUrgent()))
			}
			lines = append(lines, say(msg.InboxItemLine(i+1, mark, it.Title)))
			if it.Detail != "" {
				lines = append(lines, st.Muted.Render(ui.Detail(it.Detail, width)))
			}
			if it.Fix != nil {
				lines = append(lines, ui.Command(c, say(msg.InboxItemFix(FixText(c, it.Fix)))))
			}
		}
		if len(shown) < len(items) {
			lines = append(lines, st.Muted.Render(say(msg.InboxMore(len(items)-len(shown)))))
		}
	}
	var auto []state.Entry
	for _, d := range done {
		if !d.Error {
			auto = append(auto, d)
		}
	}
	if len(auto) > 0 {
		lines = append(lines, "", st.Muted.Render(say(msg.InboxDoneHeading())))
		for _, d := range auto[max(0, len(auto)-doneShown):] {
			lines = append(lines, st.Muted.Render(say(msg.InboxDoneItem(d.Msg))))
		}
	}
	if len(items) > 0 && items[0].Fix != nil && items[0].Fix.Args != nil {
		lines = append(lines, "", st.Muted.Render(say(msg.InboxNextHint())))
	}
	return strings.Join(lines, "\n")
}

// NudgeLine is the line the shell prints at startup, plain; "" when nothing
// needs the person.
func NudgeLine(c *app.Context, items []Item) (emoji, text string) {
	if len(items) == 0 {
		return "", ""
	}
	c = backgroundCtx(c)
	emoji = voice.Nudge(c, msg.InboxNudgeMark(), "")
	if p := persona.NudgeVoice(c); p != nil && msg.ValidEmoji(p.Emoji) {
		emoji = p.Emoji
	}
	urgent := ""
	if u := countUrgent(items); u > 0 {
		urgent = voice.Nudge(c, msg.InboxNudgeUrgent(u), "")
	}
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	return emoji, voice.Nudge(c, msg.InboxNudge(len(items), urgent), strings.Join(ids, "\x00"))
}
