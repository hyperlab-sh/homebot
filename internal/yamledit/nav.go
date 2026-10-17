package yamledit

import (
	"errors"
	"fmt"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// parseRoot parses text and returns the document's root node, nil for an
// empty or comment-only file.
func parseRoot(text []byte) (ast.Node, error) {
	f, err := parser.ParseBytes(text, 0)
	if err != nil {
		first, _, _ := strings.Cut(err.Error(), "\n")
		return nil, fmt.Errorf("yamledit: invalid YAML: %s", first)
	}
	var root ast.Node
	for _, d := range f.Docs {
		if d.Body == nil {
			continue
		}
		if _, isComment := d.Body.(*ast.CommentGroupNode); isComment {
			continue
		}
		if root != nil {
			return nil, errors.New("yamledit: the file holds more than one YAML document")
		}
		root = d.Body
	}
	return root, nil
}

// unwrap drops anchors and tags, which sit in front of the value they mark.
func unwrap(n ast.Node) ast.Node {
	for {
		switch v := n.(type) {
		case *ast.AnchorNode:
			n = v.Value
		case *ast.TagNode:
			n = v.Value
		default:
			return n
		}
	}
}

// mapping returns a mapping's entries and whether it is written in flow style.
func mapping(n ast.Node) (entries []*ast.MappingValueNode, flow, ok bool) {
	switch v := unwrap(n).(type) {
	case *ast.MappingNode:
		return v.Values, v.IsFlowStyle, true
	case *ast.MappingValueNode:
		return []*ast.MappingValueNode{v}, false, true
	}
	return nil, false, false
}

func keyName(k ast.MapKeyNode) string {
	if s, ok := k.(*ast.StringNode); ok {
		return s.Value
	}
	return strings.TrimSpace(k.String())
}

// found is where a lookup of a key path ended.
type found struct {
	// matched is how many keys of the path exist.
	matched int
	// holder is the node that holds the first missing key (or the last found
	// value when everything matched): a mapping, or nil for an empty file.
	holder ast.Node
	// entry is the mapping entry for the last matched key; nil when none matched.
	entry *ast.MappingValueNode
	// value is that entry's value, unwrapped; nil when none matched.
	value ast.Node
	// parentFlow reports whether the mapping holding entry is in flow style.
	parentFlow bool
}

// locate follows path from root as far as it exists. It fails when a value on
// the way is not a mapping.
func locate(root ast.Node, path []string) (found, error) {
	f := found{holder: root}
	cur := root
	for i, key := range path {
		if cur == nil {
			return f, nil
		}
		entries, flow, ok := mapping(cur)
		if !ok {
			if isNull(cur) && i > 0 {
				return f, nil
			}
			if i == 0 {
				return f, errors.New("yamledit: the document is not a mapping")
			}
			return f, fmt.Errorf("yamledit: %s is not a mapping", strings.Join(path[:i], "."))
		}
		var hit *ast.MappingValueNode
		for _, e := range entries {
			if keyName(e.Key) == key {
				hit = e
			}
		}
		if hit == nil {
			f.holder = cur
			return f, nil
		}
		f.matched = i + 1
		f.entry = hit
		f.parentFlow = flow
		f.value = unwrap(hit.Value)
		f.holder = hit.Value
		cur = f.value
	}
	return f, nil
}

func isNull(n ast.Node) bool {
	_, ok := unwrap(n).(*ast.NullNode)
	return ok
}

// scalarString returns the text of a scalar node as a string.
func scalarString(n ast.Node) (string, bool) {
	switch v := unwrap(n).(type) {
	case *ast.StringNode:
		return v.Value, true
	case *ast.IntegerNode, *ast.FloatNode, *ast.BoolNode, *ast.InfinityNode, *ast.NanNode:
		return v.GetToken().Value, true
	}
	return "", false
}
