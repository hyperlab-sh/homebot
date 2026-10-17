// Package tidy is what the background helper does every hour. Only chores
// that can't lose anything happen here; everything else becomes an inbox item.
package tidy

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/exports"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/gitx"
	"github.com/hyperlab-sh/homebot/internal/guard"
	"github.com/hyperlab-sh/homebot/internal/inbox"
	"github.com/hyperlab-sh/homebot/internal/linker"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/save"
	"github.com/hyperlab-sh/homebot/internal/secrets"
	"github.com/hyperlab-sh/homebot/internal/selfname"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// flock is syscall.Flock; tests replace it.
var flock = syscall.Flock

// runExports is exports.RunExports; tests replace it.
var runExports = exports.RunExports

const (
	lockName = "tidy.lock"
	// hourlyAutosaveMs and dailyAutosaveMs are how long since the last
	// autosave before one is due. They sit a little under the cadence so a
	// timer that fires a bit early still saves.
	hourlyAutosaveMs = 50 * 60_000
	dailyAutosaveMs  = 20 * 3600_000
)

// Options say what a tidy does besides its chores.
type Options struct {
	// Notify lets the inbox refresh send a desktop notification.
	Notify bool
	// NameTaken supplies cli's reserved commands and the current link plan.
	NameTaken selfname.TakenFunc
}

// Result is what a tidy did. Ran is false when another tidy held the lock,
// and nothing else is set then.
type Result struct {
	Ran bool
	// Did are the chores that changed something, as plain text; each is also
	// in the journal.
	Did []string
	// Errors are the chores that failed. Each holds the failure as recorded in
	// state.json and, beside it, the same failure as it is said at the terminal.
	Errors []Failure
	// Warnings are problems a chore met that did not stop it, as plain text;
	// each is also in the journal, where the inbox shows it.
	Warnings []string
	// Items is the inbox after the run.
	Items []inbox.Item
}

// Failure is a chore that failed: the failure as it is recorded, and the same
// failure as it is said.
type Failure struct {
	// TidyError is the failure as recorded in state.json and the journal. Its
	// message is neutral English, whatever persona speaks.
	state.TidyError
	// Speech is the message as ctx's voice says it, for the terminal only. It
	// is not part of TidyError, so nothing that stores a TidyError can take it
	// along; the tag keeps it out of any JSON of a Failure too.
	Speech string `json:"-"`
}

// Said is what the terminal prints for the failure: Speech, or the recorded
// message when none was spoken.
func (f Failure) Said() string {
	if f.Speech != "" {
		return f.Speech
	}
	return f.Message
}

// lock takes the exclusive, non-blocking flock on tidy.lock in the state
// directory. It returns nil without an error when another process holds it.
// The kernel drops the lock when the process dies, so a crashed tidy never
// leaves one behind; the file itself stays, as it is the lock's name, not
// its state.
func lock(c *app.Context) (release func(), err error) {
	if err := os.MkdirAll(c.StateDir, 0o777); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(c.StateDir, lockName), os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		return nil, err
	}
	for {
		err = flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, nil
		}
		return nil, fail.Say(msg.TidyErrorLock(f.Name(), err.Error()))
	}
	return func() { _ = f.Close() }, nil
}

// AutosaveDue says whether the config's commit cadence calls for an
// autosave now.
func AutosaveDue(c *app.Context) bool {
	cadence := config.CadenceOff
	if c.Config != nil {
		cadence = c.Config.Autosave.Commit
	}
	if cadence == config.CadenceOff {
		return false
	}
	st, err := state.Load(c)
	if err != nil {
		return false
	}
	interval := int64(dailyAutosaveMs)
	if cadence == config.CadenceHourly {
		interval = hourlyAutosaveMs
	}
	return c.Now().UnixMilli()-st.LastAutosave >= interval
}

// run is one chore's body. A chore may report more than one message (sync
// can both pull and push); the caller speaks them and records them separately.
type run func() (did []msg.Text, err error)

type warning struct{ spoken, stored string }

