package exports_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/language"

	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/exports"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/fsx"
	"github.com/hyperlab-sh/homebot/internal/persona"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// setup makes a sandbox whose homebot.yml holds one export named list.
func setup(t *testing.T, run, out string) *testutil.Box {
	t.Helper()
	sb := testutil.Sandbox(t, testutil.Options{Git: true})
	configure(t, sb, "exports:\n  - name: list\n    out: "+out+"\n    run: "+quote(run)+"\n")
	return sb
}

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// configure replaces homebot.yml and reloads it into the Context.
func configure(t *testing.T, sb *testutil.Box, yaml string) {
	t.Helper()
	sb.Files(map[string]string{"home/.home/homebot.yml": yaml})
	cfg, err := config.Load(sb.Root)
	if err != nil {
		t.Fatal(err)
	}
	sb.Ctx.Config = cfg
}

func read(t *testing.T, sb *testutil.Box, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(sb.Root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(sb *testutil.Box, rel string) bool {
	_, err := os.Lstat(filepath.Join(sb.Root, rel))
	return err == nil
}

func run(t *testing.T, sb *testutil.Box, opts exports.Options) []exports.Result {
	t.Helper()
	rs, err := exports.RunExports(sb.Ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func one(t *testing.T, sb *testutil.Box) exports.Result {
	t.Helper()
	rs := run(t, sb, exports.Options{})
	if len(rs) != 1 {
		t.Fatalf("want one result, got %+v", rs)
	}
	return rs[0]
}

func files(t *testing.T, sb *testutil.Box, rel string) []string {
	t.Helper()
	fs, err := fsx.WalkFiles(filepath.Join(sb.Root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func eq(t *testing.T, got, want any) {
	t.Helper()
	if !equal(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func equal(a, b any) bool {
	switch x := a.(type) {
	case []string:
		y, ok := b.([]string)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if x[i] != y[i] {
				return false
			}
		}
		return true
	}
	return a == b
}

func TestWritesStdoutToTheOutputFile(t *testing.T) {
	sb := setup(t, `printf 'a\nb\n'`, "manifests/{os}/list.txt")
	r := one(t, sb)
	eq(t, r.OK && r.Changed, true)
	eq(t, r.Out, "manifests/linux/list.txt")
	eq(t, read(t, sb, "manifests/linux/list.txt"), "a\nb\n")
}

func TestOutLetsTheCommandWriteTheFileItself(t *testing.T) {
	sb := setup(t, "echo from-cmd > {out}", "manifests/{os}/list.txt")
	run(t, sb, exports.Options{})
	eq(t, read(t, sb, "manifests/linux/list.txt"), "from-cmd\n")
}

func TestOutNeverWrittenFailsAndNamesTheFile(t *testing.T) {
	sb := setup(t, "echo to-stdout # {out}", "manifests/{os}/list.txt")
	sb.Files(map[string]string{"home/.home/manifests/linux/list.txt": "good\n"})
	r := one(t, sb)
	eq(t, r.OK, false)
	eq(t, r.Error, "the command did not write manifests/linux/list.txt, the file given to it as {out}")
	eq(t, read(t, sb, "manifests/linux/list.txt"), "good\n")
}

func TestNamesPickExportsAndUnknownNamesAreErrors(t *testing.T) {
	sb := setup(t, "echo a", "a.txt")
	configure(t, sb, "exports:\n  - name: a\n    out: a.txt\n    run: echo a\n  - name: b\n    out: b.txt\n    run: echo b\n  - name: mac\n    on: macos\n    out: m.txt\n    run: echo m\n")
	rs := run(t, sb, exports.Options{Names: []string{"b"}})
	eq(t, len(rs), 1)
	eq(t, rs[0].Name, "b")
	eq(t, exists(sb, "a.txt"), false)

	_, err := exports.RunExports(sb.Ctx, exports.Options{Names: []string{"--help"}})
	var ue *fail.UserError
	if !errors.As(err, &ue) || err.Error() != "No export named --help for this OS. Exports here: a, b." {
		t.Fatalf("got %v", err)
	}
	// Defined, but not for this OS: still unknown here, and nothing runs.
	_, err = exports.RunExports(sb.Ctx, exports.Options{Names: []string{"a", "mac"}})
	if err == nil || !strings.Contains(err.Error(), "No export named mac") {
		t.Fatalf("got %v", err)
	}
	eq(t, exists(sb, "a.txt"), false)

	_, err = exports.RunExports(sb.Ctx, exports.Options{Names: []string{"x", "y"}})
	if err == nil || err.Error() != "No export named x, y for this OS. Exports here: a, b." {
		t.Fatalf("got %v", err)
	}
}

func TestNoExportsHereNamesNone(t *testing.T) {
	sb := setup(t, "echo", "x")
	configure(t, sb, "profiles:\n  default: workstation\n")
	_, err := exports.RunExports(sb.Ctx, exports.Options{Names: []string{"anything"}})
	if err == nil || err.Error() != "No export named anything for this OS. Exports here: none." {
		t.Fatalf("got %v", err)
	}
}

func TestUnchangedOutputIsReportedAsUnchanged(t *testing.T) {
	sb := setup(t, "echo same", "manifests/{os}/list.txt")
	run(t, sb, exports.Options{})
	eq(t, one(t, sb).Changed, false)
}

func TestFailingCommandKeepsThePreviousFile(t *testing.T) {
	sb := setup(t, "echo broken >&2; exit 3", "manifests/{os}/list.txt")
	sb.Files(map[string]string{"home/.home/manifests/linux/list.txt": "good\n"})
	r := one(t, sb)
	eq(t, r.OK, false)
	if !strings.Contains(r.Error, "exited with 3: broken") {
		t.Fatalf("error %q", r.Error)
	}
	eq(t, read(t, sb, "manifests/linux/list.txt"), "good\n")
	st, err := state.Load(sb.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := st.Exports.Get("list")
	eq(t, ok && !got.OK, true)
	eq(t, got.Error, r.Error)
}

func TestFailureQuotesTheLastThreeStderrLines(t *testing.T) {
	sb := setup(t, "echo 1 >&2; echo 2 >&2; echo 3 >&2; echo 4 >&2; exit 4", "x.txt")
	eq(t, one(t, sb).Error, "exited with 4: 2 3 4")
}

func TestEmptyOutputNeverReplacesAGoodFile(t *testing.T) {
	sb := setup(t, "true", "manifests/{os}/list.txt")
	sb.Files(map[string]string{"home/.home/manifests/linux/list.txt": "good\n"})
	r := one(t, sb)
	eq(t, r.Error, "produced an empty file; kept the previous manifests/linux/list.txt")
	eq(t, read(t, sb, "manifests/linux/list.txt"), "good\n")
}

func TestEmptyOutputIsFineWhenThereIsNothingToKeep(t *testing.T) {
	sb := setup(t, "true", "manifests/{os}/list.txt")
	r := one(t, sb)
	eq(t, r.OK && r.Changed, true)
	eq(t, read(t, sb, "manifests/linux/list.txt"), "")
}

func TestDueOnlySkipsExportsThatRanRecently(t *testing.T) {
	sb := setup(t, "date +%N", "manifests/{os}/list.txt")
	run(t, sb, exports.Options{})
	eq(t, run(t, sb, exports.Options{DueOnly: true})[0].Skipped, true)
	later := sb.Ctx.Now().UnixMilli() + 21*3600_000
	eq(t, run(t, sb, exports.Options{DueOnly: true, Now: later})[0].Skipped, false)
}

func TestExportsForTheOtherOSDoNotRun(t *testing.T) {
	sb := setup(t, "echo", "x")
	configure(t, sb, "exports:\n  - name: mac\n    on: macos\n    out: x\n    run: echo\n")
	eq(t, len(run(t, sb, exports.Options{})), 0)
}

func TestMissingToolIsSkippedNotFailed(t *testing.T) {
	sb := setup(t, "definitely-not-a-tool --dump", "manifests/{os}/list.txt")
	sb.Files(map[string]string{"home/.home/manifests/linux/list.txt": "good\n"})
	if err := state.Update(sb.Ctx, func(s *state.State) {
		s.Exports.Set("list", state.ExportResult{At: 1, OK: false, Error: "old failure"})
	}); err != nil {
		t.Fatal(err)
	}
	r := one(t, sb)
	eq(t, r.OK && r.Skipped, true)
	eq(t, r.Reason, "definitely-not-a-tool is not installed")
	eq(t, read(t, sb, "manifests/linux/list.txt"), "good\n")
	st, err := state.Load(sb.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, ok := st.Exports.Get("list")
	eq(t, ok, false)
}

func TestMissingOutputIsNotCreatedForASkippedTool(t *testing.T) {
	sb := setup(t, "definitely-not-a-tool", "manifests/{os}/list.txt")
	run(t, sb, exports.Options{})
	eq(t, exists(sb, "manifests/linux/list.txt"), false)
}

func TestShellBuiltinsAreNotMissingTools(t *testing.T) {
	sb := setup(t, "exit 0", "x.txt")
	eq(t, one(t, sb).Skipped, false)
}

func TestProfilesFilterByDefaultProfile(t *testing.T) {
	yaml := func(extra string) string {
		return "profiles:\n  default: workstation\n" + extra + "exports:\n  - name: list\n    out: manifests/list.txt\n    run: echo hi\n    profiles: [workstation]\n"
	}
	sb := setup(t, "echo", "x")
	configure(t, sb, yaml(""))
	eq(t, len(run(t, sb, exports.Options{})), 1)
	configure(t, sb, yaml("  hosts: { testhost: container }\n"))
	eq(t, len(run(t, sb, exports.Options{})), 0)
}

func TestProfileFilterNeedsADefaultProfile(t *testing.T) {
	sb := setup(t, "echo", "x")
	configure(t, sb, "exports:\n  - name: list\n    out: x\n    run: echo\n    profiles: [workstation]\n  - name: all\n    out: y\n    run: echo\n")
	got := exports.ExportsFor(sb.Ctx)
	eq(t, len(got), 1)
	eq(t, got[0].Name, "all")
}

func TestNilConfigMeansNoExports(t *testing.T) {
	sb := setup(t, "echo", "x")
	sb.Ctx.Config = nil
	eq(t, len(exports.ExportsFor(sb.Ctx)), 0)
}

func TestHostInOutCreatesParentDirectories(t *testing.T) {
	sb := setup(t, "echo hi", "manifests/hosts/{host}/apt.txt")
	r := one(t, sb)
	eq(t, r.OK, true)
	eq(t, r.Out, "manifests/hosts/testhost/apt.txt")
	eq(t, read(t, sb, "manifests/hosts/testhost/apt.txt"), "hi\n")
}

func TestUnknownVariableInOutIsAnError(t *testing.T) {
	sb := setup(t, "echo hi", "manifests/{nope}/x")
	_, err := exports.RunExports(sb.Ctx, exports.Options{})
	if err == nil || !strings.Contains(err.Error(), "homebot.yml export list: unknown variable {nope}") {
		t.Fatalf("got %v", err)
	}
	eq(t, exports.ExportOutPath(sb.Ctx, sb.Ctx.Config.Exports[0]), filepath.Join(sb.Root, "manifests/{nope}/x"))
}

func TestExportOutPath(t *testing.T) {
	sb := setup(t, "echo", "manifests/{os}/{host}/{arch}.txt")
	got := exports.ExportOutPath(sb.Ctx, sb.Ctx.Config.Exports[0])
	eq(t, got, filepath.Join(sb.Root, "manifests", "linux", "testhost", sb.Ctx.Arch+".txt"))
}

func TestLeavesNoTempFilesBehind(t *testing.T) {
	sb := setup(t, "exit 1", "manifests/{os}/list.txt")
	run(t, sb, exports.Options{})
	eq(t, exists(sb, "manifests/linux"), true)
	eq(t, len(files(t, sb, "manifests/linux")), 0)
	out := sb.GitIn(sb.Root, "status", "--porcelain", "--untracked-files=all")
	eq(t, strings.Contains(out, ".tmp-") || strings.Contains(out, ".homebot-export-"), false)
}

func TestStateRecordsOutcomes(t *testing.T) {
	sb := setup(t, "echo ok", "x.txt")
	run(t, sb, exports.Options{})
	st, err := state.Load(sb.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := st.Exports.Get("list")
	eq(t, got.OK, true)
	eq(t, got.At, testutil.TestNow)
}

// Per profile: exports "per profile".

const perProfile = `profiles:
  default: workstation
  hosts: { testhost: server }
exports:
  - name: apt
    on: linux
    profiles: [workstation]
    out: manifests/linux/apt.txt
    run: echo workstation
`

func TestTwoExportsMayShareANameWhenProfilesDoNotOverlap(t *testing.T) {
	sb := setup(t, "echo", "x")
	configure(t, sb, perProfile+"  - name: apt\n    profiles: [server]\n    out: manifests/server/apt.txt\n    run: echo server\n")
	specs := exports.ExportsFor(sb.Ctx)
	eq(t, len(specs), 1)
	eq(t, specs[0].Out, "manifests/server/apt.txt")
	rs := run(t, sb, exports.Options{Names: []string{"apt"}})
	eq(t, rs[0].OK, true)
	eq(t, rs[0].Out, "manifests/server/apt.txt")
	eq(t, read(t, sb, "manifests/server/apt.txt"), "server\n")
	eq(t, exists(sb, "manifests/linux/apt.txt"), false)
}

func TestSameNameOnDifferentSystemsLoads(t *testing.T) {
	sb := setup(t, "echo", "x")
	sb.Files(map[string]string{"home/.home/homebot.yml": perProfile + "  - name: apt\n    on: macos\n    out: manifests/macos/apt.txt\n    run: echo mac\n"})
	if _, err := config.Load(sb.Root); err != nil {
		t.Fatal(err)
	}
}

func TestOverlappingProfilesOrNoneIsADuplicate(t *testing.T) {
	sb := setup(t, "echo", "x")
	sb.Files(map[string]string{"home/.home/homebot.yml": perProfile + "  - name: apt\n    profiles: [server, workstation]\n    out: x\n    run: y\n"})
	_, err := config.Load(sb.Root)
	if err == nil || !strings.Contains(err.Error(), `exports[1]: duplicate export name "apt"`) {
		t.Fatalf("got %v", err)
	}
	sb.Files(map[string]string{"home/.home/homebot.yml": perProfile + "  - name: apt\n    out: x\n    run: y\n"})
	_, err = config.Load(sb.Root)
	if err == nil || !strings.Contains(err.Error(), "give each one profiles that don't overlap") {
		t.Fatalf("got %v", err)
	}
}

// Folder exports.

const folderCmd = "mkdir -p {out}/a && printf 1 > {out}/a/x && printf 2 > {out}/y"

func TestFolderExportIsNotRewrittenWhenUnchanged(t *testing.T) {
	sb := setup(t, folderCmd, "manifests/{os}/repos")
	r := one(t, sb)
	eq(t, r.OK && r.Changed, true)
	eq(t, r.Out, "manifests/linux/repos")
	eq(t, files(t, sb, "manifests/linux/repos"), []string{"a/x", "y"})
	r = one(t, sb)
	eq(t, r.OK && !r.Changed, true)
}

func TestChangedFolderReplacesTheOldOneWhole(t *testing.T) {
	sb := setup(t, folderCmd, "manifests/{os}/repos")
	run(t, sb, exports.Options{})
	configure(t, sb, "exports:\n  - name: list\n    out: manifests/{os}/repos\n    run: \"mkdir -p {out} && printf 3 > {out}/y\"\n")
	r := one(t, sb)
	eq(t, r.OK && r.Changed, true)
	eq(t, files(t, sb, "manifests/linux/repos"), []string{"y"})
	eq(t, read(t, sb, "manifests/linux/repos/y"), "3")
}

func TestEmptyFolderNeverReplacesAGoodOne(t *testing.T) {
	sb := setup(t, "mkdir -p {out} && printf 2 > {out}/y", "manifests/{os}/repos")
	run(t, sb, exports.Options{})
	configure(t, sb, "exports:\n  - name: list\n    out: manifests/{os}/repos\n    run: \"mkdir -p {out}\"\n")
	r := one(t, sb)
	eq(t, r.OK || r.Changed, false)
	if !strings.Contains(r.Error, "empty folder") {
		t.Fatalf("error %q", r.Error)
	}
	eq(t, files(t, sb, "manifests/linux/repos"), []string{"y"})
}

func TestFailingFolderCommandLeavesNoTempFolder(t *testing.T) {
	sb := setup(t, "mkdir -p {out} && printf 2 > {out}/y && exit 3", "manifests/{os}/repos")
	eq(t, one(t, sb).OK, false)
	eq(t, len(files(t, sb, "manifests/linux")), 0)
	entries, err := os.ReadDir(filepath.Join(sb.Root, "manifests/linux"))
	if err != nil {
		t.Fatal(err)
	}
	eq(t, len(entries), 0)
}

func TestExportCommandsCanRunHomebotItself(t *testing.T) {
	sb := setup(t, "homebot make-repos {out}", "manifests/linux/apt-repos")
	bin := sb.WithStubs(map[string]string{"mybot": `[ "$1" = make-repos ] || exit 9
mkdir -p "$2/keyrings" && echo noble > "$2/origin" && echo K > "$2/keyrings/k.gpg"`})
	sb.Ctx.Exe = filepath.Join(bin, "mybot")
	r := one(t, sb)
	eq(t, r.OK && r.Changed, true)
	eq(t, files(t, sb, "manifests/linux/apt-repos"), []string{"keyrings/k.gpg", "origin"})
}

// The homebot() function the command runs under is defined from the binary's
// path: a space or a quote in it must not split or end the word.
func TestExportCommandsRunHomebotThroughAPathWithSpaces(t *testing.T) {
	for _, dir := range []string{"my tools", "it's here", `two  spaces "and" $HOME`} {
		t.Run(dir, func(t *testing.T) {
			sb := setup(t, "homebot hello 'a b' {out}", "manifests/linux/hello.txt")
			bin := filepath.Join(t.TempDir(), dir)
			if err := os.MkdirAll(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			script := filepath.Join(bin, "my bot")
			if err := os.WriteFile(script, []byte("#!/bin/sh\n[ \"$1\" = hello ] || exit 9\nprintf '%s|%s' \"$2\" \"$#\" > \"$3\"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			sb.Ctx.Exe = script
			r := one(t, sb)
			if !r.OK || !r.Changed {
				t.Fatalf("result %+v", r)
			}
			eq(t, read(t, sb, "manifests/linux/hello.txt"), "a b|3")
		})
	}
}

// {out} is quoted too: an output path with a space is one word.
func TestAnOutputPathWithSpacesIsOneWord(t *testing.T) {
	sb := setup(t, "printf spaced > {out}", "manifests/my out/the file.txt")
	r := one(t, sb)
	if !r.OK || !r.Changed {
		t.Fatalf("result %+v", r)
	}
	eq(t, read(t, sb, "manifests/my out/the file.txt"), "spaced")
}

func TestSelfCommandQuotesTheBinaryPath(t *testing.T) {
	sb := setup(t, "echo", "x")
	sb.Ctx.Exe = "/opt/my tools/homebot"
	eq(t, exports.SelfCommand(sb.Ctx), "'/opt/my tools/homebot'")
	sb.Ctx.Exe = "/usr/bin/homebot"
	eq(t, exports.SelfCommand(sb.Ctx), "/usr/bin/homebot")
}

func TestHomebotRootIsSetForTheCommand(t *testing.T) {
	sb := setup(t, `printf %s "$HOMEBOT_ROOT"`, "x.txt")
	run(t, sb, exports.Options{})
	eq(t, read(t, sb, "x.txt"), sb.Root)
}

// F5: config.Load checks out as written; RunExports checks it again once
// {host} and the like are filled in and links made since are resolved,
// before it writes or removes anything.
func TestRunExportsRefusesAnOutThatLeavesTheRepoOnceFilledIn(t *testing.T) {
	cases := []struct {
		name, host, out, id string
		setup               func(t *testing.T, sb *testutil.Box) string
	}{
		{name: "host ..", host: "..", out: "'{host}'", id: "config.error.export_out_outside"},
		{name: "host . is the repo", host: ".", out: "'{host}'", id: "config.error.export_out_root"},
		{name: "host private", host: "private", out: "'{host}/list.txt'", id: "config.error.export_out_private"},
		{name: "host .git", host: ".git", out: "'{host}/list.txt'", id: "config.error.export_out_git"},
		{name: "a link made after load", host: "testhost", out: "gen/list.txt", id: "config.error.export_out_symlink",
			setup: func(t *testing.T, sb *testutil.Box) string {
				outside := t.TempDir()
				if err := os.WriteFile(filepath.Join(outside, "list.txt"), []byte("keep me\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(sb.Root, "gen")); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(outside, "list.txt")
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sb := setup(t, "echo replaced", c.out)
			sb.Ctx.Host = c.host
			kept := filepath.Join(sb.Root, "private/.gitignore")
			if c.setup != nil {
				kept = c.setup(t, sb)
			}
			before, err := os.ReadFile(kept)
			if err != nil {
				t.Fatal(err)
			}
			_, err = exports.RunExports(sb.Ctx, exports.Options{})
			var ue *fail.UserError
			if !errors.As(err, &ue) {
				t.Fatalf("RunExports: %v, want a *fail.UserError", err)
			}
			if text, ok := ue.Text(); !ok || text.ID() != c.id {
				t.Errorf("the error holds %s, want %s", text, c.id)
			}
			if !strings.Contains(err.Error(), "homebot.yml export list") {
				t.Errorf("error = %q, want it to name the export", err)
			}
			if after, err := os.ReadFile(kept); err != nil || string(after) != string(before) {
				t.Errorf("%s = %q, %v; want it untouched", kept, after, err)
			}
			eq(t, exports.ExportOutPath(sb.Ctx, sb.Ctx.Config.Exports[0]), "")
		})
	}
}

// tree records every path under root: its kind, and a file's contents or a
// link's target. Two equal trees hold the same things.
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		switch {
		case d.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			got[rel] = "link " + target
		case d.IsDir():
			got[rel] = "dir"
		default:
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			got[rel] = "file " + string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func sameMap(t *testing.T, before, after map[string]string) {
	t.Helper()
	for k, v := range before {
		if w, ok := after[k]; !ok {
			t.Errorf("%s is gone", k)
		} else if w != v {
			t.Errorf("%s changed: %q, was %q", k, w, v)
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			t.Errorf("%s appeared", k)
		}
	}
}

// R3: out is refused, before anything is written or removed, when it is the
// repo or a path homebot manages, or sits inside one, in any case. The names
// get past config.Load through {host}, so RunExports's own check is the one
// tested.
func TestRunExportsRefusesAnOutHomebotManages(t *testing.T) {
	cases := []struct{ host, out, id string }{
		{".", "'{host}'", "config.error.export_out_root"},
		{"autolink", "'{host}'", "config.error.export_out_managed"},
		{"autolink/common", "'{host}'", "config.error.export_out_managed_inside"},
		{"manifests", "'{host}'", "config.error.export_out_managed"},
		{"manifests/profiles", "'{host}'", "config.error.export_out_managed"},
		{"manifests/actions", "'{host}'", "config.error.export_out_managed"},
		{"furniture", "'{host}'", "config.error.export_out_managed"},
		{"homebot", "'{host}'", "config.error.export_out_managed"},
		{"private", "'{host}'", "config.error.export_out_private"},
		{".git", "'{host}'", "config.error.export_out_git"},
		{"homebot.yml", "'{host}'", "config.error.export_out_managed"},
		{"decisions.yml", "'{host}'", "config.error.export_out_managed"},
		{".gitignore", "'{host}'", "config.error.export_out_managed"},
		{".gitattributes", "'{host}'", "config.error.export_out_managed"},
		{"AUTOLINK", "'{host}/common/.zshrc'", "config.error.export_out_managed_inside"},
		{"Manifests/Profiles", "'{host}/work.yml'", "config.error.export_out_managed_inside"},
		{"Furniture", "'{host}'", "config.error.export_out_managed"},
	}
	for _, c := range cases {
		t.Run(c.host+" "+c.out, func(t *testing.T) {
			sb := setup(t, "echo replaced", c.out)
			sb.Files(map[string]string{
				"home/.home/autolink/common/.zshrc":      "zsh\n",
				"home/.home/manifests/profiles/work.yml": "name: work\n",
				"home/.home/manifests/actions/hello.sh":  "echo hi\n",
				"home/.home/furniture/Library/x.plist":   "plist\n",
				"home/.home/homebot/hooks/pre-commit":    "old hook\n",
				"home/.home/decisions.yml":               "decisions: {}\n",
				"home/.home/.gitignore":                  "*.tmp\n",
				"home/.home/.gitattributes":              "*.age binary\n",
				"home/.home/private/common/token":        "secret\n",
			})
			sb.Ctx.Host = c.host
			before := tree(t, sb.Root)
			_, err := exports.RunExports(sb.Ctx, exports.Options{})
			var ue *fail.UserError
			if !errors.As(err, &ue) {
				t.Fatalf("RunExports: %v, want a *fail.UserError", err)
			}
			if text, ok := ue.Text(); !ok || text.ID() != c.id {
				t.Errorf("the error holds %s, want %s", text, c.id)
			}
			sameMap(t, before, tree(t, sb.Root))
			eq(t, exports.ExportOutPath(sb.Ctx, sb.Ctx.Config.Exports[0]), "")
		})
	}
}

// R3: an export never deletes something of a different kind. A file is not
// put where a folder is, nor a folder where a file is; the export fails,
// says why, and leaves the tree as it was.
func TestRunExportsNeverReplacesAFolderWithAFileOrTheOtherWay(t *testing.T) {
	cases := []struct {
		name, run, id, text string
		files               map[string]string
	}{
		{
			name: "a file where a folder is", run: "echo replaced",
			id: "export.error.out_is_folder", text: "wrote a file, but manifests/list is a folder; kept the folder. Remove it yourself, or give the export another out",
			files: map[string]string{"home/.home/manifests/list/keep.txt": "keep me\n"},
		},
		{
			name: "a folder where a file is", run: "mkdir {out} && echo x > {out}/a",
			id: "export.error.out_is_file", text: "wrote a folder, but manifests/list is a file; kept the file. Remove it yourself, or give the export another out",
			files: map[string]string{"home/.home/manifests/list": "keep me\n"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sb := setup(t, c.run, "manifests/list")
			sb.Files(c.files)
			before := tree(t, sb.Root)
			r := one(t, sb)
			if r.OK || r.Changed {
				t.Errorf("result = %+v, want a failure", r)
			}
			eq(t, r.Error, c.text)
			sameMap(t, before, tree(t, sb.Root))
			st, err := state.Load(sb.Ctx)
			if err != nil {
				t.Fatal(err)
			}
			last, ok := st.Exports.Get("list")
			if !ok || last.OK || last.Error != c.text {
				t.Errorf("state = %+v, %v; want the failure recorded", last, ok)
			}
		})
	}
}

// An unknown export name is a said error: Error() (what a caller may store)
// is the English under any persona, and voice.ErrorText (the terminal) is the
// persona's line.
func TestUnknownExportNameIsSaidNotStoredVoiced(t *testing.T) {
	sb := setup(t, "echo a", "a.txt")
	yes := true
	sb.Ctx.StdoutTerminal = &yes
	persona.Override(sb.Ctx, &persona.Voice{ID: "hostile", Name: "Hostile", Lines: map[string][]persona.Line{
		"export.error.unknown": {{Text: "PERSONA NO {names}, HAVE {known}", Lang: language.English}},
	}})
	_, err := exports.RunExports(sb.Ctx, exports.Options{Names: []string{"nope"}})
	if err == nil {
		t.Fatal("unknown name accepted")
	}
	if got, want := err.Error(), "No export named nope for this OS. Exports here: list."; got != want {
		t.Errorf("stored error = %q, want %q", got, want)
	}
	if got, want := voice.ErrorText(sb.Ctx, err), "PERSONA NO nope, HAVE list"; got != want {
		t.Errorf("printed error = %q, want %q", got, want)
	}
}
