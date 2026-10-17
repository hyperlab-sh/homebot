package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// homebot link says why an op is blocked with the reason of the planner, in the
// persona's voice on a terminal and as the neutral English everywhere else.
// Nothing it stores holds the persona's words.

// speaksReasons is a persona with a line for one conflict and one layout error.
func speaksReasons() *persona.Voice {
	return &persona.Voice{ID: "hostile", Name: "Hostile", Lines: map[string][]persona.Line{
		"conflict.different": {{Text: "PERSONA CONFLICT", Lang: language.English}},
		"layout.old_name":    {{Text: "PERSONA OLD NAME {raw} {new}", Lang: language.English}},
	}}
}

// blockedRepo is a repo with a link that cannot be made (the old layout) and
// one that is in the way (a different file already in $HOME).
func blockedRepo(sb *testutil.Box) {
	sb.Files(map[string]string{
		"home/.home/autolink/common/dot-config/x": "x",
		"home/.home/autolink/common/differ":       "repo",
		"home/.differ":                            "mine",
	})
}

const (
	neutralConflict = "already exists with different content"
	neutralOldName  = `"dot-config" is the old layout: rename it to "config"`
)

func runLinkArgs(t *testing.T, sb *testutil.Box, args ...string) {
	t.Helper()
	cmd := newLinkCmd(sb.Ctx)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs(args)
	// The exit status 1 of a plan with invalid ops is not what is tested.
	_ = cmd.Execute()
}

func TestLinkDryRunSaysTheReasonsInThePersonasVoice(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{})
	yes := true
	sb.Ctx.StdoutTerminal = &yes
	persona.Override(sb.Ctx, speaksReasons())
	blockedRepo(sb)

	runLinkArgs(t, sb, "--dry-run")

	out := sb.Stdout.String()
	for _, want := range []string{"PERSONA CONFLICT", "PERSONA OLD NAME dot-config config"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run lacks %q:\n%s", want, out)
		}
	}
	for _, neutral := range []string{neutralConflict, neutralOldName} {
		if strings.Contains(out, neutral) {
			t.Errorf("dry run holds the neutral %q:\n%s", neutral, out)
		}
	}
}

func TestLinkSaysTheReasonsInThePersonasVoiceAndStoresNothingOfIt(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{})
	yes := true
	sb.Ctx.StdoutTerminal = &yes
	persona.Override(sb.Ctx, speaksReasons())
	blockedRepo(sb)

	runLinkArgs(t, sb)

	out := sb.Stdout.String()
	for _, want := range []string{"PERSONA CONFLICT", "PERSONA OLD NAME dot-config config"} {
		if !strings.Contains(out, want) {
			t.Errorf("link lacks %q:\n%s", want, out)
		}
	}

	// Everything the command left on disk besides the links: the state, the
	// journal, the inbox caches.
	for _, dir := range []string{sb.Ctx.StateDir, sb.Ctx.CacheDir} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(body), "PERSONA") {
				t.Errorf("%s holds the persona's words:\n%s", path, body)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestLinkReasonsAreNeutralWhenPipedOrPlain(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(sb *testutil.Box)
	}{
		{"piped", func(sb *testutil.Box) {
			no := false
			sb.Ctx.StdoutTerminal = &no
		}},
		{"--plain", func(sb *testutil.Box) {
			yes := true
			sb.Ctx.StdoutTerminal = &yes
			sb.Ctx.Plain = true
		}},
	} {
		for _, args := range [][]string{{"--dry-run"}, nil} {
			t.Run(tc.name+strings.Join(append([]string{""}, args...), " "), func(t *testing.T) {
				sb := testutil.Sandbox(t, testutil.Options{})
				persona.Override(sb.Ctx, speaksReasons())
				tc.setup(sb)
				blockedRepo(sb)

				runLinkArgs(t, sb, args...)

				out := sb.Stdout.String()
				if strings.Contains(out, "PERSONA") {
					t.Errorf("output holds the persona's words:\n%s", out)
				}
				for _, want := range []string{neutralConflict, neutralOldName} {
					if !strings.Contains(out, want) {
						t.Errorf("output lacks %q:\n%s", want, out)
					}
				}
			})
		}
	}
}
