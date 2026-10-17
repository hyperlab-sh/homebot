package cli

import (
	"errors"
	"math"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

// TestUnpackFrom pins --from to JavaScript's Number() plus Number.isInteger,
// which is how the TypeScript read it.
func TestUnpackFrom(t *testing.T) {
	const nan = "--from takes a step number"
	cases := []struct {
		in   string
		want int
		err  string
	}{
		// plain integers
		{"1", 1, ""},
		{"7", 7, ""},
		{"007", 7, ""}, // decimal, not octal
		{"+3", 3, ""},
		{"-2", -2, ""},
		{"1.0", 1, ""},
		{"5.", 5, ""},
		{".5e1", 5, ""},
		{"2.50e1", 25, ""},
		// empty and white space are 0
		{"", 0, ""},
		{"   ", 0, ""},
		{"\t\n", 0, ""},
		{"\xc2\xa0\xe3\x80\x80", 0, ""},
		{"\xef\xbb\xbf4\xe2\x80\xa8", 4, ""},
		// white space is trimmed
		{"  3  ", 3, ""},
		{"\n3\t", 3, ""},
		{" 0x10 ", 16, ""},
		// prefixes
		{"0x10", 16, ""},
		{"0X1f", 31, ""},
		{"0o17", 15, ""},
		{"0O7", 7, ""},
		{"0b101", 5, ""},
		{"0B11", 3, ""},
		{"0x0", 0, ""},
		// exponents
		{"1e1", 10, ""},
		{"1E2", 100, ""},
		{"1e+2", 100, ""},
		{"25e-1", 0, nan}, // 2.5
		{"20e-1", 2, ""},
		{"-0", 0, ""},
		{"-0x10", 0, nan}, // no sign on a prefixed number
		{"+0x10", 0, nan},
		// not integers
		{"2.5", 0, nan},
		{"0.1", 0, nan},
		{"1e-1", 0, nan},
		// not numbers
		{"x", 0, nan},
		{"two", 0, nan},
		{"1x", 0, nan},
		{"1 2", 0, nan},
		{"0x", 0, nan},
		{"0xg", 0, nan},
		{"0b2", 0, nan},
		{"0o8", 0, nan},
		{"0x1.8", 0, nan},
		{"0x_1", 0, nan},
		{"1_000", 0, nan},
		{"--1", 0, nan},
		{"+-1", 0, nan},
		{"1e", 0, nan},
		{"e1", 0, nan},
		{".", 0, nan},
		{"+", 0, nan},
		{"-", 0, nan},
		{"1,5", 0, nan},
		{"\xd9\xa1", 0, nan}, // an Arabic-Indic digit
		{"\u0085 3", 0, nan},
		{"NaN", 0, nan},
		{"nan", 0, nan},
		{"inf", 0, nan},
		{"Inf", 0, nan},
		{"infinity", 0, nan},
		{"0x1p3", 0, nan},
		// infinities are not integers
		{"Infinity", 0, nan},
		{"+Infinity", 0, nan},
		{"-Infinity", 0, nan},
		{"1e999", 0, nan},
		{"-1e999", 0, nan},
		{"0xfffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", 0, nan}, // overflows a float64
		// huge integers stay integers, clamped to something out of any range
		{"1e21", 1 << 53, ""},
		{"-1e21", -(1 << 53), ""},
		{"99999999999999999999", 1 << 53, ""},
	}
	sb := testutil.Sandbox(t, testutil.Options{})
	for _, tc := range cases {
		got, err := unpackFrom(sb.Ctx, tc.in)
		if tc.err != "" {
			var ue *fail.UserError
			if !errors.As(err, &ue) || ue.Msg != tc.err {
				t.Errorf("unpackFrom(%q) = %d, %v; want the error %q", tc.in, got, err, tc.err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("unpackFrom(%q) = %d, %v; want %d", tc.in, got, err, tc.want)
		}
	}
}

func TestUnpackJSNumber(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want float64
	}{
		{"2.5", 2.5},
		{"-2.5", -2.5},
		{"1e-1", 0.1},
		{"0x7fffffffffffffff", 9223372036854775807},
		{"Infinity", math.Inf(1)},
		{"-Infinity", math.Inf(-1)},
		{"1e999", math.Inf(1)},
		{"", 0},
	} {
		if got := unpackJSNumber(tc.in); got != tc.want {
			t.Errorf("unpackJSNumber(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
	for _, in := range []string{"x", "NaN", "0x", "1 2"} {
		if got := unpackJSNumber(in); !math.IsNaN(got) {
			t.Errorf("unpackJSNumber(%q) = %v, want NaN", in, got)
		}
	}
	if got := unpackJSNumber("-0"); got != 0 || !math.Signbit(got) {
		t.Errorf("unpackJSNumber(-0) = %v, want -0", got)
	}
}

// With no home repo anywhere, that is what unpack says, even for a bad
// --from: the TypeScript located the repo before it read any option.
func TestUnpackLocatesTheRepoBeforeReadingFrom(t *testing.T) {
	c, stdout, _ := noRepo(t)
	want := c.LocateRoot()
	if want == nil {
		t.Fatal("noRepo found a repo")
	}
	cmd := newUnpackCmd(c)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs([]string{"--from", "x"})
	err := cmd.Execute()
	if err == nil || err.Error() != want.Error() {
		t.Errorf("err = %v, want the no-repo error %q", err, want)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q", stdout)
	}
}

// A profile typed as "" is a name the TypeScript refused, not a request for
// the default.
func TestUnpackRefusesAnEmptyProfileName(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{})
	sb.Files(map[string]string{"home/.home/manifests/profiles/workstation.yml": "steps:\n  - run: echo ran > ran.txt\n"})
	cmd := newUnpackCmd(sb.Ctx)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs([]string{""})
	err := cmd.Execute()
	want := "Which profile? Pass one (homebot unpack workstation) or set profiles.default in homebot.yml."
	var ue *fail.UserError
	if !errors.As(err, &ue) || ue.Msg != want {
		t.Errorf("err = %v, want %q", err, want)
	}
	if sb.Stdout.Len() != 0 {
		t.Errorf("stdout = %q", sb.Stdout)
	}
}
