package msg_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/msg/catalog"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// plain renders texts in the sandbox's voice, the neutral one, unstyled.
func plain(t *testing.T) func(msg.Text) string {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	return func(text msg.Text) string { return voice.Plain(ctx, text) }
}

func TestTheNeutralVoice(t *testing.T) {
	say := plain(t)
	cases := []struct {
		text msg.Text
		want string
	}{
		{msg.InboxHeaderCount(3), "3 things need you"},
		{msg.InboxHeaderCount(1), "1 thing needs you"},
		{msg.InboxHeaderCount(0), "0 things need you"},
		{msg.InventoryProfileUnpacked(msg.FormatAgoMinutes(5)), "unpacked 5m ago"},
		{msg.InventoryProfileUnpacked(msg.InventoryHelperNeverRan()), "unpacked never"},
		{msg.FormatSize("1.5", "KB"), "1.5 KB"},
		{msg.FormatAgoJustNow(), "just now"},
		// The appended commands, byte for byte what src/inbox.ts prints.
		{msg.InboxNextHint(), "  Do the first one now: homebot next"},
		{msg.InboxMore(4), "  ...and 4 more: homebot --all"},
		{msg.InboxNudgeMark(), "🚚"},
		{msg.InboxNudge(3, " (1 urgent)"), "homebot: 3 things need you (1 urgent), run: homebot"},
		{msg.InboxNudge(1, ""), "homebot: 1 thing needs you, run: homebot"},
		{msg.InboxNotifyBody("Backup failed", " (+2 more)"), "Backup failed (+2 more). Run: homebot"},
		{msg.Text{}, ""},
	}
	for _, c := range cases {
		if got := say(c.text); got != c.want {
			t.Errorf("%s: %q, want %q", c.text.ID(), got, c.want)
		}
	}
}

func TestTextCarriesItsEntry(t *testing.T) {
	say := plain(t)
	ago := msg.FormatAgoHours(2)
	text := msg.InboxTidyFailedTitle("tidy", ago)
	if text.ID() != "inbox.tidy_failed.title" || text.Template() != "Automatic {step_name} failed {ago}" || !text.Persona() || text.Append() != "" {
		t.Errorf("entry: id %q, template %q, persona %v, append %q", text.ID(), text.Template(), text.Persona(), text.Append())
	}
	want := []msg.Arg{{Name: "step_name", Type: catalog.String, Value: "tidy"}, {Name: "ago", Type: catalog.Duration, Value: ago}}
	if d := cmp.Diff(want, text.Args(), cmp.Comparer(func(a, b msg.Text) bool { return a.ID() == b.ID() && say(a) == say(b) })); d != "" {
		t.Errorf("Args (-want +got):\n%s", d)
	}
	if msg.InboxNextHint().Append() != " {self} next" {
		t.Error("inbox.next_hint lost its append")
	}
	if msg.FormatAgoMinutes(1).Persona() {
		t.Error("format.ago.minutes is not voiced")
	}
}

func TestATextPrintedWithoutTheVoiceShowsTheSlip(t *testing.T) {
	ago := msg.FormatAgoHours(2)
	title := msg.InboxTidyFailedTitle("tidy", ago)
	var zero msg.Text
	cases := []struct {
		name, got, want string
	}{
		{"Sprint", fmt.Sprint(title), "!msg(inbox.tidy_failed.title)"},
		{"%v", fmt.Sprintf("%v", title), "!msg(inbox.tidy_failed.title)"},
		{"%s", fmt.Sprintf("Failed: %s.", title), "Failed: !msg(inbox.tidy_failed.title)."},
		{"%+v", fmt.Sprintf("%+v", title), "!msg(inbox.tidy_failed.title)"},
		{"a pointer", fmt.Sprint(&title), "!msg(inbox.tidy_failed.title)"},
		{"no placeholders", fmt.Sprint(msg.InboxNextHint()), "!msg(inbox.next_hint)"},
		{"a nested Text", fmt.Sprint(ago), "!msg(format.ago.hours)"},
		{"the zero Text", fmt.Sprint(zero), "!msg()"},
		{"beside other values", fmt.Sprintln("Inbox:", msg.InboxNextHint(), 3), "Inbox: !msg(inbox.next_hint) 3\n"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, tc.got, tc.want)
		}
	}
	if s := fmt.Sprint(title); strings.Contains(s, "Automatic") || strings.Contains(s, "tidy failed") || strings.Contains(s, "2h") {
		t.Errorf("an unrendered Text shows words or values: %q", s)
	}

	sb := testutil.Sandbox(t, testutil.Options{})
	ui.Println(sb.Ctx, title)
	if got := sb.Stdout.String(); got != "!msg(inbox.tidy_failed.title)\n" {
		t.Errorf("ui.Println of an unrendered Text printed %q", got)
	}
}

func TestArgsIsACopy(t *testing.T) {
	text := msg.FormatSize("1", "B")
	text.Args()[0].Value = "999"
	if got := plain(t)(text); got != "1 B" {
		t.Errorf("changing Args() changed the Text: %q", got)
	}
}

func TestZeroText(t *testing.T) {
	var z msg.Text
	if z.ID() != "" || z.Template() != "" || z.Append() != "" || z.Persona() || z.Args() != nil || plain(t)(z) != "" {
		t.Error("the zero Text should be empty")
	}
}

func TestValues(t *testing.T) {
	text := msg.InventoryExportsOk("brew", msg.FormatAgoDays(2))
	got := text.Values("Tester", func(n msg.Text) string { return "<" + n.ID() + ">" })
	want := map[string]any{"bot": "Tester", "self": "homebot", "name": "brew", "ago": "<format.ago.days>"}
	if d := cmp.Diff(want, got); d != "" {
		t.Errorf("Values (-want +got):\n%s", d)
	}
}

func TestCatalogIsACopy(t *testing.T) {
	first := testutil.Catalog(t)
	if len(first) == 0 || first[0].ID != "format.ago.just_now" {
		t.Fatalf("Catalog() starts with %v", first[:min(1, len(first))])
	}
	first[0].ID = "changed"
	for i := range first {
		if len(first[i].Placeholders) > 0 {
			first[i].Placeholders[0].Name = "changed"
			break
		}
	}
	again := testutil.Catalog(t)
	if again[0].ID != "format.ago.just_now" {
		t.Error("Catalog() hands out its own slice")
	}
	for _, e := range again {
		for _, p := range e.Placeholders {
			if p.Name == "changed" {
				t.Fatalf("%s: Catalog() hands out its placeholders", e.ID)
			}
		}
	}
}
