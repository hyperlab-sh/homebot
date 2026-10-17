package msg

import (
	"fmt"
	"strings"

	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/msg/catalog"
)

// Speaker is who says a Text: a persona (internal/voice), or Neutral.
type Speaker interface {
	// Name is the speaker, what {bot} becomes. A name UnsafeRune finds
	// anything in is never printed: the renderer then speaks as Neutral.
	Name() string
	// Line is the speaker's text for id and the language to format it in;
	// the zero language.Tag (language.Und) means English. A fallback is
	// always formatted as English.
	//
	// The line is in the catalog's syntax, and ok reports whether the
	// speaker has one. It is asked only for messages a persona may reword,
	// and used only if CheckLine passes it; the language decides which
	// plural form a count picks in it.
	Line(id string) (line string, lang language.Tag, ok bool)
}

// Neutral is the speaker without a persona: the catalog's English, said by
// homebot.
type Neutral struct{}

// Name is homebot.
func (Neutral) Name() string { return NeutralName }

// Line never has a line of its own.
func (Neutral) Line(string) (string, language.Tag, bool) { return "", language.Und, false }

// Speak is t as s says it: t's text, or s's line for it, filled with t's
// values, then the append. style, when not nil, gets each value with its
// placeholder's name and returns it styled. Nested messages, such as a
// duration, are said by s as well, without styles.
//
// Speak is the last line of defence against a bad persona, whatever loaded
// it: s's line is used only when CheckLine passes it, and otherwise t is
// said with the catalog's text. When UnsafeRune finds anything in s's name,
// Speak says t as Neutral does. A line s supplies is formatted with the
// language s gives for it (English for language.Und); the catalog's text
// always with English rules.
//
// Speak is the one renderer behind voice.Render and voice.Plain and the
// Context-free text of a fail.Say error. A forbidigo rule (.golangci.yml)
// keeps every other package off it: they speak through the voice, which
// picks the Context's persona.
func Speak(s Speaker, t Text, style func(name, value string) string) string {
	return speak(s, t, NeutralName, style)
}

// SpeakWithSelf renders a printed message with the command name that works on
// this machine. Stored text continues through Speak with homebot.
func SpeakWithSelf(s Speaker, t Text, self string, style func(name, value string) string) string {
	if self == "" {
		self = NeutralName
	}
	return speak(s, t, self, style)
}

func speak(s Speaker, t Text, self string, style func(name, value string) string) string {
	if t.m == nil {
		return ""
	}
	if _, _, unsafe := UnsafeRune(s.Name()); unsafe {
		s = Neutral{}
	}
	tmpl, lang, ok := catalog.Template{}, language.English, false
	if t.m.persona {
		if line, lineLang, has := s.Line(t.m.id); has {
			var err *LineError
			if tmpl, err = checkLine(t.m, line); err == nil {
				ok, lang = true, lineLang
				if lang == language.Und {
					lang = language.English
				}
			}
		}
	}
	if !ok {
		var err error
		tmpl, err = catalog.Parse(t.m.text)
		if err != nil {
			panic("msg: catalog text of " + t.m.id + " does not parse; run go generate: " + err.Error())
		}
		lang = language.English
	}
	bot := s.Name()
	if self != NeutralName {
		bot = self
	}
	values := t.ValuesWithSelf(bot, self, func(nested Text) string { return speak(s, nested, self, nil) })
	return tmpl.Format(lang, values, style) + strings.ReplaceAll(t.m.append, catalog.SelfRef, self)
}

// LineProblem is why the renderer will not say a persona line.
type LineProblem int

// The problems CheckLine finds, in the order it looks for them.
const (
	// LineUnknownID: no catalog entry has the id.
	LineUnknownID LineProblem = iota + 1
	// LineNotVoiced: the entry is not persona: true, so no persona line
	// is ever asked for.
	LineNotVoiced
	// LineUnsafe: UnsafeRune finds Rune at byte At of the line.
	LineUnsafe
	// LineSyntax: the line does not parse; Err says why.
	LineSyntax
	// LineUnknownName: the line names {Name}, which the entry does not
	// declare. {bot} is no exception: it is known only in entries that
	// declare bot.
	LineUnknownName
	// LineSelector: the line puts a plural selector on {Name}, which is not
	// an int placeholder.
	LineSelector
	// LineMissing: the line leaves out {Name}, a placeholder that is not
	// optional (a fixed one included).
	LineMissing
)

// LineError is a persona line CheckLine refuses, and why.
type LineError struct {
	ID      string
	Problem LineProblem
	Name    string // the placeholder, for LineUnknownName, LineSelector and LineMissing
	Rune    rune   // for LineUnsafe
	At      int    // the byte offset in the line, for LineUnsafe
	Err     error  // for LineSyntax
}

func (e *LineError) Error() string {
	var why string
	switch e.Problem {
	case LineUnknownID:
		why = "no catalog entry has this id"
	case LineNotVoiced:
		why = "the entry is not persona: true"
	case LineUnsafe:
		why = fmt.Sprintf("%U at byte %d is not allowed", e.Rune, e.At)
	case LineSyntax:
		why = e.Err.Error()
	case LineUnknownName:
		why = fmt.Sprintf("{%s} is not a placeholder of the entry", e.Name)
	case LineSelector:
		why = fmt.Sprintf("{%s} is not an int, so it takes no plural forms", e.Name)
	case LineMissing:
		why = fmt.Sprintf("{%s} is required", e.Name)
	}
	return "msg: persona line for " + e.ID + ": " + why
}

func (e *LineError) Unwrap() error { return e.Err }

// CheckLine reports whether the renderer would say line, a persona's text
// for the entry id, and if not, why, as a *LineError: the one rule set the
// renderer and the persona validator share, so a line that validates is a
// line that is said. A line passes when the entry is persona: true and the
// line holds nothing UnsafeRune finds, parses, names only placeholders the
// entry declares ({bot} only where the entry declares bot), puts plural
// selectors only on int placeholders and names every placeholder that is
// not optional, fixed ones included. It finds the first problem only.
func CheckLine(id, line string) error {
	i, ok := index()[id]
	if !ok {
		return &LineError{ID: id, Problem: LineUnknownID}
	}
	if _, err := checkLine(&messages[i], line); err != nil {
		return err
	}
	return nil
}

// checkLine parses line for m if CheckLine passes it.
func checkLine(m *message, line string) (catalog.Template, *LineError) {
	refuse := func(e LineError) (catalog.Template, *LineError) {
		e.ID = m.id
		return catalog.Template{}, &e
	}
	if !m.persona {
		return refuse(LineError{Problem: LineNotVoiced})
	}
	if r, at, unsafe := UnsafeRune(line); unsafe {
		return refuse(LineError{Problem: LineUnsafe, Rune: r, At: at})
	}
	tmpl, err := catalog.Parse(line)
	if err != nil {
		return refuse(LineError{Problem: LineSyntax, Err: err})
	}
	slots := make(map[string]Slot, len(m.slots))
	for _, sl := range m.slots {
		slots[sl.Name] = sl
	}
	used := map[string]bool{}
	for _, r := range tmpl.Refs() {
		sl, declared := slots[r.Name]
		switch {
		case !declared:
			return refuse(LineError{Problem: LineUnknownName, Name: r.Name})
		case r.Forms != nil && sl.Type != catalog.Int:
			return refuse(LineError{Problem: LineSelector, Name: r.Name})
		}
		used[r.Name] = true
	}
	for _, sl := range m.slots {
		if !sl.Optional && !used[sl.Name] {
			return refuse(LineError{Problem: LineMissing, Name: sl.Name})
		}
	}
	return tmpl, nil
}
