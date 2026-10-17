package triage

import (
	"os"
	"regexp"
	"slices"
	"testing"
)

const decisionsHeader = "# Written by `homebot triage`; editing by hand is fine.\n# Paths (globs allowed) that homebot will never offer to track again.\n"

func TestLoadIgnoresWithoutAFileIsNothing(t *testing.T) {
	b := newBox(t, false)
	if got := b.ignores(); got != nil {
		t.Errorf("LoadIgnores = %q, want nil", got)
	}
}

func TestLoadIgnoresReadsTheList(t *testing.T) {
	cases := map[string][]string{
		"empty file":         nil,
		"only comments":      nil,
		"no ignore key":      nil,
		"null ignore":        nil,
		"empty list":         {},
		"block list":         {"~/.a", "~/name with: colon", "~/.config/*"},
		"flow list":          {"~/.a", "~/.b"},
		"quoted and literal": {"yes", "123", "~/x"},
	}
	files := map[string]string{
		"empty file":         "",
		"only comments":      "# nothing yet\n",
		"no ignore key":      "other: 1\n",
		"null ignore":        "ignore:\n",
		"empty list":         "ignore: []\n",
		"block list":         decisionsHeader + "ignore:\n  - ~/.a\n  - \"~/name with: colon\"\n  - ~/.config/*\n",
		"flow list":          "ignore: [~/.a, ~/.b]\n",
		"quoted and literal": "ignore:\n  - yes\n  - \"123\"\n  - |-\n    ~/x\n",
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			b := newBox(t, false)
			b.repo(map[string]string{"decisions.yml": files[name]})
			if got := b.ignores(); !slices.Equal(got, want) {
				t.Errorf("LoadIgnores = %q, want %q", got, want)
			}
		})
	}
}

func TestLoadIgnoresRefusesWhatIsNotAListOfPaths(t *testing.T) {
	for name, text := range map[string]string{
		"a string": "ignore: ~/.bashrc\n",
		"a map":    "ignore:\n  a: b\n",
		"a number": "ignore:\n  - ~/.a\n  - 12\n",
		"a bool":   "ignore:\n  - true\n",
		"a null":   "ignore:\n  - ~\n",
		"a list":   "ignore:\n  - [a]\n",
	} {
		t.Run(name, func(t *testing.T) {
			b := newBox(t, false)
			b.repo(map[string]string{"decisions.yml": text})
			_, err := LoadIgnores(b.Ctx)
			wantUserError(t, err, `^`+regexp.QuoteMeta(b.r("decisions.yml"))+`: ignore must be a list of paths$`)
		})
	}
}

func TestLoadIgnoresNamesWhereTheYAMLIsBroken(t *testing.T) {
	b := newBox(t, false)
	b.repo(map[string]string{"decisions.yml": "ignore:\n  - ~/.a\n - [broken\n"})
	_, err := LoadIgnores(b.Ctx)
	wantUserError(t, err, `^`+regexp.QuoteMeta(b.r("decisions.yml"))+`:\d+:\d+: invalid YAML: `)
}

func TestLoadIgnoresRefusesTheOldFileName(t *testing.T) {
	b := newBox(t, false)
	b.repo(map[string]string{"decisions.yaml": "ignore: []\n"})
	_, err := LoadIgnores(b.Ctx)
	wantUserError(t, err, `^homebot reads decisions\.yml now: rename `+regexp.QuoteMeta(b.r("decisions.yaml"))+` to decisions\.yml$`)
	// Once decisions.yml exists, the old file is not looked at.
	b.repo(map[string]string{"decisions.yml": "ignore: [~/.a]\n"})
	if got := b.ignores(); !slices.Equal(got, []string{"~/.a"}) {
		t.Errorf("LoadIgnores = %q, want ~/.a", got)
	}
}

