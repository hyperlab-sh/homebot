package voice

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func fixtureVoice() *persona.Voice {
	return &persona.Voice{ID: "tester", Name: "Tester", Emoji: "🧪", Lines: map[string][]persona.Line{
		"inbox.header.count": {
			{Text: "{n:# note|# notes} A", Lang: language.English},
			{Text: "{n:# note|# notes} B", Lang: language.English},
			{Text: "{n:# note|# notes} C", Lang: language.English},
		},
	}}
}

func TestVoiceSelectionAndNeutralRecord(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	v := fixtureVoice()
	persona.Override(ctx, v)
	yes, no := true, false
	ctx.StdoutTerminal = &yes
	text := msg.InboxHeaderCount(2)
	first := Plain(ctx, text)
	if !strings.HasSuffix(first, " notes A") && !strings.HasSuffix(first, " notes B") && !strings.HasSuffix(first, " notes C") {
		t.Fatalf("voiced result = %q", first)
	}
	if got := Plain(ctx, text); got != first {
		t.Fatalf("one id changed within a run: %q", got)
	}
	if got := Record(ctx, text); got != "2 things need you" {
		t.Errorf("Record = %q", got)
	}
	ctx.StdoutTerminal = &no
	if got := Plain(ctx, text); got != "2 things need you" {
		t.Errorf("piped output = %q", got)
	}
	ctx.ForceVoice = true
	if got := Plain(ctx, text); !strings.Contains(got, "notes") {
		t.Errorf("forced voice = %q", got)
	}
	ctx.Plain = true
	if got := Plain(ctx, text); got != "2 things need you" {
		t.Errorf("plain output = %q", got)
	}
}

