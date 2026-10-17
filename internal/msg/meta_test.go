package msg_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/msg/catalog"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// The generated table is the catalog: every entry, in order, with its
// placeholders.
func TestEntriesAreTheCatalog(t *testing.T) {
	cat := testutil.Catalog(t)
	got := msg.Entries()
	if len(got) != len(cat) {
		t.Fatalf("%d entries, the catalog has %d", len(got), len(cat))
	}
	for i, e := range cat {
		want := msg.Meta{ID: e.ID, Kind: e.Kind, Class: e.Class, Text: e.Text, Append: e.Append, Persona: e.Persona}
		for _, p := range e.Placeholders {
			value, _ := p.Fixed()
			want.Slots = append(want.Slots, msg.Slot{Name: p.Name, Type: p.Type, Optional: p.Optional, Value: value, Meaning: p.Meaning})
		}
		m := got[i]
		for j := range m.Slots {
			m.Slots[j].Sample = ""
		}
		if d := cmp.Diff(want, m); d != "" {
			t.Errorf("%s (-catalog +table):\n%s", e.ID, d)
		}
		if l, ok := msg.Lookup(e.ID); !ok || l.ID != e.ID {
			t.Errorf("Lookup(%q) = %v, %v", e.ID, l.ID, ok)
		}
	}
	if _, ok := msg.Lookup("no.such.entry"); ok {
		t.Error("Lookup found an id the catalog lacks")
	}
}

func TestEntriesAreACopy(t *testing.T) {
	m, _ := msg.Lookup("action.link.warning.in_way")
	m.Slots[1].Value = "changed"
	msg.Entries()[0].ID = "changed"
	if again, _ := msg.Lookup("action.link.warning.in_way"); again.Slots[1].Value != "{self} link" {
		t.Error("Lookup hands out the table's slots")
	}
	if msg.Entries()[0].ID == "changed" {
		t.Error("Entries hands out the table")
	}
}

// The samples are the generator's: what samples_gen_test.go passes the
// accessors.
func TestSlotSamples(t *testing.T) {
	m, _ := msg.Lookup("inventory.exports.ok")
	want := map[string]string{"bot": "", "name": "<name>", "ago": "3h ago"}
	for _, sl := range m.Slots {
		if w, ok := want[sl.Name]; ok && sl.Sample != w {
			t.Errorf("%s: sample %q, want %q", sl.Name, sl.Sample, w)
		}
	}
	m, _ = msg.Lookup("action.link.warning.in_way")
	if got := []string{m.Slots[0].Sample, m.Slots[1].Sample}; !cmp.Equal(got, []string{"3", "homebot link"}) {
		t.Errorf("samples %q", got)
	}
}

// tag marks each value with its placeholder's name, so a value in the wrong
// place shows.
func tag(name, value string) string { return "<" + name + "=" + value + ">" }

// values are the sample values of m's slots, typed as an accessor takes
// them; fixed slots and bot are left out.
func values(t *testing.T, m msg.Meta) map[string]any {
	t.Helper()
	out := map[string]any{}
	for _, sl := range m.Slots {
		switch {
		case sl.Value != "" || sl.Name == catalog.Bot || sl.Name == catalog.Self:
		case sl.Type == catalog.Int:
			n, err := strconv.Atoi(sl.Sample)
			if err != nil {
				t.Fatalf("%s: int sample %q: %v", m.ID, sl.Sample, err)
			}
			out[sl.Name] = n
		default:
			out[sl.Name] = sl.Sample
		}
	}
	return out
}

