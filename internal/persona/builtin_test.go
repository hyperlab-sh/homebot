package persona

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/msg/catalog"
)

// The Fable pass flips this after writing and reviewing the lines and souls.
const linesPending = false

var updateBuiltinHeaders = flag.Bool("update-builtin-headers", false, "deliberately refresh the built-in header contract")

type frozenHeader struct {
	Schema  int    `json:"schema"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Emoji   string `json:"emoji"`
	Extends string `json:"extends"`
	Lang    string `json:"lang"`
	Colors  Colors `json:"colors"`
}

func allBuiltins(t *testing.T) []*File {
	t.Helper()
	var files []*File
	for _, id := range Builtins() {
		f, ds, err := readBuiltin(id)
		if err != nil || f == nil || len(ds) != 0 {
			t.Fatalf("built-in %s: %v %+v", id, err, ds)
		}
		files = append(files, f)
	}
	return files
}

func manyIDs(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("fable", "many.txt"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, raw := range strings.Split(string(data), "\n") {
		id := strings.TrimSpace(raw)
		if id != "" && !strings.HasPrefix(id, "#") {
			out[id] = true
		}
	}
	return out
}

func TestBuiltinHeadersFrozen(t *testing.T) {
	headers := map[string]frozenHeader{}
	for _, f := range allBuiltins(t) {
		headers[f.ID] = frozenHeader{Schema: f.schema, ID: f.ID, Name: f.Name,
			Emoji: f.Emoji, Extends: f.Extends, Lang: f.Lang, Colors: f.Colors}
	}
	want, err := json.MarshalIndent(headers, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	if *updateBuiltinHeaders {
		if err := os.WriteFile("builtin/headers.golden", want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile("builtin/headers.golden")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("built-in header changed outside the reviewed author contract; update builtin/headers.golden deliberately")
	}
}

func pendingLines(t *testing.T) {
	t.Helper()
	if linesPending {
		t.Skip("Fable lines and SOUL.md files pending")
	}
}

func TestBuiltinCoverage(t *testing.T) {
	pendingLines(t)
	many := manyIDs(t)
	for _, f := range allBuiltins(t) {
		for _, m := range msg.Entries() {
			if !m.Persona || f.ID == Default && !many[m.ID] {
				continue
			}
			if len(f.Lines[m.ID]) == 0 {
				t.Errorf("%s missing %s", f.ID, m.ID)
			}
		}
	}
}

func TestBuiltinVariantCounts(t *testing.T) {
	pendingLines(t)
	many := manyIDs(t)
	for _, f := range allBuiltins(t) {
		for id, variants := range f.Lines {
			min, max := variantBounds(f.ID == Default, many[id])
			if len(variants) < min || len(variants) > max {
				t.Errorf("%s %s has %d variants; want %d–%d", f.ID, id, len(variants), min, max)
			}
		}
	}
}

func variantBounds(isDefault, isMany bool) (min, max int) {
	if isMany {
		return 3, 5
	}
	if isDefault {
		return 0, 1
	}
	return 1, 2
}

func TestVariantBounds(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		defaultVoice, manyID bool
		min, max             int
	}{
		{"default many", true, true, 3, 5},
		{"default ordinary", true, false, 0, 1},
		{"other many", false, true, 3, 5},
		{"other ordinary", false, false, 1, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			min, max := variantBounds(tc.defaultVoice, tc.manyID)
			if min != tc.min || max != tc.max {
				t.Errorf("bounds = %d–%d, want %d–%d", min, max, tc.min, tc.max)
			}
		})
	}
}

type variantSpeaker struct {
	id, text string
}

func (s variantSpeaker) Name() string { return "SPEAKER_SENTINEL" }
func (s variantSpeaker) Line(id string) (string, language.Tag, bool) {
	return s.text, language.English, id == s.id
}

func TestBuiltinPlaceholders(t *testing.T) {
	pendingLines(t)
	for _, f := range allBuiltins(t) {
		for id, variants := range f.Lines {
			meta, found := msg.Lookup(id)
			if !found || !meta.Persona {
				t.Errorf("%s has unknown or unvoiced id %s", f.ID, id)
				continue
			}
			english, err := catalog.Parse(meta.Text)
			if err != nil {
				t.Fatalf("catalog %s: %v", id, err)
			}
			for _, line := range variants {
				if err := msg.CheckLine(id, line); err != nil {
					t.Errorf("%s %s: %v", f.ID, id, err)
					continue
				}
				candidate, err := catalog.Parse(line)
				if err != nil {
					t.Errorf("%s %s: %v", f.ID, id, err)
					continue
				}
				for _, count := range []int{1, 2, 5} {
					values := map[string]any{}
					var markers, fixed []string
					for _, sl := range meta.Slots {
						if sl.Name == catalog.Bot || sl.Name == catalog.Self {
							continue
						}
						if sl.Value != "" {
							// The rendered text holds the fixed value with {self}
							// expanded, so the marker is the value as the renderer
							// formats it (added below, from Text.Values).
							fixed = append(fixed, sl.Name)
							continue
						}
						if sl.Type == catalog.Int {
							values[sl.Name] = count
							if countShown(english, sl.Name, count) && !countShown(candidate, sl.Name, count) {
								t.Errorf("%s %s drops the %d count in %q", f.ID, id, count, line)
							}
							continue
						}
						marker := "VALUE_" + sl.Name + "_SENTINEL"
						values[sl.Name] = marker
						if !sl.Optional {
							markers = append(markers, marker)
						}
					}
					text, err := msg.Build(id, values)
					if err != nil {
						t.Errorf("%s %s: %v", f.ID, id, err)
						continue
					}
					rendered := text.Values(msg.NeutralName, nil)
					for _, name := range fixed {
						value, ok := rendered[name].(string)
						if !ok || value == "" || strings.Contains(value, catalog.SelfRef) {
							t.Fatalf("%s %s: fixed %s renders as %#v", f.ID, id, name, rendered[name])
						}
						markers = append(markers, value)
					}
					spoken := msg.Speak(variantSpeaker{id: id, text: line}, text, nil)
					for _, sl := range meta.Slots {
						if sl.Type == catalog.Int && countShown(english, sl.Name, count) && !strings.Contains(spoken, strconv.Itoa(count)) {
							t.Errorf("%s %s rendered without the %d count: %q", f.ID, id, count, spoken)
						}
					}
					for _, marker := range markers {
						if !strings.Contains(spoken, marker) {
							t.Errorf("%s %s dropped %q in %q", f.ID, id, marker, spoken)
						}
					}
				}
			}
		}
	}
}

// countShown reports whether the English selected form, or a candidate's
// corresponding form, prints the integer rather than just selecting wording.
// All built-in lines use English: 1 selects one, every other count selects other.
func countShown(tmpl catalog.Template, name string, count int) bool {
	category := "other"
	if count == 1 {
		category = "one"
	}
	for _, ref := range tmpl.Refs() {
		if ref.Name != name {
			continue
		}
		if ref.Forms == nil {
			return true
		}
		for _, form := range ref.Forms {
			if form.Category == category && strings.Contains(form.Text, "#") {
				return true
			}
		}
	}
	return false
}

func TestCountShownChecksEachPluralBranch(t *testing.T) {
	for _, tc := range []struct {
		line      string
		one, many bool
	}{
		{"{n:# thing|# things}", true, true},
		{"{n:# thing|things}", true, false},
		{"{n:thing|# things}", false, true},
		{"{n}", true, true},
	} {
		tmpl, err := catalog.Parse(tc.line)
		if err != nil {
			t.Fatal(err)
		}
		if one, many := countShown(tmpl, "n", 1), countShown(tmpl, "n", 2); one != tc.one || many != tc.many {
			t.Errorf("%q count display = (%v, %v), want (%v, %v)", tc.line, one, many, tc.one, tc.many)
		}
	}
}

func TestBuiltinLength(t *testing.T) {
	pendingLines(t)
	for _, f := range allBuiltins(t) {
		for _, d := range Validate(nil, f, ValidateOptions{}) {
			if d.Text.ID() == "persona.diag.w5" {
				t.Errorf("%s line %s variant %d exceeds author length cap", f.ID, d.LineID, d.Variant)
			}
		}
	}
}

func TestBuiltinPlainText(t *testing.T) {
	pendingLines(t)
	for _, f := range allBuiltins(t) {
		for id, lines := range f.Lines {
			for _, line := range lines {
				if badPlainText(line) {
					t.Errorf("%s %s contains markup, emoji or unsafe text", f.ID, id)
				}
			}
		}
		if badPlainText(f.Tagline) {
			t.Errorf("%s tagline contains markup, emoji or unsafe text", f.ID)
		}
	}
}

func badPlainText(s string) bool {
	if _, _, unsafe := msg.UnsafeRune(s); unsafe {
		return true
	}
	if strings.Contains(s, "**") || strings.Contains(s, "__") || strings.Contains(s, "`") || strings.Contains(s, "]( ") || strings.Contains(s, "](") {
		return true
	}
	for _, r := range s {
		if unicode.Is(unicode.So, r) {
			return true
		}
	}
	return false
}