// Run does the hourly chores, in this order: the commit guard, seal,
// exports, autosave, sync, link. A chore that fails is recorded (journal,
// state.json) and the rest still run, except that a failed guard skips the
// autosave, which is then recorded as failed with the reason. It then stamps state.LastTidy,
// refreshes the inbox and returns. When another tidy holds the lock it does
// nothing and returns Result{Ran: false}.
func Run(c *app.Context, o Options) (Result, error) {
	if err := c.LocateRoot(); err != nil {
		return Result{}, err
	}
	release, err := lock(c)
	if err != nil {
		return Result{}, err
	}
	if release == nil {
		return Result{}, nil
	}
	defer release()

	res := Result{Ran: true}
	var failed []Failure
	step := func(name string, fn run) error {
		did, err := fn()
		if cerr := c.Context().Err(); cerr != nil {
			// Ctrl-C or SIGTERM stops between chores, before state.json is touched.
			return cerr
		}
		if err == nil && len(did) > 0 {
			spoken, stored := make([]string, 0, len(did)), make([]string, 0, len(did))
			for _, text := range did {
				spoken = append(spoken, voice.Plain(c, text))
				stored = append(stored, voice.Record(c, text))
			}
			res.Did = append(res.Did, strings.Join(spoken, ", "))
			// As in the TypeScript, a journal write that fails after a chore
			// succeeded counts as that chore failing.
			err = journal(c, strings.Join(stored, ", "), false)
		}
		if err != nil {
			message := err.Error()
			failed = append(failed, Failure{
				TidyError: state.TidyError{Step: name, Message: message, At: c.Now().UnixMilli()},
				Speech:    voice.ErrorText(c, err),
			})
			return journal(c, voice.Record(c, msg.TidyJournalFailed(name, message)), true)
		}
		return nil
	}

	repo := gitx.IsRepo(c, c.Root)
	if repo {
		if err := step("guard", func() ([]msg.Text, error) { return ensureGuard(c) }); err != nil {
			return res, err
		}
	}
	if err := step("seal", func() ([]msg.Text, error) { return sealStep(c) }); err != nil {
		return res, err
	}
	if err := step("export", func() ([]msg.Text, error) {
		did, warnings, err := exportStep(c)
		// Journaled like what a chore did, so the inbox shows them; a
		// journal write that fails is the chore failing, as for did.
		for _, w := range warnings {
			res.Warnings = append(res.Warnings, w.spoken)
			if jerr := journal(c, w.stored, false); jerr != nil {
				return nil, errors.Join(err, jerr)
			}
		}
		return did, err
	}); err != nil {
		return res, err
	}
	if repo && AutosaveDue(c) {
		autosave := func() ([]msg.Text, error) { return autosaveStep(c) }
		// No autosave without the guard in place: the step is recorded as
		// failed with the reason, like any other chore failure (D14).
		if slices.ContainsFunc(failed, func(f Failure) bool { return f.Step == "guard" }) {
			autosave = func() ([]msg.Text, error) {
				return nil, fail.Say(msg.TidyErrorAutosaveNoGuard())
			}
		}
		if err := step("autosave", autosave); err != nil {
			return res, err
		}
	}
	if repo {
		if err := step("sync", func() ([]msg.Text, error) { return syncStep(c) }); err != nil {
			return res, err
		}
	}
	if err := step("link", func() ([]msg.Text, error) { return linkStep(c) }); err != nil {
		return res, err
	}
	if err := step("name", func() ([]msg.Text, error) { return nameStep(c, o.NameTaken) }); err != nil {
		return res, err
	}

	// Only the recorded form of each failure goes to state.json.
	var recorded []state.TidyError
	for _, f := range failed {
		recorded = append(recorded, f.TidyError)
	}
	if err := state.Update(c, func(s *state.State) {
		s.LastTidy = c.Now().UnixMilli()
		s.TidyErrors = recorded
	}); err != nil {
		return res, err
	}
	res.Errors = failed
	res.Items, err = inbox.Refresh(c, inbox.Options{Notify: o.Notify, NameTaken: o.NameTaken})
	return res, err
}

