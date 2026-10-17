package linker

import (
	"regexp"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

type nameCase struct {
	raw  string
	top  bool
	want Name
}

func checkNames(t *testing.T, cases []nameCase) {
	t.Helper()
	for _, c := range cases {
		got, ok := ParseName(c.raw, c.top)
		if !ok {
			t.Errorf("ParseName(%q, %v) is ignored", c.raw, c.top)
			continue
		}
		if d := cmp.Diff(c.want, got); d != "" {
			t.Errorf("ParseName(%q, %v) (-want +got):\n%s", c.raw, c.top, d)
		}
	}
}

func TestTopLevelNamesGainADotDeeperNamesAreLiteral(t *testing.T) {
	checkNames(t, []nameCase{
		{"config", true, Name{Home: ".config"}},
		{"zshrc", true, Name{Home: ".zshrc"}},
		{".zshrc", false, Name{Home: ".zshrc"}},
		{"nvim", false, Name{Home: "nvim"}},
	})
}

func TestLibraryKeepsItsNameAtTheTop(t *testing.T) {
	checkNames(t, []nameCase{
		{"Library", true, Name{Home: "Library"}},
		{"(Library)", true, Name{Home: "Library", Marked: true}},
		{"Library", false, Name{Home: "Library"}},
	})
}

func TestMarkedNameIsDroppedFromTheHomeName(t *testing.T) {
	checkNames(t, []nameCase{
		{"(claude)", true, Name{Home: ".claude", Marked: true}},
		{"(sessions)", false, Name{Home: "sessions", Marked: true}},
		{"(com.system76.Thing)", false, Name{Home: "com.system76.Thing", Marked: true}},
	})
}

func TestDotNamesAtTheTopOfALayerAreIgnored(t *testing.T) {
	for _, raw := range []string{".autolink.yml", ".hidden"} {
		if got, ok := ParseName(raw, true); ok {
			t.Errorf("ParseName(%q, true) = %+v, want ignored", raw, got)
		}
	}
}

func TestParenthesesThatAreNotAWholeNameMarkerAreLiteral(t *testing.T) {
	checkNames(t, []nameCase{
		{"file(1).txt", false, Name{Home: "file(1).txt"}},
		{"(draft", false, Name{Home: "(draft"}},
		{"draft)", false, Name{Home: "draft)"}},
	})
}

func TestDotPrefixIsOnlySpecialAtTheTop(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	got, ok := ParseName("dot-config", true)
	if !ok || got.Problem == nil {
		t.Fatalf("ParseName(dot-config, true) = %+v, %v; want a problem", got, ok)
	}
	if p := voice.Plain(b.Ctx, *got.Problem); !regexp.MustCompile(`rename it to "config"`).MatchString(p) {
		t.Errorf("problem %q does not say to rename it to config", p)
	}
	checkNames(t, []nameCase{{"dot-graph.svg", false, Name{Home: "dot-graph.svg"}}})
}

func TestParseNameRejects(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	cases := []struct {
		raw     string
		top     bool
		message string
	}{
		{"()", false, `no name`},
		{"((a))", false, `one pair of \(\), like "\(a\)"`},
		{"(a))", false, `one pair`},
		{"((a)", false, `one pair`},
		{"(.foo)", true, `rename it to "\(foo\)"`},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			got, ok := ParseName(c.raw, c.top)
			if !ok || got.Problem == nil {
				t.Fatalf("ParseName(%q, %v) = %+v, %v; want a problem", c.raw, c.top, got, ok)
			}
			if p := voice.Plain(b.Ctx, *got.Problem); !regexp.MustCompile(c.message).MatchString(p) {
				t.Errorf("problem %q does not match %s", p, c.message)
			}
		})
	}
}

// The exact wording of every name problem, as the TypeScript wrote it.
func TestParseNameProblemText(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	cases := []struct {
		raw  string
		top  bool
		want string
	}{
		{"dot-config", true, `"dot-config" is the old layout: rename it to "config"`},
		{"()", true, `"()" has no name inside`},
		{"((a))", false, `"((a))": wrap the name in one pair of (), like "(a)"`},
		{"(((a)))", true, `"(((a)))": wrap the name in one pair of (), like "(a)"`},
		{"(a))", false, `"(a))": wrap the name in one pair of (), like "(a)"`},
		{"(.foo)", true, `"(.foo)": top-level names get their dot from homebot; rename it to "(foo)"`},
	}
	for _, c := range cases {
		got, _ := ParseName(c.raw, c.top)
		if got.Problem == nil {
			t.Errorf("ParseName(%q, %v) has no problem", c.raw, c.top)
			continue
		}
		if p := voice.Plain(b.Ctx, *got.Problem); p != c.want {
			t.Errorf("ParseName(%q, %v) problem = %q, want %q", c.raw, c.top, p, c.want)
		}
	}
	// Deeper down, (.foo) is a fine name and dot- is literal.
	checkNames(t, []nameCase{
		{"(.foo)", false, Name{Home: ".foo", Marked: true}},
		{"dot-config", false, Name{Home: "dot-config"}},
		{"(", false, Name{Home: "("}},
		{")", false, Name{Home: ")"}},
	})
}

// (.) and (..) are no names: the TypeScript read (..) below the top of a
// layer as the folder above, so autolink/common/config/(..)/zshrc linked
// ~/zshrc, and enough of them reached above $HOME. They are refused at every
// level, with their own reason.
func TestDotAndDotDotInParenthesesAreRefusedEverywhere(t *testing.T) {
	b := testutil.Sandbox(t, testutil.Options{})
	for _, c := range []struct {
		raw string
		top bool
	}{{"(..)", false}, {"(.)", false}, {"(..)", true}, {"(.)", true}} {
		got, ok := ParseName(c.raw, c.top)
		if !ok || got.Problem == nil {
			t.Errorf("ParseName(%q, %v) = %+v, %v; want a problem", c.raw, c.top, got, ok)
			continue
		}
		want := `"` + c.raw + `" names no folder: . and .. would link outside the folder it is in; rename it`
		if p := voice.Plain(b.Ctx, *got.Problem); p != want {
			t.Errorf("ParseName(%q, %v) problem = %q, want %q", c.raw, c.top, p, want)
		}
	}
	// Names that only start or end with dots are fine below the top.
	checkNames(t, []nameCase{
		{"(...)", false, Name{Home: "...", Marked: true}},
		{"(..foo)", false, Name{Home: "..foo", Marked: true}},
	})
}
