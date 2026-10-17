package cli

import "slices"

// fixedReserved are homebot's names besides the registered commands: the
// commands the root adds itself, cobra's and man's, and homebot.
var fixedReserved = []string{"help", "version", "completion", "man", "homebot"}

// reservedNames are homebot's own names, which the user's name for it may
// not take (naming plan D3), sorted: every registered command, hidden ones
// included, and the fixed list. It is selfname.Taken.Reserved. The shells'
// words and the programs homebot runs are not in it: selfname.Check has its
// own lists for those (selfname.ShellWords, selfname.ToolNames), with their
// own reasons.
func reservedNames() []string {
	names := slices.Clone(fixedReserved)
	for _, r := range registry {
		names = append(names, r.name)
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// ReservedNames exposes the command names to the generated picker preview.
// The CLI still checks them on the current machine before changing anything.
func ReservedNames() []string { return reservedNames() }