// Build with the generator's sample values says exactly what the typed
// accessor says with the same values, styled or not.
func TestBuild(t *testing.T) {
	built := 0
	for _, s := range samples {
		if strings.HasSuffix(s.label, " (1)") {
			continue
		}
		m, ok := msg.Lookup(s.text.ID())
		if !ok {
			t.Fatalf("%s: not in the table", s.label)
		}
		text, err := msg.Build(m.ID, values(t, m))
		if err != nil {
			t.Errorf("%s: %v", m.ID, err)
			continue
		}
		built++
		if text.ID() != m.ID || text.Persona() != m.Persona || text.Append() != m.Append {
			t.Errorf("%s: built %s", m.ID, text.ID())
		}
		for _, style := range []func(string, string) string{nil, tag} {
			if got, want := msg.Speak(msg.Neutral{}, text, style), msg.Speak(msg.Neutral{}, s.text, style); got != want {
				t.Errorf("%s: Build says %q, the accessor %q", m.ID, got, want)
			}
		}
	}
	if built != len(msg.Entries()) {
		t.Errorf("built %d of %d entries", built, len(msg.Entries()))
	}
}

func TestBuildFillsFixedAndOptionalSlots(t *testing.T) {
	text, err := msg.Build("action.link.warning.in_way", map[string]any{"n": 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := msg.Speak(msg.Neutral{}, text, nil); got != ", 2 in the way (see: homebot link)" {
		t.Errorf("fixed slot: %q", got)
	}
	// last is optional: left out, it is empty, as InboxNeverUnpackedTitle(p, "") has it.
	text, err = msg.Build("inbox.never_unpacked.title", map[string]any{"profile": "desk"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := msg.Speak(msg.Neutral{}, text, nil), msg.Speak(msg.Neutral{}, msg.InboxNeverUnpackedTitle("desk", ""), nil); got != want {
		t.Errorf("optional slot left out: %q, want %q", got, want)
	}
	// A duration takes a Text, as the accessors do, or a string.
	for _, ago := range []any{msg.FormatAgoDays(2), "2d ago"} {
		text, err := msg.Build("inventory.exports.ok", map[string]any{"name": "brew", "ago": ago})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := msg.Speak(msg.Neutral{}, text, nil), msg.Speak(msg.Neutral{}, msg.InventoryExportsOk("brew", msg.FormatAgoDays(2)), nil); got != want {
			t.Errorf("duration %T: %q, want %q", ago, got, want)
		}
	}
}

func TestBuildRefuses(t *testing.T) {
	for _, c := range []struct {
		name, id string
		values   map[string]any
		want     string
	}{
		{"an unknown id", "no.such.entry", nil, `no catalog entry "no.such.entry"`},
		{"a missing required value", "inbox.header.count", nil, `"n" is required`},
		{"an unknown name", "inbox.header.count", map[string]any{"n": 1, "zzz": 1}, `"zzz" is not a placeholder of inbox.header.count`},
		{"a value for a fixed slot", "action.link.warning.in_way", map[string]any{"n": 1, "cmd": "rm -rf"}, `"cmd" is fixed to "{self} link"`},
		{"a value for bot", "inbox.notify.title", map[string]any{"bot": "x"}, "the voice fills it"},
		{"a string for an int", "inbox.header.count", map[string]any{"n": "1"}, `"n" is a int placeholder; string does not fit it`},
		{"an int64 for an int", "inbox.header.count", map[string]any{"n": int64(1)}, "int64 does not fit"},
		{"an int for a string", "format.size", map[string]any{"value": 1, "unit": "KB"}, `"value" is a string placeholder; int does not fit it`},
		{"a Text for a path", "pack.error.missing", map[string]any{"path": msg.FormatAgoDays(1)}, "msg.Text does not fit"},
		{"an int for a duration", "inventory.exports.ok", map[string]any{"name": "x", "ago": 3}, `"ago" is a duration placeholder; int does not fit it`},
		{"nil", "format.size", map[string]any{"value": nil, "unit": "KB"}, "<nil> does not fit"},
	} {
		t.Run(c.name, func(t *testing.T) {
			text, err := msg.Build(c.id, c.values)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to say %q", err, c.want)
			}
			if text.ID() != "" {
				t.Errorf("a refused Build returned %s", text.ID())
			}
		})
	}
}