func denylist(t *testing.T) []*regexp.Regexp {
	t.Helper()
	data, err := os.ReadFile("builtin/denylist.txt")
	if err != nil {
		t.Fatal(err)
	}
	var out []*regexp.Regexp
	for _, raw := range strings.Split(string(data), "\n") {
		phrase := strings.TrimSpace(raw)
		if phrase == "" || strings.HasPrefix(phrase, "#") {
			continue
		}
		out = append(out, denyPattern(phrase))
	}
	return out
}

// denyPattern matches phrase as whole words without regard to case, and
// treats the gap inside it as interchangeable: one space, one hyphen or
// nothing. One entry then covers every spelling of a name: the hyphenated
// form in the list also denies the spaced and the joined ones, and the spaced
// form also denies the hyphenated and the joined ones. A phrase with no gap is
// matched as written. (No name is spelled out in this file: TestNamesStayInBuiltin
// reads every Go file outside builtin/.)
//
// The gap is one character, not any run of them, so a name is not found
// across a whole sentence. A name that ends in a single letter or digit is
// not found when an abbreviation follows it ("... e.g."): the letter is then
// the start of the next word. A phrase that starts with = is matched in the
// case it is written in, for a title that is also ordinary English in
// lowercase; its gap stays interchangeable, so the title is denied however it
// is spaced or hyphenated.
func denyPattern(phrase string) *regexp.Regexp {
	flags := `(?i)`
	if exact, ok := strings.CutPrefix(phrase, "="); ok {
		phrase, flags = exact, ""
	}
	words := strings.FieldsFunc(phrase, func(r rune) bool { return r == ' ' || r == '-' })
	for i, w := range words {
		words[i] = regexp.QuoteMeta(w)
	}
	end := `\b`
	if last := words[len(words)-1]; len(words) > 1 && len(last) == 1 {
		end = `\b(?:$|[^.]|\.(?:$|\s))`
	}
	return regexp.MustCompile(flags + `\b` + strings.Join(words, `[\s-]?`) + end)
}

