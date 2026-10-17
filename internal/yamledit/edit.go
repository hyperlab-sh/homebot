package yamledit

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/goccy/go-yaml/ast"
)

// SetScalar sets the scalar at path to value, creating the key (and any
// missing parents) at the end of its parent mapping. value is plain text, not
// YAML: it is quoted when it would not read back as the same string. Every
// byte outside the changed scalar, or the inserted lines, is left as it was.
func SetScalar(src []byte, path []string, value string) ([]byte, error) {
	if len(path) == 0 {
		return nil, errors.New("yamledit: SetScalar needs a non-empty path")
	}
	e, err := newEditor(src, path)
	if err != nil {
		return nil, err
	}
	var out []byte
	if e.found.matched == len(path) {
		out, err = e.replaceScalar(value)
	} else {
		out, err = e.insertMissing(leaf{scalar: value})
	}
	if err != nil {
		return nil, err
	}
	if err := verifyScalar(out, path, value); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteScalar removes the scalar entry at path (its key and value) and
// leaves every other byte as it was: comment lines around it stay, and so do
// the comments of its neighbours. A trailing comment on the entry's own line
// goes with it. A path that does not exist is not an error: src comes back
// unchanged, so "turn it off" is safe to repeat. A block scalar, a mapping
// or a sequence at path is refused rather than half deleted.
func DeleteScalar(src []byte, path []string) ([]byte, error) {
	if len(path) == 0 {
		return nil, errors.New("yamledit: DeleteScalar needs a non-empty path")
	}
	e, err := newEditor(src, path)
	if err != nil {
		return nil, err
	}
	if e.found.matched != len(path) {
		return src, nil
	}
	if _, ok := scalarString(e.found.value); !ok && !isNull(e.found.value) {
		return nil, fmt.Errorf("yamledit: %s is not a scalar", strings.Join(path, "."))
	}
	var out []byte
	if e.found.parentFlow {
		out, err = e.deleteFromFlow()
	} else {
		out, err = e.deleteLine()
	}
	if err != nil {
		return nil, err
	}
	root, err := parseRoot(out)
	if err != nil {
		return nil, fmt.Errorf("yamledit: the edit produced invalid YAML (%w)", err)
	}
	if f, err := locate(root, path); err != nil || f.matched == len(path) {
		return nil, fmt.Errorf("yamledit: %s is still there after the edit", strings.Join(path, "."))
	}
	return out, nil
}

// deleteLine removes the lines of a block mapping entry.
func (e *editor) deleteLine() ([]byte, error) {
	ent := e.found.entry
	keyLine := ent.Key.GetToken().Position.Line - 1
	end, err := e.entryEnd(ent, ent.Key.GetToken().Position.Column-1)
	if err != nil {
		return nil, err
	}
	from, to := e.s.lines[keyLine].start, len(e.s.text)
	if end+1 < len(e.s.lines) {
		to = e.s.lines[end+1].start
	} else if e.s.lines[end].end == len(e.s.text) && keyLine > 0 {
		// The last line has no line break of its own: take the one before it
		// instead, so the file still ends the way it did.
		from = e.s.lines[keyLine-1].end
	}
	return splice(e.s.text, from, to, ""), nil
}

// deleteFromFlow removes one member of a flow mapping, with one comma.
func (e *editor) deleteFromFlow() ([]byte, error) {
	ent := e.found.entry
	text := e.s.text
	from := e.s.offset(ent.Key.GetToken().Position.Line, ent.Key.GetToken().Position.Column)
	to := e.pos(ent) + 1
	if !isNull(e.found.value) || e.explicitNullLen(e.found.value) > 0 {
		start := e.pos(e.found.value)
		end, err := scalarEnd(text, start, true)
		if err != nil {
			return nil, err
		}
		to = end
	}
	isSpace := func(b byte) bool { return b == ' ' || b == '\t' }
	if isNull(e.found.value) {
		// "key: " with no value: the spaces after the colon go with it.
		for to < len(text) && isSpace(text[to]) {
			to++
		}
	}
	if out, ok := deleteFlowLine(text, from, to, isSpace); ok {
		return out, nil
	}
	after := to
	for after < len(text) && isSpace(text[after]) {
		after++
	}
	if after < len(text) && text[after] == ',' {
		after++
		for after < len(text) && isSpace(text[after]) {
			after++
		}
		return splice(text, from, after, ""), nil
	}
	before := from
	for before > 0 && isSpace(text[before-1]) {
		before--
	}
	if before > 0 && text[before-1] == ',' {
		before--
		return splice(text, before, to, ""), nil
	}
	return splice(text, from, to, ""), nil
}

// AppendToSequence adds item to the end of the sequence at path, creating the
// sequence (and any missing parents) when it does not exist. An empty path
// means the document itself. item is plain text, quoted when needed. Every
// other byte is left as it was.
//
// Known limits: a comment line after a multi-line last item stays after the
// appended item, and into an empty key the item goes directly under the key
// line, so a deeper-indented comment under that key ends up after it.
func AppendToSequence(src []byte, path []string, item string) ([]byte, error) {
	e, err := newEditor(src, path)
	if err != nil {
		return nil, err
	}
	var out []byte
	before := 0
	switch {
	case len(path) == 0 && e.root == nil:
		out = e.s.appendLines([]string{"- " + render(item, false)})
	case e.found.matched == len(path):
		target := e.root
		if len(path) > 0 {
			target = e.found.value
		}
		before, out, err = e.appendItem(target, item)
	default:
		out, err = e.insertMissing(leaf{item: &item})
	}
	if err != nil {
		return nil, err
	}
	if err := verifySequence(out, path, before, item); err != nil {
		return nil, err
	}
	return out, nil
}

// leaf is what a missing key chain ends in: a scalar, or a sequence of one item.
type leaf struct {
	scalar string
	item   *string
}

type editor struct {
	s     *source
	root  ast.Node
	path  []string
	found found
	step  int
}

func newEditor(src []byte, path []string) (*editor, error) {
	root, err := parseRoot(src)
	if err != nil {
		return nil, err
	}
	f, err := locate(root, path)
	if err != nil {
		return nil, err
	}
	e := &editor{s: newSource(src), root: root, path: path, found: f}
	e.step = e.indentStep()
	return e, nil
}

// pos returns the byte offset of a node's first token.
func (e *editor) pos(n ast.Node) int {
	p := n.GetToken().Position
	return e.s.offset(p.Line, p.Column)
}

func (e *editor) lineOf(off int) int {
	i := sort.Search(len(e.s.lines), func(i int) bool { return e.s.lines[i].start > off })
	return i - 1
}

// indentStep is how far this file indents a nested block mapping, 2 when it
// has none to learn from.
func (e *editor) indentStep() int {
	step := 0
	var walk func(n ast.Node)
	walk = func(n ast.Node) {
		if n == nil || step != 0 {
			return
		}
		if entries, flow, ok := mapping(n); ok && !flow {
			for _, ent := range entries {
				child, childFlow, isMap := mapping(ent.Value)
				if isMap && !childFlow && child[0].Key.GetToken().Position.Line > ent.Key.GetToken().Position.Line {
					if d := child[0].Key.GetToken().Position.Column - ent.Key.GetToken().Position.Column; d > 0 {
						step = d
						return
					}
				}
				walk(ent.Value)
			}
		}
		if seq, ok := unwrap(n).(*ast.SequenceNode); ok {
			for _, item := range seq.Values {
				walk(item)
			}
		}
	}
	walk(e.root)
	if step == 0 {
		return 2
	}
	return step
}

func (e *editor) replaceScalar(value string) ([]byte, error) {
	entry, node := e.found.entry, e.found.value
	name := strings.Join(e.path, ".")
	switch node.(type) {
	case *ast.NullNode:
		colon := e.pos(entry)
		start, end := colon+1, colon+1
		if n := e.explicitNullLen(node); n > 0 {
			start = e.pos(node)
			return splice(e.s.text, start, start+n, render(value, e.found.parentFlow)), nil
		}
		return splice(e.s.text, start, end, " "+render(value, e.found.parentFlow)), nil
	case *ast.StringNode, *ast.IntegerNode, *ast.FloatNode, *ast.BoolNode, *ast.InfinityNode, *ast.NanNode:
		start := e.pos(node)
		end, err := scalarEnd(e.s.text, start, e.found.parentFlow)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		return splice(e.s.text, start, end, render(value, e.found.parentFlow)), nil
	case *ast.LiteralNode:
		return nil, fmt.Errorf("yamledit: %s is a block scalar, which is not supported", name)
	}
	return nil, fmt.Errorf("yamledit: %s is not a scalar", name)
}

// explicitNullLen is the length of a written-out null ("null", "~"), or 0 for
// an empty value.
func (e *editor) explicitNullLen(n ast.Node) int {
	rest := string(e.s.text[e.pos(n):])
	for _, lit := range []string{"null", "Null", "NULL", "~"} {
		if strings.HasPrefix(rest, lit) {
			return len(lit)
		}
	}
	return 0
}

// insertMissing adds the part of the path that does not exist yet, ending in l.
func (e *editor) insertMissing(l leaf) ([]byte, error) {
	rest := e.path[e.found.matched:]
	parent := e.root
	if e.found.matched > 0 {
		parent = e.found.value
	}
	if parent == nil {
		return e.s.appendLines(blockLines(rest, 0, e.step, l)), nil
	}
	if entries, flow, ok := mapping(parent); ok {
		if flow {
			return e.insertIntoFlow(parent, len(entries), flowText(rest, l))
		}
		return e.insertIntoBlockMapping(entries, rest, l)
	}
	if _, isNull := parent.(*ast.NullNode); isNull && e.found.matched > 0 {
		return e.fillNull(parent, rest, l)
	}
	if e.found.matched == 0 {
		return nil, errors.New("yamledit: the document is not a mapping")
	}
	return nil, fmt.Errorf("yamledit: %s is not a mapping", strings.Join(e.path[:e.found.matched], "."))
}

func (e *editor) insertIntoBlockMapping(entries []*ast.MappingValueNode, rest []string, l leaf) ([]byte, error) {
	lastEntry := entries[len(entries)-1]
	indent := entries[0].Key.GetToken().Position.Column - 1
	end, err := e.entryEnd(lastEntry, indent)
	if err != nil {
		return nil, err
	}
	return e.s.insertAfterLine(end, blockLines(rest, indent, e.step, l)), nil
}

// entryEnd returns the last line of a block mapping entry whose key sits at
// column indent.
func (e *editor) entryEnd(ent *ast.MappingValueNode, indent int) (int, error) {
	keyLine := ent.Key.GetToken().Position.Line - 1
	switch v := unwrap(ent.Value).(type) {
	case *ast.MappingNode:
		if v.IsFlowStyle {
			return e.flowEndLine(v)
		}
	case *ast.SequenceNode:
		if v.IsFlowStyle {
			return e.flowEndLine(v)
		}
		return e.s.blockEnd(keyLine, indent, true, false), nil
	case *ast.LiteralNode:
		return e.s.blockEnd(keyLine, indent, false, true), nil
	}
	return e.s.blockEnd(keyLine, indent, false, false), nil
}

func (e *editor) flowEndLine(n ast.Node) (int, error) {
	closeAt, err := flowClose(e.s.text, e.pos(n))
	if err != nil {
		return 0, err
	}
	return e.lineOf(closeAt), nil
}

// fillNull gives an empty-valued key (`hosts:`) a block value.
func (e *editor) fillNull(null ast.Node, rest []string, l leaf) ([]byte, error) {
	ent := e.found.entry
	if e.found.parentFlow {
		return nil, fmt.Errorf("yamledit: %s has an empty value inside a flow mapping", strings.Join(e.path[:e.found.matched], "."))
	}
	text := e.s.text
	keyLine := ent.Key.GetToken().Position.Line - 1
	if n := e.explicitNullLen(null); n > 0 {
		start := e.pos(null)
		end := start + n
		for start > 0 && text[start-1] == ' ' {
			start--
		}
		text = splice(text, start, end, "")
		e = &editor{s: newSource(text), step: e.step}
	}
	indent := ent.Key.GetToken().Position.Column - 1 + e.step
	return e.s.insertAfterLine(keyLine, blockLines(rest, indent, e.step, l)), nil
}

// insertIntoFlow adds text as a new member of the flow collection n, which has
// count members.
func (e *editor) insertIntoFlow(n ast.Node, count int, text string) ([]byte, error) {
	open := e.pos(n)
	if count == 0 {
		return splice(e.s.text, open+1, open+1, text), nil
	}
	closeAt, err := flowClose(e.s.text, open)
	if err != nil {
		return nil, err
	}
	at := closeAt
	for at > open && strings.ContainsRune(" \t\r\n", rune(e.s.text[at-1])) {
		at--
	}
	sep := ", "
	if e.s.text[at-1] == ',' {
		sep = " "
	}
	return splice(e.s.text, at, at, sep+text), nil
}

// appendItem adds item to the sequence (or empty value) target and returns the
// sequence's length before the edit.
func (e *editor) appendItem(target ast.Node, item string) (int, []byte, error) {
	name := strings.Join(e.path, ".")
	switch v := unwrap(target).(type) {
	case *ast.SequenceNode:
		if v.IsFlowStyle {
			out, err := e.insertIntoFlow(v, len(v.Values), render(item, true))
			return len(v.Values), out, err
		}
		dash := v.GetToken().Position
		first := dash.Line - 1
		end := e.s.blockEnd(first, dash.Column-1, true, false)
		return len(v.Values), e.s.insertAfterLine(end, []string{strings.Repeat(" ", dash.Column-1) + "- " + render(item, false)}), nil
	case *ast.NullNode:
		if len(e.path) == 0 {
			return 0, nil, fmt.Errorf("yamledit: the document is empty or null")
		}
		out, err := e.fillNull(v, nil, leaf{item: &item})
		return 0, out, err
	}
	return 0, nil, fmt.Errorf("yamledit: %s is not a sequence", name)
}

// blockLines renders keys as nested block mappings starting at indent, ending
// in l.
func blockLines(keys []string, indent, step int, l leaf) []string {
	var lines []string
	for i, k := range keys {
		pad := strings.Repeat(" ", indent+i*step)
		last := i == len(keys)-1
		switch {
		case last && l.item == nil:
			lines = append(lines, pad+render(k, false)+": "+render(l.scalar, false))
		default:
			lines = append(lines, pad+render(k, false)+":")
		}
	}
	if l.item != nil {
		pad := strings.Repeat(" ", indent+len(keys)*step)
		if len(keys) == 0 {
			pad = strings.Repeat(" ", indent)
		}
		lines = append(lines, pad+"- "+render(*l.item, false))
	}
	return lines
}

// flowText renders keys as nested flow mappings ending in l.
func flowText(keys []string, l leaf) string {
	k := render(keys[0], true)
	if len(keys) > 1 {
		return k + ": {" + flowText(keys[1:], l) + "}"
	}
	if l.item != nil {
		return k + ": [" + render(*l.item, true) + "]"
	}
	return k + ": " + render(l.scalar, true)
}

func verifyScalar(out []byte, path []string, want string) error {
	root, err := parseRoot(out)
	if err != nil {
		return fmt.Errorf("yamledit: the edit produced invalid YAML (%w)", err)
	}
	f, err := locate(root, path)
	if err != nil || f.matched != len(path) {
		return fmt.Errorf("yamledit: the edit did not land at %s", strings.Join(path, "."))
	}
	if got, ok := scalarString(f.value); !ok || got != want {
		return fmt.Errorf("yamledit: %s reads back as %q, not %q", strings.Join(path, "."), got, want)
	}
	return nil
}

func verifySequence(out []byte, path []string, before int, item string) error {
	root, err := parseRoot(out)
	if err != nil {
		return fmt.Errorf("yamledit: the edit produced invalid YAML (%w)", err)
	}
	target := root
	if len(path) > 0 {
		f, err := locate(root, path)
		if err != nil || f.matched != len(path) {
			return fmt.Errorf("yamledit: the edit did not land at %s", strings.Join(path, "."))
		}
		target = f.value
	}
	seq, ok := unwrap(target).(*ast.SequenceNode)
	if !ok || len(seq.Values) != before+1 {
		return fmt.Errorf("yamledit: %s is not a sequence of %d items after the edit", strings.Join(path, "."), before+1)
	}
	if got, ok := scalarString(seq.Values[before]); !ok || got != item {
		return fmt.Errorf("yamledit: the new item reads back as %q, not %q", got, item)
	}
	return nil
}

// deleteFlowLine handles a member that is alone on its line(s) in a flow
// mapping written over several lines: the whole line goes, and when no comma
// followed the member, the comma that ended the line before goes too. ok is
// false when the member shares its line with anything else.
func deleteFlowLine(text []byte, from, to int, isSpace func(byte) bool) ([]byte, bool) {
	ls := from
	for ls > 0 && isSpace(text[ls-1]) {
		ls--
	}
	if ls > 0 && text[ls-1] != '\n' {
		return nil, false
	}
	hadComma := false
	le := to
	for le < len(text) && isSpace(text[le]) {
		le++
	}
	if le < len(text) && text[le] == ',' {
		hadComma = true
		le++
		for le < len(text) && isSpace(text[le]) {
			le++
		}
	}
	if le < len(text) && text[le] == '\r' {
		le++
	}
	if le >= len(text) || text[le] != '\n' {
		return nil, false
	}
	le++
	if hadComma {
		return splice(text, ls, le, ""), true
	}
	p := ls
	for p > 0 && (isSpace(text[p-1]) || text[p-1] == '\n' || text[p-1] == '\r') {
		p--
	}
	if p > 0 && text[p-1] == ',' {
		out := splice(text, ls, le, "")
		return splice(out, p-1, p, ""), true
	}
	return splice(text, ls, le, ""), true
}
