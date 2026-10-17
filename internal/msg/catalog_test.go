package msg

import (
	"errors"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

type placeholder struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Optional bool   `yaml:"optional"`
	Value    string `yaml:"value"`
	Meaning  string `yaml:"meaning"`
	Forms    string `yaml:"forms"`
}

type entry struct {
	ID           string        `yaml:"id"`
	Kind         string        `yaml:"kind"`
	Text         string        `yaml:"text"`
	Append       *string       `yaml:"append"`
	Persona      *bool         `yaml:"persona"`
	Class        string        `yaml:"class"`
	Notes        string        `yaml:"notes"`
	Placeholders []placeholder `yaml:"placeholders"`
}

func loadCatalog(t *testing.T) []entry {
	t.Helper()
	raw, err := os.ReadFile("catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Messages []entry `yaml:"messages"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Messages) == 0 {
		t.Fatal("catalog has no messages")
	}
	return doc.Messages
}

// rawEntries decodes every entry as a generic map, to catch fields the typed struct ignores.
func rawEntries(t *testing.T) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile("catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Messages []map[string]any `yaml:"messages"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Messages
}

var personaKinds = map[string]bool{
	"error": true, "warning": true, "status": true, "success": true, "prompt": true,
	"hint": true, "inbox": true, "nudge": true, "notification": true,
}

// wantPersona is the rule of plan 6.2, before explicit opt-outs.
func wantPersona(e entry) bool {
	area, _, _ := strings.Cut(e.ID, ".")
	return personaKinds[e.Kind] && (e.Class == "voice" || e.Class == "mixed") && area != "guard"
}

// notVoiced starts the note of an entry the rule selects that stays fixed.
const notVoiced = "Not voiced:"

