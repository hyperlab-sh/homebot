// Package ui is how homebot lays text out on a terminal: the palette, the
// writers that fit colours to the terminal, wrapping, and the small
// formatting helpers of src/ui.ts. The words come from internal/msg and
// internal/voice; ui decides only where they go and how they look.
//
// Everything homebot prints goes through Println and Eprintln, or the
// writers Out and Err for anything else. Styled text (Styles, Command,
// voice.Render) always carries full-colour escape sequences; only these
// writers bring them down to what the terminal shows, or remove them for a
// pipe, a file, NO_COLOR or TERM=dumb. Never write to ctx.Stdout or
// ctx.Stderr directly, nor to a cobra command's writers or Print methods
// (golangci-lint's forbidigo refuses them outside ui and tests), and never
// keep styled text: use voice.Plain for anything stored or sent elsewhere.
package ui
