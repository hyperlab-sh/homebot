package profile

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func parse(t *testing.T, text string) *Profile {
	t.Helper()
	p, err := Parse(text, "p.yml", "p")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustList(t *testing.T, dir string) []string {
	t.Helper()
	names, err := listProfilesIn(dir)
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func wantError(t *testing.T, err error, pattern string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want one matching %q", pattern)
	}
	if !regexp.MustCompile(pattern).MatchString(err.Error()) {
		t.Fatalf("error %q does not match %q", err, pattern)
	}
}

// TS: parseProfileYaml > reads the documented shapes
func TestParseReadsTheDocumentedShapes(t *testing.T) {
	p := parse(t, `
description: Desk
include: base
steps:
  - brew: "{os}/Brewfile"
  - fonts: [JetBrainsMono, FiraCode]
  - apt: linux/apt.txt
    on: linux
  - unlock
  - link:
  - run: echo hi
    name: Say hi
    on: [linux, macos]
`)
	if p.Description != "Desk" {
		t.Errorf("description %q", p.Description)
	}
	if diff := cmp.Diff([]string{"base"}, p.Include); diff != "" {
		t.Errorf("include: %s", diff)
	}
	type row struct {
		Action string
		Args   []string
		On     []app.OS
	}
	var got []row
	for _, s := range p.Steps {
		got = append(got, row{s.Action, s.Args, s.On})
	}
	want := []row{
		{"brew", []string{"{os}/Brewfile"}, nil},
		{"fonts", []string{"JetBrainsMono", "FiraCode"}, nil},
		{"apt", []string{"linux/apt.txt"}, []app.OS{app.Linux}},
		{"unlock", []string{}, nil},
		{"link", []string{}, nil},
		{"run", []string{"echo hi"}, []app.OS{app.Linux, app.MacOS}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("steps (-want +got):\n%s", diff)
	}
	if got := DescribeStep(p.Steps[5]); got != "Say hi" {
		t.Errorf("named step described as %q", got)
	}
	if got := DescribeStep(p.Steps[1]); got != "fonts JetBrainsMono, FiraCode" {
		t.Errorf("step described as %q", got)
	}
	if got := DescribeStep(p.Steps[3]); got != "unlock" {
		t.Errorf("step without args described as %q", got)
	}
	if want := (StepSource{File: "p.yml", Index: 2}); p.Steps[2].Source != want {
		t.Errorf("source %+v, want %+v", p.Steps[2].Source, want)
	}
}

// TS: parseProfileYaml > turns numbers and booleans into strings
func TestParseTurnsNumbersAndBooleansIntoStrings(t *testing.T) {
	p := parse(t, "steps:\n  - run: [1, true]")
	if diff := cmp.Diff([]string{"1", "true"}, p.Steps[0].Args); diff != "" {
		t.Error(diff)
	}
}

// TS: parseProfileYaml > an empty file is an empty profile
func TestParseEmptyFileIsEmptyProfile(t *testing.T) {
	for _, text := range []string{"", "# only a comment\n", "~", "null\n"} {
		if p := parse(t, text); len(p.Steps) != 0 || len(p.Include) != 0 {
			t.Errorf("%q gave %+v", text, p)
		}
	}
}

// TS: parseProfileYaml > rejects %p (each)
func TestParseRejects(t *testing.T) {
	cases := []struct{ yaml, message string }{
		{"steps:\n  - brew: a\n    apt: b", `exactly one action, found brew, apt`},
		{"steps:\n  - on: linux", `exactly one action, found none`},
		{"steps:\n  - brew: a\n    on: windows", `unknown OS "windows"`},
		{"steps:\n  - brew: a\n    on: []", `"on" must be`},
		{"steps:\n  - brew: {file: x}", `arguments must be`},
		{"steps:\n  - Brew: x", `not a valid action name`},
		{"steps:\n  - 42", `expected "- action: arguments"`},
		{"steps: nope", `steps must be a list`},
		{"stpes: []", `unknown key "stpes"`},
		{"include: [1]", `include must be`},
		{"- a\n- b", `expected a mapping`},
		{"steps: [", `invalid YAML`},
		{"steps:\n  - brew: a\n    name: 3", `name must be text`},
	}
	for _, c := range cases {
		t.Run(c.yaml, func(t *testing.T) {
			_, err := Parse(c.yaml, "p.yml", "p")
			wantError(t, err, c.message)
		})
	}
}

// Cases beyond the TypeScript list; the whole message is pinned.
func TestParseRejectsWithExactMessages(t *testing.T) {
	cases := []struct{ yaml, message string }{
		{"include:\n", "p.yml: include must be a profile name or a list of them"},
		{"description: 3", "p.yml: description must be text"},
		{"steps:\n  - brew: a\n    on:\n", `p.yml step 1: "on" must be linux, macos, or a list of them`},
		{"steps:\n  - brew: a\n    on: 3", `p.yml step 1: "on" must be linux, macos, or a list of them`},
		{"steps:\n  - brew: a\n    on: [3]", `p.yml step 1: unknown OS "3" in "on" (use linux or macos)`},
		{"steps:\n  - \n", `p.yml step 1: expected "- action: arguments"`},
		{"steps:\n  - Unlock", `p.yml step 1: "Unlock" is not a valid action name`},
		{"steps:\n  - brew: [a, [b]]", "p.yml step 1: arguments must be a value or a list of values"},
		{"steps:\n  - brew: [a, ~]", "p.yml step 1: arguments must be a value or a list of values"},
		{"steps:\n  - brew: a\n    2: b", "p.yml step 1: each step needs exactly one action, found 2, brew"},
		{"b: 1\n2: x\n1: y\nsteps: []", `p.yml: unknown key "1" (expected description, include, steps)`},
		{"steps:\n  - brew: a\n---\nsteps:\n  - brew: b\n", `p.yml: expected a mapping with "steps:"`},
		{"a: 1\n---\n", `p.yml: expected a mapping with "steps:"`},
	}
	for _, c := range cases {
		t.Run(c.yaml, func(t *testing.T) {
			_, err := Parse(c.yaml, "p.yml", "p")
			if err == nil || err.Error() != c.message {
				t.Errorf("error %v, want %q", err, c.message)
			}
		})
	}
}

func TestParseAcceptsABOM(t *testing.T) {
	p := parse(t, "\uFEFFdescription: Desk\nsteps:\n  - unlock\n")
	if p.Description != "Desk" || len(p.Steps) != 1 {
		t.Errorf("%+v", p)
	}
}

// Anchors with no value are null; only the reported line may change.
func TestParseEmptyAnchorTouchesOnlyItself(t *testing.T) {
	cases := []struct {
		yaml string
		want [][]string
	}{
		{"steps:\n  - brew: &a\n      - x\n  - apt: &b\n", [][]string{{"x"}, {}}},
		{"steps:\n  - run: a - &b\n  - run: &c\n", [][]string{{"a - &b"}, {}}},
		{"steps:\n  - run: &c\n  - run: &d\n  - run: z\n", [][]string{{}, {}, {"z"}}},
	}
	for _, c := range cases {
		t.Run(c.yaml, func(t *testing.T) {
			p := parse(t, c.yaml)
			var got [][]string
			for _, s := range p.Steps {
				got = append(got, s.Args)
			}
			if diff := cmp.Diff(c.want, got); diff != "" {
				t.Error(diff)
			}
		})
	}
}

func TestListProfilesSurfacesReadErrors(t *testing.T) {
	file := filepath.Join(t.TempDir(), "notadir")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := listProfilesIn(file); err == nil {
		t.Error("a file where the directory should be listed fine")
	}
	_, err := Resolve(ResolveEnv{ProfilesDir: file, OS: app.Linux}, "x")
	if err == nil || strings.HasPrefix(err.Error(), "No profile named") {
		t.Errorf("error %v hides the read failure", err)
	}
}

// TS: parseProfileYaml > errors name the file and step
func TestParseErrorsNameTheFileAndStep(t *testing.T) {
	_, err := Parse("steps:\n  - brew: a\n  - brew: a\n    apt: b", "p.yml", "p")
	wantError(t, err, `^p\.yml step 2:`)
}

func TestParseDuplicateKeysLastWins(t *testing.T) {
	p := parse(t, "steps:\n  - brew: a\n    brew: b\n")
	if diff := cmp.Diff([]string{"b"}, p.Steps[0].Args); diff != "" {
		t.Error(diff)
	}
}

func TestParseAnchorsAndAliases(t *testing.T) {
	p := parse(t, "steps:\n  - fonts: &f [a, b]\n  - apt: *f\n")
	if diff := cmp.Diff([]string{"a", "b"}, p.Steps[1].Args); diff != "" {
		t.Error(diff)
	}
}

func TestDescribeStepNameBeatsArgs(t *testing.T) {
	if got := DescribeStep(Step{Action: "brew", Args: []string{"x"}, Name: "Packages"}); got != "Packages" {
		t.Error(got)
	}
}

type profiles map[string]string

func setup(t *testing.T, files profiles) (dir string, resolve func(name string, os app.OS) ([]Step, error)) {
	t.Helper()
	dir = t.TempDir()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, func(name string, osName app.OS) ([]Step, error) {
		return Resolve(ResolveEnv{ProfilesDir: dir, OS: osName, Host: "forge", Arch: "x64"}, name)
	}
}

func mustResolve(t *testing.T, resolve func(string, app.OS) ([]Step, error), name string, osName app.OS) []Step {
	t.Helper()
	steps, err := resolve(name, osName)
	if err != nil {
		t.Fatal(err)
	}
	return steps
}

// TS: resolveProfile > includes come first, each profile once, steps filtered by OS
func TestResolveIncludesFirstOnceFilteredByOS(t *testing.T) {
	_, resolve := setup(t, profiles{
		"base.yml": "steps:\n  - brew: common/Brewfile\n",
		"dev.yml":  "include: base\nsteps:\n  - run: dev\n",
		"desk.yml": "include: [base, dev]\nsteps:\n  - apt: x\n    on: linux\n  - brew: y\n    on: macos\n",
	})
	var got []string
	for _, s := range mustResolve(t, resolve, "desk", app.Linux) {
		got = append(got, s.Action+":"+strings.Join(s.Args, ","))
	}
	if diff := cmp.Diff([]string{"brew:common/Brewfile", "run:dev", "apt:x"}, got); diff != "" {
		t.Error(diff)
	}
	var actions []string
	for _, s := range mustResolve(t, resolve, "desk", app.MacOS) {
		actions = append(actions, s.Action)
	}
	if diff := cmp.Diff([]string{"brew", "run", "brew"}, actions); diff != "" {
		t.Error(diff)
	}
}

// TS: resolveProfile > fills in {os}, {host} and {arch}
func TestResolveFillsInVariables(t *testing.T) {
	_, resolve := setup(t, profiles{"a.yml": "steps:\n  - brew: \"{os}/{host}/{arch}\"\n"})
	if got := mustResolve(t, resolve, "a", app.Linux)[0].Args; !cmp.Equal(got, []string{"linux/forge/x64"}) {
		t.Error(got)
	}
}

// TS: resolveProfile > unknown variables fail with the step location
func TestResolveUnknownVariableNamesTheStep(t *testing.T) {
	dir, resolve := setup(t, profiles{"a.yml": "steps:\n  - brew: \"{user}\"\n"})
	_, err := resolve("a", app.Linux)
	want := filepath.Join(dir, "a.yml") + " step 1: unknown variable {user} (available: {os}, {host}, {arch})"
	if err == nil || err.Error() != want {
		t.Errorf("error %v, want %q", err, want)
	}
}

// TS: resolveProfile > include loops are reported with the chain
func TestResolveIncludeLoopReportsChain(t *testing.T) {
	_, resolve := setup(t, profiles{"a.yml": "include: b\n", "b.yml": "include: c\n", "c.yml": "include: a\n"})
	_, err := resolve("a", app.Linux)
	wantError(t, err, `Profiles include each other in a loop: a -> b -> c -> a$`)
}

// TS: resolveProfile > a profile including itself is a loop
func TestResolveSelfIncludeIsALoop(t *testing.T) {
	_, resolve := setup(t, profiles{"a.yml": "include: a\n"})
	_, err := resolve("a", app.Linux)
	wantError(t, err, `a -> a$`)
}

// TS: resolveProfile > missing profiles list what exists
func TestResolveMissingProfileListsWhatExists(t *testing.T) {
	dir, resolve := setup(t, profiles{"one.yml": "", "two.yml": ""})
	_, err := resolve("three", app.Linux)
	want := `No profile named "three" in ` + dir + `. Available: one, two`
	if err == nil || err.Error() != want {
		t.Errorf("error %v, want %q", err, want)
	}
	if diff := cmp.Diff([]string{"one", "two"}, mustList(t, dir)); diff != "" {
		t.Error(diff)
	}
}

func TestResolveMissingProfileInEmptyDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nope")
	_, err := Resolve(ResolveEnv{ProfilesDir: dir, OS: app.Linux}, "x")
	if want := `No profile named "x" in ` + dir + `.`; err == nil || err.Error() != want {
		t.Errorf("error %v, want %q", err, want)
	}
	if got := mustList(t, dir); len(got) != 0 {
		t.Errorf("listing a missing dir gave %v", got)
	}
}

// TS: resolveProfile > .yaml profiles are not read, and loading one says to rename it
func TestResolveYamlExtensionSaysRename(t *testing.T) {
	dir, resolve := setup(t, profiles{"a.yaml": "steps:\n  - unlock\n", "b.yml": ""})
	if diff := cmp.Diff([]string{"b"}, mustList(t, dir)); diff != "" {
		t.Error(diff)
	}
	_, err := resolve("a", app.Linux)
	wantError(t, err, `rename .*a\.yaml to a\.yml`)
}

// TS: resolveProfile > the hash changes when what runs changes, not when labels do
func TestStepsHashIgnoresLabels(t *testing.T) {
	dir, resolve := setup(t, profiles{"a.yml": "steps:\n  - brew: x\n"})
	write := func(text string) {
		if err := os.WriteFile(filepath.Join(dir, "a.yml"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before := StepsHash(mustResolve(t, resolve, "a", app.Linux))
	write("steps:\n  - brew: x\n    name: Packages\n")
	if StepsHash(mustResolve(t, resolve, "a", app.Linux)) != before {
		t.Error("a label changed the hash")
	}
	write("steps:\n  - brew: y\n")
	if StepsHash(mustResolve(t, resolve, "a", app.Linux)) == before {
		t.Error("an argument change kept the hash")
	}
}

func TestResolveDoesNotMutateTheProfile(t *testing.T) {
	_, resolve := setup(t, profiles{"a.yml": "steps:\n  - brew: \"{os}\"\n"})
	a := mustResolve(t, resolve, "a", app.Linux)
	b := mustResolve(t, resolve, "a", app.MacOS)
	if a[0].Args[0] != "linux" || b[0].Args[0] != "macos" {
		t.Errorf("%v %v", a[0].Args, b[0].Args)
	}
}

func TestInterpolate(t *testing.T) {
	vars := []Var{{"os", "linux"}, {"host", "h"}}
	got, err := Interpolate("{os}-{host}-{os} {not closed} $1 {}", vars, "x")
	if err != nil || got != "linux-h-linux {not closed} $1 {}" {
		t.Errorf("got %q, %v", got, err)
	}
	_, err = Interpolate("{os}{nope}{nah}", vars, "p.yml step 3")
	if err == nil || err.Error() != "p.yml step 3: unknown variable {nope} (available: {os}, {host})" {
		t.Errorf("error %v", err)
	}
}

func TestDefaultProfile(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	ctx := box.Ctx

	ctx.Config = nil
	if name, ok := DefaultProfile(ctx); ok || name != "" {
		t.Errorf("nil config gave %q, %v", name, ok)
	}

	cfg := config.Default()
	cfg.Profiles.Default = "workstation"
	ctx.Config = cfg
	if name, ok := DefaultProfile(ctx); !ok || name != "workstation" {
		t.Errorf("default gave %q, %v", name, ok)
	}

	cfg.Profiles.Hosts[ctx.Host] = "laptop"
	if name, ok := DefaultProfile(ctx); !ok || name != "laptop" {
		t.Errorf("host entry gave %q, %v", name, ok)
	}

	cfg.Profiles.Default = ""
	delete(cfg.Profiles.Hosts, ctx.Host)
	cfg.Profiles.Hosts["elsewhere"] = "x"
	if name, ok := DefaultProfile(ctx); ok || name != "" {
		t.Errorf("another host's entry gave %q, %v", name, ok)
	}
}

func TestResolveForAndListProfilesUseTheRepo(t *testing.T) {
	box := testutil.Sandbox(t, testutil.Options{})
	box.Files(map[string]string{
		"home/.home/manifests/profiles/b.yml": "steps:\n  - brew: \"{os}/{host}/{arch}\"\n",
		"home/.home/manifests/profiles/a.yml": "include: b\n",
		"home/.home/manifests/profiles/c.txt": "",
	})
	if diff := cmp.Diff([]string{"a", "b"}, ListProfiles(box.Ctx)); diff != "" {
		t.Error(diff)
	}
	steps, err := ResolveFor(box.Ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	want := "linux/" + testutil.TestHost + "/" + box.Ctx.Arch
	if len(steps) != 1 || steps[0].Args[0] != want {
		t.Errorf("steps %+v, want arg %q", steps, want)
	}
}

func TestJSNumberString(t *testing.T) {
	cases := map[float64]string{
		1: "1", -1: "-1", 0.5: "0.5", 1e21: "1e+21", 1e20: "100000000000000000000",
		1.5e-7: "1.5e-7", 1e-6: "0.000001", 123456789012345680000: "123456789012345680000",
		1.7976931348623157e308: "1.7976931348623157e+308", -1e21: "-1e+21", -1.5e-7: "-1.5e-7", -123.5: "-123.5", 5e-324: "5e-324",
	}
	for f, want := range cases {
		if got := jsNumberString(f); got != want {
			t.Errorf("jsNumberString(%v) = %q, want %q", f, got, want)
		}
	}
}

// Parse and Resolve have no Context, so their errors carry their message for
// the CLI to say in the Context's voice (fail.Say).
func TestErrorsCarryTheirMessage(t *testing.T) {
	said := func(t *testing.T, err error, id string) {
		t.Helper()
		var ue *fail.UserError
		if !errors.As(err, &ue) {
			t.Fatalf("error %v is not a *fail.UserError", err)
		}
		if text, ok := ue.Text(); !ok || text.ID() != id {
			t.Errorf("the error %q holds %s, want %s", err, text, id)
		}
	}
	for _, c := range []struct{ yaml, id string }{
		{"steps: [", "manifest.error.yaml"},
		{"- a\n", "manifest.error.not_mapping"},
		{"nope: 1\n", "manifest.error.unknown_key"},
		{"steps:\n  - on: linux\n", "manifest.error.one_action.none"},
		{"steps:\n  - {brew: a, apt: b}\n", "manifest.error.one_action"},
		{"steps:\n  - brew: a\n    on: bsd\n", "manifest.error.on_os"},
	} {
		_, err := Parse(c.yaml, "p.yml", "p")
		said(t, err, c.id)
	}
	_, resolve := setup(t, profiles{"a.yml": "include: a\n", "b.yml": "steps:\n  - brew: \"{user}\"\n", "c.yaml": ""})
	for name, id := range map[string]string{
		"a": "manifest.error.loop",
		"b": "manifest.error.unknown_variable",
		"c": "manifest.error.old_extension",
		"d": "manifest.error.no_profile.available",
	} {
		_, err := resolve(name, app.Linux)
		said(t, err, id)
	}
	_, err := Resolve(ResolveEnv{ProfilesDir: t.TempDir(), OS: app.Linux}, "x")
	said(t, err, "manifest.error.no_profile")
}
