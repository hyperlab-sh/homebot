// Package selfname is the name the user gives their homebot (naming plan
// D2 to D4): whether a name may be used on this machine (Check), the link
// ~/.local/bin/<name> that makes it a command (Link, Adopt, Unlink), the
// per-machine record of that link (Record), and the name homebot prints
// (Effective, Status).
//
// selfname imports only app, config and proc. It never imports fsx (which
// imports voice, and voice imports selfname), so it writes its files with
// os. It never prints and never renders a msg.Text: a Verdict and an Error
// carry a reason code and a path, and the caller (cli, tidy) turns them into
// catalog text. It never touches a file or link that is not its own (R12).
package selfname

import "github.com/hyperlab-sh/homebot/internal/app"

// Reason says why a name is refused (Check) or why a link could not be
// made or removed (Error). The zero value is no reason: the name may be used.
type Reason string

const (
	// Syntax: the name fails config.ValidName.
	Syntax Reason = "syntax"
	// Reserved: homebot uses the name (a command, help, version,
	// completion, man, homebot).
	Reserved Reason = "reserved"
	// Shell: a shell uses the name (a reserved word or builtin, ShellWords).
	Shell Reason = "shell"
	// Tool: homebot runs a program by that name (ToolNames).
	Tool Reason = "tool"
	// OnPath: a file by that name is in a folder of PATH, or of a fixed
	// system folder; Path names it.
	OnPath Reason = "on_path"
	// InRepo: the home repo places ~/.local/bin/<name> (local/bin/<name>);
	// Path is the repo's file.
	InRepo Reason = "in_repo"
	// Foreign: ~/.local/bin/<name> exists and is neither ours nor a link to
	// homebot; Path names it, with its spelling on disk. homebot never
	// replaces it.
	Foreign Reason = "foreign"
	// SyncedBin: ~/.local/bin or ~/.local is a symlink, so another tool
	// manages it and the link would be synced; Path names it.
	SyncedBin Reason = "synced_bin"
	// TempBlocked: something that is not a link to homebot sits at the
	// temporary path a re-point uses (~/.local/bin/.<name>.homebot-tmp);
	// Path names it. The current link is kept as it is.
	TempBlocked Reason = "temp_blocked"
	// NoBin: ~/.local/bin does not exist, and this caller may not create it
	// (the helper never does, D4); Path names it.
	NoBin Reason = "no_bin"
	// NoLink: Adopt found no link at ~/.local/bin/<name>; Path names it.
	NoLink Reason = "no_link"
	// NoHomebot: there is no stable path named homebot to point the link
	// at (proc.SelfPath); Path is what SelfPath gave.
	NoHomebot Reason = "no_homebot"
)

// Existing is what ~/.local/bin/<name> holds, for a name Check allows.
type Existing int

const (
	// Absent: nothing; Link makes the link.
	Absent Existing = iota
	// Ours: a link this machine's record says homebot made, pointing at a
	// homebot binary (R12). Link may re-point it; Unlink removes it.
	Ours
	// Adoptable: a symlink the user made, pointing at homebot. It is used
	// as it is, and never re-pointed or removed (D4).
	Adoptable
)

// Taken holds the lists only the caller can build (D3).
type Taken struct {
	// Reserved are homebot's own names: every registered command, hidden
	// ones included, and help, version, completion, man and homebot
	// (cli.reservedNames). Shell words and tool names are not in it:
	// Check has its own lists for those, with their own reasons.
	Reserved []string
	// Planned maps each name the home repo would place in ~/.local/bin
	// (linker.PlanLinks) to the repo's file that would be linked there.
	Planned map[string]string
}

// TakenFunc supplies the command and repo reservations after config has been
// read (or refreshed after a sync). It keeps selfname independent of cli.
type TakenFunc func(*app.Context) (Taken, error)

// Verdict is what Check says about a name.
type Verdict struct {
	// Reason is why the name is refused; empty when it may be used.
	Reason Reason
	// Path is the file the reason is about, when there is one.
	Path string
	// Existing is what ~/.local/bin/<name> holds, for an allowed name.
	Existing Existing
	// InWorkTree is the one warning (not an error): ~/.local/bin is inside
	// a git work tree, so another tool could commit the link.
	InWorkTree bool
}

// OK reports whether the name may be used.
func (v Verdict) OK() bool { return v.Reason == "" }

// Error is why Link, Adopt or Unlink changed nothing, or stopped. An error
// of the file system comes back as it is, not as an Error.
type Error struct {
	Reason Reason
	Path   string
}

func (e *Error) Error() string {
	if e.Path == "" {
		return "selfname: " + string(e.Reason)
	}
	return "selfname: " + string(e.Reason) + ": " + e.Path
}

// Rec is the naming record, <StateDir>/name (D2): which link homebot made
// or adopted on this machine. It is per machine and never synced.
type Rec struct {
	// Name is the name the link is for.
	Name string
	// Target is what the link points at, as written.
	Target string
	// Made is true when homebot made the link, false when it adopted the
	// user's.
	Made bool
	// Pending marks an absent path claimed before symlink creation. It is
	// never ownership evidence after a crash.
	Pending bool
	// Old and OldMade are set only during a rename, until the old link is
	// removed (D4): the old name, and whether homebot made its link.
	Old     string
	OldMade bool
}

// Why says why the effective name is homebot although a name is recorded.
type Why string

const (
	// Working: the name works on this machine; it is the effective name.
	Working Why = ""
	// NoRecord: there is no naming record.
	NoRecord Why = "no_record"
	// BadRecord: the record is unreadable, or holds a name that fails the
	// syntax.
	BadRecord Why = "bad_record"
	// LinkBroken: ~/.local/bin/<name> does not resolve to the same file as
	// proc.SelfPath; Path names the link.
	LinkBroken Why = "link_broken"
	// NotOnPath: the name is not found on PATH (~/.local/bin is not on it).
	NotOnPath Why = "not_on_path"
	// Shadowed: another program by that name comes first on PATH; Path
	// names it.
	Shadowed Why = "shadowed"
)

// State is the effective name and how it was reached (D2).
type State struct {
	// Name is the effective name: the recorded name when it works on this
	// machine, else homebot.
	Name string
	// Rec is the record, when HasRecord.
	Rec       Rec
	HasRecord bool
	// Why is why Name is homebot although a name is recorded.
	Why Why
	// Path is the file Why is about, when there is one.
	Path string
}

// ChangeKind is what a change did to a link.
type ChangeKind string

const (
	Made      ChangeKind = "made"
	Adopted   ChangeKind = "adopted"
	Repointed ChangeKind = "repointed"
	Removed   ChangeKind = "removed"
)

// Change is one change Link, Adopt or Unlink made in ~/.local/bin.
type Change struct {
	Kind ChangeKind
	// Name is the name the link is for, Path the link, Target what it
	// points at (empty for Removed).
	Name, Path, Target string
}

// OnChange is called for every change Link, Adopt and Unlink make, after
// the record says so, so the caller can add a journal line (D4: every make,
// adopt, re-point and remove adds one). selfname can't write the journal
// itself: the line is catalog text, and selfname never renders any. cli sets
// it once, before any command runs; tests that set it must not run in
// parallel.
var OnChange = func(c *app.Context, ch Change) {}
