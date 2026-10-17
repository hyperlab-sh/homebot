package profile

import (
	"cmp"
	"errors"
	"math"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
)

// The decoded YAML is plain Go data, typed the way a JavaScript YAML parser
// types it: nil (null), string, float64 (every number), bool, []any and
// *mapping.

// mapping is a YAML mapping that remembers the order its keys came in.
type mapping struct {
	keys []string
	vals map[string]any
}

// orderedKeys lists the keys in the order JavaScript enumerates an object's
// properties: integer-like keys ascending, then the rest as they came.
func (m *mapping) orderedKeys() []string {
	var ints, rest []string
	for _, k := range m.keys {
		if isArrayIndex(k) {
			ints = append(ints, k)
		} else {
			rest = append(rest, k)
		}
	}
	slices.SortFunc(ints, func(a, b string) int {
		x, _ := strconv.ParseUint(a, 10, 32)
		y, _ := strconv.ParseUint(b, 10, 32)
		return cmp.Compare(x, y)
	})
	return append(ints, rest...)
}

// isArrayIndex reports whether k is a canonical array index: no sign, no
// leading zeros, below 2^32-1.
func isArrayIndex(k string) bool {
	if k == "" || (len(k) > 1 && k[0] == '0') {
		return false
	}
	n, err := strconv.ParseUint(k, 10, 64)
	return err == nil && n < 1<<32-1 && strconv.FormatUint(n, 10) == k
}

func (m *mapping) get(key string) (any, bool) {
	v, ok := m.vals[key]
	return v, ok
}

