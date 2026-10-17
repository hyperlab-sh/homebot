package triage

import (
	"context"
	"os"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// picker is the full-screen triage: the current item with a glimpse of
// what a folder holds, the menu, and what was decided so far. One key
// answers; there is no Enter to press. It decides through the same Decide
// as the line prompt, synchronously in Update, so a decision is saved
// before the next item shows.
type picker struct {
	c     *app.Context
	items []Candidate
	i     int      // the item being asked about
	desc  string   // Describe of items[i]
	peek  []string // what items[i] holds when it is a folder
	// done holds one line per decision made, rendered. Each item is
	// answered at most once, so it never holds more lines than items.
	done        []string
	wrong       bool // the last key meant nothing here
	quit        bool
	interrupted bool // the person pressed Ctrl-C
	width       int
	height      int
	// decide carries out an answer; result in production.
	decide func(Candidate, Answer) (string, bool)
}

// peekNames is how many names of a folder the picker shows at most.
const peekNames = 8

func newPicker(c *app.Context, items []Candidate) *picker {
	p := &picker{c: c, items: items}
	p.decide = func(cand Candidate, a Answer) (string, bool) { return result(c, cand, a) }
	p.load()
	return p
}

// load reads what the picker shows about the current item.
func (p *picker) load() {
	p.desc, p.peek = "", nil
	if p.i >= len(p.items) {
		return
	}
	cand := p.items[p.i]
	p.desc = Describe(p.c, cand)
	if entries, err := os.ReadDir(cand.Abs); err == nil {
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() {
				name += "/"
			}
			p.peek = append(p.peek, name)
		}
		slices.Sort(p.peek)
	}
}

func (p *picker) Init() tea.Cmd { return nil }

func (p *picker) Update(m tea.Msg) (tea.Model, tea.Cmd) {
	switch m := m.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = m.Width, m.Height
	case tea.KeyPressMsg:
		// Keys typed ahead can arrive after tea.Quit, before the program
		// stops: they must decide nothing.
		if p.quit {
			return p, nil
		}
		key := m.String()
		switch key {
		case "ctrl+c":
			p.interrupted = true
			key = "q"
		case "esc":
			key = "q"
		case "enter":
			key = ""
		}
		answer, ok := answerFor(p.items[p.i], key)
		if !ok {
			p.wrong = true
			return p, nil
		}
		p.wrong = false
		if answer == Quit {
			p.quit = true
			return p, tea.Quit
		}
		if line, ok := p.decide(p.items[p.i], answer); ok {
			p.done = append(p.done, line)
		}
		p.i++
		p.load()
		if p.i >= len(p.items) {
			p.quit = true
			return p, tea.Quit
		}
	}
	return p, nil
}

// View is the header, the latest decisions and the current item. Once the
// picker has quit it is empty: runPicker prints the header and every
// decision after the program ends, so none is lost to a short terminal.
// While asking, only the decisions that fit the terminal's height show.
func (p *picker) View() tea.View {
	if p.quit {
		return tea.NewView("")
	}
	c := p.c
	st := ui.Styles(c)
	cand := p.items[p.i]
	item := []string{"", p.itemLine(cand)}
	if len(p.peek) > 0 {
		item = append(item, st.Muted.Render(p.peekLine()))
	}
	item = append(item, st.Muted.Render(voice.Render(c, menu(c, cand))))
	if p.wrong {
		item = append(item, st.Muted.Render(voice.Render(c, msg.TriageHintPick())))
	}
	lines := []string{header(c, len(p.items))}
	used := len(lines)
	for _, l := range item {
		used += strings.Count(l, "\n") + 1
	}
	lines = append(lines, p.recent(used)...)
	lines = append(lines, item...)
	return tea.NewView(strings.Join(lines, "\n") + "\n")
}

// recent is the latest decision lines that fit beside used other lines, all
// of them while the height is unknown.
func (p *picker) recent(used int) []string {
	var lines []string
	for _, d := range p.done {
		lines = append(lines, strings.Split(d, "\n")...)
	}
	if p.height <= 0 {
		return lines
	}
	room := max(p.height-used, 0)
	if len(lines) > room {
		lines = lines[len(lines)-room:]
	}
	return lines
}

// summary is what stays on the terminal once the picker ends: the header
// and every decision.
func (p *picker) summary() string {
	return strings.Join(append([]string{header(p.c, len(p.items))}, p.done...), "\n")
}

// itemLine is the prompt's "[i/n] key  (description)" for the current item.
func (p *picker) itemLine(cand Candidate) string {
	st := ui.Styles(p.c)
	line := voice.Render(p.c, msg.TriagePromptItem(p.i+1, len(p.items), cand.Key, st.Muted.Render(p.desc)))
	counter, rest, _ := strings.Cut(line, " ")
	return st.Accent.Render(counter) + " " + rest
}

// peekLine is "  a, b/, c, ..." : the folder's first names, as many as fit
// the terminal (and peekNames at most).
func (p *picker) peekLine() string {
	width := p.width
	if width <= 0 {
		width = 80
	}
	line := " "
	for i, name := range p.peek {
		if i == peekNames {
			return line + ", ..."
		}
		sep := " "
		if i > 0 {
			sep = ", "
		}
		next := line + sep + name
		if i > 0 && len(next)+len(", ...") > width {
			return line + ", ..."
		}
		line = next
	}
	return line
}

// runPicker asks about items in the picker on the terminal, then prints
// what was decided. Ctrl-C exits 130, as it does in the line prompt.
func runPicker(c *app.Context, items []Candidate) error {
	in, out, _ := ui.Attached(c)
	p := newPicker(c, items)
	prog := tea.NewProgram(p,
		tea.WithInput(in), tea.WithOutput(out),
		tea.WithEnvironment(c.Env.Environ()), tea.WithContext(c.Context()))
	_, err := prog.Run()
	if err == nil || len(p.done) > 0 {
		ui.Println(c, p.summary())
	}
	if ctx := c.Context(); ctx.Err() != nil {
		return context.Cause(ctx)
	}
	if err != nil {
		return err
	}
	if p.interrupted {
		return fail.Silently(130)
	}
	return nil
}
