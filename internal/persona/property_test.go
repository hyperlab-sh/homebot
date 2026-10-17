package persona

import (
	"fmt"
	"math/rand"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/quick"
	"unicode/utf8"

	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/msg/catalog"
)

// The property test of Task 4 (plan S5 and the Tests list): take the English
// of every voiced entry, mutate its placeholders (drop, rename, duplicate,
// reorder) and its layout, and require that Validate agrees with an oracle
// written from the S5 rules. For every variant Validate accepts, the entry
// built with msg.Build from distinct sentinel values and spoken by a test
// speaker that returns the variant must keep every required and fixed value
// (N5).
//
// The oracle shares no code with the validator: it has its own scanner for
// the placeholder syntax and its own reading of V14 to V24 and V26.

// pref is a placeholder found by the oracle's scanner.
type pref struct {
	name, forms string
	selector    bool
	src         string
	start, end  int // byte offsets of src in the line
}

var (
	pName       = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	pPrefix     = regexp.MustCompile(`^\{[a-z][a-z0-9_]*\}: `)
	pCategories = []string{"zero", "one", "two", "few", "many", "other"}
)

// pScan reads the placeholders of line: {name}, {name:forms}, with {{ and }}
// as literal braces. ok is false when the line does not parse: an unclosed {,
// a lone }, a nested {, a bad name or bad forms.
func pScan(line string) (refs []pref, ok bool) {
	for i := 0; i < len(line); {
		switch {
		case strings.HasPrefix(line[i:], "{{"), strings.HasPrefix(line[i:], "}}"):
			i += 2
		case line[i] == '}':
			return nil, false
		case line[i] == '{':
			end := strings.IndexByte(line[i:], '}')
			if end < 0 {
				return nil, false
			}
			src := line[i : i+end+1]
			body := src[1 : len(src)-1]
			if strings.Contains(body, "{") {
				return nil, false
			}
			name, forms, selector := strings.Cut(body, ":")
			if !pName.MatchString(name) || selector && !pFormsOK(forms) {
				return nil, false
			}
			refs = append(refs, pref{name: name, forms: forms, selector: selector, src: src, start: i, end: i + end + 1})
			i += end + 1
		default:
			i++
		}
	}
	return refs, true
}

// pFormsOK: "a|b" (one|other), or "one=a|few=b|other=c" with every form
// named and not empty, no category twice and an other.
func pFormsOK(forms string) bool {
	parts := strings.Split(forms, "|")
	if len(parts) < 2 {
		return false
	}
	named := func(p string) (cat, text string, ok bool) {
		cat, text, found := strings.Cut(p, "=")
		return cat, text, found && slices.Contains(pCategories, cat)
	}
	if _, _, ok := named(parts[0]); !ok {
		if len(parts) != 2 {
			return false
		}
		for _, p := range parts {
			if _, _, ok := named(p); p == "" || ok {
				return false
			}
		}
		return true
	}
	seen := map[string]bool{}
	for _, p := range parts {
		cat, text, ok := named(p)
		if !ok || text == "" || seen[cat] {
			return false
		}
		seen[cat] = true
	}
	return seen["other"]
}