func (m *mapping) set(key string, v any) {
	if _, ok := m.vals[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.vals[key] = v
}

var (
	decimalNumber = regexp.MustCompile(`^[-+]?(\.[0-9]+|[0-9]+(\.[0-9]*)?)([eE][-+]?[0-9]+)?$`)
	hexNumber     = regexp.MustCompile(`^([-+]?)0x([0-9a-fA-F]+)$`)
	octalNumber   = regexp.MustCompile(`^0o([0-7]+)$`)
	infinity      = regexp.MustCompile(`^([-+]?)\.(inf|Inf|INF)$`)
	notANumber    = regexp.MustCompile(`^\.(nan|NaN|NAN)$`)
)

// plainScalar types an unquoted scalar the way Bun.YAML does: null, true and
// false in three spellings each, decimal numbers, 0x and 0o integers, .inf
// and .nan. Everything else, including yes/no, dates and 0b101, is text.
func plainScalar(s string) any {
	switch s {
	case "", "null", "Null", "NULL", "~":
		return nil
	case "true", "True", "TRUE":
		return true
	case "false", "False", "FALSE":
		return false
	}
	if decimalNumber.MatchString(s) {
		f, err := strconv.ParseFloat(s, 64)
		if err == nil || errors.Is(err, strconv.ErrRange) {
			return f
		}
	}
	if m := hexNumber.FindStringSubmatch(s); m != nil {
		return bigToFloat(m[1], m[2], 16)
	}
	if m := octalNumber.FindStringSubmatch(s); m != nil {
		return bigToFloat("", m[1], 8)
	}
	if m := infinity.FindStringSubmatch(s); m != nil {
		if m[1] == "-" {
			return math.Inf(-1)
		}
		return math.Inf(1)
	}
	if notANumber.MatchString(s) {
		return math.NaN()
	}
	return s
}

func bigToFloat(sign, digits string, base int) float64 {
	n, _ := new(big.Int).SetString(digits, base)
	f, _ := new(big.Float).SetInt(n).Float64()
	if sign == "-" {
		return -f
	}
	return f
}

// jsString is JavaScript's String(value) for decoded YAML.
func jsString(v any) string {
	switch v := v.(type) {
	case nil:
		return "null"
	case string:
		return v
	case float64:
		return jsNumberString(v)
	case bool:
		return strconv.FormatBool(v)
	case []any:
		parts := make([]string, len(v))
		for i, e := range v {
			if e != nil {
				parts[i] = jsString(e)
			}
		}
		return strings.Join(parts, ",")
	}
	return "[object Object]"
}

// errUnexpectedToken stands for the parse failures Bun reports as
// "Unexpected token".
var errUnexpectedToken = errors.New("YAML Parse error: Unexpected token")

// decodeYAML parses text and returns its first document, nil when empty.
func decodeYAML(text string) (any, error) {
	text = strings.TrimPrefix(text, "\uFEFF")
	f, err := parseTolerant(text)
	if err != nil {
		return nil, errors.New("YAML Parse error: " + firstLine(err.Error()))
	}
	if len(f.Docs) > 1 {
		// Bun returns a list for several documents, which is never a profile.
		return []any{}, nil
	}
	if len(f.Docs) == 0 || f.Docs[0].Body == nil {
		return nil, nil
	}
	d := &decoder{anchors: map[string]any{}}
	return d.node(f.Docs[0].Body)
}

var parseErrorPosition = regexp.MustCompile(`^\[(\d+):(\d+)\] undefined anchor value`)

// parseTolerant parses text. The parser refuses an anchor with no value
// ("key: &a"), which is a null; each one it reports gets a "~" right after
// the anchor on the reported line, and nothing else is touched.
func parseTolerant(text string) (*ast.File, error) {
	for range 1000 {
		f, err := parser.ParseBytes([]byte(text), 0, parser.AllowDuplicateMapKey())
		if err == nil {
			return f, nil
		}
		m := parseErrorPosition.FindStringSubmatch(firstLine(err.Error()))
		if m == nil {
			return nil, err
		}
		lineNo, _ := strconv.Atoi(m[1])
		col, _ := strconv.Atoi(m[2])
		lines := strings.Split(text, "\n")
		if lineNo < 1 || lineNo > len(lines) || col < 1 || col > len(lines[lineNo-1]) {
			return nil, err
		}
		line := lines[lineNo-1]
		end := col - 1
		for end < len(line) && line[end] != ' ' && line[end] != '\t' && line[end] != '\r' {
			end++
		}
		lines[lineNo-1] = line[:end] + " ~" + line[end:]
		text = strings.Join(lines, "\n")
	}
	return nil, errUnexpectedToken
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

type decoder struct {
	anchors map[string]any
}

func (d *decoder) node(n ast.Node) (any, error) {
	switch n := n.(type) {
	case nil, *ast.NullNode:
		return nil, nil
	case *ast.MappingNode:
		return d.mapping(n.Values)
	case *ast.MappingValueNode:
		return d.mapping([]*ast.MappingValueNode{n})
	case *ast.SequenceNode:
		out := make([]any, 0, len(n.Values))
		for _, e := range n.Values {
			v, err := d.node(e)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case *ast.AnchorNode:
		v, err := d.node(n.Value)
		if err != nil {
			return nil, err
		}
		d.anchors[n.Name.String()] = v
		return v, nil
	case *ast.AliasNode:
		v, ok := d.anchors[n.Value.String()]
		if !ok {
			return nil, errors.New("YAML Parse error: Unresolved alias")
		}
		return v, nil
	case *ast.TagNode:
		return d.tagged(n)
	case *ast.LiteralNode:
		return n.Value.Value, nil
	case *ast.MappingKeyNode:
		return nil, errUnexpectedToken
	case *ast.StringNode:
		return scalarText(n.Token, n.Value)
	case ast.ScalarNode:
		tk := n.GetToken()
		return scalarText(tk, tk.Value)
	}
	return nil, errUnexpectedToken
}

// scalarText types one scalar token. Quoted scalars are always text; plain
// ones are typed by plainScalar, except that YAML indicators cannot start them.
func scalarText(tk *token.Token, text string) (any, error) {
	if tk.Type == token.SingleQuoteType || tk.Type == token.DoubleQuoteType {
		return text, nil
	}
	if text == "?" || strings.HasPrefix(text, "%") || strings.HasPrefix(text, "@") || strings.HasPrefix(text, "`") {
		return nil, errUnexpectedToken
	}
	return plainScalar(text), nil
}

// tagged decodes "!!int 3" and friends. !!int and !!float read the value as a
// number; every other tag, known or not, gives the value's text.
func (d *decoder) tagged(n *ast.TagNode) (any, error) {
	text := ""
	switch v := n.Value.(type) {
	case nil, *ast.NullNode:
	case *ast.StringNode:
		text = v.Value
	case ast.ScalarNode:
		text = v.GetToken().Value
	default:
		return d.node(n.Value)
	}
	switch n.Start.Value {
	case "!!int", "!!float":
		if f, ok := plainScalar(text).(float64); ok {
			return f, nil
		}
	}
	return text, nil
}

func (d *decoder) mapping(values []*ast.MappingValueNode) (*mapping, error) {
	m := &mapping{vals: map[string]any{}}
	for _, kv := range values {
		key, err := d.node(kv.Key)
		if err != nil {
			return nil, err
		}
		v, err := d.node(kv.Value)
		if err != nil {
			return nil, err
		}
		m.set(jsString(key), v)
	}
	return m, nil
}
