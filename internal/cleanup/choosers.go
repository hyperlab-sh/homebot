package cleanup

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/term"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// DefaultChooser is fzf when it is installed, with the diff in its preview
// pane, and a select otherwise. The select is a numbered prompt on plain
// lines, huh's accessible mode, when ACCESSIBLE is set to anything or when
// stdout is not a terminal (homebot cleanup | tee log), where a full-screen
// menu would draw where no one sees it.
func DefaultChooser(c *app.Context) Chooser {
	if proc.HasCommand(c, "fzf") {
		return fzfChooser(c)
	}
	return selectChooser(c, c.Env.Get("ACCESSIBLE") != "" || !stdoutIsTerminal(c))
}

// stdoutIsTerminal reports whether the stdout the select would draw on is a
// terminal.
func stdoutIsTerminal(c *app.Context) bool {
	_, out, _ := ui.Attached(c)
	f, ok := out.(term.File)
	return ok && term.IsTerminal(f.Fd())
}

// choices are the answers offered for q, in menu order. Quit is not among
// them: fzf stops with Esc, and selectChooser adds it.
func choices(q Question) []Answer {
	if q.CanUse {
		return []Answer{Delete, Keep, Use, Merge}
	}
	return []Answer{Delete, Keep}
}

// choiceLine is the menu line for answer: the word, padded, then what it
// does; merge names its tool.
func choiceLine(c *app.Context, answer Answer) string {
	var meaning msg.Text
	tool := ""
	switch answer {
	case Delete:
		meaning = msg.CleanupChoiceDelete()
	case Keep:
		meaning = msg.CleanupChoiceKeep()
	case Use:
		meaning = msg.CleanupChoiceUse()
	case Merge:
		meaning = msg.CleanupChoiceMerge()
		tool = " (git add -p)"
		if hasLazygit(c) {
			tool = " (lazygit)"
		}
	default:
		meaning = msg.CleanupChoiceQuit()
	}
	return voice.Plain(c, msg.CleanupChoiceLine(padRight(string(answer), 7), voice.Plain(c, meaning), tool))
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// same is what stands for an empty diff.
func same(c *app.Context) string { return voice.Plain(c, msg.CleanupDiffSame()) }

// fzfChooser asks with one fzf menu per backup, the diff in the preview
// pane. Esc, or anything but a known word, means Quit.
func fzfChooser(c *app.Context) Chooser {
	preview := filepath.Join(c.CacheDir, "cleanup-preview.diff")
	return func(b Backup, q Question) (Answer, error) {
		if err := os.MkdirAll(c.CacheDir, 0o755); err != nil {
			return Quit, err
		}
		diff := q.Diff
		if diff == "" {
			diff = same(c) + "\n"
		}
		if err := os.WriteFile(preview, []byte(diff), 0o644); err != nil {
			return Quit, err
		}
		var menu strings.Builder
		for _, a := range choices(q) {
			menu.WriteString(choiceLine(c, a) + "\n")
		}
		header := voice.Plain(c, msg.CleanupFzfHeader(ui.Tildify(b.Path, c.Home), q.Index, q.Total))
		if q.CanUse && hasLazygit(c) {
			header += voice.Plain(c, msg.CleanupFzfHeaderMerge(voice.Plain(c, msg.CleanupPickerLazygit())))
		}
		argv := []string{
			"fzf", "--no-sort", "--layout=reverse", "--no-multi",
			"--prompt", voice.Plain(c, msg.CleanupFzfPrompt()), "--header", header,
			"--preview", "cat " + ui.ShellQuote(preview), "--preview-window", "down,75%,wrap",
		}
		var out bytes.Buffer
		_, _, errOut := ui.Attached(c)
		code, err := runTool(c, c.Env, "", argv, strings.NewReader(menu.String()), &out, errOut)
		if err != nil {
			return Quit, err
		}
		if code != 0 {
			return Quit, nil
		}
		if fields := strings.Fields(out.String()); len(fields) > 0 {
			switch a := Answer(fields[0]); a {
			case Delete, Keep, Use, Merge:
				return a, nil
			}
		}
		return Quit, nil
	}
}

// selectChooser prints the diff and asks with a huh select; Enter alone
// keeps, and so does a closed stdin, also after a wrong entry. Accessible
// mode asks for a number on a plain line instead of drawing the menu.
//
// It is the one place that asks without fzf: replacing the select (with the
// TypeScript's letter prompt, say) changes only this function.
func selectChooser(c *app.Context, accessible bool) Chooser {
	return func(b Backup, q Question) (Answer, error) {
		st := ui.Styles(c)
		ui.Println(c)
		if q.Diff != "" {
			ui.Println(c, strings.TrimRight(q.Diff, " \t\r\n"))
		} else {
			ui.Println(c, st.Muted.Render(same(c)))
		}
		answers := append(choices(q), Quit)
		options := make([]huh.Option[Answer], len(answers))
		for i, a := range answers {
			options[i] = huh.NewOption(choiceLine(c, a), a)
		}
		answer := Keep
		title := voice.Plain(c, msg.CleanupPromptHeader(q.Index, q.Total, ui.Tildify(b.Path, c.Home)))
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[Answer]().Title(title).Options(options...).Value(&answer),
		)).WithShowHelp(false)
		if accessible {
			// One byte at a time, so the next question reads the next line.
			form = form.WithAccessible(true).WithInput(&byteReader{r: c.Stdin}).WithOutput(ui.Out(c))
		} else {
			in, out, _ := ui.Attached(c)
			// WithProgramOptions replaces the options, so it comes first.
			form = form.WithProgramOptions(tea.WithEnvironment(c.Env.Environ())).WithInput(in).WithOutput(out)
		}
		err := form.RunWithContext(c.Context())
		if ctxErr := c.Context().Err(); ctxErr != nil {
			return Quit, ctxErr
		}
		// Ctrl-C in the select stops the command with 130, as the triage
		// picker and the signal path do, so `homebot cleanup && ...` stops
		// there; no summary, no inbox refresh. Choosing Quit is still a
		// clean exit 0.
		if errors.Is(err, huh.ErrUserAborted) {
			return Quit, fail.Silently(130)
		}
		if err != nil {
			return Quit, err
		}
		return answer, nil
	}
}

// byteReader reads one byte per Read, so a reader that buffers what it reads
// never takes more than the line it needs. Past the end of its input, or
// after any read error, it reads empty lines: huh's accessible prompt answers end of input with the
// last line typed, even a wrong one, and then indexes its options with it;
// an empty line instead takes the default, which always exists here.
type byteReader struct {
	r   io.Reader
	eof bool
}

func (b *byteReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if !b.eof {
		// Any error ends the input, not only io.EOF: an EIO on a hung-up
		// terminal would also stop huh at the last line typed.
		n, err := b.r.Read(p[:1])
		if err != nil {
			b.eof = true
		}
		if n > 0 || err == nil {
			return n, nil
		}
	}
	p[0] = '\n'
	return 1, nil
}

// toolGrace is how long a tool has to exit after SIGTERM when homebot is
// stopped, before SIGKILL; as proc.Run gives, so lazygit can restore the
// terminal.
const toolGrace = 500 * time.Millisecond

// runTool runs argv in dir (the current folder when empty) with the given
// streams and environment, and returns its exit code. An error means it
// could not run at all.
func runTool(c *app.Context, env app.Env, dir string, argv []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	return proc.RunAttached(c, argv, proc.AttachedOptions{
		Dir: dir, Env: env,
		Stdin: stdin, Stdout: stdout, Stderr: stderr,
		Grace: toolGrace,
	})
}