// oracle is the set of error codes S5 gives line as a variant of meta: the
// first problem of V14 to V17 (the one rule set of the renderer, which stops
// at the first), else every layout error of V18 to V24 and V26.
func oracle(meta msg.Meta, line string) []string {
	refs, ok := pScan(line)
	if !ok {
		return []string{"v14"}
	}
	slots := map[string]msg.Slot{}
	for _, sl := range meta.Slots {
		slots[sl.Name] = sl
	}
	used := map[string]bool{}
	for _, r := range refs {
		sl, declared := slots[r.name]
		if !declared {
			return []string{"v16"}
		}
		if r.selector && sl.Type != catalog.Int {
			return []string{"v17"}
		}
		used[r.name] = true
	}
	for _, sl := range meta.Slots {
		if !sl.Optional && !used[sl.Name] {
			return []string{"v15"}
		}
	}

	english := meta.Text
	engRefs, _ := pScan(english)
	got := map[string]bool{}
	// V18, a count hidden: the English shows the count, with '#' in a
	// selector or as a bare int placeholder, and the variant uses the
	// placeholder but none of its references shows the count (a bare one,
	// or a selector with '#').
	for _, e := range engRefs {
		shown := e.selector && strings.Contains(e.forms, "#") || !e.selector && slots[e.name].Type == catalog.Int
		if !shown {
			continue
		}
		used, kept := false, false
		for _, r := range refs {
			if r.name == e.name {
				used = true
				kept = kept || !r.selector || strings.Contains(r.forms, "#")
			}
		}
		if used && !kept {
			got["v18"] = true
		}
	}
	// V19, leading and trailing whitespace.
	space := " \t\n\r"
	if len(line)-len(strings.TrimLeft(line, space)) != len(english)-len(strings.TrimLeft(english, space)) ||
		line[len(strings.TrimRight(line, space)):] != english[len(strings.TrimRight(english, space)):] {
		got["v19"] = true
	}
	// V20, a leading "{p}: ".
	if prefix := pPrefix.FindString(english); prefix != "" && !strings.HasPrefix(line, prefix) {
		got["v20"] = true
	}
	// V21, leading punctuation.
	if first, _ := utf8.DecodeRuneInString(english); strings.ContainsRune(",;(", first) {
		if lineFirst, _ := utf8.DecodeRuneInString(line); lineFirst != first {
			got["v21"] = true
		}
	}
	// V22, the character before an appended command.
	if meta.Append != "" {
		want, _ := utf8.DecodeLastRuneInString(strings.TrimRight(english, space))
		have, _ := utf8.DecodeLastRuneInString(strings.TrimRight(line, space))
		if want != have {
			got["v22"] = true
		}
	}
	// V23, placeholders that touch in the English must touch in the variant.
	for i := 0; i+1 < len(engRefs); i++ {
		if engRefs[i].end != engRefs[i+1].start {
			continue
		}
		touching := false
		for j := 0; j+1 < len(refs); j++ {
			touching = touching || refs[j].name == engRefs[i].name && refs[j+1].name == engRefs[i+1].name && refs[j].end == refs[j+1].start
		}
		if !touching {
			got["v23"] = true
		}
	}
	// V24, the number of line breaks.
	if strings.Count(line, "\n") != strings.Count(english, "\n") {
		got["v24"] = true
	}
	// V26, the hard cap.
	if utf8.RuneCountInString(line) > 4*utf8.RuneCountInString(english)+200 {
		got["v26"] = true
	}
	out := make([]string, 0, len(got))
	for code := range got {
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}

// errorCodes are the codes of the errors Validate reports for line as a
// variant of meta.
func errorCodes(meta msg.Meta, line string) []string {
	f := &File{ID: "tester", Name: "Tester", Emoji: "🧪", schemaSet: true, schema: 1,
		Lines: map[string][]string{meta.ID: {line}}}
	got := map[string]bool{}
	for _, d := range Validate(nil, f, ValidateOptions{}) {
		if d.Severity == Error {
			got[strings.TrimPrefix(d.Text.ID(), "persona.diag.")] = true
		}
	}
	out := make([]string, 0, len(got))
	for code := range got {
		out = append(out, code)
	}
	sort.Strings(out)
	return out
}

// mutation changes line. i and j pick placeholders (modulo how many there
// are); false says it does not apply to this line.
type mutation struct {
	name  string
	apply func(meta msg.Meta, line string, i, j int) (string, bool)
}

// replaceRef puts repl where refs[k] is.
func replaceRef(line string, refs []pref, k int, repl string) string {
	return line[:refs[k].start] + repl + line[refs[k].end:]
}

// onRef lifts a change of one placeholder.
func onRef(change func(meta msg.Meta, r pref) (string, bool)) func(msg.Meta, string, int, int) (string, bool) {
	return func(meta msg.Meta, line string, i, _ int) (string, bool) {
		refs, ok := pScan(line)
		if !ok || len(refs) == 0 {
			return line, false
		}
		k := i % len(refs)
		repl, applies := change(meta, refs[k])
		if !applies {
			return line, false
		}
		return replaceRef(line, refs, k, repl), true
	}
}

func lastRune(s string) (r rune, size int) { return utf8.DecodeLastRuneInString(s) }

var mutations = []mutation{
	{"drop", onRef(func(msg.Meta, pref) (string, bool) { return "", true })},
	{"rename", onRef(func(_ msg.Meta, r pref) (string, bool) {
		rest := strings.TrimPrefix(r.src[1:], r.name)
		return "{bad_slot" + rest, true
	})},
	{"duplicate", onRef(func(_ msg.Meta, r pref) (string, bool) { return r.src + " " + r.src, true })},
	{"duplicate touching", onRef(func(_ msg.Meta, r pref) (string, bool) { return r.src + r.src, true })},
	{"reorder", func(_ msg.Meta, line string, i, j int) (string, bool) {
		refs, ok := pScan(line)
		if !ok || len(refs) < 2 {
			return line, false
		}
		a, b := i%len(refs), j%len(refs)
		if a == b {
			b = (a + 1) % len(refs)
		}
		if a > b {
			a, b = b, a
		}
		// The later one first, so the earlier offsets stay true.
		out := replaceRef(line, refs, b, refs[a].src)
		return replaceRef(out, refs, a, refs[b].src), true
	}},
	// A plural selector with no '#' on a placeholder that has none: V17 on
	// a string, V18 on a count, which the English shows as it is ({n}).
	{"selector", onRef(func(meta msg.Meta, r pref) (string, bool) {
		for _, sl := range meta.Slots {
			if sl.Name == r.name {
				return r.src[:len(r.src)-1] + ":x|y}", !r.selector
			}
		}
		return "", false
	})},
	{"hide the count", onRef(func(_ msg.Meta, r pref) (string, bool) {
		if !r.selector || !strings.Contains(r.forms, "#") {
			return "", false
		}
		return "{" + r.name + ":" + strings.ReplaceAll(r.forms, "#", "N") + "}", true
	})},
	{"space before", func(_ msg.Meta, line string, _, _ int) (string, bool) { return " " + line, true }},
	{"space after", func(_ msg.Meta, line string, _, _ int) (string, bool) { return line + " ", true }},
	{"newline after", func(_ msg.Meta, line string, _, _ int) (string, bool) { return line + "\n", true }},
	{"unclosed brace", func(_ msg.Meta, line string, _, _ int) (string, bool) { return line + " {", true }},
	{"lone close brace", func(_ msg.Meta, line string, _, _ int) (string, bool) { return line + " }", true }},
	{"no position", func(_ msg.Meta, line string, _, _ int) (string, bool) {
		prefix := pPrefix.FindString(line)
		return strings.TrimPrefix(line, prefix), prefix != ""
	}},
	{"no leading punctuation", func(_ msg.Meta, line string, _, _ int) (string, bool) {
		if line == "" || !strings.ContainsRune(",;(", rune(line[0])) {
			return line, false
		}
		return line[1:], true
	}},
	{"other last character", func(_ msg.Meta, line string, _, _ int) (string, bool) {
		trimmed := strings.TrimRight(line, " \t\n\r")
		r, size := lastRune(trimmed)
		if size == 0 {
			return line, false
		}
		replacement := "!"
		if r == '!' {
			replacement = "?"
		}
		return trimmed[:len(trimmed)-size] + replacement + line[len(trimmed):], true
	}},
	{"far too long", func(_ msg.Meta, line string, _, _ int) (string, bool) {
		n := utf8.RuneCountInString(line)
		if n == 0 {
			return line, false
		}
		// The cap is 4 × the English + 200 runes; the English is at most
		// as long as the line, so this is always past it.
		return strings.Repeat(line+" ", 4+200/n+2), true
	}},
}

// voicedEntries are the entries the property test mutates: those a persona
// may reword whose English is itself a valid variant (no braces written as
// {{ or }}, which the oracle's scanner would have to treat as text).
func voicedEntries(t testing.TB) []msg.Meta {
	t.Helper()
	var out []msg.Meta
	for _, meta := range msg.Entries() {
		if !meta.Persona || strings.Contains(meta.Text, "{{") || strings.Contains(meta.Text, "}}") ||
			msg.CheckLine(meta.ID, meta.Text) != nil {
			continue
		}
		out = append(out, meta)
	}
	if len(out) == 0 {
		t.Fatal("no voiced entries")
	}
	return out
}

// agrees is "" when Validate and the oracle say the same of line, and then,
// when the line is accepted, the spoken line keeps its values.
func agrees(meta msg.Meta, line string) string {
	want, got := oracle(meta, line), errorCodes(meta, line)
	if !slices.Equal(want, got) {
		return fmt.Sprintf("%s: %q: the oracle says %v, Validate says %v", meta.ID, line, want, got)
	}
	if len(got) == 0 {
		return keepsValues(meta, line)
	}
	return ""
}

// lineSpeaker says one line for every id, as a persona would, in English.
type lineSpeaker struct{ name, line string }

func (s lineSpeaker) Name() string { return s.name }
func (s lineSpeaker) Line(string) (string, language.Tag, bool) {
	return s.line, language.English, true
}

// keepsValues builds meta with a distinct sentinel for every placeholder it
// takes, has a speaker say line, and reports the first required or fixed
// value that did not survive. The speaker's name stands for {bot}, and the
// command word for {self}.
func keepsValues(meta msg.Meta, line string) string {
	values := map[string]any{}
	for i, sl := range meta.Slots {
		switch {
		case sl.Value != "", sl.Name == catalog.Bot, sl.Name == catalog.Self:
		case sl.Type == catalog.Int:
			values[sl.Name] = 4242 + i // many in English, and in every language
		default:
			values[sl.Name] = fmt.Sprintf("QX_%s_%d", sl.Name, i)
		}
	}
	text, err := msg.Build(meta.ID, values)
	if err != nil {
		return fmt.Sprintf("%s: Build: %v", meta.ID, err)
	}
	speaker := lineSpeaker{name: "Tester", line: line}
	for _, said := range []struct {
		how, out, bot, self string
	}{
		{"Speak", msg.Speak(speaker, text, nil), "Tester", "homebot"},
		{"SpeakWithSelf", msg.SpeakWithSelf(speaker, text, "zork", nil), "zork", "zork"},
	} {
		for _, sl := range meta.Slots {
			if sl.Optional {
				continue
			}
			var want string
			switch {
			case sl.Name == catalog.Bot:
				want = said.bot
			case sl.Name == catalog.Self:
				want = said.self
			case sl.Value != "":
				want = strings.ReplaceAll(sl.Value, catalog.SelfRef, said.self)
			default:
				want = fmt.Sprint(values[sl.Name])
			}
			if !strings.Contains(said.out, want) {
				return fmt.Sprintf("%s: %s said %q for the variant %q: the %s value %q is gone", meta.ID, said.how, said.out, line, sl.Name, want)
			}
		}
	}
	return ""
}

// The catalog's own English must keep its values too, or the property below
// would fail for a reason that is not the validator's.
func TestCatalogEnglishKeepsItsRequiredValues(t *testing.T) {
	for _, meta := range voicedEntries(t) {
		if problem := keepsValues(meta, meta.Text); problem != "" {
			t.Error(problem)
		}
		if got := errorCodes(meta, meta.Text); len(got) != 0 {
			t.Errorf("%s: the English is not a valid variant of itself: %v", meta.ID, got)
		}
		if got := oracle(meta, meta.Text); len(got) != 0 {
			t.Errorf("%s: the oracle refuses the English: %v", meta.ID, got)
		}
	}
}

// The oracle is checked against answers written by hand, so that agreeing
// with it means something.
func TestOracleKnownAnswers(t *testing.T) {
	meta := func(id string) msg.Meta {
		m, ok := msg.Lookup(id)
		if !ok {
			t.Fatalf("no entry %s", id)
		}
		return m
	}
	refused := meta("save.error.refused") // "Commit refused: {reason}"
	committed := meta("save.success.committed")
	changed := meta("action.link.status.changed") // "{action} {n} changed": the English shows the count bare
	cases := []struct {
		name string
		meta msg.Meta
		line string
		want []string
	}{
		{"as is", refused, "Commit refused: {reason}", nil},
		{"another voice", refused, "No can do, {reason}", nil},
		{"dropped", refused, "Commit refused", []string{"v15"}},
		{"renamed", refused, "Commit refused: {why}", []string{"v16"}},
		{"duplicated", refused, "Commit refused: {reason} ({reason})", nil},
		{"selector on a string", refused, "Commit refused: {reason:a|b}", []string{"v17"}},
		{"unclosed", refused, "Commit refused: {reason} {", []string{"v14"}},
		{"lone brace", refused, "Commit refused: {reason} }", []string{"v14"}},
		{"leading space", refused, " Commit refused: {reason}", []string{"v19"}},
		{"trailing newline", refused, "Commit refused: {reason}\n", []string{"v19", "v24"}},
		{"too long", refused, strings.Repeat("x", 288) + " {reason}", []string{"v26"}},
		{"at the cap", refused, strings.Repeat("x", 287) + " {reason}", nil},
		{"count shown", committed, "saved {n:# thing|# things}", nil},
		{"count hidden", committed, "saved {n:a thing|some things}", []string{"v18"}},
		{"count dropped", committed, "saved", []string{"v15"}},
		{"bare count kept", changed, "{action} {n} changed", nil},
		{"bare count in a selector", changed, "{action} {n:# file|# files} changed", nil},
		{"bare count hidden by a selector", changed, "{action} {n:one file|some files} changed", []string{"v18"}},
		{"bare count kept, selector picks the noun", changed, "{action} {n} {n:file|files} changed", nil},
		{"selector first, bare count kept", changed, "{action} {n:file|files}, {n} in all", nil},
		{"plain English selector, bare count kept", committed, "saved {n} {n:thing|things}", nil},
		{"plain English selector, # form kept", committed, "saved {n:thing|things} ({n:# time|# times})", nil},
		{"two selectors, none shows the count", changed, "{action} {n:one|some} {n:file|files} changed", []string{"v18"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := oracle(tc.meta, tc.line); !slices.Equal(got, tc.want) {
				t.Errorf("oracle(%q) = %v, want %v", tc.line, got, tc.want)
			}
			if got := errorCodes(tc.meta, tc.line); !slices.Equal(got, tc.want) {
				t.Errorf("Validate(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

// Every mutation of every placeholder of every voiced entry, once.
func TestPlaceholderMutationsSweep(t *testing.T) {
	var checked, accepted int
	for _, meta := range voicedEntries(t) {
		refs, _ := pScan(meta.Text)
		for _, m := range mutations {
			for i := range max(len(refs), 1) {
				for j := range max(len(refs), 1) {
					line, applies := m.apply(meta, meta.Text, i, j)
					if !applies {
						continue
					}
					checked++
					if len(oracle(meta, line)) == 0 {
						accepted++
					}
					if problem := agrees(meta, line); problem != "" {
						t.Errorf("%s: %s", m.name, problem)
					}
				}
			}
		}
	}
	// The sweep must not pass by mutating nothing, or by refusing it all.
	if checked < 1000 || accepted == 0 || accepted == checked {
		t.Errorf("the sweep checked %d mutations, %d of them valid", checked, accepted)
	}
}

// Random sequences of one to three mutations, seeded.
func TestPlaceholderMutationsAgreeWithTheOracle(t *testing.T) {
	entries := voicedEntries(t)
	check := func(entry uint16, seed int64) bool {
		meta := entries[int(entry)%len(entries)]
		rng := rand.New(rand.NewSource(seed))
		line := meta.Text
		for range 1 + rng.Intn(3) {
			m := mutations[rng.Intn(len(mutations))]
			if mutated, applies := m.apply(meta, line, rng.Intn(8), rng.Intn(8)); applies {
				line = mutated
			}
		}
		if problem := agrees(meta, line); problem != "" {
			t.Log(problem)
			return false
		}
		return true
	}
	if err := quick.Check(check, &quick.Config{MaxCount: 4000, Rand: rand.New(rand.NewSource(20261014))}); err != nil {
		t.Fatal(err)
	}
}

func FuzzParseValidate(f *testing.F) {
	for _, seed := range []string{
		testHeader,
		testHeader + "[lines]\n'save.error.refused' = ['hello {reason}']\n",
		"schema = [\n",
		"schema = 1\nname = '\x1b[31m'\nemoji = '🧪'\n",
		// Hostile headers.
		"schema = 1\nname = 'Tester'\nemoji = '$(touch pwned)'\n",
		"schema = 1\nname = 'Tester'\nemoji = '1️⃣'\n",
		testHeader + "tagline = '`id` $(touch pwned)'\n",
		testHeader + "tagline = \"two\\nlines\"\n",
		testHeader + "extends = '../x'\nlang = 'not a tag'\n",
		testHeader + "[colors.accent]\nlight = 256\ndark = '#12'\n",
		testHeader + "[colors.surprise]\nlight = true\n",
		"schema = 1\nname = \"\\u202eTester\"\nemoji = '🧪'\n",
		// Hostile lines.
		testHeader + "[lines]\n'save.error.refused' = ['\\e[31m{reason}', '\\x1b[0m{reason}', '{reason}\\u009b']\n",
		testHeader + "[lines]\n'save.error.refused\\u202e' = ['{reason}']\n'' = ['x']\n",
		testHeader + "[lines]\n'save.error.refused' = ['{reason', '}{reason}', '{{reason}}', '{reason:a|b}', '{bad slot}']\n",
		testHeader + "[lines]\n'action.link.status.changed' = ['{action} {n:one=# a|few=# b|other=# c}', '{action}{n:#|#}']\n",
		testHeader + "[lines]\n'cli.version' = ['{version}']\n'nope' = []\n",
	} {
		f.Add(seed)
	}
	ctx := &app.Context{}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > maxFileBytes {
			return
		}
		file, ds := Parse("tester.toml", []byte(source))
		if file != nil {
			ds = append(ds, Validate(nil, file, ValidateOptions{})...)
		}
		for _, d := range ds {
			if d.Text.ID() == "" {
				t.Fatal("diagnostic without a catalog entry")
			}
			said := msg.Speak(msg.Neutral{}, d.Text, nil)
			if r, at, unsafe := msg.UnsafeRune(said); unsafe || strings.ContainsAny(said, "\n\r\t") {
				t.Fatalf("diagnostic %s carries %q at %d (%q)", d.Text.ID(), r, at, said)
			}
		}
		if file == nil {
			return
		}
		// What the validator lets through is clean: a persona that
		// loads can say nothing the renderer would not.
		if len(headerErrors(ds)) == 0 {
			c := file.Colors
			for field, value := range map[string]string{"name": file.Name, "tagline": file.Tagline, "extends": file.Extends,
				"lang": file.Lang, "accent light": c.Accent.Light, "accent dark": c.Accent.Dark, "ok light": c.OK.Light,
				"ok dark": c.OK.Dark, "muted light": c.Muted.Light, "muted dark": c.Muted.Dark} {
				if r, _, unsafe := msg.UnsafeRune(value); unsafe || strings.ContainsAny(value, "\n\r\t") {
					t.Fatalf("an accepted %s has %q: %q", field, r, value)
				}
			}
			if !msg.ValidEmoji(file.Emoji) || strings.IndexFunc(file.Emoji, func(r rune) bool { return r < utf8.RuneSelf }) >= 0 {
				t.Fatalf("an accepted emoji: %q", file.Emoji)
			}
		}
		for id, variants := range validVariants(ctx, file) {
			for _, line := range variants {
				if err := msg.CheckLine(id, line); err != nil {
					t.Fatalf("a kept variant of %s that the renderer refuses: %q: %v", id, line, err)
				}
				if r, _, unsafe := msg.UnsafeRune(line); unsafe {
					t.Fatalf("a kept variant of %s has %q: %q", id, r, line)
				}
			}
		}
	})
}
