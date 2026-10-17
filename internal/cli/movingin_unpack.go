package cli

import (
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/unpack"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func init() { register(MovingIn, "unpack", newUnpackCmd) }

func newUnpackCmd(c *app.Context) *cobra.Command {
	var dryRun bool
	var from string
	cmd := &cobra.Command{
		Use:  "unpack",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := c.LocateRoot(); err != nil {
				return err
			}
			opts := unpack.Options{DryRun: dryRun}
			if cmd.Flags().Changed("from") {
				n, err := unpackFrom(c, from)
				if err != nil {
					return err
				}
				opts.From = &n
			}
			name := ""
			if len(args) > 0 {
				// An empty word is a name, not "use the default": the TypeScript's
				// !name check refused it.
				if args[0] == "" {
					return fail.User(voice.Plain(c, msg.UnpackErrorNoProfile()))
				}
				name = args[0]
			}
			if err := unpack.Run(c, name, opts); err != nil {
				return err
			}
			refreshInbox(c)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "")
	cmd.Flags().StringVar(&from, "from", "", "")
	return cmd
}

// unpackFrom reads the --from value as the TypeScript did: Number(value),
// which must be an integer. Anything else is "--from takes a step number".
// A value too large for an int is clamped to one that is out of any profile's
// range, so unpack.Run reports the range.
func unpackFrom(c *app.Context, value string) (int, error) {
	f := unpackJSNumber(value)
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
		return 0, fail.User(voice.Plain(c, msg.UnpackErrorFromNan()))
	}
	const limit = 1 << 53
	return int(max(-limit, min(limit, f))), nil
}

// unpackDecimal is JavaScript's StrDecimalLiteral without the sign and
// Infinity: digits with an optional fraction and exponent, ASCII only.
var unpackDecimal = regexp.MustCompile(`^(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$`)

// unpackJSNumber is JavaScript's Number(string): NaN for text that is not a
// number, an empty or all-white-space string is 0, and 0x, 0o and 0b prefixes
// (without a sign) read hexadecimal, octal and binary.
func unpackJSNumber(s string) float64 {
	s = strings.TrimFunc(s, unpackJSSpace)
	if s == "" {
		return 0
	}
	if len(s) > 2 && s[0] == '0' {
		base := 0
		switch s[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			n, ok := new(big.Int).SetString(s[2:], base)
			if !ok || strings.ContainsAny(s[2:], "+-_") {
				return math.NaN()
			}
			f, _ := new(big.Float).SetInt(n).Float64()
			return f
		}
	}
	sign := 1.0
	rest := s
	switch s[0] {
	case '+':
		rest = s[1:]
	case '-':
		sign, rest = -1, s[1:]
	}
	if rest == "Infinity" {
		return sign * math.Inf(1)
	}
	if !unpackDecimal.MatchString(rest) {
		return math.NaN()
	}
	f, _ := strconv.ParseFloat(rest, 64) // out of range gives ±Inf, as in JavaScript
	return sign * f
}

// unpackJSSpace is JavaScript's WhiteSpace and LineTerminator.
func unpackJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', 0xFEFF, 0x2028, 0x2029:
		return true
	}
	return unicode.Is(unicode.Zs, r)
}
