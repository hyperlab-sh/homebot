package catalog

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
)

// Template is a message text parsed into literal runs and placeholders. The
// catalog's English and every persona variant share this syntax:
//
//   - {name} is replaced by the value of name;
//   - {n:# thing|# things} picks a form by the count n (one|other), and
//     {n:one=# coisa|many=# de coisas|other=# coisas} names the CLDR
//     categories; '#' in a form stands for the number;
//   - {{ and }} are literal braces.
type Template struct {
	parts []part
}

// part is a literal run when ref is nil, else a placeholder.
type part struct {
	lit string
	ref *Ref
}

// Ref is one placeholder in a text: {name}, or {name:forms}, a plural
// selector.
type Ref struct {
	Name string
	// Forms are a selector's forms by CLDR category; nil for a plain
	// placeholder. The two-form shorthand a|b is read as one=a|other=b.
	Forms []Form
	src   string
}

// Form is one branch of a plural selector.
type Form struct {
	// Category is zero, one, two, few, many or other.
	Category string
	Text     string
}

// categories are the CLDR plural categories, by x/text's Form.
var categories = map[plural.Form]string{
	plural.Other: "other",
	plural.Zero:  "zero",
	plural.One:   "one",
	plural.Two:   "two",
	plural.Few:   "few",
	plural.Many:  "many",
}

var nameRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ValidName reports whether s may name a placeholder: a lowercase letter,
// then lowercase letters, digits and underscores.
func ValidName(s string) bool {
	return nameRE.MatchString(s)
}

// Parse parses text in the catalog's syntax.
func Parse(text string) (Template, error) {
	var t Template
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			t.parts = append(t.parts, part{lit: lit.String()})
			lit.Reset()
		}
	}
	for i := 0; i < len(text); {
		switch {
		case strings.HasPrefix(text[i:], "{{"):
			lit.WriteByte('{')
			i += 2
		case strings.HasPrefix(text[i:], "}}"):
			lit.WriteByte('}')
			i += 2
		case text[i] == '}':
			return Template{}, fmt.Errorf("lone } at byte %d (write }} for a brace)", i)
		case text[i] == '{':
			end := strings.IndexByte(text[i:], '}')
			if end < 0 {
				return Template{}, fmt.Errorf("unclosed { at byte %d (write {{ for a brace)", i)
			}
			ref, err := parseRef(text[i : i+end+1])
			if err != nil {
				return Template{}, err
			}
			flush()
			t.parts = append(t.parts, part{ref: ref})
			i += end + 1
		default:
			lit.WriteByte(text[i])
			i++
		}
	}
	flush()
	return t, nil
}

// parseRef parses one placeholder, braces included.
func parseRef(src string) (*Ref, error) {
	body := src[1 : len(src)-1]
	if strings.Contains(body, "{") {
		return nil, fmt.Errorf("placeholders don't nest: %s", src)
	}
	name, forms, isSelector := strings.Cut(body, ":")
	if !ValidName(name) {
		return nil, fmt.Errorf("bad placeholder name %q in %s", name, src)
	}
	ref := &Ref{Name: name, src: src}
	if isSelector {
		f, err := parseForms(forms)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", src, err)
		}
		ref.Forms = f
	}
	return ref, nil
}

// parseForms reads "a|b" (one|other) or "one=a|other=b".
func parseForms(forms string) ([]Form, error) {
	parts := strings.Split(forms, "|")
	if len(parts) < 2 {
		return nil, fmt.Errorf("a plural needs at least two forms")
	}
	if _, _, named := category(parts[0]); !named {
		if len(parts) != 2 {
			return nil, fmt.Errorf("forms without categories must be exactly one|other")
		}
		for _, p := range parts {
			if _, _, named := category(p); p == "" || named {
				return nil, fmt.Errorf("empty form, or named and unnamed forms mixed")
			}
		}
		return []Form{{"one", parts[0]}, {"other", parts[1]}}, nil
	}
	out := make([]Form, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		cat, text, named := category(p)
		switch {
		case !named:
			return nil, fmt.Errorf("form %q is not category=text", p)
		case text == "":
			return nil, fmt.Errorf("empty form for %s", cat)
		case seen[cat]:
			return nil, fmt.Errorf("category %s given twice", cat)
		}
		seen[cat] = true
		out = append(out, Form{cat, text})
	}
	if !seen["other"] {
		return nil, fmt.Errorf("named forms need an other form")
	}
	return out, nil
}

// category splits "one=text" when the part before = is a CLDR category.
func category(form string) (cat, text string, ok bool) {
	cat, text, found := strings.Cut(form, "=")
	if !found {
		return "", "", false
	}
	for _, c := range categories {
		if c == cat {
			return cat, text, true
		}
	}
	return "", "", false
}

// Refs lists the placeholders of t in the order they appear.
func (t Template) Refs() []Ref {
	var refs []Ref
	for _, p := range t.parts {
		if p.ref != nil {
			refs = append(refs, *p.ref)
		}
	}
	return refs
}

// Format renders t. A placeholder takes its value from values: an int is
// written in decimal, a string as it is. A plural selector picks its form by
// lang's CLDR cardinal rules, falling back to other when the category it
// needs is missing, and writes the number for each '#'. A placeholder without
// a value, or a selector whose value is not an int, is written as it stands
// in the text, so the gap shows.
//
// style, when not nil, is given each value as written, with its placeholder's
// name, and returns it styled; the text around values is never passed.
func (t Template) Format(lang language.Tag, values map[string]any, style func(name, value string) string) string {
	if style == nil {
		style = func(_, value string) string { return value }
	}
	var b strings.Builder
	for _, p := range t.parts {
		if p.ref == nil {
			b.WriteString(p.lit)
			continue
		}
		v, ok := values[p.ref.Name]
		switch {
		case !ok:
			b.WriteString(p.ref.src)
		case p.ref.Forms == nil:
			b.WriteString(style(p.ref.Name, valueString(v)))
		default:
			n, isInt := v.(int)
			if !isInt {
				b.WriteString(p.ref.src)
				continue
			}
			b.WriteString(strings.ReplaceAll(p.ref.pick(lang, n), "#", style(p.ref.Name, strconv.Itoa(n))))
		}
	}
	return b.String()
}

// pick returns the form lang's rules choose for n.
func (r *Ref) pick(lang language.Tag, n int) string {
	abs := n
	if abs < 0 {
		abs = -abs
		if abs < 0 { // math.MinInt
			abs = math.MaxInt
		}
	}
	want := categories[plural.Cardinal.MatchPlural(lang, abs, 0, 0, 0, 0)]
	other := ""
	for _, f := range r.Forms {
		if f.Category == want {
			return f.Text
		}
		if f.Category == "other" {
			other = f.Text
		}
	}
	return other
}

func valueString(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	}
	return fmt.Sprint(v)
}
