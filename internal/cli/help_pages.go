package cli

import (
	"strings"

	"github.com/hyperlab-sh/homebot/internal/msg"
)

// page is one command's help, as COMMAND_HELP in help.ts held it.
type page struct {
	name  string
	group Group
	// usage is shown after "Usage:", and in the overview's left column.
	usage msg.Text
	// summary is the command's line in the overview.
	summary msg.Text
	// about is the paragraph or two of the command's own page.
	about    msg.Text
	options  []option
	examples []example
	// verbatim commands get their arguments as typed, as the TypeScript
	// handed them over without parsing options: "--x" and "--" are arguments
	// like any other.
	verbatim bool
	// noRepo commands run outside a home repo, so a mistake on their command
	// line is reported without looking for one first.
	noRepo bool
}

// option is a row of a page's Options table.
type option struct {
	// flag is the left column, as help.ts wrote it: "-m, --message TEXT".
	flag    string
	meaning msg.Text
}

// name is the option's long name, "message" for "-m, --message TEXT".
func (o option) name() string {
	for _, f := range strings.Fields(o.flag) {
		if long, ok := strings.CutPrefix(f, "--"); ok {
			return long
		}
	}
	return ""
}

// example is a row of a page's Examples table.
type example struct {
	command string
	meaning msg.Text
}

