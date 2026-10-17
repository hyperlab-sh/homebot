package voice

import (
	"testing"

	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/ui"
)

func TestPlainIsTheNeutralVoice(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	for _, c := range []struct {
		text msg.Text
		want string
	}{
		{msg.InboxHeaderCount(2), "2 things need you"},
		{msg.InboxNotifyTitle(), "homebot needs you"},
		{msg.InventoryProfileUnpacked(msg.FormatAgoDays(3)), "unpacked 3d ago"},
		{msg.PackErrorMissing("~/x"), "~/x does not exist."},
		{msg.InboxNextHint(), "  Do the first one now: homebot next"},
		{msg.Text{}, ""},
	} {
		if got := Plain(ctx, c.text); got != c.want {
			t.Errorf("%s: Plain = %q, want %q", c.text.ID(), got, c.want)
		}
	}
}

func TestRenderStylesCountsAndPaths(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	p := ui.Styles(ctx)
	for _, c := range []struct {
		text msg.Text
		want string
	}{
		{msg.InboxHeaderCount(2), p.Count.Render("2") + " things need you"},
		{msg.InboxHeaderCount(1), p.Count.Render("1") + " thing needs you"},
		{msg.PackErrorMissing("~/x"), p.Path.Render("~/x") + " does not exist."},
		{msg.FormatAgoHours(3), p.Count.Render("3") + "h ago"},
		// A duration is a value like any other: it is not styled inside.
		{msg.InventoryProfileUnpacked(msg.FormatAgoDays(3)), "unpacked 3d ago"},
		// Strings, the speaker and the append are not styled.
		{msg.InboxNotifyTitle(), "homebot needs you"},
		{msg.InboxNextHint(), "  Do the first one now: homebot next"},
		{msg.PackErrorMissing(""), " does not exist."},
	} {
		if got := Render(ctx, c.text); got != c.want {
			t.Errorf("%s: Render = %q, want %q", c.text.ID(), got, c.want)
		}
	}
	if p.Count.Render("2") == "2" || p.Path.Render("~/x") == "~/x" {
		t.Fatal("the count and path styles add nothing; the test proves nothing")
	}
}

func TestNeutralPersona(t *testing.T) {
	var p Persona = Neutral{}
	if p.Name() != "homebot" {
		t.Errorf("neutral is %q", p.Name())
	}
	if line, _, ok := p.Line("inbox.header.count"); ok || line != "" {
		t.Errorf("neutral has a line of its own: %q", line)
	}
}

// fake is a persona for testing the seam; homebot has none yet.
type fake struct {
	lang  language.Tag
	lines map[string]string
}

func (fake) Name() string { return "Tester" }
func (f fake) Line(id string) (string, language.Tag, bool) {
	l, ok := f.lines[id]
	return l, f.lang, ok
}

func TestAPersonaRewordsOnlyWhatItMay(t *testing.T) {
	p := fake{lang: language.English, lines: map[string]string{
		"inbox.header.count":       "{n:# thing|# things}. Sigh.",
		"keys.status.creating":     "A key, then. {bot} wants a passphrase.",
		"inbox.notify.title":       "{bot} has noticed {nope}",
		"inbox.header.clear":       "{bot}, all clear",
		"inbox.next_hint":          "  Fine, start here:",
		"inbox.tidy_failed.title":  "{step_name} broke {ago}. Of course.",
		"format.ago.hours":         "{h} dreary hours ago",
		"inbox.header.urgent":      " (many urgent)",
		"inbox.helper_stale.title": "{nope",
	}}
	cases := []struct {
		text msg.Text
		want string
	}{
		{msg.InboxHeaderCount(1), "1 thing. Sigh."},
		{msg.InboxHeaderCount(4), "4 things. Sigh."},
		// {bot} is the persona's name where the entry declares it.
		{msg.KeysStatusCreating(), "A key, then. Tester wants a passphrase."},
		// The append stays after a persona's line.
		{msg.InboxNextHint(), "  Fine, start here: homebot next"},
		// format.ago.* and the urgency suffix are not the persona's to change.
		{msg.InboxTidyFailedTitle("tidy", msg.FormatAgoHours(2)), "tidy broke 2h ago. Of course."},
		{msg.InboxHeaderUrgent(2), " (2 urgent)"},
		// A broken line falls back to the catalog.
		{msg.InboxHelperStaleTitle(msg.FormatAgoMinutes(5)), "The background helper last ran 5m ago"},
		// So does a line with a placeholder the entry does not declare,
		// {bot} included: the catalog's text, with the persona's name.
		{msg.InboxNotifyTitle(), "Tester needs you"},
		{msg.InboxHeaderClear(), "nothing needs you. Home is in order."},
	}
	for _, c := range cases {
		if got := render(p, c.text); got != c.want {
			t.Errorf("%s: %q, want %q", c.text.ID(), got, c.want)
		}
	}
}

func TestPluralsFollowThePersonaLanguage(t *testing.T) {
	p := fake{lang: language.Polish, lines: map[string]string{
		"inbox.header.count": "{n:one=# rzecz|few=# rzeczy|other=# rzeczy!}",
		"inbox.more":         "{n:# a|# b}",
	}}
	for n, want := range map[int]string{1: "1 rzecz", 3: "3 rzeczy", 5: "5 rzeczy!", 22: "22 rzeczy"} {
		if got := render(p, msg.InboxHeaderCount(n)); got != want {
			t.Errorf("n=%d: %q, want %q", n, got, want)
		}
	}
	if got := render(p, msg.InboxMore(5)); got != "5 b homebot --all" {
		t.Errorf("shorthand forms in Polish: %q", got)
	}
}
