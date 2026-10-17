package cli

import (
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/hyperlab-sh/homebot/internal/msg"
)

// optionSpec is an option a command takes, as node:util's parseArgs was
// told about it.
type optionSpec struct {
	name string
	// short is the option's letter, or "".
	short string
	// value says the option takes a value (parseArgs' type "string").
	value bool
}

// optionsOf lists the options cmd takes: its flags, without help.
func optionsOf(cmd *cobra.Command) []optionSpec {
	var specs []optionSpec
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Name != "help" {
			specs = append(specs, optionSpec{name: f.Name, short: f.Shorthand, value: f.Value.Type() != "bool"})
		}
	})
	return specs
}

// optionToken is an option on the command line, as parseArgs tokenized it.
type optionToken struct {
	name string
	// raw is the option as parseArgs names it in errors.
	raw string
	// value is the option's value; has says there is one.
	value string
	has   bool
	// inline says the value was part of the same word: --message=x, -mx.
	inline bool
}

// checkOptions finds the first mistake in args that node:util's parseArgs,
// strict and with positionals allowed, threw on in the TypeScript, in the
// words Bun 1.4.2 used. It returns the zero Text when there is none.
func checkOptions(args []string, specs []optionSpec) msg.Text {
	for _, t := range tokenize(args, specs) {
		spec, known := findOption(specs, t.name)
		switch {
		case !known:
			return msg.CLIErrorOptionUnknown(t.raw)
		case spec.value && !t.has:
			return msg.CLIErrorOptionMissingValue(spec.flags())
		case !spec.value && t.has:
			return msg.CLIErrorOptionNoValue(spec.flags())
		case t.has && !t.inline && optionLike(t.value):
			if strings.HasPrefix(t.raw, "--") {
				return msg.CLIErrorOptionAmbiguous(t.raw)
			}
			return msg.CLIErrorOptionAmbiguousShort(t.raw, t.name)
		}
	}
	return msg.Text{}
}

// tokenize reads args the way parseArgs did, up to "--", and returns its
// options. A value option takes the next word whatever it is; strictness
// complains about it afterwards. Letters grouped as -ax are named without
// their dash ('x'), as Bun named them.
func tokenize(args []string, specs []optionSpec) []optionToken {
	var tokens []optionToken
	rest := append([]string(nil), args...)
	next := func(t *optionToken) {
		if takesValue(specs, t.name) && len(rest) > 0 {
			t.value, t.has, rest = rest[0], true, rest[1:]
		}
	}
	for len(rest) > 0 {
		arg := rest[0]
		rest = rest[1:]
		letters := []rune(arg)
		short := len(letters) >= 2 && letters[0] == '-' && letters[1] != '-'
		switch {
		case arg == "--":
			return tokens
		case short && len(letters) == 2:
			t := optionToken{name: longFor(specs, string(letters[1])), raw: arg}
			next(&t)
			tokens = append(tokens, t)
		case short && !takesValue(specs, longFor(specs, string(letters[1]))):
			for i := 1; i < len(letters); i++ {
				t := optionToken{name: longFor(specs, string(letters[i])), raw: string(letters[i])}
				if takesValue(specs, t.name) && i < len(letters)-1 {
					t.value, t.has, t.inline = string(letters[i+1:]), true, true
					tokens = append(tokens, t)
					break
				}
				if i == len(letters)-1 {
					next(&t)
				}
				tokens = append(tokens, t)
			}
		case short:
			tokens = append(tokens, optionToken{
				name: longFor(specs, string(letters[1])), raw: "-" + string(letters[1]),
				value: string(letters[2:]), has: true, inline: true,
			})
		case len(letters) > 2 && strings.HasPrefix(arg, "--") && !strings.Contains(arg[3:], "="):
			t := optionToken{name: arg[2:], raw: arg}
			next(&t)
			tokens = append(tokens, t)
		case len(letters) > 2 && strings.HasPrefix(arg, "--"):
			eq := strings.Index(arg, "=")
			tokens = append(tokens, optionToken{name: arg[2:eq], raw: arg[:eq], value: arg[eq+1:], has: true, inline: true})
		}
	}
	return tokens
}

// longFor is the name of the option with letter short, or short itself when
// none has it, as parseArgs looked it up.
func longFor(specs []optionSpec, short string) string {
	for _, s := range specs {
		if s.short == short {
			return s.name
		}
	}
	return short
}

func findOption(specs []optionSpec, name string) (optionSpec, bool) {
	for _, s := range specs {
		if s.name == name {
			return s, true
		}
	}
	return optionSpec{}, false
}

func takesValue(specs []optionSpec, name string) bool {
	s, ok := findOption(specs, name)
	return ok && s.value
}

// optionLike is a value that looks like an option: a dash and more.
func optionLike(v string) bool {
	return utf8.RuneCountInString(v) > 1 && strings.HasPrefix(v, "-")
}

// flags names the option as parseArgs' errors did: "-m, --message".
func (s optionSpec) flags() string {
	if s.short != "" {
		return "-" + s.short + ", --" + s.name
	}
	return "--" + s.name
}
