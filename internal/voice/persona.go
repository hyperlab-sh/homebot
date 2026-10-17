// Package voice turns messages (msg.Text) into the words homebot says.
//
// Use Render for what is printed now, with ui.Println or ui.Eprintln (or
// through ui.Out or ui.Err): it styles counts and paths, and the ui writers
// fit those styles to the terminal. Use Record for journals, state, files in
// the home repo, commit messages and forge arguments. Notifications and the
// nudge cache use Nudge.
//
// An error that can be stored uses fail.Say. Its Error text stays neutral;
// the CLI's error handler voices it with ErrorText when it prints the line.
//
// A persona supplies safe alternatives to the catalog's English; neutral
// output always speaks as homebot.
package voice

import "github.com/hyperlab-sh/homebot/internal/msg"

// Persona is a voice: a speaker's name and its own wording for the messages
// a persona may reword, each line with the language to format it in.
type Persona = msg.Speaker

// Neutral is the voice without a persona: the catalog's English.
type Neutral = msg.Neutral