// Each spelling of a denylisted name is denied, whichever one the list
// holds. The names are put together here from their parts so that
// TestNamesStayInBuiltin, which reads every Go file outside builtin/, does
// not find them.
func TestDenylistCoversEverySpelling(t *testing.T) {
	patterns := denylist(t)
	denied := func(text string) bool {
		for _, p := range patterns {
			if p.MatchString(text) {
				return true
			}
		}
		return false
	}
	for _, spellings := range [][]string{
		{"HAL" + " " + "9000", "HAL" + "-" + "9000", "HAL" + "9000", "hal" + "-" + "9000"},
		{"R2" + "-" + "D2", "R2" + " " + "D2", "R2" + "D2", "r2" + "d2"},
		{"C" + "-" + "3PO", "C" + " " + "3PO", "C" + "3PO", "c" + "3po"},
		{"WALL" + "-" + "E", "Wall" + " " + "E", "WALL" + "E", "wall" + "e"},
		{"Johnny" + " " + "5", "Johnny" + "-" + "5", "Johnny" + "5"},
		{"Miss" + " " + "Minute", "Miss" + "-" + "Minute", "Miss" + "Minute"},
		{"Short" + " " + "Circuit", "Short" + "-" + "Circuit", "Short" + "Circuit"},
	} {
		for _, spelling := range spellings {
			if !denied(spelling) {
				t.Errorf("denylist lets %q through", spelling)
			}
			if !denied("Say " + spelling + ", then stop.") {
				t.Errorf("denylist misses %q inside a sentence", spelling)
			}
		}
	}
	// A name that ends the sentence is still a name, "e.g." or not.
	for _, bad := range []string{"I like " + "Wall" + " " + "E.", "I like " + "Wall" + "-" + "E.", "Is it " + "Wall" + " " + "E?"} {
		if !denied(bad) {
			t.Errorf("denylist lets %q through", bad)
		}
	}
	// Whole words only: a longer word that happens to start or end the same
	// way is not a name.
	for _, ok := range []string{"wallet", "a wall exists", "walleye", "shallow end", "hal", "r2", "c3", "miss a minute"} {
		if denied(ok) {
			t.Errorf("denylist refuses %q", ok)
		}
	}
	// Ordinary English that shares letters with a name is not the name: a
	// wall followed by "e.g.", and the electrical term.
	for _, ok := range []string{"a wall e.g. of bricks", "paint a wall e.g.", "the " + "short" + "-" + "circuit" + " tripped", "a " + "short" + " " + "circuit" + " tripped", "Short" + "-" + "circuit" + " protection", "Wall" + " e.g. a fence"} {
		if denied(ok) {
			t.Errorf("denylist refuses %q", ok)
		}
	}
}

