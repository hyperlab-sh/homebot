package save

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// headline matches the guard's first line, "<name> refused this commit:",
// with or without the "homebot: " an error line carries.
var headline = regexp.MustCompile(`^(homebot: )?\S.* refused this commit:?$`)

const bullet = "  - "

// Refusal explains why git refused a commit, from what git and its hooks
// printed. With the guard's output ("  - " starts each problem), the reason
// is the first problem with the lines under it (the command that fixes it)
// and the details are the other problems. Otherwise the reason is the first
// thing git said and the details are the next lines. The reason is said in
// the neutral voice: it is stored as state.Blocked.Reason.
//
// The format is pinned by testdata/guard/refusal-*.txt.
func Refusal(c *app.Context, output string) (reason string, details []string) {
	reason, details, _ = refusal(c, output)
	return reason, details
}

// refusal also returns the first line's Text for the printed error. The
// string reason remains neutral because it is stored as Blocked.Reason.
func refusal(c *app.Context, output string) (reason string, details []string, summaryText msg.Text) {
	output = ansi.Strip(output)
	var lines []string
	for _, l := range strings.Split(output, "\n") {
		t := strings.TrimSpace(l)
		// The headline is never indented; a bullet ending in the same words is a problem.
		if t == "" || (l == strings.TrimLeft(l, " \t") && headline.MatchString(t)) {
			continue
		}
		lines = append(lines, l)
	}
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, bullet) {
			start = i
			break
		}
	}
	if start >= 0 {
		end := start + 1
		for end < len(lines) && !strings.HasPrefix(lines[end], bullet) {
			end++
		}
		summaryText = msg.SaveErrorRefused(lines[start][len(bullet):])
		parts := []string{voice.Record(c, summaryText)}
		for _, l := range lines[start+1 : end] {
			parts = append(parts, l[min(2, len(l)):])
		}
		return strings.Join(parts, "\n"), lines[end:], summaryText
	}
	said := fail.ToolSaid(output, 3)
	first := voice.Record(c, msg.SaveErrorRefusedNoReason())
	if len(said) > 0 {
		first = said[0]
		for _, l := range said[1:] {
			details = append(details, "  "+l)
		}
	}
	summaryText = msg.SaveErrorRefused(first)
	return voice.Record(c, summaryText), details, summaryText
}
