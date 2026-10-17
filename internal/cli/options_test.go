package cli

import (
	"testing"

	"github.com/spf13/pflag"

	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// What node:util's parseArgs threw in Bun 1.4.2, the runtime the parity
// scripts were recorded with, for these options:
//
//	all: boolean, short a; message: string, short m; dry-run: boolean;
//	from: string
//
// Recorded by running parseArgs({args, options, allowPositionals: true,
// strict: true}) on each line's args; "" is no error.
func TestCheckOptionsSaysWhatParseArgsSaid(t *testing.T) {
	specs := []optionSpec{
		{name: "all", short: "a"},
		{name: "message", short: "m", value: true},
		{name: "dry-run"},
		{name: "from", value: true},
	}
	c := testutil.Sandbox(t, testutil.Options{}).Ctx
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{}, ""},
		{[]string{"x", "y"}, ""},
		{[]string{"--all"}, ""},
		{[]string{"-a"}, ""},
		{[]string{"--nope"}, "Unknown option '--nope'. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"--nope\""},
		{[]string{"-x"}, "Unknown option '-x'. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"-x\""},
		{[]string{"x", "--nope", "--message"}, "Unknown option '--nope'. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"--nope\""},
		{[]string{"--", "--nope"}, ""},
		{[]string{"--all", "--", "-x"}, ""},
		{[]string{"--message"}, "Option '-m, --message <value>' argument missing"},
		{[]string{"-m"}, "Option '-m, --message <value>' argument missing"},
		{[]string{"--from"}, "Option '--from <value>' argument missing"},
		{[]string{"--message", "hi"}, ""},
		{[]string{"--message=hi"}, ""},
		{[]string{"-mhi"}, ""},
		{[]string{"-m", "hi"}, ""},
		{[]string{"-m=hi"}, ""},
		{[]string{"--message", "--all"}, "Option '--message' argument is ambiguous.\nDid you forget to specify the option argument for '--message'?\nTo specify an option argument starting with a dash use '--message=-XYZ'."},
		{[]string{"-m", "--all"}, "Option '-m' argument is ambiguous.\nDid you forget to specify the option argument for '-m'?\nTo specify an option argument starting with a dash use '--message=-XYZ' or '-m-XYZ'."},
		{[]string{"--message", "-"}, ""},
		{[]string{"--message", "-1"}, "Option '--message' argument is ambiguous.\nDid you forget to specify the option argument for '--message'?\nTo specify an option argument starting with a dash use '--message=-XYZ'."},
		{[]string{"--from", "-2"}, "Option '--from' argument is ambiguous.\nDid you forget to specify the option argument for '--from'?\nTo specify an option argument starting with a dash use '--from=-XYZ'."},
		{[]string{"-am", "-1"}, "Option 'm' argument is ambiguous.\nDid you forget to specify the option argument for 'm'?\nTo specify an option argument starting with a dash use '--message=-XYZ' or 'm-XYZ'."},
		{[]string{"-am", "--x"}, "Option 'm' argument is ambiguous.\nDid you forget to specify the option argument for 'm'?\nTo specify an option argument starting with a dash use '--message=-XYZ' or 'm-XYZ'."},
		{[]string{"--message=-1"}, ""},
		{[]string{"-m-1"}, ""},
		{[]string{"--all=1"}, "Option '-a, --all' does not take an argument"},
		{[]string{"--all="}, "Option '-a, --all' does not take an argument"},
		{[]string{"--dry-run=false"}, "Option '--dry-run' does not take an argument"},
		{[]string{"-a=1"}, "Unknown option '='. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"=\""},
		{[]string{"-am"}, "Option '-m, --message <value>' argument missing"},
		{[]string{"-am", "v"}, ""},
		{[]string{"-ax"}, "Unknown option 'x'. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"x\""},
		{[]string{"-xa"}, "Unknown option 'x'. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"x\""},
		{[]string{"-xm"}, "Unknown option 'x'. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"x\""},
		{[]string{"-mx"}, ""},
		{[]string{"-ma=1"}, ""},
		{[]string{"-aa=1"}, "Unknown option '='. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"=\""},
		{[]string{"-ab", "-c"}, "Unknown option 'b'. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"b\""},
		{[]string{"-a", "-h"}, "Unknown option '-h'. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"-h\""},
		{[]string{"--x\"y\\z"}, "Unknown option '--x\"y\\z'. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"--x\"y\\z\""},
		{[]string{"--dry-run=", "--nope"}, "Option '--dry-run' does not take an argument"},
		{[]string{"--=x"}, "Unknown option '--=x'. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"--=x\""},
		{[]string{"--a=b"}, "Unknown option '--a'. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"--a\""},
		{[]string{"-"}, ""},
		{[]string{"--"}, ""},
		{[]string{"-é"}, "Unknown option '-é'. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"-é\""},
		{[]string{"--é"}, "Unknown option '--é'. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"--é\""},
		{[]string{"--message", "a", "--message", "b"}, ""},
		{[]string{"--from", "1", "--nope"}, "Unknown option '--nope'. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"--nope\""},
		{[]string{"--nope", "--from"}, "Unknown option '--nope'. To specify a positional argument starting with a '-', place it at the end of the command after '--', as in '-- \"--nope\""},
		{[]string{"--from", "--", "--nope"}, "Option '--from' argument is ambiguous.\nDid you forget to specify the option argument for '--from'?\nTo specify an option argument starting with a dash use '--from=-XYZ'."},
	} {
		got := ""
		if text := checkOptions(tc.args, specs); text.ID() != "" {
			got = voice.Plain(c, text)
		}
		if got != tc.want {
			t.Errorf("checkOptions(%q)\n got %q\nwant %q", tc.args, got, tc.want)
		}
	}
}

func TestOptionsOfACommandAreItsFlags(t *testing.T) {
	cmd := fake("save", func(f *pflag.FlagSet) {
		f.StringP("message", "m", "", "")
		f.Bool("no-push", false, "")
	}, nil)(nil)
	cmd.InitDefaultHelpFlag()
	got := optionsOf(cmd)
	want := []optionSpec{{name: "message", short: "m", value: true}, {name: "no-push"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("optionsOf = %+v, want %+v (help is not an option)", got, want)
	}
}
