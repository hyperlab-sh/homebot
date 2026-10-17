package inbox

import (
	"testing"

	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func TestNotificationTextIsAboutTheUrgentItems(t *testing.T) {
	c := testutil.Sandbox(t, testutil.Options{}).Ctx
	for _, tc := range []struct {
		name        string
		items       []Item
		title, body string
	}{
		{"nothing", nil, "", ""},
		{"only todos", []Item{{ID: "t", Level: Todo, Title: "meh"}}, "", ""},
		{"one urgent among todos", []Item{
			{ID: "t", Level: Todo, Title: "meh"},
			{ID: "a", Level: Urgent, Title: "Bad"},
			{ID: "u", Level: Todo, Title: "also meh"},
		}, "homebot needs you", "Bad. Run: homebot"},
		{"the first urgent one, and how many more", []Item{
			{ID: "b", Level: Urgent, Title: "Worse"},
			{ID: "t", Level: Todo, Title: "meh"},
			{ID: "a", Level: Urgent, Title: "Bad"},
			{ID: "c", Level: Urgent, Title: "Also bad"},
		}, "homebot needs you", "Worse (+2 more). Run: homebot"},
	} {
		title, body := NotificationText(c, tc.items)
		if title != tc.title || body != tc.body {
			t.Errorf("%s: NotificationText = %q, %q; want %q, %q", tc.name, title, body, tc.title, tc.body)
		}
	}
}

func TestNotificationTextSeparatelyCleansEmojiTitleAndBody(t *testing.T) {
	c := testutil.Sandbox(t, testutil.Options{}).Ctx
	persona.Override(c, &persona.Voice{ID: "tester", Name: "Tester", Emoji: "👩‍💻"})
	title, body := NotificationText(c, []Item{{ID: "x", Level: Urgent, Title: "bad\u202e\x1b news"}})
	if title != "👩‍💻 Tester needs you" || body != "bad news. Run: homebot" {
		t.Errorf("NotificationText = %q, %q", title, body)
	}
	if got := notificationTitle("👩‍💻", "Need\nhelp\u202e"); got != "👩‍💻 Need help" {
		t.Errorf("one-line notification title = %q", got)
	}
}

func TestNotificationDropsInvalidPersonaEmoji(t *testing.T) {
	c := testutil.Sandbox(t, testutil.Options{}).Ctx
	persona.Override(c, &persona.Voice{ID: "tester", Name: "Tester", Emoji: "$(touch pwned)"})
	title, _ := NotificationText(c, []Item{{ID: "x", Level: Urgent, Title: "Bad"}})
	if title != "Tester needs you" {
		t.Errorf("invalid emoji title = %q", title)
	}
}

func TestGVariantStringQuotesWhatGdbusWouldMisread(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", `""`},
		{"plain", `"plain"`},
		{"it's", `"it's"`},
		{`say "hi"`, `"say \"hi\""`},
		{`C:\dir\n`, `"C:\\dir\\n"`},
		{"one\ntwo", `"one\ntwo"`},
		{"a\r\tb", `"a\r\tb"`},
		{"a\x01b\x7f", `"a\u0001b\u007f"`},
		{"é 🧪 \u200d", "\"é 🧪 \u200d\""},
		{`'\"` + "\n", `"'\\\"\n"`},
	} {
		if got := gvariantString(tc.in); got != tc.want {
			t.Errorf("gvariantString(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}
