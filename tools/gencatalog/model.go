package main

import (
	"errors"
	"fmt"
	"go/token"
	"regexp"
	"strings"

	"github.com/hyperlab-sh/homebot/internal/msg/catalog"
)

// accessor is one generated function: a catalog entry and its Go signature.
type accessor struct {
	entry  catalog.Entry
	name   string
	params []param
	// plural reports whether the text holds a plural selector.
	plural bool
}

// param is one value of the Text an accessor returns, in the entry's
// declaration order: an argument of the accessor, or a fixed placeholder,
// whose value the accessor fills in itself.
type param struct {
	name  string // as in the catalog
	ident string // as in Go; empty for a fixed placeholder
	typ   catalog.Type
	// fixed reports a placeholder with a value; value is that value.
	fixed bool
	value string
}

// args are the accessor's parameters: its params that are not fixed.
func (a accessor) args() []param {
	var out []param
	for _, p := range a.params {
		if !p.fixed {
			out = append(out, p)
		}
	}
	return out
}

var idRE = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)+$`)

// handWritten are the exported names of internal/msg that are not
// generated; no accessor may take one. The compiler catches a clash too, but
// only after the file is written.
var handWritten = map[string]bool{
	"Text": true, "Arg": true, "Catalog": true, "NeutralName": true, "Neutral": true, "Speaker": true,
	"Speak": true, "SpeakWithSelf": true, "Slot": true, "Meta": true, "Entries": true, "Lookup": true, "Build": true,
	"UnsafeRune": true, "StripUnsafe": true, "ValidEmoji": true,
	"CheckLine": true, "LineError": true, "LineProblem": true, "LineUnknownID": true, "LineNotVoiced": true,
	"LineUnsafe": true, "LineSyntax": true, "LineUnknownName": true, "LineSelector": true, "LineMissing": true,
}

// bodyNames are the package-level names an accessor body refers to; a
// parameter must not shadow them.
var bodyNames = map[string]bool{"messages": true, "catalog": true}

// initialisms are written in capitals inside Go names, as Go spells them.
var initialisms = map[string]string{
	"api": "API", "ascii": "ASCII", "cli": "CLI", "cpu": "CPU", "css": "CSS", "dns": "DNS",
	"eof": "EOF", "html": "HTML", "http": "HTTP", "https": "HTTPS", "id": "ID", "ip": "IP",
	"json": "JSON", "os": "OS", "ssh": "SSH", "sql": "SQL", "tcp": "TCP", "tls": "TLS",
	"toml": "TOML", "tty": "TTY", "ui": "UI", "uid": "UID", "uri": "URI", "url": "URL",
	"utf8": "UTF8", "uuid": "UUID", "xml": "XML", "yaml": "YAML",
}

// accessors checks every entry of f and returns the functions to generate,
// in catalog order. The error lists every problem found, one per line.
func accessors(f *catalog.File) ([]accessor, error) {
	var errs []error
	fail := func(id, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", id, fmt.Sprintf(format, args...)))
	}
	ids := map[string]bool{}
	funcs := map[string]string{}
	out := make([]accessor, 0, len(f.Messages))
	for _, e := range f.Messages {
		if !idRE.MatchString(e.ID) {
			fail(e.ID, "id must be dotted lowercase words, like inbox.header.count")
			continue
		}
		if ids[e.ID] {
			fail(e.ID, "id given twice")
			continue
		}
		ids[e.ID] = true
		a := accessor{entry: e, name: goName(e.ID, true)}
		if !token.IsIdentifier(a.name) || !token.IsExported(a.name) {
			fail(e.ID, "accessor name %s is not an exported Go identifier", a.name)
		} else if other, taken := funcs[a.name]; taken {
			fail(e.ID, "accessor %s is also the name of %s", a.name, other)
		} else if handWritten[a.name] {
			fail(e.ID, "accessor %s clashes with a hand-written identifier of internal/msg", a.name)
		}
		funcs[a.name] = e.ID

		tmpl, err := catalog.Parse(e.Text)
		if err != nil {
			fail(e.ID, "text: %v", err)
			continue
		}
		if !literalButSelf(e.Append) {
			fail(e.ID, "append %q must be literal text, with no placeholder but %s", e.Append, catalog.SelfRef)
		}
		declared := map[string]catalog.Placeholder{}
		idents := map[string]string{}
		for _, p := range e.Placeholders {
			if _, dup := declared[p.Name]; dup {
				fail(e.ID, "placeholder %s declared twice", p.Name)
				continue
			}
			declared[p.Name] = p
			if !catalog.ValidName(p.Name) {
				fail(e.ID, "placeholder name %q must be lowercase words joined by _", p.Name)
				continue
			}
			if !p.Type.Valid() {
				fail(e.ID, "placeholder %s has type %q; want int, string, path or duration", p.Name, p.Type)
				continue
			}
			value, fixed := p.Fixed()
			if fixed {
				switch {
				case value == "":
					fail(e.ID, "placeholder %s has an empty value; a fixed placeholder needs the fact it stands for", p.Name)
					continue
				case p.Type != catalog.String:
					fail(e.ID, "placeholder %s has a value, so it must be a string, not %s", p.Name, p.Type)
					continue
				case p.Optional:
					fail(e.ID, "placeholder %s has a value, so it cannot be optional", p.Name)
					continue
				case p.Forms != "":
					fail(e.ID, "placeholder %s has a value, so it cannot have plural forms", p.Name)
					continue
				case !literalButSelf(value):
					fail(e.ID, "placeholder %s has the value %q; a value holds no placeholder but %s", p.Name, value, catalog.SelfRef)
					continue
				}
			}
			if p.Name == catalog.Self {
				if p.Type != catalog.String || p.Optional || fixed {
					fail(e.ID, "self must be a string that is not optional and has no value; the renderer fills it")
				}
				continue
			}
			if p.Name == catalog.Bot {
				if p.Type != catalog.String || !p.Optional {
					fail(e.ID, "bot must be an optional string")
				}
				continue
			}
			if fixed {
				a.params = append(a.params, param{name: p.Name, typ: p.Type, fixed: true, value: value})
				continue
			}
			ident := paramIdent(p.Name)
			if other, taken := idents[ident]; taken {
				fail(e.ID, "placeholders %s and %s are both the parameter %s", other, p.Name, ident)
				continue
			}
			idents[ident] = p.Name
			a.params = append(a.params, param{name: p.Name, ident: ident, typ: p.Type})
		}
		used := map[string]bool{}
		for _, r := range tmpl.Refs() {
			used[r.Name] = true
			p, ok := declared[r.Name]
			switch {
			case !ok:
				fail(e.ID, "{%s} is not declared under placeholders", r.Name)
			case r.Forms != nil && p.Type != catalog.Int:
				fail(e.ID, "plural selector {%s:...} needs an int placeholder, not %s", r.Name, p.Type)
			}
			if r.Forms != nil {
				a.plural = true
			}
		}
		for _, p := range e.Placeholders {
			switch {
			case used[p.Name]:
			case p.Value != nil:
				fail(e.ID, "placeholder %s has a value, but the text does not use it", p.Name)
			case !p.Optional:
				fail(e.ID, "placeholder %s is declared and not optional, but the text does not use it", p.Name)
			}
		}
		out = append(out, a)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

// literalButSelf reports whether s holds no brace outside {self}: an append
// or a fixed value may name the command word and nothing else, and the
// renderer expands it there one level deep.
func literalButSelf(s string) bool {
	return !strings.ContainsAny(strings.ReplaceAll(s, catalog.SelfRef, ""), "{}")
}

// goName joins the words of a catalog id or placeholder name in camel case:
// inbox.header.count is InboxHeaderCount, ssh_url is sshURL.
func goName(s string, exported bool) string {
	var b strings.Builder
	for i, w := range strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == '_' }) {
		switch up, ok := initialisms[w]; {
		case i == 0 && !exported:
			b.WriteString(w)
		case ok:
			b.WriteString(up)
		default:
			b.WriteString(strings.ToUpper(w[:1]) + w[1:])
		}
	}
	return b.String()
}

// paramIdent is the Go parameter for a placeholder. Names Go reserves or
// predeclares, and names the accessor body uses, get an Arg suffix.
func paramIdent(name string) string {
	ident := goName(name, false)
	if token.IsKeyword(ident) || predeclared[ident] || bodyNames[ident] {
		ident += "Arg"
	}
	return ident
}

// predeclared are Go's predeclared identifiers.
var predeclared = map[string]bool{
	"any": true, "append": true, "bool": true, "byte": true, "cap": true, "clear": true, "close": true,
	"comparable": true, "complex": true, "complex64": true, "complex128": true, "copy": true,
	"delete": true, "error": true, "false": true, "float32": true, "float64": true, "imag": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true, "iota": true, "len": true,
	"make": true, "max": true, "min": true, "new": true, "nil": true, "panic": true, "print": true,
	"println": true, "real": true, "recover": true, "rune": true, "string": true, "true": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true, "uintptr": true,
}