// pages is every command's page, in help.ts's order, which the overview
// keeps. Every page has a command in the root (TestEveryHelpPageHasACommand),
// and every command a page.
var pages = []page{
	{
		name: "inbox", group: Attention,
		usage: msg.HelpInboxUsage(), summary: msg.HelpInboxSummary(), about: msg.HelpInboxAbout(),
		options: []option{{"--all", msg.HelpInboxOptionAll()}},
	},
	{
		name: "next", group: Attention, verbatim: true,
		usage: msg.HelpNextUsage(), summary: msg.HelpNextSummary(), about: msg.HelpNextAbout(),
	},
	{
		name: "unpack", group: MovingIn,
		usage: msg.HelpUnpackUsage(), summary: msg.HelpUnpackSummary(), about: msg.HelpUnpackAbout(),
		options: []option{
			{"--dry-run", msg.HelpUnpackOptionDryRun()},
			{"--from N", msg.HelpUnpackOptionFrom()},
		},
		examples: []example{
			{"unpack --dry-run", msg.HelpUnpackExample1()},
			{"unpack server --from 4", msg.HelpUnpackExample2()},
		},
	},
	{
		name: "pack", group: MovingIn,
		usage: msg.HelpPackUsage(), summary: msg.HelpPackSummary(), about: msg.HelpPackAbout(),
		options: []option{
			{"--layer common|linux|macos", msg.HelpPackOptionLayer()},
			{"--private", msg.HelpPackOptionPrivate()},
		},
		examples: []example{
			{"pack ~/.config/zed", msg.HelpPackExample1()},
			{"pack --layer linux ~/.config/foo", msg.HelpPackExample2()},
			{"pack --private ~/.ssh/id_ed25519", msg.HelpPackExample3()},
		},
	},
	{
		name: "link", group: MovingIn,
		usage: msg.HelpLinkUsage(), summary: msg.HelpLinkSummary(), about: msg.HelpLinkAbout(),
		options: []option{
			{"--dry-run", msg.HelpLinkOptionDryRun()},
			{"--backup", msg.HelpLinkOptionBackup()},
		},
	},
	// The TypeScript's unlink ignored its arguments. The Go port refuses
	// options: "unlink --dry-run" must not remove every link.
	{
		name: "unlink", group: MovingIn,
		usage: msg.HelpUnlinkUsage(), summary: msg.HelpUnlinkSummary(), about: msg.HelpUnlinkAbout(),
	},
	{
		name: "cleanup", group: MovingIn,
		usage: msg.HelpCleanupUsage(), summary: msg.HelpCleanupSummary(), about: msg.HelpCleanupAbout(),
		options: []option{{"--list", msg.HelpCleanupOptionList()}},
	},
	{
		name: "keys", group: Secrets, verbatim: true,
		usage: msg.HelpKeysUsage(), summary: msg.HelpKeysSummary(), about: msg.HelpKeysAbout(),
	},
	{
		name: "unlock", group: Secrets,
		usage: msg.HelpUnlockUsage(), summary: msg.HelpUnlockSummary(), about: msg.HelpUnlockAbout(),
		options: []option{{"--theirs", msg.HelpUnlockOptionTheirs()}},
	},
	{
		name: "lock", group: Secrets,
		usage: msg.HelpLockUsage(), summary: msg.HelpLockSummary(), about: msg.HelpLockAbout(),
		options: []option{
			{"--ours", msg.HelpLockOptionOurs()},
			{"--wipe", msg.HelpLockOptionWipe()},
		},
	},
	{
		name: "triage", group: KeepingOrder, verbatim: true,
		usage: msg.HelpTriageUsage(), summary: msg.HelpTriageSummary(), about: msg.HelpTriageAbout(),
	},
	{
		name: "inventory", group: KeepingOrder, verbatim: true,
		usage: msg.HelpInventoryUsage(), summary: msg.HelpInventorySummary(), about: msg.HelpInventoryAbout(),
	},
	{
		name: "remote", group: KeepingOrder,
		usage: msg.HelpRemoteUsage(), summary: msg.HelpRemoteSummary(), about: msg.HelpRemoteAbout(),
		examples: []example{
			{"remote", msg.HelpRemoteExample1()},
			{"remote git@github.com:you/home.git", msg.HelpRemoteExample2()},
		},
	},
	{
		name: "save", group: KeepingOrder,
		usage: msg.HelpSaveUsage(), summary: msg.HelpSaveSummary(), about: msg.HelpSaveAbout(),
		options: []option{
			{"-m, --message TEXT", msg.HelpSaveOptionMessage()},
			{"--no-push", msg.HelpSaveOptionNoPush()},
		},
	},
	{
		name: "export", group: KeepingOrder, verbatim: true,
		usage: msg.HelpExportUsage(), summary: msg.HelpExportSummary(), about: msg.HelpExportAbout(),
		examples: []example{{"export apt apt-repos", msg.HelpExportExample1()}},
	},
	{
		name: "tidy", group: KeepingOrder,
		usage: msg.HelpTidyUsage(), summary: msg.HelpTidySummary(), about: msg.HelpTidyAbout(),
		options: []option{
			{"--notify", msg.HelpTidyOptionNotify()},
			{"--quiet", msg.HelpTidyOptionQuiet()},
		},
	},
	{
		name: "setup", group: KeepingOrder, noRepo: true,
		usage: msg.HelpSetupUsage(), summary: msg.HelpSetupSummary(), about: msg.HelpSetupAbout(),
		options: []option{
			{"--remove", msg.HelpSetupOptionRemove()},
			{"--name WORD", msg.HelpSetupOptionName()},
			{"--persona ID", msg.HelpSetupOptionPersona()},
		},
	},
	{
		name: "actions", group: KeepingOrder, verbatim: true,
		usage: msg.HelpActionsUsage(), summary: msg.HelpActionsSummary(), about: msg.HelpActionsAbout(),
	},
	{
		name: "action", group: KeepingOrder, verbatim: true,
		usage: msg.HelpActionUsage(), summary: msg.HelpActionSummary(), about: msg.HelpActionAbout(),
		examples: []example{{"action apt-repos linux/apt-repos", msg.HelpActionExample1()}},
	},
	{
		name: "shell", group: KeepingOrder, verbatim: true, noRepo: true,
		usage: msg.HelpShellUsage(), summary: msg.HelpShellSummary(), about: msg.HelpShellAbout(),
	},
	{
		name: "name", group: KeepingOrder,
		usage: msg.HelpNameUsage(), summary: msg.HelpNameSummary(), about: msg.HelpNameAbout(),
		options: []option{{"--off", msg.HelpNameOptionOff()}},
	},
	{
		name: "persona", group: KeepingOrder, noRepo: true,
		usage: msg.HelpPersonaUsage(), summary: msg.HelpPersonaSummary(), about: msg.HelpPersonaAbout(),
		options: []option{
			{"--from ID", msg.HelpPersonaOptionFrom()},
			{"--event ID", msg.HelpPersonaOptionEvent()},
			{"--all-variants", msg.HelpPersonaOptionAllVariants()},
		},
	},
	{
		name: "refresh", group: Hidden, verbatim: true,
		usage: msg.HelpRefreshUsage(), summary: msg.HelpRefreshSummary(), about: msg.HelpRefreshAbout(),
	},
	{
		name: "hook", group: Hidden, verbatim: true,
		usage: msg.HelpHookUsage(), summary: msg.HelpHookSummary(), about: msg.HelpHookAbout(),
	},
	{
		name: "completion", group: Hidden, noRepo: true,
		usage: msg.HelpCompletionUsage(), summary: msg.HelpCompletionSummary(), about: msg.HelpCompletionAbout(),
		examples: []example{{"completion zsh", msg.HelpCompletionExample()}},
	},
	// The Go port's own: the TypeScript had only --version and -v.
	{
		name: "version", group: Hidden, noRepo: true,
		usage: msg.HelpVersionUsage(), summary: msg.HelpVersionSummary(), about: msg.HelpVersionAbout(),
		options: []option{{"--check", msg.HelpVersionOptionCheck()}},
	},
}