func TestDenylistSparesCatalog(t *testing.T) {
	patterns := denylist(t)
	for _, m := range msg.Entries() {
		for _, p := range patterns {
			if p.MatchString(m.Text) {
				t.Errorf("catalog %s matches forbidden phrase %s", m.ID, p.String())
			}
		}
	}
}

// TestBuiltinDenylist keeps a built-in's own name out of its lines and
// tagline: the name reaches them only through {bot}, so a renamed copy of the
// persona still reads right. Every spelling the denylist matcher treats as
// equal counts (spaced, hyphenated, joined), for the display name and the id
// alike. Other characters, places and works may be named: the personas are
// impersonations (owner's ruling of 2026-10-16), and the full denylist polices
// Go code, testdata and the catalog instead (TestNamesStayInBuiltin,
// TestDenylistSparesCatalog).
func TestBuiltinDenylist(t *testing.T) {
	pendingLines(t)
	for _, f := range allBuiltins(t) {
		patterns := []*regexp.Regexp{denyPattern(f.Name), denyPattern(f.ID)}
		texts := map[string]string{"tagline": f.Tagline}
		for id, lines := range f.Lines {
			for i, line := range lines {
				texts[fmt.Sprintf("%s[%d]", id, i)] = line
			}
		}
		for where, text := range texts {
			for _, p := range patterns {
				if p.MatchString(text) {
					t.Errorf("%s %s spells the persona's own name (%s); use {bot}", f.ID, where, p.String())
				}
			}
		}
	}
}

func TestBuiltinNoAddedFacts(t *testing.T) {
	pendingLines(t)
	for _, f := range allBuiltins(t) {
		for _, d := range Validate(nil, f, ValidateOptions{}) {
			if d.Text.ID() == "persona.diag.w7" {
				t.Errorf("%s line %s variant %d adds a fact", f.ID, d.LineID, d.Variant)
			}
		}
	}
}

