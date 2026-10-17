package msg

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestRecordOnlyNotVoiced catches persona lines that production code only
// records. It deliberately checks direct accessor calls: a Text first assigned
// to a variable cannot be classified by this syntax-only scan.
func TestRecordOnlyNotVoiced(t *testing.T) {
	accessors := generatedAccessors(t)
	type uses struct{ record, other int }
	seen := map[string]uses{}
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "messages_gen.go" {
			return nil
		}
		f, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		msgNames := map[string]bool{}
		if f.Name.Name == "msg" && strings.Contains(path, string(filepath.Separator)+"msg"+string(filepath.Separator)) {
			msgNames[""] = true
		}
		for _, imp := range f.Imports {
			if imp.Path.Value != `"github.com/hyperlab-sh/homebot/internal/msg"` {
				continue
			}
			name := "msg"
			if imp.Name != nil {
				name = imp.Name.Name
			}
			msgNames[name] = true
		}
		if len(msgNames) == 0 {
			return nil
		}
		var parents []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				parents = parents[:len(parents)-1]
				return true
			}
			if call, ok := n.(*ast.CallExpr); ok {
				name := ""
				switch fun := call.Fun.(type) {
				case *ast.SelectorExpr:
					if x, ok := fun.X.(*ast.Ident); ok && msgNames[x.Name] {
						name = fun.Sel.Name
					}
				case *ast.Ident:
					if msgNames[""] {
						name = fun.Name
					}
				}
				if id, ok := accessors[name]; ok {
					u := seen[id]
					if len(parents) > 0 && directRecord(parents[len(parents)-1]) {
						u.record++
					} else {
						u.other++ // fail.Say is a voiced use at print.
					}
					seen[id] = u
				}
			}
			parents = append(parents, n)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range loadCatalog(t) {
		if e.Persona == nil || !*e.Persona {
			continue
		}
		u := seen[e.ID]
		if u.record > 0 && u.other == 0 {
			t.Errorf("%s: persona: true but all %d direct production uses are voice.Record", e.ID, u.record)
		}
	}
}

// storedOnly are ids whose text is stored or sent away and never printed as
// voiced text, so they must never be voiced: not persona: true, and no {bot}
// slot, which would make them voiced by name. TestRecordOnlyNotVoiced cannot
// see most of them: they reach voice.Record through a variable, a helper or
// a format argument, so this list is their guard. Add an id here when it is
// only ever recorded; remove it only by making the code print it voiced.
//
// unpack.hint.resume is not on the list on purpose: it is stored neutral and
// printed voiced (voice.NestWith).
var storedOnly = map[string]string{
	"tidy.journal.failed":          "the journal entry of a failed step; renderInbox filters it out",
	"link.error.changed":           "the changed-path lines of tidy's link step, which end up in TidyError and the journal",
	"layout.must_mark.every_layer": "a fragment spliced, as English, into layout.must_mark, whose Op keeps the neutral Reason to store and the unrendered ReasonText to print",
	"name.journal.made":            "the journal entry for a made name link",
	"name.journal.adopted":         "the journal entry for an adopted name link",
	"name.journal.repointed":       "the journal entry for a repointed name link",
	"name.journal.removed":         "the journal entry for a removed name link",
	"remote.repo_description":      "the description of the repository created on the forge; it lives there after creation",
	"save.commit.autosave":         "a git commit subject",
	"save.commit.manual":           "a git commit subject",
	"save.commit.body":             "a git commit body",
	"save.commit.body_item":        "a line of a git commit body",
	"save.commit.body_more":        "the last line of a git commit body",
}

func TestStoredOnlyIDsStayUnvoiced(t *testing.T) {
	byID := map[string]entry{}
	for _, e := range loadCatalog(t) {
		byID[e.ID] = e
	}
	for id, why := range storedOnly {
		e, ok := byID[id]
		if !ok {
			t.Errorf("%s: on the stored-only list (%s) but not in the catalog; remove it from the list", id, why)
			continue
		}
		if e.Persona != nil && *e.Persona {
			t.Errorf("%s: persona: true, but it is only stored or sent away (%s); voiced text must not be stored", id, why)
		}
		for _, p := range e.Placeholders {
			if p.Name == "bot" {
				t.Errorf("%s: has a {bot} slot, which voices it by the persona's name (%s)", id, why)
			}
		}
	}
}

func directRecord(parent ast.Node) bool {
	call, ok := parent.(*ast.CallExpr)
	if !ok {
		return false
	}
	fun, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || fun.Sel.Name != "Record" {
		return false
	}
	x, ok := fun.X.(*ast.Ident)
	return ok && x.Name == "voice"
}

func generatedAccessors(t *testing.T) map[string]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "messages_gen.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Doc == nil {
			continue
		}
		line, _, _ := strings.Cut(fn.Doc.Text(), "\n")
		prefix := fn.Name.Name + " is "
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		id, _, ok := strings.Cut(strings.TrimPrefix(line, prefix), ":")
		if ok {
			out[fn.Name.Name] = id
		}
	}
	if len(out) == 0 {
		t.Fatal("no generated accessors found")
	}
	return out
}