var idPattern = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)+$`)

func TestIDsUniqueAndWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range loadCatalog(t) {
		if !idPattern.MatchString(e.ID) {
			t.Errorf("id %q does not match %s", e.ID, idPattern)
		}
		if seen[e.ID] {
			t.Errorf("duplicate id %q", e.ID)
		}
		seen[e.ID] = true
	}
}

// An entry the rule selects is voiced, or says why not in a note starting
// "Not voiced:" (the urgency suffixes, the action scripts' echoes, the option
// parser's wording). An entry the rule does not select is never voiced.
func TestPersonaFlagFollowsRule(t *testing.T) {
	for _, e := range loadCatalog(t) {
		voiced := e.Persona != nil && *e.Persona
		switch {
		case wantPersona(e) && !voiced:
			if !strings.HasPrefix(e.Notes, notVoiced) {
				t.Errorf("%s (kind %s, class %s): the rule says persona: true; persona: false needs a note starting %q", e.ID, e.Kind, e.Class, notVoiced)
			}
		case !wantPersona(e) && voiced:
			t.Errorf("%s (kind %s, class %s): must not have persona: true", e.ID, e.Kind, e.Class)
		}
	}
	for _, id := range []string{"inbox.header.urgent", "inbox.nudge.urgent"} {
		if e := byID(t)[id]; e.Persona == nil || *e.Persona {
			t.Errorf("%s: urgency is never voiced away (plan 6.9), so it must have persona: false", id)
		}
	}
}

// An append keeps a command out of a persona's words, so only an entry a
// persona may reword needs one, and it is literal text.
func TestAppendOnlyOnPersonaEntries(t *testing.T) {
	appends := 0
	for _, e := range loadCatalog(t) {
		if e.Append == nil {
			continue
		}
		appends++
		if e.Persona == nil || !*e.Persona {
			t.Errorf("%s: append %q on an entry without persona: true", e.ID, *e.Append)
		}
		if *e.Append == "" {
			t.Errorf("%s: an empty append; leave it out", e.ID)
		}
	}
	if appends == 0 {
		t.Error("no entry has an append; the decoder is broken")
	}
}

// unliftedFiles hold the voiced mixed entries whose facts are not lifted
// yet, one id per line. They only shrink: an id that is no longer voiced
// mixed must leave its list, and an entry may not become voiced mixed.
var unliftedFiles = []string{"testdata/unlifted-a.txt", "testdata/unlifted-b.txt"}

// readUnlifted is the ids of the ratchet files, by file. A missing file is
// an empty list.
func readUnlifted(t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, path := range unliftedFiles {
		raw, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if id := strings.TrimSpace(line); id != "" {
				out[path] = append(out[path], id)
			}
		}
	}
	return out
}

// Every persona: true entry is class voice: its facts arrive through
// placeholders or the append. The lift is a ratchet: today's voiced mixed
// entries are listed, and the lists only shrink.
func TestPersonaEntriesAreVoice(t *testing.T) {
	cat := byID(t)
	listed := map[string]string{}
	for path, ids := range readUnlifted(t) {
		for _, id := range ids {
			if other, dup := listed[id]; dup {
				t.Errorf("%s: %s is also listed in %s", path, id, other)
			}
			listed[id] = path
			e, ok := cat[id]
			switch {
			case !ok:
				t.Errorf("%s: %s is not in the catalog; remove it from the list", path, id)
			case e.Persona == nil || !*e.Persona || e.Class != "mixed":
				t.Errorf("%s: %s is no longer voiced mixed (class %s); remove it from the list", path, id, e.Class)
			}
		}
	}
	for _, e := range loadCatalog(t) {
		if e.Persona == nil || !*e.Persona || e.Class == "voice" {
			continue
		}
		if _, ok := listed[e.ID]; !ok {
			t.Errorf("%s: persona: true needs class voice, not %s: lift the facts into fixed placeholders or the append", e.ID, e.Class)
		}
	}
}

// neverVoiced says why e may never be voiced (plan Q13), or "".
func neverVoiced(e entry) string {
	area, _, _ := strings.Cut(e.ID, ".")
	switch {
	case e.Kind == "help" || e.Kind == "commit" || e.Kind == "label" || e.Kind == "choice":
		return "kind " + e.Kind
	case e.Class == "fact":
		return "class fact"
	case area == "guard":
		return "the guard"
	case strings.HasPrefix(e.ID, "persona.diag.") || strings.HasPrefix(e.ID, "persona.warning."):
		return "a persona diagnostic reads the same under every persona"
	}
	return ""
}

func TestNeverVoiced(t *testing.T) {
	for _, e := range loadCatalog(t) {
		if why := neverVoiced(e); why != "" && e.Persona != nil && *e.Persona {
			t.Errorf("%s: never voiced (%s), but has persona: true", e.ID, why)
		}
	}
}

func TestNeverVoicedRule(t *testing.T) {
	for _, c := range []struct {
		e     entry
		never bool
	}{
		{entry{ID: "help.unpack.summary", Kind: "help", Class: "voice"}, true},
		{entry{ID: "save.commit.message", Kind: "commit", Class: "voice"}, true},
		{entry{ID: "inventory.label.ok", Kind: "label", Class: "voice"}, true},
		{entry{ID: "cleanup.choice.keep", Kind: "choice", Class: "voice"}, true},
		{entry{ID: "cli.usage.keys", Kind: "status", Class: "fact"}, true},
		{entry{ID: "guard.plaintext", Kind: "error", Class: "voice"}, true},
		{entry{ID: "persona.diag.unknown_key", Kind: "error", Class: "voice"}, true},
		{entry{ID: "persona.warning.unusable", Kind: "warning", Class: "voice"}, true},
		{entry{ID: "persona.use.greeting", Kind: "success", Class: "voice"}, false},
		{entry{ID: "inbox.header.count", Kind: "inbox", Class: "voice"}, false},
		{entry{ID: "guardian.error.x", Kind: "error", Class: "voice"}, false},
	} {
		if got := neverVoiced(c.e) != ""; got != c.never {
			t.Errorf("%s (kind %s, class %s): never voiced = %v, want %v", c.e.ID, c.e.Kind, c.e.Class, got, c.never)
		}
	}
}

// A fixed placeholder is a fact the generator fills in: a non-empty string,
// never optional, and documented as fixed.
func TestFixedPlaceholders(t *testing.T) {
	fixed := 0
	for _, raw := range rawEntries(t) {
		ps, _ := raw["placeholders"].([]any)
		for _, p := range ps {
			p, _ := p.(map[string]any)
			v, has := p["value"]
			if !has {
				continue
			}
			fixed++
			id, name := raw["id"], p["name"]
			if s, ok := v.(string); !ok || s == "" {
				t.Errorf("%v: placeholder %v has value %#v; want a non-empty string", id, name, v)
			}
			if p["type"] != "string" || p["optional"] == true {
				t.Errorf("%v: fixed placeholder %v must be a string that is not optional", id, name)
			}
			if m, _ := p["meaning"].(string); !strings.HasSuffix(m, "(fixed)") {
				t.Errorf("%v: fixed placeholder %v needs a meaning ending in \"(fixed)\", not %q", id, name, m)
			}
		}
	}
	if fixed == 0 {
		t.Error("no fixed placeholder found; action.link.warning.in_way has one")
	}
}

func TestNoRetiredFields(t *testing.T) {
	for _, e := range rawEntries(t) {
		if _, ok := e["emoji"]; ok {
			t.Errorf("%v: emoji field must be gone", e["id"])
		}
	}
}

func TestNoAgentAreaOrID(t *testing.T) {
	retired := map[string]bool{"inbox.no_age.title": true}
	for _, e := range loadCatalog(t) {
		if retired[e.ID] {
			t.Errorf("%s was deleted in the port", e.ID)
		}
		for _, seg := range strings.Split(e.ID, ".") {
			if seg == "agent" || strings.Contains(seg, "agent") {
				t.Errorf("%s: id still mentions agent", e.ID)
			}
		}
		for _, p := range e.Placeholders {
			if strings.Contains(p.Name, "agent") {
				t.Errorf("%s: placeholder %q still mentions agent", e.ID, p.Name)
			}
		}
	}
}

var validTypes = map[string]bool{"int": true, "string": true, "path": true, "duration": true}

func TestPlaceholdersHaveValidTypes(t *testing.T) {
	for _, e := range loadCatalog(t) {
		seen := map[string]bool{}
		for _, p := range e.Placeholders {
			if !validTypes[p.Type] {
				t.Errorf("%s: placeholder %q has type %q", e.ID, p.Name, p.Type)
			}
			if seen[p.Name] {
				t.Errorf("%s: placeholder %q declared twice", e.ID, p.Name)
			}
			seen[p.Name] = true
			if p.Meaning == "" {
				t.Errorf("%s: placeholder %q has no meaning", e.ID, p.Name)
			}
		}
	}
}

var pluralCategories = map[string]bool{"zero": true, "one": true, "two": true, "few": true, "many": true, "other": true}

// parsedRef is one {name} or {name:forms} found in a text.
type parsedRef struct {
	name  string
	forms string
	plain bool
}

// scanText extracts the placeholders of a catalog text. {{ and }} are literal braces.
func scanText(text string) (refs []parsedRef, err error) {
	for i := 0; i < len(text); {
		switch {
		case strings.HasPrefix(text[i:], "{{"), strings.HasPrefix(text[i:], "}}"):
			i += 2
		case text[i] == '}':
			return nil, errUnbalanced("lone } at byte " + strconv.Itoa(i))
		case text[i] == '{':
			end := strings.IndexByte(text[i:], '}')
			if end < 0 {
				return nil, errUnbalanced("unclosed { at byte " + strconv.Itoa(i))
			}
			body := text[i+1 : i+end]
			if strings.Contains(body, "{") {
				return nil, errUnbalanced("nested { in " + body)
			}
			name, forms, hasForms := strings.Cut(body, ":")
			refs = append(refs, parsedRef{name: name, forms: forms, plain: !hasForms})
			i += end + 1
		default:
			i++
		}
	}
	return refs, nil
}

type errUnbalanced string

func (e errUnbalanced) Error() string { return string(e) }

var nameChars = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func TestTextPlaceholdersMatchDeclarations(t *testing.T) {
	for _, e := range loadCatalog(t) {
		refs, err := scanText(e.Text)
		if err != nil {
			t.Errorf("%s: %v in %q", e.ID, err, e.Text)
			continue
		}
		declared := map[string]placeholder{}
		for _, p := range e.Placeholders {
			declared[p.Name] = p
		}
		used := map[string]bool{}
		for _, r := range refs {
			if !nameChars.MatchString(r.name) {
				t.Errorf("%s: bad placeholder name %q", e.ID, r.name)
				continue
			}
			used[r.name] = true
			if _, ok := declared[r.name]; !ok {
				t.Errorf("%s: {%s} in text is not declared", e.ID, r.name)
			}
		}
		for _, p := range e.Placeholders {
			if !used[p.Name] && !p.Optional {
				t.Errorf("%s: placeholder %q is declared, not optional, and absent from text %q", e.ID, p.Name, e.Text)
			}
		}
		if e.Append != nil && strings.ContainsAny(strings.ReplaceAll(*e.Append, "{self}", ""), "{}") {
			t.Errorf("%s: append %q must be literal except for {self}", e.ID, *e.Append)
		}
	}
}

func TestPluralSyntaxWellFormed(t *testing.T) {
	plurals := 0
	for _, e := range loadCatalog(t) {
		refs, err := scanText(e.Text)
		if err != nil {
			continue // reported by TestTextPlaceholdersMatchDeclarations
		}
		declared := map[string]placeholder{}
		for _, p := range e.Placeholders {
			declared[p.Name] = p
		}
		for _, r := range refs {
			if r.plain {
				continue
			}
			plurals++
			if declared[r.name].Type != "int" {
				t.Errorf("%s: plural selector {%s:...} must be an int placeholder", e.ID, r.name)
			}
			if declared[r.name].Forms != r.forms {
				t.Errorf("%s: {%s:%s} in text but forms is %q", e.ID, r.name, r.forms, declared[r.name].Forms)
			}
			if msg := checkForms(r.forms); msg != "" {
				t.Errorf("%s: {%s:%s}: %s", e.ID, r.name, r.forms, msg)
			}
		}
	}
	if plurals == 0 {
		t.Error("no plural selectors found; the scanner is broken")
	}
}

// checkForms validates "a|b" (one|other) or "one=a|other=b" and returns a problem, or "".
func checkForms(forms string) string {
	parts := strings.Split(forms, "|")
	if len(parts) < 2 {
		return "needs at least two forms"
	}
	named := strings.Contains(parts[0], "=") && pluralCategories[strings.SplitN(parts[0], "=", 2)[0]]
	if !named {
		if len(parts) != 2 {
			return "unnamed forms must be exactly one|other"
		}
		for _, p := range parts {
			if p == "" || strings.Contains(p, "=") && pluralCategories[strings.SplitN(p, "=", 2)[0]] {
				return "empty form or mixed named and unnamed forms"
			}
		}
		return ""
	}
	seen := map[string]bool{}
	for _, p := range parts {
		cat, body, ok := strings.Cut(p, "=")
		if !ok || !pluralCategories[cat] {
			return "form " + p + " is not category=text"
		}
		if body == "" {
			return "empty form for " + cat
		}
		if seen[cat] {
			return "duplicate category " + cat
		}
		seen[cat] = true
	}
	if !seen["other"] {
		return "named forms need an other category"
	}
	return ""
}

func TestScanTextEdgeCases(t *testing.T) {
	good := map[string]int{
		"plain":                      0,
		"{a} and {b}":                2,
		"{{literal}} {a}":            1,
		"{n:# thing|# things} {{x}}": 1,
		"":                           0,
	}
	for text, want := range good {
		refs, err := scanText(text)
		if err != nil || len(refs) != want {
			t.Errorf("scanText(%q) = %v, %v; want %d refs", text, refs, err, want)
		}
	}
	for _, text := range []string{"{a", "a}", "{{a}", "{a{b}}", "}{"} {
		if _, err := scanText(text); err == nil {
			t.Errorf("scanText(%q) should fail", text)
		}
	}
	for forms, ok := range map[string]bool{
		"# a|# b":             true,
		"one=# a|other=# b":   true,
		"one=a|few=b|other=c": true,
		"# a":                 false,
		"# a|# b|# c":         false,
		"one=a|two=b":         false,
		"one=a|one=b|other=c": false,
		"a|":                  false,
		"one=a|b":             false,
		"colour=a|other=b":    false,
	} {
		if got := checkForms(forms) == ""; got != ok {
			t.Errorf("checkForms(%q) valid=%v, want %v", forms, got, ok)
		}
	}
}

func byID(t *testing.T) map[string]entry {
	t.Helper()
	m := map[string]entry{}
	for _, e := range loadCatalog(t) {
		m[e.ID] = e
	}
	return m
}

// TestNeutralOutputMatchesTypeScript pins text + append to the strings src/inbox.ts prints.
func TestNeutralOutputMatchesTypeScript(t *testing.T) {
	cat := byID(t)
	cases := []struct {
		id   string
		subs []string // old/new pairs applied to text before appending
		want string
	}{
		{"inbox.next_hint", nil, "  Do the first one now: homebot next"},
		{"inbox.more", []string{"{n}", "4"}, "  ...and 4 more: homebot --all"},
		{"inbox.nudge", []string{"{bot}", "homebot", "{n:# thing needs|# things need}", "3 things need", "{urgent}", " (1 urgent)"},
			"🚚 homebot: 3 things need you (1 urgent), run: homebot"},
		{"inbox.notify.body", []string{"{title}", "Backup failed", "{more}", " (+2 more)"}, "Backup failed (+2 more). Run: homebot"},
	}
	for _, c := range cases {
		e, ok := cat[c.id]
		if !ok || e.Append == nil {
			t.Errorf("%s: missing or has no append", c.id)
			continue
		}
		got := strings.NewReplacer(c.subs...).Replace(e.Text + *e.Append)
		if c.id == "inbox.nudge" {
			got = cat["inbox.nudge.mark"].Text + " " + got
		}
		got = strings.ReplaceAll(got, "{self}", "homebot")
		if got != c.want {
			t.Errorf("%s: neutral output %q, want %q", c.id, got, c.want)
		}
	}
}

func TestBotPlaceholderRules(t *testing.T) {
	botEntries := 0
	for _, e := range loadCatalog(t) {
		refs, err := scanText(e.Text)
		if err != nil {
			continue
		}
		uses := false
		for _, r := range refs {
			if r.name == "bot" {
				uses = true
			}
		}
		var decl *placeholder
		for i, p := range e.Placeholders {
			if p.Name == "bot" {
				decl = &e.Placeholders[i]
			}
		}
		if decl != nil && (decl.Type != "string" || !decl.Optional) {
			t.Errorf("%s: bot must be an optional string", e.ID)
		}
		if !uses {
			continue
		}
		botEntries++
		if decl == nil {
			t.Errorf("%s: {bot} used but not declared", e.ID)
		}
		if e.ID != "inbox.header.prefix" && (e.Persona == nil || !*e.Persona) {
			t.Errorf("%s: {bot} only belongs in persona: true entries", e.ID)
		}
		if e.Kind == "error" {
			t.Errorf("%s: errors keep the literal program name, not {bot}", e.ID)
		}
		if area, _, _ := strings.Cut(e.ID, "."); area == "guard" {
			t.Errorf("%s: the guard never uses {bot}", e.ID)
		}
	}
	if botEntries == 0 {
		t.Error("no entry uses {bot}")
	}
}

// The catalog is a reference for what the text says, not a record of which
// porting task wrote it: no "# Task 12" section comments, no "(Task 19)"
// in a note.
func TestCatalogCarriesNoPortingTaskProvenance(t *testing.T) {
	raw, err := os.ReadFile("catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	task := regexp.MustCompile(`(?i)\b(task|wave) [0-9A-Z]\b`)
	for i, line := range strings.Split(string(raw), "\n") {
		if m := task.FindString(line); m != "" {
			t.Errorf("catalog.yaml:%d mentions %q: %s", i+1, m, strings.TrimSpace(line))
		}
	}
}
