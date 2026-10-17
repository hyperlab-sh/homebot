package fail

import "strconv"

// Exit ends the command with exit code Code and prints nothing more: the
// command has already said what went wrong, as the TypeScript did when it
// set process.exitCode. Error() is never shown to a person.
//
// Wrapping an *Exit prints nothing either: the CLI prints nothing for an
// error whose chain holds one, so the "link: " of fmt.Errorf("link: %w",
// exit) is never seen. Say what went wrong before returning it.
type Exit struct {
	Code int
}

func (e *Exit) Error() string { return "exit status " + strconv.Itoa(e.Code) }

// Silently returns an *Exit with code.
func Silently(code int) *Exit { return &Exit{Code: code} }
