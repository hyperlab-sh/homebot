package main

import (
	"strings"
	"testing"
)

func TestCheckExitCodesRewritesEveryHomebot(t *testing.T) {
	script := strings.Join([]string{
		"[!exec:git] skip 'git is missing'",
		"",
		"# exit: 0",
		"exec homebot link --dry-run",
		"# exit: 1",
		"! exec homebot frobnicate",
		"# exit: 3",
		"[unix] ! exec homebot",
		"exec git init -q",
		"[exec:homebot] exists home/.home/homebot.yml",
		"exec cat xdg/cache/homebot/nudge",
		"exec cat home/.home/homebot.yml",
		"stdout 'homebot: x'",
		"-- home/notes --",
		"exec homebot is only a file here",
		"",
	}, "\n")
	got, err := checkExitCodes("notes.txtar", script)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"[!exec:git] skip 'git is missing'",
		"",
		"# exit: 0",
		"execcode 0 homebot link --dry-run",
		"# exit: 1",
		"! execcode 1 homebot frobnicate",
		"# exit: 3",
		"[unix] ! execcode 3 homebot",
		"exec git init -q",
		"[exec:homebot] exists home/.home/homebot.yml",
		"exec cat xdg/cache/homebot/nudge",
		"exec cat home/.home/homebot.yml",
		"stdout 'homebot: x'",
		"-- home/notes --",
		"exec homebot is only a file here",
		"",
	}, "\n")
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if strings.Count(got, "\n") != strings.Count(script, "\n") {
		t.Error("the copy's line numbers moved")
	}
}

func TestCheckExitCodesRefusesMistakes(t *testing.T) {
	cases := []struct {
		name, script, want string
	}{
		{"no comment", "exec homebot\n", "1: exec homebot without an # exit: N comment"},
		{"no comment, negated", "# exit: 0\nexec homebot\n! exec homebot x\n", "3: exec homebot without"},
		{"not a number", "# exit: one\nexec homebot\n", `1: "one" is not an exit code`},
		{"negative", "# exit: -1\nexec homebot\n", `"-1" is not an exit code`},
		{"too big", "# exit: 256\nexec homebot\n", `"256" is not an exit code`},
		{"empty", "# exit:\nexec homebot\n", `"" is not an exit code`},
		{"a gap", "# exit: 0\n\nexec homebot\n", "1: # exit: 0 is not right above an exec homebot"},
		{"another program", "# exit: 0\nexec git status\n", "is not right above an exec homebot"},
		{"two comments", "# exit: 0\n# exit: 1\nexec homebot\n", "2: two # exit: comments in a row"},
		{"at the end", "exec true\n# exit: 0", "2: # exit: 0 is not right above an exec homebot"},
		{"before the files", "# exit: 0\n-- f --\nexec homebot\n", "1: # exit: 0 is not right above an exec homebot"},
		{"through env", "# exit: 0\nexec env X=1 homebot\n", "2: runs homebot in a form the exit checker does not know"},
		{"indented", "# exit: 0\n  exec homebot\n", "2: runs homebot in a form"},
		{"either way", "# exit: 0\n? exec homebot\n", "2: runs homebot in a form"},
		{"in the background", "# exit: 0\nexec homebot link &\n", "2: runs homebot in a form"},
		{"inside sh -c", "exec sh -c 'homebot triage; echo'\n", "1: runs homebot in a form"},
		{"inside sh -c, conditioned", "[linux] ! exec sh -c 'echo; homebot'\n", "1: runs homebot in a form"},
		{"inside sh -c, with a comment", "# exit: 0\nexec sh -c 'homebot triage'\n", "2: runs homebot in a form"},
		{"by a path", "# exit: 0\nexec $WORK/bin/homebot link\n", "2: runs homebot in a form"},
		{"by a relative path", "# exit: 0\nexec ./homebot\n", "2: runs homebot in a form"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := checkExitCodes("notes.txtar", tc.script)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one containing %q (script became %q)", err, tc.want, got)
			}
		})
	}
}

// A listed line runs unchecked in its own script only, and takes no
// comment.
func TestUncheckedLines(t *testing.T) {
	const line = "exec sh -c 'homebot triage; echo'"
	if _, err := checkExitCodes("triage-noninteractive.txtar", line+"\n"); err != nil {
		t.Errorf("the listed line: %v", err)
	}
	if _, err := checkExitCodes("other.txtar", line+"\n"); err == nil {
		t.Error("the line passed in a script it is not listed for")
	}
	_, err := checkExitCodes("triage-noninteractive.txtar", "# exit: 0\n"+line+"\n")
	if err == nil || !strings.Contains(err.Error(), "1: # exit: 0 is not right above an exec homebot") {
		t.Errorf("a comment above the listed line: %v", err)
	}
	for script, lines := range unchecked {
		for l, reason := range lines {
			if !runsHomebot(l) || recognised(l) || reason == "" {
				t.Errorf("%s: %q needs no listing, or has no reason", script, l)
			}
		}
	}
}

func TestExeccode(t *testing.T) {
	cases := []struct {
		name, script string
		// want is what the log must hold; "" means the script passes.
		want string
	}{
		{"the code matches", "execcode 0 sh -c 'exit 0'\n! execcode 3 sh -c 'exit 3'\n", ""},
		{"the wrong code", "! execcode 1 sh -c 'exit 3'\n", "sh exited with 3, want 1"},
		{"failure where success was recorded", "execcode 0 sh -c 'exit 1'\n", "sh exited with 1, want 0"},
		{"success where failure was recorded", "! execcode 1 true\n", "true exited with 0, want 1"},
		{"a non-zero code without !", "execcode 1 sh -c 'exit 1'\n", `# exit: 1 disagrees with the line's "exec"`},
		{"zero with !", "! execcode 0 true\n", `# exit: 0 disagrees with the line's "! exec"`},
		{"not a code", "execcode x true\n", `execcode: "x" is not an exit code`},
		{"no program", "execcode 0\n", "usage: execcode <code> <program> [args...]"},
		{"no such program", "execcode 0 no-such-program-here\n", "no-such-program-here"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			failed, log := runScript(t, tc.script)
			switch {
			case tc.want == "" && failed:
				t.Errorf("the script failed:\n%s", log)
			case tc.want != "" && !failed:
				t.Errorf("the script passed:\n%s", log)
			case !strings.Contains(log, tc.want):
				t.Errorf("log lacks %q:\n%s", tc.want, log)
			}
		})
	}
}

// The parity scripts as they are checked in: each exec homebot has its
// comment.
func TestEveryParityScriptRecordsItsExitCodes(t *testing.T) {
	files := scripts(t, "parity")
	if len(files) == 0 {
		t.Skip("no testdata/parity/*.txtar scripts")
	}
	if got := withExitCodes(t, files); len(got) != len(files) {
		t.Errorf("%d copies of %d scripts", len(got), len(files))
	}
}
