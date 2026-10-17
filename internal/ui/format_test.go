package ui_test

import (
	"testing"
	"time"

	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// The expected values below are what src/ui.ts returns for the same input.

func TestAgo(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	now := time.UnixMilli(testutil.TestNow)
	for _, c := range []struct {
		before int64 // milliseconds
		want   string
	}{
		{0, "just now"},
		{499, "just now"},
		{59_499, "just now"},
		{59_500, "1m ago"},
		{89_999, "2m ago"},
		{90_000, "2m ago"},
		{3_569_499, "59m ago"},
		{3_569_500, "1h ago"},
		{5_400_000, "2h ago"},
		{170_999_999, "2d ago"},
		{171_000_000, "2d ago"},
		{172_800_000, "2d ago"},
		{30 * 86_400_000, "30d ago"},
		{365 * 86_400_000, "365d ago"},
		{1_000_000_000_000, "11574d ago"},
		{-499, "just now"},
		{-10_000, "just now"},
	} {
		if got := voice.Plain(ctx, ui.Ago(now.Add(-time.Duration(c.before)*time.Millisecond), now)); got != c.want {
			t.Errorf("%dms before now: %q, want %q", c.before, got, c.want)
		}
	}
}

func TestHumanSize(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	for _, c := range []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1025, "1.0 KB"},
		{1280, "1.3 KB"}, // 1.25 rounds up, as toFixed does
		{1331, "1.3 KB"},
		{1536, "1.5 KB"},
		{10188, "9.9 KB"},
		{10189, "10.0 KB"},
		{10239, "10.0 KB"},
		{10240, "10 KB"},
		{10751, "10 KB"},
		{10752, "11 KB"},
		{1048575, "1024 KB"},
		{1048576, "1.0 MB"},
		{1572864, "1.5 MB"},
		{1073741823, "1024 MB"},
		{1073741824, "1.0 GB"},
		{1610612736, "1.5 GB"},
		{1099511627776, "1024 GB"},
		{5000 * 1073741824, "5000 GB"},
		{-1, "-1 B"},
		{-2048, "-2048 B"},
	} {
		if got := voice.Plain(ctx, ui.HumanSize(c.bytes)); got != c.want {
			t.Errorf("HumanSize(%d) = %q, want %q", c.bytes, got, c.want)
		}
	}
}

func TestCommand(t *testing.T) {
	term := newTerminal(t, trueColor, "")
	line := voice.Render(term.ctx, msg.FormatCommand("homebot save"))
	got := ui.Command(term.ctx, line)
	if want := "  " + ui.Styles(term.ctx).Command.Render("homebot save"); got != want {
		t.Errorf("Command = %q, want %q", got, want)
	}
	for in, want := range map[string]string{
		"":        "",
		"x":       ui.Styles(term.ctx).Command.Render("x"),
		"    a b": "    " + ui.Styles(term.ctx).Command.Render("a b"),
		"  ":      "  ",
	} {
		if got := ui.Command(term.ctx, in); got != want {
			t.Errorf("Command(%q) = %q, want %q", in, got, want)
		}
	}
}
