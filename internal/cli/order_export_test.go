package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/exports"
	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/tidy"
	"github.com/hyperlab-sh/homebot/internal/ui"
)

const warning = "The temporary folder /r/manifests/.homebot-export-1 could not be removed. It is safe to delete."

// exportResults is one export that went through with a warning, one that
// failed with a warning, and one without.
var exportResults = []exports.Result{
	{Name: "a", Out: "manifests/a.txt", OK: true, Changed: true, Warning: warning},
	{Name: "b", Out: "manifests/b.txt", Error: "exited with 1", Warning: warning + " (b)"},
	{Name: "c", Out: "manifests/c.txt", OK: true},
}

// oneStream is a Context whose stdout and stderr are the same buffer, so the
// order of what both print shows.
func oneStream(t *testing.T) (*app.Context, *bytes.Buffer) {
	t.Helper()
	work := t.TempDir()
	var out bytes.Buffer
	c, err := app.New(app.Options{Env: app.NewEnv(testutil.SandboxEnv(work)), Cwd: work, OS: app.Linux, Stdout: &out, Stderr: &out})
	if err != nil {
		t.Fatal(err)
	}
	return c, &out
}

// Each warning comes right under its export's line, whether the export went
// through or failed.
func TestExportWarningsComeUnderTheirExportsLine(t *testing.T) {
	c, out := oneStream(t)
	printExportResults(c, exportResults)
	want := "updated   manifests/a.txt\n" +
		warning + "\n" +
		"failed    b: exited with 1\n" +
		warning + " (b)\n" +
		"unchanged manifests/c.txt\n"
	if got := out.String(); got != want {
		t.Errorf("output\n got %q\nwant %q", got, want)
	}
}

// A failing export is run for real under a persona that rewords the failure.
// What RunExports stores (the result's Error and state.json) is the English;
// what printExportResults puts on the terminal is the persona's line.
func TestExportFailureKeepsStoredErrorNeutralAndPrintsPersona(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{Git: true})
	sb.Files(map[string]string{"home/.home/homebot.yml": "exports:\n  - name: demo\n    out: manifests/demo.txt\n    run: \"echo broken >&2; exit 7\"\n"})
	cfg, err := config.Load(sb.Root)
	if err != nil {
		t.Fatal(err)
	}
	c := sb.Ctx
	c.Config = cfg
	yes := true
	c.StdoutTerminal = &yes
	persona.Override(c, &persona.Voice{ID: "hostile", Name: "Hostile", Lines: map[string][]persona.Line{
		"export.error.exit": {{Text: "PERSONA EXIT {code}{stderr}", Lang: language.English}},
	}})

	results, err := exports.RunExports(c, exports.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].OK {
		t.Fatalf("results = %+v, want one failure", results)
	}
	const neutral = "exited with 7: broken"
	if got := results[0].Error; got != neutral {
		t.Errorf("stored export error = %q, want %q", got, neutral)
	}
	st, err := state.Load(c)
	if err != nil {
		t.Fatal(err)
	}
	if stored, ok := st.Exports.Get("demo"); !ok || stored.Error != neutral {
		t.Errorf("state.json export = %+v, want error %q", stored, neutral)
	}
	printExportResults(c, results)
	if got := sb.Stdout.String(); !strings.Contains(got, "PERSONA EXIT 7: broken") || strings.Contains(got, neutral) {
		t.Errorf("printed export error = %q, want the persona's line", got)
	}
	// Printing does not touch what was stored.
	if results[0].Error != neutral {
		t.Errorf("printing changed the stored error to %q", results[0].Error)
	}
}

// The warnings go to stderr, the export lines to stdout; with colour, a
// warning is in the warning style of stderr's palette.
func TestExportWarningsGoToStderrInTheWarningStyle(t *testing.T) {
	for _, tc := range []struct {
		name   string
		env    map[string]string
		styled bool
	}{
		{"a pipe", nil, false},
		{"forced", map[string]string{"CLICOLOR_FORCE": "1"}, true},
		{"forced, NO_COLOR", map[string]string{"CLICOLOR_FORCE": "1", "NO_COLOR": "1"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, stdout, stderr := coloured(t, tc.env)
			printExportResults(c, exportResults)
			if strings.Contains(stdout.String(), warning) {
				t.Errorf("stdout %q holds a warning", stdout.String())
			}
			todo := ui.ErrStyles(c).Todo
			want := todo.Render(warning) + "\n" + todo.Render(warning+" (b)") + "\n"
			if !tc.styled {
				want = warning + "\n" + warning + " (b)\n"
			}
			got := stderr.String()
			if !tc.styled {
				// NO_COLOR keeps the writer's bare resets (ESC[m), never a colour.
				if strings.Contains(got, "\x1b[38") {
					t.Errorf("stderr %q, want no colour", got)
				}
				got = strings.ReplaceAll(got, "\x1b[m", "")
			}
			if got != want {
				t.Errorf("stderr\n got %q\nwant %q", got, want)
			}
			if tc.styled && !strings.HasPrefix(got, "\x1b[38") {
				t.Errorf("stderr %q, want the warning coloured", got)
			}
		})
	}
}