func TestRotationNeverRepeatsLastAndPersistsOnce(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	path := filepath.Join(ctx.CacheDir, "voice.json")
	if err := os.MkdirAll(ctx.CacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"tester":{"inbox.header.count":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	persona.Override(ctx, fixtureVoice())
	yes := true
	ctx.StdoutTerminal = &yes
	first := Plain(ctx, msg.InboxHeaderCount(1))
	if strings.HasSuffix(first, " B") {
		t.Fatalf("repeated last variant: %q", first)
	}
	if got := Plain(ctx, msg.InboxHeaderCount(1)); got != first {
		t.Errorf("changed in process: %q → %q", first, got)
	}
	SaveChoices(ctx)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var recorded map[string]map[string]int
	if err := json.Unmarshal(data, &recorded); err != nil {
		t.Fatal(err)
	}
	if got := recorded["tester"]["inbox.header.count"]; got == 1 || got < 0 || got > 2 {
		t.Errorf("saved index = %d", got)
	}
	if mode := fileMode(t, path); mode != 0o600 {
		t.Errorf("mode = %o", mode)
	}
	SaveChoices(ctx)
	if got, err := os.ReadFile(path); err != nil || string(got) != string(data) {
		t.Errorf("second save changed file: %q, %v", got, err)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestNudgeStableAcrossKeysWithoutTouchingRotation(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	persona.Override(ctx, fixtureVoice())
	text := msg.InboxHeaderCount(3)
	first := Nudge(ctx, text, "item-1|1")
	for i := 0; i < 20; i++ {
		if got := Nudge(ctx, text, "item-1|1"); got != first {
			t.Fatalf("stable pick changed: %q → %q", first, got)
		}
	}
	if choices(ctx).dirty {
		t.Error("stable picks changed random rotation")
	}
	// The key is what the pick follows: some other key picks another variant,
	// and each key keeps its own pick.
	picks := map[string]string{}
	for i := 0; i < 30; i++ {
		key := fmt.Sprintf("item-%d|1", i)
		got := Nudge(ctx, text, key)
		if again := Nudge(ctx, text, key); again != got {
			t.Fatalf("key %q changed: %q → %q", key, got, again)
		}
		picks[got] = key
	}
	if len(picks) < 2 {
		t.Errorf("30 keys all picked %v: the key does not choose", picks)
	}
	if choices(ctx).dirty {
		t.Error("picking by key changed random rotation")
	}
	ctx.Plain = true
	if got := Nudge(ctx, text, "item-1|1"); got != "3 things need you" {
		t.Errorf("plain nudge = %q", got)
	}
}

func TestSeededChoiceIsDeterministicAndCacheFailureIsHarmless(t *testing.T) {
	pick := func(t *testing.T) string {
		t.Helper()
		ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
		persona.Override(ctx, fixtureVoice())
		yes := true
		ctx.StdoutTerminal = &yes
		return Plain(ctx, msg.InboxHeaderCount(2))
	}
	if a, b := pick(t), pick(t); a != b {
		t.Errorf("HOMEBOT_TEST_SEED produced different picks: %q, %q", a, b)
	}

	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	persona.Override(ctx, fixtureVoice())
	yes := true
	ctx.StdoutTerminal = &yes
	ctx.CacheDir = filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(ctx.CacheDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Plain(ctx, msg.InboxHeaderCount(2)); !strings.Contains(got, "notes") {
		t.Errorf("unreadable cache prevented speaking: %q", got)
	}
	SaveChoices(ctx)
}

// Fifty goroutines ask for the same id at once, round after round, each round
// on a fresh Context with its own seed: every one must get the round's single
// choice, and the history must name it. Run it under -race; the repeated
// rounds and the shared start line are what give the detector something to
// see.
func TestRotationIsConcurrent(t *testing.T) {
	for round := 0; round < 25; round++ {
		ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
		ctx.Rand = rand.New(rand.NewPCG(uint64(round), 7))
		persona.Override(ctx, fixtureVoice())
		yes := true
		ctx.StdoutTerminal = &yes
		var ready, wg sync.WaitGroup
		start := make(chan struct{})
		results := make(chan string, 50)
		ready.Add(50)
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ready.Done()
				<-start
				results <- Plain(ctx, msg.InboxHeaderCount(4))
			}()
		}
		ready.Wait()
		close(start)
		wg.Wait()
		close(results)
		first := ""
		for got := range results {
			if first == "" {
				first = got
			}
			if got != first {
				t.Fatalf("round %d: concurrent choice changed: %q, %q", round, first, got)
			}
		}
		state := choices(ctx)
		state.mu.Lock()
		picked, last := state.picked["tester"]["inbox.header.count"], state.last["tester"]["inbox.header.count"]
		state.mu.Unlock()
		if want := "4 notes " + string(rune('A'+picked)); first != want || last != picked {
			t.Fatalf("round %d: said %q, picked %d, history %d", round, first, picked, last)
		}
	}
}

// A run that says nothing in a persona never reads the history file, and one
// that does reads it once: a change to the file after the first pick is not
// seen.
func TestHistoryIsReadOnceAndOnlyWhenPicking(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	path := filepath.Join(ctx.CacheDir, "voice.json")
	if err := os.MkdirAll(ctx.CacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"tester":{"inbox.header.count":1}}`)
	persona.Override(ctx, fixtureVoice())
	yes := true
	ctx.StdoutTerminal = &yes

	// Neutral text, Record, a plain run and the end-of-run save: no read.
	Plain(ctx, msg.ConfigErrorNotMapping("homebot.yml"))
	Record(ctx, msg.InboxHeaderCount(1))
	SaveChoices(ctx)
	if choices(ctx).loaded {
		t.Fatal("the history was read although nothing was picked")
	}
	if got, _ := os.ReadFile(path); string(got) != `{"tester":{"inbox.header.count":1}}` {
		t.Errorf("an unpicked run rewrote the history: %q", got)
	}

	first := Plain(ctx, msg.InboxHeaderCount(1))
	if !choices(ctx).loaded {
		t.Fatal("the first pick did not read the history")
	}
	if strings.HasSuffix(first, " B") {
		t.Errorf("history ignored: repeated variant B: %q", first)
	}
	// Replace the file under the running process. Saving must keep this
	// process's reading of the old file and its pick; whether a later
	// writer's "other" entry survives is best effort (the plan reads once
	// per process and writes once at exit), so it is not asserted.
	write(`{"other":{"x":1}}`)
	SaveChoices(ctx)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var recorded map[string]map[string]int
	if err := json.Unmarshal(data, &recorded); err != nil {
		t.Fatal(err)
	}
	if len(recorded["tester"]) != 1 {
		t.Errorf("saved history = %v, want this run's reading of the old file plus its pick", recorded)
	}
}

// brokenVoice has a middle variant the renderer refuses: it names no {n}, and
// the entry requires it.
func brokenVoice() *persona.Voice {
	return &persona.Voice{ID: "tester", Name: "Tester", Lines: map[string][]persona.Line{
		"inbox.header.count": {
			{Text: "{n:# note|# notes} A", Lang: language.English},
			{Text: "no count here", Lang: language.English},
			{Text: "{n:# note|# notes} C", Lang: language.English},
		},
	}}
}

// A variant that fails validation is never said, never picked and never
// counts as used, so the history keeps naming what was said.
func TestRotationNeverRecordsARejectedVariant(t *testing.T) {
	if err := msg.CheckLine("inbox.header.count", "no count here"); err == nil {
		t.Fatal("the fixture's broken variant is valid; the test would prove nothing")
	}
	for seed := uint64(0); seed < 40; seed++ {
		ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
		ctx.Rand = rand.New(rand.NewPCG(seed, 1))
		persona.Override(ctx, brokenVoice())
		yes := true
		ctx.StdoutTerminal = &yes
		got := Plain(ctx, msg.InboxHeaderCount(2))
		if got != "2 notes A" && got != "2 notes C" {
			t.Fatalf("seed %d said %q", seed, got)
		}
		SaveChoices(ctx)
		data, err := os.ReadFile(filepath.Join(ctx.CacheDir, "voice.json"))
		if err != nil {
			t.Fatal(err)
		}
		var recorded map[string]map[string]int
		if err := json.Unmarshal(data, &recorded); err != nil {
			t.Fatal(err)
		}
		if want := map[string]int{"A": 0, "C": 2}[got[len("2 notes "):]]; recorded["tester"]["inbox.header.count"] != want {
			t.Fatalf("seed %d said %q, history %v", seed, got, recorded)
		}
	}
}

// With nothing usable to say, the catalog speaks and the history is untouched
// (not even created).
func TestRotationWithNoUsableVariantRecordsNothing(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	persona.Override(ctx, &persona.Voice{ID: "tester", Name: "Tester", Lines: map[string][]persona.Line{
		"inbox.header.count": {
			{Text: "no count here", Lang: language.English},
			{Text: "none here either", Lang: language.English},
		},
	}})
	yes := true
	ctx.StdoutTerminal = &yes
	for i := 0; i < 5; i++ {
		if got := Plain(ctx, msg.InboxHeaderCount(2)); got != "2 things need you" {
			t.Fatalf("got %q, want the catalog's line", got)
		}
	}
	if choices(ctx).dirty {
		t.Error("a rejected variant marked the history dirty")
	}
	SaveChoices(ctx)
	if _, err := os.Stat(filepath.Join(ctx.CacheDir, "voice.json")); !os.IsNotExist(err) {
		t.Errorf("history written for rejected variants: %v", err)
	}
}
