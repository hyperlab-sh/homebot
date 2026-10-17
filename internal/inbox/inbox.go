// Package inbox works out what needs the person: it builds the list of items,
// writes the nudge and count caches, and notifies (src/inbox.ts).
package inbox

import (
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/gitx"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/selfname"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// Level says how loudly an item asks.
type Level string

const (
	Urgent Level = "urgent"
	Todo   Level = "todo"
)

// Fix is a homebot subcommand to run (Args) or a shell line to show (Shell).
type Fix struct {
	Args  []string
	Shell string
}

// Item is one thing that needs the person. Title and Detail are plain text
// (voice.Plain), already said for the terminal or notification that shows
// them; the cache files hold only the nudge line, symbol and counts.
type Item struct {
	ID     string
	Level  Level
	Title  string
	Detail string
	Fix    *Fix
}

// Options says what Refresh does besides rebuilding the caches.
type Options struct {
	Notify    bool               // tidy: notify about new urgent items, then daily
	NameTaken selfname.TakenFunc // cli's D3 reservations, read after sync
}

// FixText uses the command name that works on this machine. Fix.Args
// stays name-free in the inbox state and can be used on every machine.
func FixText(c *app.Context, f *Fix) string {
	if f == nil {
		return ""
	}
	if f.Args != nil {
		return selfname.Effective(c) + " " + strings.Join(f.Args, " ")
	}
	return f.Shell
}

// env is what every check reads: loaded once, shared, never written.
type env struct {
	c         *app.Context
	st        *state.State
	repo      string // c.Root, tildified, for the shell lines
	now       time.Time
	repoOK    bool
	nameTaken selfname.TakenFunc
}

// ms is now as JavaScript milliseconds, the unit of the state file.
func (e *env) ms() int64 { return e.now.UnixMilli() }

// check is one of the inbox's checks. A check with an id is run "safely",
// as the TypeScript did: an error it returns becomes an urgent
// broken:<id> item after the items it already found, with retry as the fix.
// An error from a check without an id fails the whole inbox.
type check struct {
	id    string
	retry func(e *env) *Fix
	run   func(e *env) ([]Item, error)
}

// Collect builds the inbox without writing anything (next uses it). The
// checks run in parallel; their items keep the TypeScript's order, then the
// urgent ones move to the front.
func Collect(c *app.Context, taken ...selfname.TakenFunc) ([]Item, error) {
	if err := c.LocateRoot(); err != nil {
		return nil, err
	}
	st, err := state.Load(c)
	if err != nil {
		return nil, err
	}
	e := &env{
		c:      c,
		st:     st,
		repo:   ui.Tildify(c.Root, c.Home),
		now:    c.Now(),
		repoOK: gitx.IsRepo(c, c.Root),
	}
	if len(taken) != 0 {
		e.nameTaken = taken[0]
	}
	return collect(e, checks)
}

func collect(e *env, checks []check) ([]Item, error) {
	slots := make([][]Item, len(checks))
	var g errgroup.Group
	for i, ch := range checks {
		g.Go(func() error {
			items, err := ch.run(e)
			if err != nil {
				if ch.id == "" {
					return err
				}
				items = append(items, Item{
					ID:     "broken:" + ch.id,
					Level:  Urgent,
					Title:  voice.Plain(e.c, msg.InboxBrokenTitle(ch.id)),
					Detail: voice.ErrorText(e.c, err),
					Fix:    ch.retry(e),
				})
			}
			slots[i] = items
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	var urgent, todo []Item
	for _, slot := range slots {
		for _, it := range slot {
			if it.Level == Urgent {
				urgent = append(urgent, it)
			} else {
				todo = append(todo, it)
			}
		}
	}
	return append(urgent, todo...), nil
}

// Refresh collects, rewrites the nudge, symbol and count cache files from the
// persona in effect now, and notifies when o.Notify. It rewrites all three on
// every run, so a changed persona shows up as soon as anything calls it.
func Refresh(c *app.Context, o Options) ([]Item, error) {
	items, err := Collect(c, o.NameTaken)
	if err != nil {
		return nil, err
	}
	if err := WriteCache(c, items); err != nil {
		return items, err
	}
	if o.Notify {
		if _, err := Notify(c, items); err != nil {
			return items, err
		}
	}
	return items, nil
}

// WriteCache writes the three cache files the shell reads at startup without
// running anything: nudge (the one-line start-up message, led by the persona's
// emoji or the truck), symbol (the persona's emoji for the zsh prompt, removed
// when there is no valid one to show) and count (the item count). The nudge
// and symbol are neutral for persona: none, --plain and a broken persona
// selected in homebot.yml (see backgroundCtx).
func WriteCache(c *app.Context, items []Item) error {
	emoji, text := NudgeLine(c, items)
	if err := state.WriteNudge(c, emoji, text); err != nil {
		return err
	}
	symbol := ""
	if v := persona.NudgeVoice(backgroundCtx(c)); v != nil {
		symbol = v.Emoji
	}
	if err := state.WriteSymbol(c, symbol); err != nil {
		return err
	}
	return state.WriteCount(c, len(items), countUrgent(items) > 0)
}

// configProblem is why the persona selected in homebot.yml cannot be used, or
// nil. A bad HOMEBOT_PERSONA is not one: it only warns, in that terminal.
func configProblem(c *app.Context) *persona.Problem {
	if p := persona.ActiveProblem(c); p != nil && p.Origin == persona.FromConfig {
		return p
	}
	return nil
}

// backgroundCtx is the Context whose voice the shell caches and desktop
// notifications use. A broken persona in homebot.yml falls back to Neatpik for
// what the person reads in a terminal, but what runs unattended stays neutral
// while the inbox offers the repair (persona_broken), so the nudge, the prompt
// symbol, the urgent suffix and the notifications all agree.
func backgroundCtx(c *app.Context) *app.Context {
	if configProblem(c) == nil {
		return c
	}
	return c.Fork(map[string]string{"HOMEBOT_PERSONA": persona.None})
}

func countUrgent(items []Item) int {
	n := 0
	for _, it := range items {
		if it.Level == Urgent {
			n++
		}
	}
	return n
}
