// Package cli builds homebot's command tree: the root command and every
// command registered through register (see registry.go).
package cli

import (
	"runtime/debug"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/update"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// Build says which homebot this is. cmd/homebot fills it from -ldflags or
// the build info Go records.
type Build struct {
	Version string
	Commit  string
	Date    string
	Channel string
}

// NewRoot builds the command tree: help, version and every registered
// command, each described by its help page (help_pages.go). Building it reads
// nothing from c but the streams and environment ui's writers are made from,
// and the voice; the commands use c when they run. Execute runs it.
//
// Before any command runs, the root points c at the command's context, so
// Ctrl-C and SIGTERM stop the programs a command runs through c. Subcommands
// must not set their own PersistentPreRunE, which would replace the root's.
func NewRoot(c *app.Context, build Build) *cobra.Command {
	c.BuildVersion, c.BuildChannel = build.Version, build.Channel
	root := &cobra.Command{
		Use: "homebot",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			*c = *c.WithContext(cmd.Context())
			return nil
		},
		// Execute prints errors itself (Report), and a mistake is never
		// followed by a usage dump.
		SilenceErrors: true,
		SilenceUsage:  true,
		// The TypeScript had no completion command.
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	}
	// The dispatcher sends root help to the help command; cobra's own help
	// of the root, and of a command without a page, is the overview too.
	root.SetHelpFunc(func(*cobra.Command, []string) { ui.Println(c, overview(c)) })
	root.SetIn(c.Stdin)
	// cobra's own text (an argument error, a flag error) goes through ui's
	// writers, like everything else homebot prints.
	root.SetOut(ui.Out(c))
	root.SetErr(ui.Err(c))
	// The dispatcher finds the mistakes parseArgs did, in its words; pflag's
	// are for anything it lets through.
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return newOptionError(c, pathBelowRoot(cmd), err.Error())
	})
	help := newHelpCmd(c)
	root.SetHelpCommand(help)
	root.AddCommand(help, newVersionCmd(c, build))
	addRegistered(root, c)
	describe(root, c)
	return root
}

// pathBelowRoot is cmd's command path without the root: "keys init" for
// homebot keys init, and "" for the root itself.
func pathBelowRoot(cmd *cobra.Command) string {
	if !cmd.HasParent() {
		return ""
	}
	return strings.TrimPrefix(cmd.CommandPath(), cmd.Root().Name()+" ")
}

func newVersionCmd(c *app.Context, build Build) *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use: "version",
		// Checked in RunE, so the mistake is said from the catalog.
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fail.User(voice.Plain(c, msg.CLIErrorVersionArgs(args[0])))
			}
			if !check {
				ui.Println(c, voice.Render(c, msg.CLIVersion(build.Version)))
				return nil
			}
			return versionCheck(c, build)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "")
	return cmd
}

func versionCheck(c *app.Context, build Build) error {
	if c.Config == nil {
		_ = c.LocateRoot()
	} // a source build may check outside a home repo.
	info, _ := debug.ReadBuildInfo()
	return versionCheckWithInfo(c, build, info)
}

func versionCheckWithInfo(c *app.Context, build Build, info *debug.BuildInfo) error {
	i := update.Detect(c, update.Build{Version: build.Version, Channel: build.Channel}, info)
	r, err := update.Latest(c) // exactly one request, only on explicit --check.
	if err != nil {
		return fail.Say(msg.UpdateErrorCheck(err.Error()))
	}
	return versionReport(c, build, i, r)
}

// versionReport prints what version --check says about install i and the
// latest release r. It reads local state and makes no request.
func versionReport(c *app.Context, build Build, i update.Install, r update.Release) error {
	ui.Println(c, voice.Render(c, msg.UpdateVersionCurrent(build.Version)))
	ui.Println(c, voice.Render(c, msg.UpdateVersionLatest(r.Version)))
	ui.Println(c, voice.Render(c, msg.UpdateVersionMethod(string(i.Method))))
	checks := update.ChecksRelease(i)
	if checks {
		// A build that is already at or past the latest release has nothing to
		// upgrade to: no command, and nothing about automatic updates.
		if newer, err := update.Newer(i.Version, r.Version); err == nil && !newer {
			ui.Println(c, voice.Render(c, msg.UpdateVersionUpToDate()))
			return nil
		}
	}
	if command := update.ManualCommand(i, r); command != "" {
		ui.Println(c, voice.Render(c, msg.UpdateVersionCommand(command)))
	}
	if !checks {
		kind := msg.UpdateVersionDevelopment()
		if i.Method == update.Source && build.Version != "dev" {
			kind = msg.UpdateVersionSource()
		}
		ui.Println(c, voice.Render(c, kind))
		return nil
	}
	if c.Config == nil || c.Config.Update != config.UpdateAuto {
		return nil
	}
	if i.Method != update.Cask && i.Method != update.Script {
		ui.Println(c, voice.Render(c, msg.UpdateVersionAutoManual()))
		return nil
	}
	if i.Method == update.Script && !update.CosignReady(c) {
		ui.Println(c, voice.Render(c, msg.UpdateVersionAutoCosign(update.CosignMin())))
		return nil
	}
	st, err := state.Load(c)
	if err != nil {
		return err
	}
	gate := update.AutoGate(st, r.Version, c.Now())
	when := gate.At.UTC().Format(time.RFC3339)
	switch gate.Phase {
	case update.PhaseUnseen:
		ui.Println(c, voice.Render(c, msg.UpdateVersionAutoUnseen()))
	case update.PhaseSoaking:
		ui.Println(c, voice.Render(c, msg.UpdateVersionAutoSoak(when)))
	case update.PhaseFailed:
		why := gate.Failure.Error
		if why == "" {
			why = gate.Failure.Reason
		}
		ui.Println(c, voice.Render(c, msg.UpdateVersionAutoFailed(r.Version, why, when)))
	case update.PhaseRetrying:
		ui.Println(c, voice.Render(c, msg.UpdateVersionAutoRetry(when)))
	case update.PhaseReady:
		if gate.At.After(c.Now()) {
			ui.Println(c, voice.Render(c, msg.UpdateVersionAutoReadyAfter(when)))
		} else {
			ui.Println(c, voice.Render(c, msg.UpdateVersionAutoReady()))
		}
	}
	return nil
}