func TestSoulFiles(t *testing.T) {
	pendingLines(t)
	for _, f := range allBuiltins(t) {
		data, err := os.ReadFile(filepath.Join("builtin", f.ID, "SOUL.md"))
		if err != nil {
			t.Error(err)
			continue
		}
		for _, heading := range []string{"# " + f.Name, "## Who", "## How it talks", "## Never", "## Bad news and urgency", "## Samples"} {
			if !strings.Contains(string(data), heading) {
				t.Errorf("%s SOUL.md missing %q", f.ID, heading)
			}
		}
		if !strings.Contains(string(data), f.Tagline) {
			t.Errorf("%s SOUL.md missing tagline", f.ID)
		}
		if err := checkSoulSamples(string(data), f.Lines); err != nil {
			t.Errorf("%s SOUL.md: %v", f.ID, err)
		}
	}
}

// checkSoulSamples requires five distinct examples copied from the actual
// line table, each accompanied by its id. This keeps a bare heading or
// invented example from satisfying the author's review packet.
func checkSoulSamples(soul string, lines map[string][]string) error {
	_, section, ok := strings.Cut(soul, "## Samples\n")
	if !ok {
		return fmt.Errorf("missing Samples section")
	}
	var examples []string
	for _, raw := range strings.Split(section, "\n") {
		if strings.HasPrefix(raw, "## ") {
			break
		}
		if line := strings.TrimSpace(raw); line != "" {
			examples = append(examples, line)
		}
	}
	if len(examples) != 5 {
		return fmt.Errorf("Samples has %d nonempty lines; want five id/line examples", len(examples))
	}
	seen := map[string]bool{}
	for _, example := range examples {
		matched := ""
		words := strings.FieldsFunc(example, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' && r != '-' && r != '_'
		})
		for id, variants := range lines {
			if !slices.Contains(words, id) {
				continue
			}
			for _, variant := range variants {
				if strings.Contains(example, variant) {
					if matched != "" {
						return fmt.Errorf("sample %q names more than one line", example)
					}
					matched = id
					break
				}
			}
		}
		if matched == "" {
			return fmt.Errorf("sample %q does not pair a line with its id", example)
		}
		if seen[matched] {
			return fmt.Errorf("sample id %q repeated", matched)
		}
		seen[matched] = true
	}
	return nil
}

func TestSoulSamplesRequireFiveActualLines(t *testing.T) {
	lines := map[string][]string{}
	var examples []string
	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("example.%d", i)
		line := fmt.Sprintf("Message %d", i)
		lines[id] = []string{line}
		examples = append(examples, "- "+id+": "+line)
	}
	good := "## Samples\n" + strings.Join(examples, "\n") + "\n"
	if err := checkSoulSamples(good, lines); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		"## Samples\n",
		"## Samples\n" + strings.Join(examples[:4], "\n") + "\n",
		strings.Replace(good, "Message 5", "Invented line", 1),
		strings.Replace(good, "example.5", "example.4", 1),
	} {
		if err := checkSoulSamples(bad, lines); err == nil {
			t.Errorf("accepted invalid Samples section %q", bad)
		}
	}
}

func TestManyIDsAreVoiced(t *testing.T) {
	many := manyIDs(t)
	known := map[string]bool{}
	for _, m := range msg.Entries() {
		known[m.ID] = m.Persona
		if m.Persona && m.Kind == "success" && !many[m.ID] {
			t.Errorf("success id %s needs 3–5 variants", m.ID)
		}
	}
	for id := range many {
		if voiced, exists := known[id]; exists && !voiced {
			t.Errorf("many.txt id %s is not voiced", id)
		}
	}
}

func TestBuiltinHeaderCount(t *testing.T) {
	if count := len(Builtins()); count != 8 {
		t.Fatalf("want eight built-ins, have %d", count)
	}
	ids := Builtins()
	if !slices.Contains(ids, Default) {
		t.Fatal("default built-in missing")
	}
	for _, id := range ids {
		if !ValidID(id) {
			t.Fatalf("invalid built-in id %q", id)
		}
	}
}