// subpages are the pages of the commands under a top-level command, which
// gives each of them its own text instead of answering with its parent's.
// They are named "<command> <subcommand>", and they are not in pages: the
// overview, the man page and the shell completion list top-level commands
// only. The parent's page lists them under "Commands:". A subcommand without
// a page answers with its parent's (keys init).
var subpages = []page{
	{
		name:  "persona list",
		usage: msg.HelpPersonaListUsage(), summary: msg.HelpPersonaListSummary(), about: msg.HelpPersonaListAbout(),
	},
	{
		name:  "persona show",
		usage: msg.HelpPersonaShowUsage(), summary: msg.HelpPersonaShowSummary(), about: msg.HelpPersonaShowAbout(),
	},
	{
		name:  "persona use",
		usage: msg.HelpPersonaUseUsage(), summary: msg.HelpPersonaUseSummary(), about: msg.HelpPersonaUseAbout(),
	},
	{
		name:  "persona new",
		usage: msg.HelpPersonaNewUsage(), summary: msg.HelpPersonaNewSummary(), about: msg.HelpPersonaNewAbout(),
		options: []option{{"--from ID", msg.HelpPersonaOptionFrom()}},
	},
	{
		name:  "persona validate",
		usage: msg.HelpPersonaValidateUsage(), summary: msg.HelpPersonaValidateSummary(), about: msg.HelpPersonaValidateAbout(),
	},
	{
		name:  "persona preview",
		usage: msg.HelpPersonaPreviewUsage(), summary: msg.HelpPersonaPreviewSummary(), about: msg.HelpPersonaPreviewAbout(),
		options: []option{
			{"--event ID", msg.HelpPersonaOptionEvent()},
			{"--all-variants", msg.HelpPersonaOptionAllVariants()},
		},
	},
	{
		name:  "persona ids",
		usage: msg.HelpPersonaIdsUsage(), summary: msg.HelpPersonaIdsSummary(), about: msg.HelpPersonaIdsAbout(),
	},
}

// subpageOf returns the page of the subcommand sub of the command name, and
// whether it has one.
func subpageOf(name, sub string) (page, bool) {
	for _, p := range subpages {
		if p.name == name+" "+sub {
			return p, true
		}
	}
	return page{}, false
}

// subpagesOf are the pages of the subcommands of the command name, in order.
func subpagesOf(name string) []page {
	var out []page
	for _, p := range subpages {
		if strings.HasPrefix(p.name, name+" ") {
			out = append(out, p)
		}
	}
	return out
}

// pageOf returns the page of the command name, and whether it has one.
func pageOf(name string) (page, bool) {
	for _, p := range pages {
		if p.name == name {
			return p, true
		}
	}
	return page{}, false
}