// nameStep uses the config as it stands after sync, including a newly pulled
// name. A D3 refusal is a machine-local inbox item, not a failed tidy chore.
func nameStep(c *app.Context, takenFunc selfname.TakenFunc) ([]msg.Text, error) {
	cfg, err := config.LoadDebug(c.Root, c.Debug)
	if err != nil {
		return nil, err
	}
	c.Config = cfg
	name := cfg.Name
	if name == "" {
		_, had := selfname.Record(c)
		if err := selfname.Unlink(c); err != nil {
			return nil, err
		}
		if had {
			return []msg.Text{msg.TidyDidNameOff()}, nil
		}
		return nil, nil
	}
	var taken selfname.Taken
	if takenFunc != nil {
		taken, err = takenFunc(c)
		if err != nil {
			return nil, err
		}
	}
	if v := selfname.Check(c, name, taken); !v.OK() {
		return nil, nil
	}
	before, had := selfname.Record(c)
	link := filepath.Join(c.Home, ".local", "bin", name)
	prior, priorErr := os.Readlink(link)
	if err := selfname.LinkInBin(c, name); err != nil {
		var refused *selfname.Error
		if errors.As(err, &refused) {
			return nil, nil
		}
		return nil, err
	}
	after, has := selfname.Record(c)
	now, nowErr := os.Readlink(link)
	if had != has || before != after || prior != now || !errors.Is(priorErr, nowErr) {
		return []msg.Text{msg.TidyDidNamed()}, nil
	}
	return nil, nil
}

func journal(c *app.Context, text string, isErr bool) error {
	return state.Journal(c, state.Entry{Msg: text, Error: isErr})
}

func ensureGuard(c *app.Context) ([]msg.Text, error) {
	changed, err := guard.InstallHooks(c)
	if err != nil || !changed {
		return nil, err
	}
	return []msg.Text{msg.TidyDidGuard()}, nil
}

func sealStep(c *app.Context) ([]msg.Text, error) {
	if _, err := os.Stat(c.Layout().Recipients); err != nil {
		return nil, nil
	}
	sealed, err := secrets.Seal(c)
	if err != nil || len(sealed.Sealed) == 0 {
		return nil, err
	}
	return []msg.Text{msg.TidyDidSealed(len(sealed.Sealed))}, nil
}

// exportStep runs the exports that are due. It returns what changed, and the
// warnings of the exports that ran, those before a failing one included.
func exportStep(c *app.Context) ([]msg.Text, []warning, error) {
	results, err := runExports(c, exports.Options{DueOnly: true})
	var warnings []warning
	for _, r := range results {
		if r.Warning != "" {
			stored := r.Warning
			if r.WarningText.ID() != "" {
				stored = voice.Record(c, r.WarningText)
			}
			warnings = append(warnings, warning{spoken: r.Warning, stored: stored})
		}
	}
	if err != nil {
		return nil, warnings, err
	}
	var outs []string
	for _, r := range results {
		if r.Changed {
			outs = append(outs, r.Out)
		}
	}
	if len(outs) == 0 {
		return nil, warnings, nil
	}
	return []msg.Text{msg.TidyDidExported(strings.Join(outs, ", "))}, warnings, nil
}

func autosaveStep(c *app.Context) ([]msg.Text, error) {
	r, err := save.Save(c, save.Options{Auto: true})
	if err != nil || !r.Committed {
		return nil, err
	}
	return []msg.Text{msg.TidyDidCommitted(len(r.Files))}, nil
}

func syncStep(c *app.Context) ([]msg.Text, error) {
	push := c.Config != nil && c.Config.Autosave.Push
	r, err := save.Sync(c, save.SyncOptions{Push: push})
	if err != nil {
		return nil, err
	}
	var parts []msg.Text
	if r.Pulled > 0 {
		parts = append(parts, msg.TidyDidPulled(r.Pulled))
	}
	if r.Pushed > 0 {
		parts = append(parts, msg.TidyDidPushed(r.Pushed))
	}
	return parts, nil
}

func linkStep(c *app.Context) ([]msg.Text, error) {
	plan, err := linker.PlanLinks(c)
	if err != nil {
		return nil, err
	}
	opts := linker.ApplyOptions{Safe: true}
	if err := linker.ApplyLinks(c, plan, opts); err != nil {
		return nil, err
	}
	n := len(plan.Changes(opts))
	if n == 0 {
		return nil, nil
	}
	return []msg.Text{msg.TidyDidLinked(n)}, nil
}