func TestNoExportsSaysSoAndWarnsNothing(t *testing.T) {
	c, out := oneStream(t)
	printExportResults(c, nil)
	if got := out.String(); strings.Count(got, "\n") != 1 || strings.Contains(got, "could not be removed") {
		t.Errorf("output %q, want the one line that says there are no exports", got)
	}
}

// When a later export fails hard, the warnings of the exports before it are
// still printed, and the error is returned to be reported.
func TestExportWarningsBeforeAHardFailureArePrinted(t *testing.T) {
	c, stdout, stderr := coloured(t, nil)
	boom := errors.New("boom")
	if err := printExportOutcome(c, exportResults[:2], boom); !errors.Is(err, boom) {
		t.Fatalf("printExportOutcome = %v, want the error", err)
	}
	if want := warning + "\n" + warning + " (b)\n"; stderr.String() != want {
		t.Errorf("stderr %q, want %q", stderr.String(), want)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout %q, want nothing", stdout.String())
	}
}

func TestExportOutcomeWithoutAnErrorPrintsTheResults(t *testing.T) {
	c, out := oneStream(t)
	if err := printExportOutcome(c, exportResults[2:], nil); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "unchanged manifests/c.txt\n" {
		t.Errorf("output %q", got)
	}
}

// homebot tidy prints the export warnings tidy met, to stderr in the warning
// style, besides what it did and what failed.
func TestTidyPrintsItsWarnings(t *testing.T) {
	r := tidy.Result{
		Ran:      true,
		Did:      []string{"refreshed manifests/a.txt"},
		Errors:   []tidy.Failure{{TidyError: state.TidyError{Step: "seal", Message: "boom"}}},
		Warnings: []string{warning},
	}
	c, out := oneStream(t)
	printTidyResult(c, r)
	got := out.String()
	for _, w := range []string{"done refreshed manifests/a.txt\n", "failed seal: boom\n", warning + "\n"} {
		if !strings.Contains(got, w) {
			t.Errorf("output %q, want %q in it", got, w)
		}
	}

	c, stdout, stderr := coloured(t, map[string]string{"CLICOLOR_FORCE": "1"})
	printTidyResult(c, r)
	if strings.Contains(stdout.String(), warning) {
		t.Errorf("stdout %q holds the warning", stdout.String())
	}
	if want := ui.ErrStyles(c).Todo.Render(warning) + "\n"; stderr.String() != want {
		t.Errorf("stderr %q, want %q", stderr.String(), want)
	}
}

func TestTidyPrintsTransientFailureSpeech(t *testing.T) {
	c, out := oneStream(t)
	r := tidy.Result{Ran: true, Errors: []tidy.Failure{{TidyError: state.TidyError{Step: "seal", Message: "neutral error"}, Speech: "PERSONA ERROR"}}}
	printTidyResult(c, r)
	if got := out.String(); !strings.Contains(got, "failed seal: PERSONA ERROR") || strings.Contains(got, "neutral error") {
		t.Errorf("tidy output = %q", got)
	}
}

func TestTidyRunPrintsPersonaWhileStoringNeutralFailure(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	b.Files(map[string]string{
		"home/.home/private/common/token":   "secret",
		"home/.home/private/recipients.txt": "",
	})
	c := b.Ctx
	yes := true
	c.StdoutTerminal = &yes
	persona.Override(c, &persona.Voice{ID: "hostile", Name: "Hostile", Lines: map[string][]persona.Line{
		"secrets.error.empty_recipients": {{Text: "{recipients}: PERSONA RECIPIENT ERROR", Lang: language.English}},
	}})
	if err := tidyRun(c, tidyFlags{}); err != nil {
		t.Fatal(err)
	}
	if got := b.Stdout.String(); !strings.Contains(got, "PERSONA RECIPIENT ERROR") {
		t.Errorf("printed tidy = %q", got)
	}
	stored, err := state.Load(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.TidyErrors) != 1 || strings.Contains(stored.TidyErrors[0].Message, "PERSONA") {
		t.Errorf("stored tidy errors = %+v", stored.TidyErrors)
	}
	entries, err := state.ReadJournal(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Msg, "PERSONA") {
			t.Errorf("stored voiced journal = %q", entry.Msg)
		}
	}
}

func TestTidyWithoutWarningsPrintsNoneToStderr(t *testing.T) {
	c, _, stderr := coloured(t, nil)
	printTidyResult(c, tidy.Result{Ran: true, Did: []string{"fixed 1 link"}})
	if stderr.Len() != 0 {
		t.Errorf("stderr %q, want nothing", stderr.String())
	}
}
