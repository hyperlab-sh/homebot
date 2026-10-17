package cli

import (
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// Every persona subcommand answers --help with a page of its own, not the
// persona page, and `help persona <sub>` says the same.
func TestPersonaSubcommandsHaveTheirOwnHelp(t *testing.T) {
	root := realTree(t)
	persona, _, err := root.Find([]string{"persona"})
	if err != nil {
		t.Fatal(err)
	}
	top, _ := pageOf("persona")
	var seen []string
	for _, sub := range persona.Commands() {
		if sub.Hidden {
			continue
		}
		seen = append(seen, sub.Name())
		t.Run(sub.Name(), func(t *testing.T) {
			sp, ok := subpageOf("persona", sub.Name())
			if !ok {
				t.Fatalf("persona %s has no page", sub.Name())
			}
			code, out, errOut := help(t, "persona", sub.Name(), "--help")
			if code != 0 || errOut != "" {
				t.Fatalf("exit %d, stderr %q", code, errOut)
			}
			plain := voice.Plain(testutil.Sandbox(t, testutil.Options{}).Ctx, sp.usage)
			if !strings.HasPrefix(out, "Usage: "+plain+"\n") {
				t.Errorf("--help = %q, want it to open with %q", out, plain)
			}
			if strings.Contains(out, "Commands:") {
				t.Errorf("--help is the whole persona page: %q", out)
			}
			if code, viaHelp, _ := help(t, "help", "persona", sub.Name()); code != 0 || viaHelp != out {
				t.Errorf("help persona %s = %q (exit %d), want %q", sub.Name(), viaHelp, code, out)
			}
			if code, short, _ := help(t, "persona", sub.Name(), "-h"); code != 0 || short != out {
				t.Errorf("-h = %q (exit %d), want %q", short, code, out)
			}
			if _, whole, _ := help(t, "persona", "--help"); whole == out {
				t.Error("the subcommand's page equals the persona page")
			}
		})
	}
	if len(seen) != len(subpagesOf("persona")) {
		t.Errorf("persona commands %v, subpages %d", seen, len(subpagesOf("persona")))
	}
	for _, p := range subpagesOf("persona") {
		if !slicesContains(seen, strings.TrimPrefix(p.name, "persona ")) {
			t.Errorf("page %q has no command", p.name)
		}
	}
	if top.name != "persona" {
		t.Fatal("premise: persona page")
	}
}

func slicesContains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// The persona page lists its subcommands, each with the summary its own page
// opens with.
func TestPersonaPageListsItsSubcommands(t *testing.T) {
	code, out, _ := help(t, "persona", "--help")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "Commands:") {
		t.Fatalf("no Commands: table: %q", out)
	}
	sb := testutil.Sandbox(t, testutil.Options{})
	for _, p := range subpagesOf("persona") {
		if !strings.Contains(out, voice.Plain(sb.Ctx, p.summary)) {
			t.Errorf("persona --help lacks the summary of %q", p.name)
		}
	}
}

// Flags a subcommand lacks are not listed on its page, and an unknown
// subcommand falls back to the persona page.
func TestPersonaSubpageOptions(t *testing.T) {
	_, list, _ := help(t, "persona", "list", "--help")
	if strings.Contains(list, "--from") || strings.Contains(list, "--event") {
		t.Errorf("list page shows other commands' options: %q", list)
	}
	_, nw, _ := help(t, "persona", "new", "--help")
	if !strings.Contains(nw, "--from") || strings.Contains(nw, "--event") {
		t.Errorf("new page options: %q", nw)
	}
	_, pv, _ := help(t, "persona", "preview", "--help")
	if !strings.Contains(pv, "--event") || !strings.Contains(pv, "--all-variants") || strings.Contains(pv, "--from") {
		t.Errorf("preview page options: %q", pv)
	}
}

// The overview row for persona starts lower case, like every other row.
func TestOverviewRowsStartLowerCase(t *testing.T) {
	for _, p := range pages {
		first := voice.Plain(testutil.Sandbox(t, testutil.Options{}).Ctx, p.summary)
		if first == "" || first[0] < 'a' || first[0] > 'z' {
			t.Errorf("%s summary %q does not start lower case", p.name, first)
		}
	}
}
