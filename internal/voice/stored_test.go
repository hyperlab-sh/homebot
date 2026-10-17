package voice_test

import (
	"strings"
	"testing"

	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/tidy"
	"github.com/hyperlab-sh/homebot/internal/unpack"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func hostileStoredVoice() *persona.Voice {
	return &persona.Voice{ID: "hostile", Name: "Hostile", Lines: map[string][]persona.Line{
		"config.error.not_mapping":       {{Text: "{file}: PERSONA CONFIG ERROR", Lang: language.English}},
		"secrets.error.empty_recipients": {{Text: "{recipients}: PERSONA RECIPIENT ERROR", Lang: language.English}},
		"secrets.error.no_identity":      {{Text: "PERSONA NO IDENTITY {identity}", Lang: language.English}},
		"tidy.did.guard":                 {{Text: "PERSONA GUARD INSTALLED", Lang: language.English}},
		"unpack.hint.resume":             {{Text: "PERSONA RESUME HINT:", Lang: language.English}},
	}}
}

func TestStoredSayErrorIsNeutral(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	c := b.Ctx
	yes := true
	c.StdoutTerminal = &yes
	persona.Override(c, hostileStoredVoice())
	text := msg.ConfigErrorNotMapping("homebot.yml")
	sayErr := fail.Say(text)
	if got, want := sayErr.Error(), voice.Record(c, text); got != want {
		t.Fatalf("stored error = %q, want %q", got, want)
	}
	if got := voice.ErrorText(c, sayErr); got != "homebot.yml: PERSONA CONFIG ERROR" {
		t.Fatalf("printed error = %q", got)
	}

	// The empty recipients file fails the seal chore after List sees a new
	// plaintext secret. Tidy stores the error in both state and its journal.
	b.Files(map[string]string{
		"home/.home/private/common/token":   "secret",
		"home/.home/private/recipients.txt": "",
	})
	result, err := tidy.Run(c, tidy.Options{})
	if err != nil {
		t.Fatalf("tidy.Run: %v", err)
	}
	if len(result.Errors) != 1 || result.Errors[0].Step != "seal" {
		t.Fatalf("tidy errors = %+v", result.Errors)
	}
	neutral := voice.Record(c, msg.SecretsErrorEncrypt("common/token")) + "\n  " + voice.Record(c, msg.SecretsErrorEmptyRecipients())
	if got := result.Errors[0].Message; got != neutral || strings.Contains(got, "PERSONA") {
		t.Errorf("result error = %q, want neutral %q", got, neutral)
	}
	if got := result.Errors[0].Speech; !strings.Contains(got, "PERSONA RECIPIENT ERROR") {
		t.Errorf("terminal error = %q, want persona", got)
	}
	stored, err := state.Load(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.TidyErrors) != 1 || stored.TidyErrors[0].Message != neutral {
		t.Errorf("state tidy errors = %+v, want %q", stored.TidyErrors, neutral)
	}
	entries, err := state.ReadJournal(c)
	if err != nil {
		t.Fatal(err)
	}
	wantJournal := voice.Record(c, msg.TidyJournalFailed("seal", neutral))
	found := false
	for _, entry := range entries {
		if entry.Msg == wantJournal && entry.Error {
			found = true
		}
		if strings.Contains(entry.Msg, "PERSONA") {
			t.Errorf("voiced journal entry = %q", entry.Msg)
		}
	}
	if !found {
		t.Errorf("journal = %+v, missing neutral failure %q", entries, wantJournal)
	}
}

func TestRealUnlockStepStoresNeutralError(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	b.Files(map[string]string{
		"home/.home/manifests/profiles/workstation.yml": "steps:\n  - unlock\n",
		"home/.home/private/common/token.age":           "invalid",
	})
	c := b.Ctx
	yes := true
	c.StdoutTerminal = &yes
	persona.Override(c, hostileStoredVoice())
	err := unpack.Run(c, "", unpack.Options{})
	if err == nil {
		t.Fatal("unlock unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), "PERSONA") || !strings.Contains(err.Error(), "keys init") {
		t.Errorf("stored unlock error = %q", err)
	}
	if got := voice.ErrorText(c, err); !strings.Contains(got, "PERSONA NO IDENTITY") {
		t.Errorf("printed unlock error = %q", got)
	}
}

func TestTidyDidIsVoicedOnlyForTerminal(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{Git: true})
	b.WithStubs(map[string]string{"homebot": "exit 0", "gitleaks": "exit 0"})
	b.GitIn(b.Root, "add", "-A")
	b.GitIn(b.Root, "commit", "-qm", "init")
	c := b.Ctx
	yes := true
	c.StdoutTerminal = &yes
	persona.Override(c, hostileStoredVoice())
	result, err := tidy.Run(c, tidy.Options{})
	if err != nil {
		t.Fatalf("tidy.Run: %v", err)
	}
	if len(result.Did) == 0 || result.Did[0] != "PERSONA GUARD INSTALLED" {
		t.Fatalf("terminal result = %q", result.Did)
	}
	entries, err := state.ReadJournal(c)
	if err != nil {
		t.Fatal(err)
	}
	want := voice.Record(c, msg.TidyDidGuard())
	if len(entries) == 0 || entries[0].Msg != want {
		t.Fatalf("journal = %+v, want first entry %q", entries, want)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Msg, "PERSONA") {
			t.Errorf("voiced journal entry = %q", entry.Msg)
		}
	}
}

// The resume hint of a failed unpack step is voiced at print and neutral when
// stored: Error() (what a journal or state would hold) is the English, and
// ErrorText (what the terminal gets) has the persona's words for the hint and
// leaves the command under it alone. Both ways a step fails are covered: an
// internal action's error (a nested error) and a script's non-zero exit.
func TestUnpackResumeHintStoredNeutralPrintedInPersona(t *testing.T) {
	command := "\n  homebot unpack workstation --from 1"
	for _, c := range []struct {
		name  string
		files map[string]string
		first string
	}{
		{"internal action", map[string]string{
			"home/.home/manifests/profiles/workstation.yml": "steps:\n  - unlock\n",
			"home/.home/private/common/token.age":           "invalid",
		}, "Step 1 failed: "},
		{"script exit", map[string]string{
			"home/.home/manifests/profiles/workstation.yml": "steps:\n  - fail: [a]\n",
			"home/.home/manifests/actions/fail.sh":          "exit 3\n",
		}, "Step 1 (fail a) exited with 3."},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := testutil.Sandbox(t, testutil.Options{})
			b.Files(c.files)
			ctx := b.Ctx
			yes := true
			ctx.StdoutTerminal = &yes
			persona.Override(ctx, hostileStoredVoice())
			err := unpack.Run(ctx, "", unpack.Options{})
			if err == nil {
				t.Fatal("the step unexpectedly succeeded")
			}
			stored := err.Error()
			if !strings.HasPrefix(stored, c.first) || !strings.HasSuffix(stored, "\nFix it, then resume:"+command) || strings.Contains(stored, "PERSONA") {
				t.Errorf("stored error = %q, want the English hint and no persona text", stored)
			}
			printed := voice.ErrorText(ctx, err)
			if !strings.HasSuffix(printed, "\nPERSONA RESUME HINT:"+command) || strings.Contains(printed, "Fix it, then resume") {
				t.Errorf("printed error = %q, want the persona's hint over the unchanged command", printed)
			}
			// Without a persona the two forms are the same text.
			persona.Override(ctx, nil)
			if got := voice.ErrorText(ctx, err); got != stored {
				t.Errorf("neutral printed error = %q, want the stored %q", got, stored)
			}
		})
	}
}
