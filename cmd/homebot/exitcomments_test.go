package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

// exitComment is the line a parity script records an exit code with.
var exitComment = regexp.MustCompile(`^#\s*exit:\s*(.*)$`)

// execHomebot is the one form of a script line that runs homebot the
// checker knows: conditions, then an optional "!", then "exec homebot".
var execHomebot = regexp.MustCompile(`^((?:\[[^\]]*\]\s*)*)(!\s+)?exec\s+homebot(\s|$)`)

// linePrefix is what comes before a script line's command: indentation,
// conditions, then "!" or "?".
var linePrefix = regexp.MustCompile(`^\s*((?:\[[^\]]*\]\s*)*)([!?]\s+)?`)

// homebotWord is homebot named as a program, by its name or by a path to
// it ("$WORK/bin/homebot", "./homebot"), not a directory or a longer name
// such as xdg/cache/homebot/nudge, homebot.yml or homebot-tidy.timer.
var homebotWord = regexp.MustCompile(`(^|[^\w.-])homebot($|[^\w./-])`)

// unchecked are the lines, by script, that run homebot where its exit code
// cannot be seen, each with the reason. A line runs homebot unchecked only
// if it is listed here.
var unchecked = map[string]map[string]string{
	"triage-noninteractive.txtar": {
		// txtar cannot hold output without a final newline (the recorder's
		// rule, copied from dothaul's TypeScript source), so the sh wrapper's
		// echo adds one, and the line's code is the echo's. The golden needs
		// the wrapper, so it stays.
		"exec sh -c 'homebot triage; echo'": "an sh wrapper adds the final newline the golden needs",
	},
}

// runsHomebot reports whether text, a script line, runs homebot in any
// form: an exec whose words name it, inside an sh -c script or after env
// included.
func runsHomebot(text string) bool {
	rest := text[len(linePrefix.FindString(text)):]
	fields := strings.Fields(rest)
	return len(fields) > 0 && (fields[0] == "exec" || fields[0] == "execcode") && homebotWord.MatchString(rest)
}

// recognised reports whether text runs homebot in the form execHomebot
// matches, in the foreground, so its exit code can be checked.
func recognised(text string) bool {
	fields := strings.Fields(text)
	return execHomebot.MatchString(text) && fields[len(fields)-1] != "&"
}

// withExitCodes copies files to a temporary directory with every
// "exec homebot" that follows a "# exit: N" comment turned into
// "execcode N homebot", so the script checks the exit code too. A copy keeps
// its file's name and line numbers. A comment that is not right above an
// exec homebot, an exec homebot without one, or a code that is not a number
// fails t.
func withExitCodes(t *testing.T, files []string) []string {
	t.Helper()
	dir := t.TempDir()
	copies := make([]string, 0, len(files))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		script, err := checkExitCodes(filepath.Base(f), string(data))
		if err != nil {
			t.Fatalf("%s:%v", f, err)
		}
		dst := filepath.Join(dir, filepath.Base(f))
		if err := os.WriteFile(dst, []byte(script), 0o644); err != nil {
			t.Fatal(err)
		}
		copies = append(copies, dst)
	}
	return copies
}

// checkExitCodes rewrites script, the file called name, for withExitCodes.
// Only the script is read, not the files archived after it. A line that
// runs homebot in another form than "exec homebot", such as
// "exec env X=1 homebot", "? exec homebot", "exec homebot &",
// "exec ./homebot" or "exec sh -c 'homebot x'", fails unless it is listed
// in unchecked.
func checkExitCodes(name, script string) (string, error) {
	lines := strings.SplitAfter(script, "\n")
	code, at := "", 0
	notAbove := func() error { return fmt.Errorf("%d: # exit: %s is not right above an exec homebot", at, code) }
	for i, line := range lines {
		text := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(text, "-- ") && strings.HasSuffix(text, " --") {
			break
		}
		if m := exitComment.FindStringSubmatch(text); m != nil {
			if code != "" {
				return "", fmt.Errorf("%d: two # exit: comments in a row", i+1)
			}
			if n, err := strconv.Atoi(m[1]); err != nil || n < 0 || n > 255 {
				return "", fmt.Errorf("%d: %q is not an exit code", i+1, m[1])
			}
			code, at = m[1], i+1
			continue
		}
		if runsHomebot(text) && !recognised(text) {
			if _, ok := unchecked[name][text]; !ok {
				return "", fmt.Errorf("%d: runs homebot in a form the exit checker does not know; write it as \"exec homebot ...\" with an # exit: N comment above, or list it in unchecked", i+1)
			}
		}
		m := execHomebot.FindStringSubmatchIndex(text)
		switch {
		case m == nil && code != "":
			return "", notAbove()
		case m == nil:
			continue
		case code == "":
			return "", fmt.Errorf("%d: exec homebot without an # exit: N comment above it", i+1)
		}
		// m[6] is where "homebot" ends; the word before it, "exec", becomes
		// "execcode N".
		start := strings.LastIndex(text[:m[6]], "exec")
		lines[i] = text[:start] + "execcode " + code + " homebot" + line[m[6]:]
		code = ""
	}
	if code != "" {
		return "", notAbove()
	}
	return strings.Join(lines, ""), nil
}

// execcode runs a program as exec does and checks its exit code:
//
//	execcode <code> <program> [args...]
//
// "! execcode" expects a code other than 0, and a plain execcode expects 0,
// so the comment and the "!" cannot disagree.
func execcode(ts *testscript.TestScript, neg bool, args []string) {
	if len(args) < 2 {
		ts.Fatalf("usage: execcode <code> <program> [args...]")
	}
	want, err := strconv.Atoi(args[0])
	if err != nil {
		ts.Fatalf("execcode: %q is not an exit code", args[0])
	}
	if neg != (want != 0) {
		ts.Fatalf("# exit: %d disagrees with the line's %q", want, map[bool]string{true: "! exec", false: "exec"}[neg])
	}
	got := 0
	if err := ts.Exec(args[1], args[2:]...); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			ts.Fatalf("%v", err)
		}
		got = exit.ExitCode()
	}
	if got != want {
		ts.Fatalf("%s exited with %d, want %d", args[1], got, want)
	}
}
