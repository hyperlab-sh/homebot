package cli

import (
	"strings"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// setupTools are the programs setup checks for besides git: gitleaks,
// without which the commit guard refuses every commit. age is not one:
// homebot seals and unseals with the Go age library, never the age binary.
var setupTools = []string{"gitleaks"}

// setupMissingTools lists the setupTools that are not on PATH, in order.
func setupMissingTools(c *app.Context) []string {
	var missing []string
	for _, t := range setupTools {
		if !proc.HasCommand(c, t) {
			missing = append(missing, t)
		}
	}
	return missing
}

// setupInstallCommand is the brew command that installs tools. setup runs
// it only when brew is on PATH; otherwise it prints it, Homebrew being the
// install path the docs give on both systems.
func setupInstallCommand(tools []string) []string {
	return append([]string{"brew", "install"}, tools...)
}

// setupCheckTools stops setup before it changes anything when a tool in
// setupTools is missing. On a terminal with brew on PATH it offers to
// install it first; otherwise it says what to run, and setup exits as a
// failed precondition does.
func setupCheckTools(c *app.Context) error {
	missing := setupMissingTools(c)
	if len(missing) == 0 {
		return nil
	}
	tools := strings.Join(missing, ", ")
	argv := setupInstallCommand(missing)
	command := strings.Join(argv, " ")
	stop := fail.User(voice.Plain(c, msg.SetupErrorToolsMissing(tools, command)))
	if !proc.HasCommand(c, "brew") || !ui.Interactive(c) {
		return stop
	}
	if !setupYes(c, msg.SetupPromptInstallTools(tools, command)) {
		return stop
	}
	in, out, errw := setupAttached(c)
	code, err := proc.RunAttached(c, argv, proc.AttachedOptions{Env: c.Env, Stdin: in, Stdout: out, Stderr: errw})
	if err != nil || code != 0 {
		return fail.User(voice.Plain(c, msg.SetupErrorInstallFailed(command, tools)))
	}
	if still := setupMissingTools(c); len(still) > 0 {
		return fail.User(voice.Plain(c, msg.SetupErrorToolsMissing(strings.Join(still, ", "), strings.Join(setupInstallCommand(still), " "))))
	}
	return nil
}

// setupYes asks t, a yes-or-no question, and says whether the answer was
// yes: Enter, y or yes, in any case. Ending the input (Ctrl-D) is no, as
// it cancels elsewhere: it never installs, unpacks, links or commits. A
// newline after it keeps what comes next on its own line.
func setupYes(c *app.Context, t msg.Text) bool {
	answer, ok := ui.AskOrEnd(c, voice.Plain(c, t))
	if !ok {
		ui.Println(c, "")
		return false
	}
	switch strings.ToLower(answer) {
	case "", "y", "yes":
		return true
	}
	return false
}

// setupCommand is t, a command to run, laid out and styled as one.
func setupCommand(c *app.Context, t msg.Text) string {
	return voice.Command(c, t)
}

// setupLater is what a declined question prints: "Not now", then the
// command that does it later.
func setupLater(c *app.Context, command string) {
	ui.Println(c, voice.Render(c, msg.RemoteStatusNotNow()))
	ui.Println(c, command)
}
