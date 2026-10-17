package guard

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// leakExit is the exit code gitleaks is asked to report a leak with. It is
// distinct from gitleaks' own error codes (1 is a failure), so a leak and a
// broken run can't be confused.
const leakExit = 99

// noGitCommand is what a gitleaks without the git subcommand (older
// releases, as Debian ships) says when asked for it.
var noGitCommand = regexp.MustCompile(`unknown command "git"`)

// scan runs gitleaks over the staged changes. A missing gitleaks, a
// gitleaks that cannot run and a leak are each a problem: the guard fails
// closed.
func scan(c *app.Context) (problem, bool) {
	if !proc.HasCommand(c, "gitleaks") {
		return problem{text: voice.Record(c, msg.GuardNoGitleaks()), commands: true}, false
	}
	common := []string{"--redact", "--exit-code", strconv.Itoa(leakExit), "--no-banner", "--log-level", "warn"}
	opts := proc.Options{Dir: c.Root}
	r := proc.Run(c, append(append([]string{"gitleaks", "git", "--pre-commit", "--staged"}, common...), c.Root), opts)
	// Only a failure falls back: a leak is a leak whatever it prints.
	if r.Code != 0 && r.Code != leakExit && noGitCommand.MatchString(r.Stderr) {
		r = proc.Run(c, append(append([]string{"gitleaks", "protect", "--staged"}, common...), "--source", c.Root), opts)
	}
	switch r.Code {
	case 0:
		return problem{}, true
	case leakExit:
		detail := strings.Split(strings.TrimSpace(r.Stdout+r.Stderr), "\n")
		for i, l := range detail {
			detail[i] = "  " + l
		}
		return problem{text: voice.Record(c, msg.GuardLeak(strings.Join(detail, "\n")))}, false
	}
	why, _, _ := strings.Cut(strings.TrimSpace(r.Stderr), "\n")
	if why == "" {
		why = voice.Record(c, msg.GuardGitleaksFailedExitCode(r.Code))
	}
	return problem{text: voice.Record(c, msg.GuardGitleaksFailed(why))}, false
}
