package ui

import (
	"strconv"
	"strings"
	"time"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/msg"
)

// Ago is how long before now at was: just now, then minutes, hours up to 47
// and days, each rounded half up as src/ui.ts rounds. A time after now is
// just now.
func Ago(at, now time.Time) msg.Text {
	s := roundDiv(max(0, now.UnixMilli()-at.UnixMilli()), 1000)
	if s < 60 {
		return msg.FormatAgoJustNow()
	}
	m := roundDiv(s, 60)
	if m < 60 {
		return msg.FormatAgoMinutes(int(m))
	}
	h := roundDiv(m, 60)
	if h < 48 {
		return msg.FormatAgoHours(int(h))
	}
	return msg.FormatAgoDays(int(roundDiv(h, 24)))
}

// roundDiv is n/d rounded half up, for n >= 0 and d > 0.
func roundDiv(n, d int64) int64 {
	return n/d + (n%d*2)/d
}

var sizeUnits = []string{"B", "KB", "MB", "GB"}

// HumanSize writes a byte count in B, KB, MB or GB (powers of 1024): whole
// numbers from 10 up, and one decimal below 10, rounded half up.
func HumanSize(bytes int64) msg.Text {
	i := 0
	for i < len(sizeUnits)-1 && bytes>>(10*(i+1)) > 0 {
		i++
	}
	if i == 0 {
		return msg.FormatSize(strconv.FormatInt(bytes, 10), sizeUnits[0])
	}
	shift := uint(10 * i)
	if bytes>>shift >= 10 {
		whole := (bytes + 1<<(shift-1)) >> shift
		return msg.FormatSize(strconv.FormatInt(whole, 10), sizeUnits[i])
	}
	tenths := (bytes*10 + 1<<(shift-1)) >> shift
	return msg.FormatSize(strconv.FormatInt(tenths/10, 10)+"."+strconv.FormatInt(tenths%10, 10), sizeUnits[i])
}

// Command styles line, a command to run as format.command renders it, in
// the command style: ui.Command(ctx, voice.Render(ctx, msg.FormatCommand(cmd))).
// The indent stays unstyled, so the command alone is highlighted, as in
// src/ui.ts.
func Command(ctx *app.Context, line string) string {
	cmd := strings.TrimLeft(line, " ")
	return line[:len(line)-len(cmd)] + Styles(ctx).Command.Render(cmd)
}
