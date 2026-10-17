package state_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sandbox(t *testing.T) *app.Context {
	t.Helper()
	return testutil.Sandbox(t, testutil.Options{}).Ctx
}

func put(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustUpdate(t *testing.T, ctx *app.Context, fn func(*state.State)) {
	t.Helper()
	if err := state.Update(ctx, fn); err != nil {
		t.Fatal(err)
	}
}

// The sequence of updates the fixture generator ran in TypeScript, replayed in
// Go, must give the same bytes after each step that has a fixture.
func TestStateUpdatesMatchTypeScript(t *testing.T) {
	ctx := sandbox(t)
	put(t, ctx.StateDir, "state.json", []byte(`{
  "futureThing": { "a": [1, 2], "b": "<x>&" },
  "lastSeen": 5
}
`))
	mustUpdate(t, ctx, func(s *state.State) {
		s.LastTidy = 1767225601000
		s.TidyErrors = []state.TidyError{{Step: "links", Message: "boom <1> &   é", At: 1767225602000}}
	})
	mustUpdate(t, ctx, func(s *state.State) {
		s.Exports.Set("zed", state.ExportResult{At: 1767225603000, OK: true})
		s.Exports.Set("alpha", state.ExportResult{At: 1767225604000, OK: false, Error: "no \"tool\"\nfound"})
	})
	mustUpdate(t, ctx, func(s *state.State) { s.Notify = &state.Notify{Key: "k", At: 1767225605000} })
	mustUpdate(t, ctx, func(s *state.State) { s.Blocked = &state.Blocked{Reason: "plaintext", At: 1767225606000} })
	mustUpdate(t, ctx, func(s *state.State) {
		s.Unpack = &state.Unpack{Profile: "workstation", Hash: "abc123", At: 1767225607000}
	})
	mustUpdate(t, ctx, func(s *state.State) { s.LastSync = 1767225608000 })
	mustUpdate(t, ctx, func(s *state.State) { s.LastAutosave = 1767225609000 })
	if diff := cmp.Diff(string(fixture(t, "state-full.json")), read(t, ctx.StateDir, "state.json")); diff != "" {
		t.Fatalf("state-full.json (-ts +go):\n%s", diff)
	}

	mustUpdate(t, ctx, func(s *state.State) { s.Notify = nil })
	mustUpdate(t, ctx, func(s *state.State) {
		s.Notify = &state.Notify{Key: "k2", At: 1767225610000}
		s.Blocked = nil
	})
	if diff := cmp.Diff(string(fixture(t, "state-readd.json")), read(t, ctx.StateDir, "state.json")); diff != "" {
		t.Fatalf("state-readd.json (-ts +go):\n%s", diff)
	}
}

func TestStateFromEmptyDirectory(t *testing.T) {
	ctx := sandbox(t)
	s, err := state.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Exports.Len() != 0 || len(s.TidyErrors) != 0 || s.LastSeen != 0 {
		t.Fatalf("not empty: %+v", s)
	}
	mustUpdate(t, ctx, func(s *state.State) { s.LastSeen = 1767225611000 })
	if diff := cmp.Diff(string(fixture(t, "state-minimal.json")), read(t, ctx.StateDir, "state.json")); diff != "" {
		t.Fatalf("(-ts +go):\n%s", diff)
	}
}

func TestEmptyUpdateWritesDefaults(t *testing.T) {
	ctx := sandbox(t)
	mustUpdate(t, ctx, func(*state.State) {})
	want := "{\n  \"exports\": {},\n  \"tidyErrors\": []\n}\n"
	if got := read(t, ctx.StateDir, "state.json"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// Go rewriting a TypeScript file with no change gives the same bytes, for
// every state fixture.
func TestNoopRewriteIsByteIdentical(t *testing.T) {
	for _, name := range []string{"state-full.json", "state-readd.json", "state-minimal.json", "state-noop-empty.json"} {
		t.Run(name, func(t *testing.T) {
			ctx := sandbox(t)
			put(t, ctx.StateDir, "state.json", fixture(t, name))
			mustUpdate(t, ctx, func(*state.State) {})
			if diff := cmp.Diff(string(fixture(t, name)), read(t, ctx.StateDir, "state.json")); diff != "" {
				t.Fatalf("(-ts +go):\n%s", diff)
			}
		})
	}
}

func TestLoadReadsEveryField(t *testing.T) {
	ctx := sandbox(t)
	put(t, ctx.StateDir, "state.json", fixture(t, "state-full.json"))
	s, err := state.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.LastTidy != 1767225601000 || s.LastSeen != 5 || s.LastSync != 1767225608000 || s.LastAutosave != 1767225609000 {
		t.Fatalf("timestamps: %+v", s)
	}
	if s.Unpack == nil || s.Unpack.Profile != "workstation" || s.Unpack.Hash != "abc123" {
		t.Fatalf("unpack: %+v", s.Unpack)
	}
	if s.Notify == nil || s.Notify.Key != "k" || s.Blocked == nil || s.Blocked.Reason != "plaintext" {
		t.Fatalf("notify/blocked: %+v %+v", s.Notify, s.Blocked)
	}
	if got := s.Exports.Keys(); !cmp.Equal(got, []string{"zed", "alpha"}) {
		t.Fatalf("export order: %v", got)
	}
	if r, _ := s.Exports.Get("alpha"); r.OK || r.Error != "no \"tool\"\nfound" {
		t.Fatalf("alpha: %+v", r)
	}
	if len(s.TidyErrors) != 1 || s.TidyErrors[0].Step != "links" {
		t.Fatalf("tidy errors: %+v", s.TidyErrors)
	}
}

func TestUnknownFieldsSurviveEditsAndKeepPlace(t *testing.T) {
	ctx := sandbox(t)
	put(t, ctx.StateDir, "state.json", []byte(`{"zzz":{"deep":[1,{"x":null}]},"lastSeen":7,"aaa":"<&>"}`))
	mustUpdate(t, ctx, func(s *state.State) { s.LastSeen = 9 })
	want := "{\n  \"exports\": {},\n  \"tidyErrors\": [],\n  \"zzz\": {\n    \"deep\": [\n      1,\n      {\n        \"x\": null\n      }\n    ]\n  },\n  \"lastSeen\": 9,\n  \"aaa\": \"<&>\"\n}\n"
	if got := read(t, ctx.StateDir, "state.json"); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUpdateFieldsRoundTripAndUnknownSiblingSurvives(t *testing.T) {
	ctx := sandbox(t)
	put(t, ctx.StateDir, "state.json", []byte(`{"future":{"keep":true},"updateCheck":{"at":100,"latest":"0.1.1","firstSeen":90,"firstAttempt":91,"lastAttempt":92},"updateFailed":{"version":"0.1.1","at":92,"reason":"verify","error":"bad signature"}}`))
	s, err := state.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.UpdateCheck == nil || s.UpdateCheck.FirstSeen != 90 || s.UpdateCheck.FirstAttempt != 91 || s.UpdateCheck.LastAttempt != 92 || s.UpdateFailed == nil || s.UpdateFailed.Reason != "verify" {
		t.Fatalf("update fields did not load: %+v", s)
	}
	mustUpdate(t, ctx, func(s *state.State) { s.UpdateCheck.At = 200 })
	got := read(t, ctx.StateDir, "state.json")
	for _, fragment := range []string{`"future"`, `"firstSeen": 90`, `"firstAttempt": 91`, `"lastAttempt": 92`, `"reason": "verify"`, `"at": 200`} {
		if !strings.Contains(got, fragment) {
			t.Errorf("missing %s in %s", fragment, got)
		}
	}
}

func TestClearedFieldsAreOmitted(t *testing.T) {
	ctx := sandbox(t)
	put(t, ctx.StateDir, "state.json", fixture(t, "state-full.json"))
	mustUpdate(t, ctx, func(s *state.State) {
		s.Blocked = nil
		s.Notify = nil
		s.Unpack = nil
		s.LastTidy = 0
	})
	got := read(t, ctx.StateDir, "state.json")
	for _, k := range []string{"blocked", "notify", "unpack", "lastTidy"} {
		if strings.Contains(got, `"`+k+`"`) {
			t.Fatalf("%s still written:\n%s", k, got)
		}
	}
}

func TestExportsDeleteAndReAdd(t *testing.T) {
	var m state.OrderedMap[int]
	m.Set("a", 1)
	m.Set("b", 2)
	m.Set("a", 3)
	m.Delete("a")
	m.Delete("missing")
	m.Set("a", 4)
	if got := m.Keys(); !cmp.Equal(got, []string{"b", "a"}) {
		t.Fatalf("keys %v", got)
	}
}

func TestInvalidStateFileIsAnErrorThatLeavesItAlone(t *testing.T) {
	ctx := sandbox(t)
	put(t, ctx.StateDir, "state.json", []byte("{nope"))
	if _, err := state.Load(ctx); err == nil || !strings.Contains(err.Error(), "safe to delete") {
		t.Fatalf("Load error: %v", err)
	}
	called := false
	if err := state.Update(ctx, func(*state.State) { called = true }); err == nil || called {
		t.Fatalf("Update on a broken file: err=%v called=%v", err, called)
	}
	if got := read(t, ctx.StateDir, "state.json"); got != "{nope" {
		t.Fatalf("file changed: %q", got)
	}
}

func TestFailedWriteKeepsTheOldFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	ctx := sandbox(t)
	mustUpdate(t, ctx, func(s *state.State) { s.LastSeen = 1 })
	before := read(t, ctx.StateDir, "state.json")
	if err := os.Chmod(ctx.StateDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ctx.StateDir, 0o755) })
	if err := state.Update(ctx, func(s *state.State) { s.LastSeen = 2 }); err == nil {
		t.Fatal("Update succeeded in a read-only directory")
	}
	if got := read(t, ctx.StateDir, "state.json"); got != before {
		t.Fatalf("old file changed:\n%s", got)
	}
	entries, _ := os.ReadDir(ctx.StateDir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestJournalMatchesTypeScript(t *testing.T) {
	ctx := sandbox(t)
	for _, e := range []state.Entry{
		{At: 1767225612000, Msg: "tidy: fixed 2 links"},
		{At: 1767225613000, Msg: "save failed <bad> & \"quoted\"   é 🚚", Error: true},
		{At: 1767225614000, Msg: "plain"},
	} {
		if err := state.Journal(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	if diff := cmp.Diff(string(fixture(t, "journal.jsonl")), read(t, ctx.StateDir, "journal.jsonl")); diff != "" {
		t.Fatalf("(-ts +go):\n%s", diff)
	}
	got, err := state.ReadJournal(ctx)
	if err != nil || len(got) != 3 || !got[1].Error || got[0].Error || got[1].Msg != "save failed <bad> & \"quoted\"   é 🚚" {
		t.Fatalf("ReadJournal: %+v %v", got, err)
	}
}

func TestJournalStampsTheClock(t *testing.T) {
	ctx := sandbox(t)
	if err := state.Journal(ctx, state.Entry{Msg: "now"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, ctx.StateDir, "journal.jsonl"); got != "{\"at\":1767225600000,\"msg\":\"now\"}\n" {
		t.Fatalf("got %q", got)
	}
}

func TestReadJournalMissingAndTorn(t *testing.T) {
	ctx := sandbox(t)
	if got, err := state.ReadJournal(ctx); err != nil || len(got) != 0 {
		t.Fatalf("missing: %v %v", got, err)
	}
	put(t, ctx.StateDir, "journal.jsonl", []byte("{\"at\":1,\"msg\":\"a\"}\n{\"at\":2,\"ms\n\n{\"at\":3,\"msg\":\"c\"}\n{\"at\":4,\"msg\":\"torn"))
	got, err := state.ReadJournal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := []state.Entry{{At: 1, Msg: "a"}, {At: 3, Msg: "c"}}; !cmp.Equal(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestLinksRoundTrip(t *testing.T) {
	for name, want := range map[string][]string{"links.json": {"/h/.config/a", "/h/.zshrc"}, "links-empty.json": {}} {
		ctx := sandbox(t)
		put(t, ctx.StateDir, "links.json", fixture(t, name))
		l, err := state.LoadLinks(ctx)
		if err != nil || (len(l.Targets) != len(want) || (len(want) > 0 && !cmp.Equal(l.Targets, want))) {
			t.Fatalf("%s: %+v %v", name, l, err)
		}
		_ = os.Remove(filepath.Join(ctx.StateDir, "links.json"))
		if err := l.Save(ctx); err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(string(fixture(t, name)), read(t, ctx.StateDir, "links.json")); diff != "" {
			t.Fatalf("%s (-ts +go):\n%s", name, diff)
		}
	}
}

func TestLinksMissingAndNil(t *testing.T) {
	ctx := sandbox(t)
	l, err := state.LoadLinks(ctx)
	if err != nil || len(l.Targets) != 0 {
		t.Fatalf("%+v %v", l, err)
	}
	if err := l.Save(ctx); err != nil {
		t.Fatal(err)
	}
	if got := read(t, ctx.StateDir, "links.json"); got != "[]\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSecretsRoundTripKeepsOrder(t *testing.T) {
	ctx := sandbox(t)
	put(t, ctx.StateDir, "secrets.json", fixture(t, "secrets.json"))
	s, err := state.LoadSecrets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Records.Keys(); !cmp.Equal(got, []string{"zeta/key", "alpha.env"}) {
		t.Fatalf("order %v", got)
	}
	if r, ok := s.Records.Get("alpha.env"); !ok || r.Plain != strings.Repeat("c", 64) || r.Age != strings.Repeat("d", 64) {
		t.Fatalf("record %+v", r)
	}
	_ = os.Remove(filepath.Join(ctx.StateDir, "secrets.json"))
	if err := s.Save(ctx); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(string(fixture(t, "secrets.json")), read(t, ctx.StateDir, "secrets.json")); diff != "" {
		t.Fatalf("(-ts +go):\n%s", diff)
	}
}

func TestSecretsEmptySavesBraces(t *testing.T) {
	ctx := sandbox(t)
	s, err := state.LoadSecrets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx); err != nil {
		t.Fatal(err)
	}
	if got := read(t, ctx.StateDir, "secrets.json"); got != "{}\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSnoozesRoundTrip(t *testing.T) {
	ctx := sandbox(t)
	put(t, ctx.StateDir, "snooze.json", fixture(t, "snooze.json"))
	s, err := state.LoadSnoozes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Until.Get("a-first"); v != 1767830416000 {
		t.Fatalf("a-first = %d", v)
	}
	_ = os.Remove(state.SnoozePath(ctx))
	if err := s.Save(ctx); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(string(fixture(t, "snooze.json")), read(t, ctx.StateDir, "snooze.json")); diff != "" {
		t.Fatalf("(-ts +go):\n%s", diff)
	}
}

func TestSnoozeAddsDaysToTheClock(t *testing.T) {
	ctx := sandbox(t)
	put(t, ctx.StateDir, "snooze.json", []byte(`{"old": 5}`))
	if err := state.Snooze(ctx, "link:x", 7); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"old\": 5,\n  \"link:x\": 1767830400000\n}\n"
	if got := read(t, ctx.StateDir, "snooze.json"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCacheFilesMatchTypeScript(t *testing.T) {
	cases := []struct {
		name string
		line string
		n    int
		urg  bool
	}{
		{"none", "", 0, false},
		{"one", "🚚 homebot: 1 thing needs you, run: homebot", 1, false},
		{"three-urgent", "🚚 homebot: 3 things need you (2 urgent), run: homebot", 3, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := sandbox(t)
			emoji, line := "", c.line
			if strings.HasPrefix(line, "🚚 ") {
				emoji, line = "🚚", strings.TrimPrefix(line, "🚚 ")
			}
			if err := state.WriteNudge(ctx, emoji, line); err != nil {
				t.Fatal(err)
			}
			if err := state.WriteCount(ctx, c.n, c.urg); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(string(fixture(t, "nudge-"+c.name)), read(t, ctx.CacheDir, "nudge")); diff != "" {
				t.Fatalf("nudge (-ts +go):\n%s", diff)
			}
			if diff := cmp.Diff(string(fixture(t, "count-"+c.name)), read(t, ctx.CacheDir, "count")); diff != "" {
				t.Fatalf("count (-ts +go):\n%s", diff)
			}
		})
	}
}

func TestCountEmptiesWhenNothingNeedsYou(t *testing.T) {
	ctx := sandbox(t)
	if err := state.WriteCount(ctx, 4, true); err != nil {
		t.Fatal(err)
	}
	if got := read(t, ctx.CacheDir, "count"); got != "4!\n" {
		t.Fatalf("got %q", got)
	}
	if err := state.WriteCount(ctx, 0, true); err != nil {
		t.Fatal(err)
	}
	if got := read(t, ctx.CacheDir, "count"); got != "" {
		t.Fatalf("got %q", got)
	}
}

// A corrupt state.json is reported in the catalog's words, with the path
// shortened to ~, like every other path homebot shows.
func TestLoadCorruptStateNamesTheFileWithATilde(t *testing.T) {
	ctx := sandbox(t)
	put(t, ctx.StateDir, "state.json", []byte("{not json"))
	_, err := state.Load(ctx)
	if err == nil {
		t.Fatal("want an error")
	}
	rel, relErr := filepath.Rel(ctx.Home, filepath.Join(ctx.StateDir, "state.json"))
	if relErr != nil || strings.HasPrefix(rel, "..") {
		t.Skipf("the sandbox's state dir is outside its home: %s", ctx.StateDir)
	}
	want := "~/" + filepath.ToSlash(rel) + " is not valid JSON. Fix or delete it (it is machine state, safe to delete)."
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
	if strings.Contains(err.Error(), ctx.Home) {
		t.Errorf("the absolute home leaks: %q", err)
	}
}
