package config

import (
	"fmt"
	"math"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/token"
)

// kind is the YAML type of a node, as the validator needs to tell them apart.
type kind int

const (
	kindNull kind = iota
	kindBool
	kindNumber
	kindString
	kindMap
	kindList
)

// entry is one key/value pair of a mapping.
type entry struct {
	key    string
	keyPos token.Position
	val    *node
}

// node is a YAML value with aliases, anchors and tags resolved away.
type node struct {
	kind    kind
	pos     token.Position
	str     string  // kindString
	boolean bool    // kindBool
	num     float64 // kindNumber
	entries []entry // kindMap
	items   []*node // kindList
}

func (n *node) get(key string) *node {
	var found *node
	for _, e := range n.entries {
		if e.key == key {
			found = e.val
		}
	}
	return found
}

// build converts a goccy AST node into a node. Anything the config cannot
// hold (merge keys, non-scalar keys) is reported as an error at its position.
func build(n ast.Node) (*node, error) {
	switch v := n.(type) {
	case nil:
		return &node{kind: kindNull}, nil
	case *ast.AnchorNode:
		return build(v.Value)
	case *ast.TagNode:
		return build(v.Value)
	case *ast.AliasNode:
		return build(v.Value)
	case *ast.NullNode:
		return &node{kind: kindNull, pos: *v.GetToken().Position}, nil
	case *ast.BoolNode:
		return &node{kind: kindBool, pos: *v.GetToken().Position, boolean: v.Value}, nil
	case *ast.IntegerNode, *ast.FloatNode, *ast.InfinityNode, *ast.NanNode:
		f, err := numberValue(n)
		if err != nil {
			return nil, err
		}
		return &node{kind: kindNumber, pos: *n.GetToken().Position, num: f}, nil
	case *ast.StringNode:
		return &node{kind: kindString, pos: *v.GetToken().Position, str: v.Value}, nil
	case *ast.LiteralNode:
		return &node{kind: kindString, pos: *v.GetToken().Position, str: v.Value.Value}, nil
	case *ast.MappingValueNode:
		return buildMapping([]*ast.MappingValueNode{v}, *v.Key.GetToken().Position)
	case *ast.MappingNode:
		pos := *v.GetToken().Position
		if len(v.Values) > 0 && !v.IsFlowStyle {
			pos = *v.Values[0].Key.GetToken().Position
		}
		return buildMapping(v.Values, pos)
	case *ast.SequenceNode:
		out := &node{kind: kindList, pos: *v.GetToken().Position}
		for _, item := range v.Values {
			child, err := build(item)
			if err != nil {
				return nil, err
			}
			out.items = append(out.items, child)
		}
		return out, nil
	}
	return nil, fmt.Errorf("unsupported YAML node %T", n)
}

func buildMapping(values []*ast.MappingValueNode, pos token.Position) (*node, error) {
	out := &node{kind: kindMap, pos: pos}
	for _, mv := range values {
		key, ok := mv.Key.(*ast.StringNode)
		var name string
		if ok {
			name = key.Value
		} else {
			name = strings.TrimSpace(mv.Key.String())
		}
		val, err := build(mv.Value)
		if err != nil {
			return nil, err
		}
		out.entries = append(out.entries, entry{key: name, keyPos: *mv.Key.GetToken().Position, val: val})
	}
	return out, nil
}

func numberValue(n ast.Node) (float64, error) {
	switch v := n.(type) {
	case *ast.IntegerNode:
		switch x := v.Value.(type) {
		case int64:
			return float64(x), nil
		case uint64:
			return float64(x), nil
		}
	case *ast.FloatNode:
		return v.Value, nil
	case *ast.InfinityNode:
		return v.Value, nil
	case *ast.NanNode:
		return math.NaN(), nil
	}
	return 0, fmt.Errorf("unsupported number %T", n)
}