func TestAddIgnoreWritesANewFileAsTheTypeScriptDid(t *testing.T) {
	b := newBox(t, false)
	if err := AddIgnore(b.Ctx, "~/.config/foo"); err != nil {
		t.Fatal(err)
	}
	want := decisionsHeader + "ignore:\n  - ~/.config/foo\n"
	if got := b.read(b.r("decisions.yml")); got != want {
		t.Errorf("decisions.yml =\n%s\nwant\n%s", got, want)
	}
}

func TestAddIgnoreQuotesWhatIsNotAPlainPath(t *testing.T) {
	b := newBox(t, false)
	if err := AddIgnore(b.Ctx, "~/name with: colon"); err != nil {
		t.Fatal(err)
	}
	want := decisionsHeader + "ignore:\n  - \"~/name with: colon\"\n"
	if got := b.read(b.r("decisions.yml")); got != want {
		t.Errorf("decisions.yml =\n%s\nwant\n%s", got, want)
	}
	if got := b.ignores(); !slices.Equal(got, []string{"~/name with: colon"}) {
		t.Errorf("read back %q", got)
	}
}

// deliberate: the TypeScript rewrote the file sorted, dropping the person's
// comments; homebot now appends, so the order is the order of decisions.
func TestAddIgnoreAppendsInOrderWithoutDuplicates(t *testing.T) {
	b := newBox(t, false)
	for _, p := range []string{"~/.b", "~/.a", "~/.b", "~/name with: colon", "~/.a"} {
		if err := AddIgnore(b.Ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := b.ignores(), []string{"~/.b", "~/.a", "~/name with: colon"}; !slices.Equal(got, want) {
		t.Errorf("LoadIgnores = %q, want %q", got, want)
	}
	want := decisionsHeader + "ignore:\n  - ~/.b\n  - ~/.a\n  - \"~/name with: colon\"\n"
	if got := b.read(b.r("decisions.yml")); got != want {
		t.Errorf("decisions.yml =\n%s\nwant\n%s", got, want)
	}
}

func TestAddIgnoreKeepsWhatThePersonWrote(t *testing.T) {
	b := newBox(t, false)
	before := "# my notes\nignore:\n  # browsers\n  - '~/.config/google-chrome*'   # all channels\n  - ~/.zcompdump*\n\n# the end\n"
	b.repo(map[string]string{"decisions.yml": before})
	if err := AddIgnore(b.Ctx, "~/.cache"); err != nil {
		t.Fatal(err)
	}
	want := "# my notes\nignore:\n  # browsers\n  - '~/.config/google-chrome*'   # all channels\n  - ~/.zcompdump*\n  - ~/.cache\n\n# the end\n"
	if got := b.read(b.r("decisions.yml")); got != want {
		t.Errorf("decisions.yml =\n%s\nwant\n%s", got, want)
	}
}

func TestAddIgnoreChangesNothingForAPathAlreadyListed(t *testing.T) {
	b := newBox(t, false)
	before := "ignore: [ '~/.a' ]   # kept as is\n"
	b.repo(map[string]string{"decisions.yml": before})
	if err := AddIgnore(b.Ctx, "~/.a"); err != nil {
		t.Fatal(err)
	}
	if got := b.read(b.r("decisions.yml")); got != before {
		t.Errorf("decisions.yml = %q, want it untouched", got)
	}
}

func TestAddIgnoreFillsAFileWithoutAList(t *testing.T) {
	for name, before := range map[string]string{
		"empty":         "",
		"only comments": "# hand-written\n",
		"null":          "ignore:\n",
		"flow":          "ignore: [~/.a]\n",
	} {
		t.Run(name, func(t *testing.T) {
			b := newBox(t, false)
			b.repo(map[string]string{"decisions.yml": before})
			if err := AddIgnore(b.Ctx, "~/.new"); err != nil {
				t.Fatal(err)
			}
			got := b.ignores()
			if len(got) == 0 || got[len(got)-1] != "~/.new" {
				t.Errorf("LoadIgnores = %q, want ~/.new last", got)
			}
		})
	}
}

func TestAddIgnoreRefusesABrokenFile(t *testing.T) {
	b := newBox(t, false)
	before := "ignore: ~/.a\n"
	b.repo(map[string]string{"decisions.yml": before})
	wantUserError(t, AddIgnore(b.Ctx, "~/.b"), `ignore must be a list of paths$`)
	if got := b.read(b.r("decisions.yml")); got != before {
		t.Errorf("decisions.yml = %q, want it untouched", got)
	}
}

func TestYAMLString(t *testing.T) {
	cases := map[string]string{
		"~/.config/foo":      "~/.config/foo",
		"~/.config/google*":  "~/.config/google*",
		"~/a b":              "~/a b",
		"~/my-app_1.0+x@y":   "~/my-app_1.0+x@y",
		"~/name with: colon": `"~/name with: colon"`,
		"~/a#b":              `"~/a#b"`,
		"~/quote\"d":         `"~/quote\"d"`,
		"~/tab\there":        `"~/tab\there"`,
		"~/café":             `"~/café"`,
		"~/<html>&":          `"~/<html>&"`,
		"~/trailing ":        `"~/trailing "`,
		" ~/leading":         `" ~/leading"`,
		"":                   `""`,
	}
	for in, want := range cases {
		if got := yamlString(in); got != want {
			t.Errorf("yamlString(%q) = %s, want %s", in, got, want)
		}
	}
}

// TestGlobMatchAsBunGlob pins globMatch to what Bun.Glob answered for each
// pair: new Bun.Glob(pattern).match(path), run by bun 1.4.2 in the test
// image. The first 39 rows were recorded from a script file, the rest with
// bun -e. A row with a deliberate reason is where Go answers the opposite on
// purpose.
func TestGlobMatchAsBunGlob(t *testing.T) {
	cases := []struct {
		pattern, path string
		bun           bool
		deliberate    string
	}{
		{"~/.zcompdump*", "~/.zcompdump-forge-5.9", true, ""},
		{"~/.config/google-chrome*", "~/.config/google-chrome-beta", true, ""},
		{"~/.zcompdump*", "~/.zshrc", false, ""},
		{"~/*", "~/.bashrc", true, ""},
		{"~/*", "~/visible", true, ""},
		{"~/.*", "~/.bashrc", true, ""},
		{"~/*", "~/.config/foo", false, ""},
		{"~/.config/*", "~/.config/.hidden", true, ""},
		{"~/**", "~/.config/foo", true, ""},
		{"~/**/foo", "~/.config/foo", true, ""},
		{"~/**/foo", "~/foo", true, ""},
		{"~/.config/**", "~/.config", false, ""},
		{"~/.config/**", "~/.config/a/b", true, ""},
		{"~/.{bashrc,zshrc}", "~/.zshrc", true, ""},
		{"~/.{a,{b,c}}rc", "~/.crc", true, ""},
		{"~/.config/{a,b/c}", "~/.config/b/c", true, ""},
		{"~/.[bz]shrc", "~/.zshrc", true, ""},
		{"~/.[!b]shrc", "~/.zshrc", true, ""},
		{"~/.[!z]shrc", "~/.zshrc", false, ""},
		{"~/.[^z]shrc", "~/.zshrc", false, ""},
		{"~/.ssh/[a-c]*", "~/.ssh/bfoo", true, ""},
		{"~/.config/?pp", "~/.config/app", true, ""},
		{"~/.config/?pp", "~/.config/pp", false, ""},
		{"~/.config/app\\*", "~/.config/app*", true, ""},
		{"~/.config/app\\*", "~/.config/appx", false, ""},
		{"~/.config/*/x", "~/.config/a/b/x", false, ""},
		{"*", "~/.bashrc", false, ""},
		{"**", "~/.bashrc", true, ""},
		{"~/.local*", "~/.local", true, ""},
		{"~/**/.git", "~/.git", true, ""},
		{"~/[.]bashrc", "~/.bashrc", true, ""},
		{"~/.config/", "~/.config", false, ""},
		{"~/.config", "~/.config/", false, ""},
		{"~/.[", "~/.[", false, ""},
		{"~/.{a,b", "~/.{a,b", false, ""},
		{"~/name with: colon", "~/name with: colon", true, ""},
		{"~/.Config/*", "~/.config/x", false, ""},
		{"~/.config/*", "~/.config/x/", false, ""},
		{"~/.cache/**/*", "~/.cache/a", true, ""},
		// Recorded for review round 1.
		{"!~/.foo", "~/.bar", true, "a leading ! is literal: in the TypeScript one !x entry ignored everything else"},
		{"!~/.foo", "~/.foo", false, ""},
		{"!~/.foo", "!~/.foo", true, ""},
		{"~/.[]a]x", "~/.]x", true, ""},
		{"~/.[]a]x", "~/.ax", true, ""},
		{"~/.[]a]x", "~/.bx", false, ""},
		{"~/.[!]a]x", "~/.]x", false, ""},
		{"~/.[!]a]x", "~/.bx", true, ""},
		{"~/.[^]]x", "~/.bx", true, ""},
		{"~/.[a]]x", "~/.a]x", true, ""},
		{"~/.[]]x", "~/.]x", true, ""},
		{"~/.cache/**/", "~/.cache", false, ""},
		{"~/.cache/**/", "~/.cache/a", false, ""},
		{"~/.cache/**/", "~/.cache/a/", true, ""},
		{"~/.cache/**/", "~/.cache/", true, ""},
		{"~/**", "~/", true, ""},
		{"~/**", "~", false, ""},
		{"~/.config/**", "~/.config/", true, ""},
		{"~/a/**", "~/a", false, ""},
		{"~/a/**/**", "~/a", false, ""},
		{"~/a/**/*", "~/a", false, ""},
		{"~/{a,b}/**", "~/a", false, ""},
		{"~/a*/**", "~/ab", false, ""},
		{"~/a/**", "~/a/b/c", true, ""},
		// Recorded for review round 2.
		{"~/**/**", "~/.config/foo", true, ""},
		{"~/.config/**/**", "~/.config/foo", true, ""},
		{"~/a/**/**", "~/a/b", true, ""},
		{"~/**/*.d/**", "~/a.d/b.d", true, ""},
		{"~/**/x/**", "~/x/x", true, ""},
	}
	for _, tc := range cases {
		want := tc.bun != (tc.deliberate != "")
		if got := globMatch(tc.pattern, tc.path); got != want {
			t.Errorf("globMatch(%q, %q) = %v, want %v (Bun.Glob said %v; %s)", tc.pattern, tc.path, got, want, tc.bun, tc.deliberate)
		}
	}
}

func TestIsIgnored(t *testing.T) {
	// From the TypeScript's "ignore entries can be globs".
	if !isIgnored("~/.zcompdump-forge-5.9", []string{"~/.zcompdump*"}) {
		t.Error("~/.zcompdump* does not ignore ~/.zcompdump-forge-5.9")
	}
	if !isIgnored("~/.config/google-chrome-beta", []string{"~/.config/google-chrome*"}) {
		t.Error("~/.config/google-chrome* does not ignore ~/.config/google-chrome-beta")
	}
	if isIgnored("~/.zshrc", []string{"~/.zcompdump*"}) {
		t.Error("~/.zcompdump* ignores ~/.zshrc")
	}
	// A pattern that is not a valid glob still ignores itself, as written.
	if !isIgnored("~/.[", []string{"~/.["}) {
		t.Error("~/.[ does not ignore itself")
	}
	if isIgnored("~/.a", nil) {
		t.Error("nothing ignores ~/.a")
	}
}

func TestDecisionsFileIsLeftAloneWhenUnreadable(t *testing.T) {
	notRoot(t)
	b := newBox(t, false)
	b.repo(map[string]string{"decisions.yml": "ignore: []\n"})
	if err := os.Chmod(b.r("decisions.yml"), 0o000); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadIgnores(b.Ctx); err == nil {
		t.Error("LoadIgnores read an unreadable file")
	}
}
