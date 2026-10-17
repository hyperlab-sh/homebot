package config

import (
	"errors"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/hyperlab-sh/homebot/internal/fail"
)

func parse(t *testing.T, yaml string) *Config {
	t.Helper()
	cfg, err := Parse([]byte(yaml), "homebot.yml")
	if err != nil {
		t.Fatalf("Parse(%q): %v", yaml, err)
	}
	return cfg
}

func parseErr(t *testing.T, yaml string) string {
	t.Helper()
	_, err := Parse([]byte(yaml), "homebot.yml")
	if err == nil {
		t.Fatalf("Parse(%q) succeeded, want an error", yaml)
	}
	var ue *fail.UserError
	if !errors.As(err, &ue) {
		t.Fatalf("error is %T, want *fail.UserError", err)
	}
	return err.Error()
}

func TestEmptyFileGivesDefaults(t *testing.T) {
	for _, src := range []string{"", "\n", "# only a comment\n", "---\n", "~\n", "null\n"} {
		if diff := cmp.Diff(Default(), parse(t, src)); diff != "" {
			t.Errorf("Parse(%q) differs from Default (-want +got):\n%s", src, diff)
		}
	}
}

func TestReadsEverySection(t *testing.T) {
	cfg := parse(t, `
profiles:
  default: desk
  hosts: { forge: workstation }
triage:
  watch: ["~/.config/*"]
autosave:
  commit: daily
  push: true
exports:
  - name: brew
    out: manifests/{os}/Brewfile
    run: brew bundle dump --force --file={out}
  - name: apt
    on: linux
    out: manifests/linux/apt.txt
    run: apt-mark showmanual
limits:
  maxFileMB: 20
`)
	want := &Config{
		Profiles: Profiles{Default: "desk", Hosts: map[string]string{"forge": "workstation"}},
		Triage:   Triage{Watch: []string{"~/.config/*"}},
		Autosave: Autosave{Commit: CadenceDaily, Push: true},
		Exports: []ExportSpec{
			{Name: "brew", On: []string{"linux", "macos"}, Out: "manifests/{os}/Brewfile", Run: "brew bundle dump --force --file={out}"},
			{Name: "apt", On: []string{"linux"}, Out: "manifests/linux/apt.txt", Run: "apt-mark showmanual"},
		},
		Limits: Limits{MaxFileMB: 20},
		Update: UpdateOff,
	}
	if diff := cmp.Diff(want, cfg); diff != "" {
		t.Errorf("config (-want +got):\n%s", diff)
	}
}

func TestDefaultsAreNotSharedBetweenParses(t *testing.T) {
	first := parse(t, "")
	first.Exports = append(first.Exports, ExportSpec{Name: "x"})
	first.Triage.Watch[0] = "changed"
	first.Profiles.Hosts["h"] = "p"
	second := parse(t, "")
	if diff := cmp.Diff(Default(), second); diff != "" {
		t.Errorf("second parse was affected by the first (-want +got):\n%s", diff)
	}
}

func TestRejects(t *testing.T) {
	cases := []struct {
		name, yaml, want string
	}{
		{"unknown key", "colour: blue", `homebot.yml:1:1: unknown key "colour" (expected profiles, triage, autosave, exports, limits, name, persona, update)`},
		{"link", "link:\n  nofold: [.claude]", `homebot.yml:1:1: "link" is gone. Wrap a folder's name in () in autolink/ to keep it a real folder, e.g. autolink/common/(claude)/settings.json`},
		{"link with null value", "link:\n", `"link" is gone`},
		{"bad cadence", "autosave:\n  commit: weekly", "homebot.yml:2:11: autosave.commit must be off, hourly or daily"},
		{"cadence not a string", "autosave:\n  commit: 5", "off, hourly or daily"},
		{"push not a bool", "autosave:\n  push: yes please", "homebot.yml:2:9: autosave.push must be true or false"},
		{"push yes is a string", "autosave:\n  push: yes", "true or false"},
		{"missing out", "exports:\n  - name: a\n    run: x", "homebot.yml:2:5: exports[0]: out is required"},
		{"empty out", "exports:\n  - {name: a, out: '', run: y}", "out is required"},
		{"number name", "exports:\n  - {name: 5, out: x, run: y}", "name is required"},
		{"duplicate name", "exports:\n  - {name: a, out: x, run: y}\n  - {name: a, out: z, run: y}", `homebot.yml:3:5: exports[1]: duplicate export name "a" (give each one profiles that don't overlap)`},
		{"bad os", "exports:\n  - {name: a, out: x, run: y, on: bsd}", `exports[0].on: unknown OS "bsd" (use linux or macos)`},
		{"bad os in a list", "exports:\n  - {name: a, out: x, run: y, on: [linux, bsd]}", `unknown OS "bsd"`},
		{"on is a number", "exports:\n  - {name: a, out: x, run: y, on: 3}", "homebot.yml:2:35: exports[0].on: expected a string or a list of strings"},
		{"watch is a number", "triage:\n  watch: 3", "homebot.yml:2:10: triage.watch: expected a string or a list of strings"},
		{"commit off is fine but nan limit is not", "limits:\n  maxFileMB: .nan", "positive number"},
		{"watch without glob", "triage:\n  watch: [~/.config]", `homebot.yml:2:11: triage.watch entries end in /* or /.* (got "~/.config")`},
		{"negative limit", "limits:\n  maxFileMB: -1", "homebot.yml:2:14: limits.maxFileMB must be a positive number"},
		{"zero limit", "limits:\n  maxFileMB: 0", "positive number"},
		{"string limit", "limits:\n  maxFileMB: big", "positive number"},
		{"hosts is a list", "profiles:\n  hosts: [a]", "homebot.yml:2:10: profiles.hosts must map host names to profile names"},
		{"host maps to a number", "profiles:\n  hosts: {a: 1}", "map host names"},
		{"default is a number", "profiles:\n  default: 5", "profiles.default must be a profile name"},
		{"profiles is a list", "profiles: [a]", "profiles must be a mapping"},
		{"empty profiles", "profiles:\n", "profiles must be a mapping"},
		{"triage is a string", "triage: x", "triage must be a mapping"},
		{"autosave is a string", "autosave: x", "autosave must be a mapping"},
		{"exports is a mapping", "exports: {a: b}", "exports must be a list"},
		{"export is a string", "exports: [x]", "exports[0]: expected a mapping with name, out, run"},
		{"limits is a list", "limits: [1]", "limits must be a mapping"},
		{"top level list", "- a\n- b", "homebot.yml:1:1: expected a mapping at the top level"},
		{"top level scalar", "hello", "expected a mapping at the top level"},
		{"two documents", "a: 1\n---\nb: 2\n", "expected a mapping at the top level"},
		{"invalid yaml", "a: b\n  c: d\n", "homebot.yml:1:4: invalid YAML: mapping value is not allowed in this context"},
		{"duplicate keys", "limits: {maxFileMB: 1}\nlimits: {maxFileMB: 2}\n", "invalid YAML"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseErr(t, c.yaml); !strings.Contains(got, c.want) {
				t.Errorf("error = %q, want it to contain %q", got, c.want)
			}
		})
	}
}

func TestNestedUnknownKeyNamesItsParent(t *testing.T) {
	cases := []struct{ name, yaml, want string }{
		{"autosave", "autosave:\n  commit: off\n  autosav: x", `homebot.yml:3:3: unknown key "autosav" under autosave (expected commit, push)`},
		{"profiles", "profiles:\n  hostz: {}", `unknown key "hostz" under profiles (expected default, hosts)`},
		{"triage", "triage:\n  watc: []", `under triage (expected watch)`},
		{"limits", "limits:\n  max: 1", `under limits (expected maxFileMB)`},
		{"export", "exports:\n  - {name: a, out: x, run: y, cron: z}", `under exports[0] (expected name, on, out, run, profiles)`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseErr(t, c.yaml); !strings.Contains(got, c.want) {
				t.Errorf("error = %q, want it to contain %q", got, c.want)
			}
		})
	}
}

func TestFirstErrorWinsInSectionOrder(t *testing.T) {
	got := parseErr(t, "limits: {maxFileMB: -1}\nautosave: {commit: weekly}\n")
	if !strings.Contains(got, "autosave.commit") {
		t.Errorf("error = %q, want the autosave error first", got)
	}
}

func TestScalarForms(t *testing.T) {
	cfg := parse(t, "limits:\n  maxFileMB: 0.5\nautosave:\n  push: false\n  commit: \"hourly\"\ntriage:\n  watch: ~/.config/*\n")
	if cfg.Limits.MaxFileMB != 0.5 || cfg.Autosave.Commit != CadenceHourly || cfg.Autosave.Push {
		t.Errorf("got %+v %+v", cfg.Limits, cfg.Autosave)
	}
	if diff := cmp.Diff([]string{"~/.config/*"}, cfg.Triage.Watch); diff != "" {
		t.Errorf("a single string should be a one-entry list (-want +got):\n%s", diff)
	}
}

func TestTriageWithoutWatchMeansNoWatchedFiles(t *testing.T) {
	for _, src := range []string{"triage: {}\n", "triage:\n  watch:\n"} {
		cfg := parse(t, src)
		if cfg.Triage.Watch == nil || len(cfg.Triage.Watch) != 0 {
			t.Errorf("%q: Watch = %#v, want empty non-nil", src, cfg.Triage.Watch)
		}
	}
}

func TestInfiniteLimitMeansNoLimit(t *testing.T) {
	if got := parse(t, "limits:\n  maxFileMB: .inf\n").Limits.MaxFileMB; !math.IsInf(got, 1) {
		t.Errorf("MaxFileMB = %v, want +Inf", got)
	}
	parseErr(t, "limits:\n  maxFileMB: -.inf\n")
}

func TestCommitOffIsAString(t *testing.T) {
	if got := parse(t, "autosave:\n  commit: off\n").Autosave.Commit; got != CadenceOff {
		t.Errorf("Commit = %q", got)
	}
}

func TestNullWatchMeansNoWatchedFiles(t *testing.T) {
	cfg := parse(t, "triage:\n  watch:\n")
	if cfg.Triage.Watch == nil || len(cfg.Triage.Watch) != 0 {
		t.Errorf("Watch = %#v, want empty non-nil", cfg.Triage.Watch)
	}
}

func TestAnchorsAndTagsAreResolved(t *testing.T) {
	cfg := parse(t, "triage:\n  watch: &w [\"~/.a/*\"]\nexports:\n  - {name: a, out: x, run: y, on: !!str linux}\n")
	if diff := cmp.Diff([]string{"~/.a/*"}, cfg.Triage.Watch); diff != "" {
		t.Errorf("watch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"linux"}, cfg.Exports[0].On); diff != "" {
		t.Errorf("on (-want +got):\n%s", diff)
	}
}

func TestCRLFFile(t *testing.T) {
	cfg := parse(t, "autosave:\r\n  commit: daily\r\nexports:\r\n  - {name: a, out: x, run: y}\r\n")
	if cfg.Autosave.Commit != CadenceDaily || len(cfg.Exports) != 1 {
		t.Errorf("got %+v", cfg)
	}
	if got := parseErr(t, "autosave:\r\n  commit: no\r\n"); !strings.Contains(got, "homebot.yml:2:11") {
		t.Errorf("error = %q, want position 2:11", got)
	}
}

func TestExportProfilesFilter(t *testing.T) {
	const base = "exports:\n  - name: a\n    out: x\n    run: y\n"
	t.Run("is parsed", func(t *testing.T) {
		got := parse(t, base+"    profiles: [workstation, laptop]\n").Exports[0].Profiles
		if diff := cmp.Diff([]string{"workstation", "laptop"}, got); diff != "" {
			t.Errorf("profiles (-want +got):\n%s", diff)
		}
		if p := parse(t, base).Exports[0].Profiles; p != nil {
			t.Errorf("unset profiles = %#v, want nil", p)
		}
	})
	t.Run("must be a list of non-empty strings", func(t *testing.T) {
		for _, bad := range []string{"workstation", "[a, 1]", `[""]`, "{a: b}"} {
			if got := parseErr(t, base+"    profiles: "+bad+"\n"); !strings.Contains(got, "profiles") {
				t.Errorf("profiles: %s: error %q does not mention profiles", bad, got)
			}
		}
	})
	t.Run("same name is allowed when profiles or OSes do not overlap", func(t *testing.T) {
		parse(t, "exports:\n  - {name: a, out: x, run: y, profiles: [p]}\n  - {name: a, out: x, run: y, profiles: [q]}\n")
		parse(t, "exports:\n  - {name: a, out: x, run: y, on: linux}\n  - {name: a, out: x, run: y, on: macos}\n")
		parseErr(t, "exports:\n  - {name: a, out: x, run: y, profiles: [p]}\n  - {name: a, out: x, run: y}\n")
		parseErr(t, "exports:\n  - {name: a, out: x, run: y, profiles: [p, q]}\n  - {name: a, out: x, run: y, profiles: [q]}\n")
	})
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("an old homebot.yaml gets a rename hint, and homebot.yml wins once present", func(t *testing.T) {
		write("homebot.yaml", "limits:\n  maxFileMB: 5\n")
		_, err := Load(dir)
		want := "homebot reads homebot.yml now: rename " + filepath.Join(dir, "homebot.yaml") + " to homebot.yml"
		if err == nil || err.Error() != want {
			t.Fatalf("Load = %v, want %q", err, want)
		}
		var ue *fail.UserError
		if !errors.As(err, &ue) {
			t.Errorf("error is %T, want *fail.UserError", err)
		}
		write("homebot.yml", "limits:\n  maxFileMB: 7\n")
		cfg, err := Load(dir)
		if err != nil || cfg.Limits.MaxFileMB != 7 {
			t.Fatalf("Load = %+v, %v; want maxFileMB 7", cfg, err)
		}
	})
	t.Run("errors carry the file's path", func(t *testing.T) {
		write("homebot.yml", "colour: blue\n")
		_, err := Load(dir)
		want := filepath.Join(dir, "homebot.yml") + `:1:1: unknown key "colour"`
		if err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Fatalf("Load = %v, want prefix %q", err, want)
		}
	})
	t.Run("a missing file is an error", func(t *testing.T) {
		if _, err := Load(t.TempDir()); err == nil {
			t.Fatal("Load of an empty dir succeeded")
		}
	})
}

// Parse has no Context, so its errors carry their message for the CLI to say
// in the Context's voice (fail.Say).
func TestErrorsCarryTheirMessage(t *testing.T) {
	for _, c := range []struct{ yaml, id string }{
		{"a: [", "config.error.yaml"},
		{"- a\n", "config.error.not_mapping"},
		{"colour: red\n", "config.error.unknown_key"},
		{"triage:\n  nope: 1\n", "config.error.unknown_key_under"},
		{"triage:\n  watch: [1]\n", "config.error.string_list"},
		{"exports:\n  - {name: a, out: b, run: c, on: bsd}\n", "config.error.unknown_os"},
		{"exports:\n  - {name: a, out: b, run: c}\n  - {name: a, out: d, run: e}\n", "config.error.export_duplicate"},
		{"limits:\n  maxFileMB: 0\n", "config.error.limits_max"},
	} {
		_, err := Parse([]byte(c.yaml), "homebot.yml")
		var ue *fail.UserError
		if !errors.As(err, &ue) {
			t.Fatalf("%q: error %v is not a *fail.UserError", c.yaml, err)
		}
		if text, ok := ue.Text(); !ok || text.ID() != c.id {
			t.Errorf("%q: the error holds %s, want %s", c.yaml, text, c.id)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "homebot.yaml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	var ue *fail.UserError
	if !errors.As(err, &ue) {
		t.Fatalf("Load: %v", err)
	}
	if text, ok := ue.Text(); !ok || text.ID() != "config.error.old_name" {
		t.Errorf("Load's error holds %s", text)
	}
}

// F5: an export replaces its out wholesale, so out must stay a path inside
// the repo that is not the repo, .git or private/.
func TestExportOutMustStayInsideTheRepo(t *testing.T) {
	cases := []struct{ out, id, text string }{
		{"private", "config.error.export_out_private", `homebot.yml:2:20: exports[0]: out private of export "a" is in private/, where your secrets are`},
		{"Private/x", "config.error.export_out_private", "is in private/"},
		{"PRIVATE/../private/b", "config.error.export_out_private", "is in private/"},
		{"./private/", "config.error.export_out_private", "is in private/"},
		{"..", "config.error.export_out_outside", `homebot.yml:2:20: exports[0]: out .. of export "a" leads outside the repo`},
		{"../elsewhere/x", "config.error.export_out_outside", "leads outside the repo"},
		{"a/../..", "config.error.export_out_outside", "leads outside the repo"},
		{"a/../../b", "config.error.export_out_outside", "leads outside the repo"},
		{".", "config.error.export_out_root", `out . of export "a" is the repo itself`},
		{"a/..", "config.error.export_out_root", "is the repo itself"},
		{"./", "config.error.export_out_root", "is the repo itself"},
		{"/abs/out", "config.error.export_out_absolute", `out /abs/out of export "a" is an absolute path`},
		{".git", "config.error.export_out_git", "is in .git"},
		{".GIT/hooks/pre-commit", "config.error.export_out_git", "is in .git"},
	}
	for _, c := range cases {
		t.Run(c.out, func(t *testing.T) {
			yaml := "exports:\n  - {name: a, out: '" + c.out + "', run: x}\n"
			_, err := Parse([]byte(yaml), "homebot.yml")
			var ue *fail.UserError
			if !errors.As(err, &ue) {
				t.Fatalf("Parse: %v, want a *fail.UserError", err)
			}
			if text, ok := ue.Text(); !ok || text.ID() != c.id {
				t.Errorf("the error holds %s, want %s", text, c.id)
			}
			if !strings.Contains(err.Error(), c.text) {
				t.Errorf("error = %q, want it to contain %q", err, c.text)
			}
		})
	}
	for _, out := range []string{"manifests/Brewfile", "out/{host}.txt", "a/../b", "private.txt", "privatestuff/x", ".gitignore-out", "x/private/y"} {
		t.Run("valid "+out, func(t *testing.T) {
			cfg := parse(t, "exports:\n  - {name: a, out: '"+out+"', run: x}\n")
			if cfg.Exports[0].Out != out {
				t.Errorf("out = %q, want %q", cfg.Exports[0].Out, out)
			}
		})
	}
}

// R3: out must not be, or sit inside, a path homebot manages, in any case.
// manifests itself is refused, but manifests/<anything else> is where
// exports go.
func TestExportOutMustNotReplaceWhatHomebotManages(t *testing.T) {
	cases := []struct{ out, managed string }{
		{"autolink", "autolink"},
		{"autolink/common", "autolink"},
		{"autolink/macos/.zshrc", "autolink"},
		{"AutoLink/linux", "autolink"},
		{"manifests", "manifests"},
		{"Manifests/", "manifests"},
		{"manifests/profiles", "manifests/profiles"},
		{"manifests/profiles/work.yml", "manifests/profiles"},
		{"MANIFESTS/Profiles", "manifests/profiles"},
		{"manifests/actions", "manifests/actions"},
		{"manifests/actions/x/y", "manifests/actions"},
		{"furniture", "furniture"},
		{"furniture/Library/x.plist", "furniture"},
		{"homebot", "homebot"},
		{"homebot/hooks/pre-commit", "homebot"},
		{"homebot.yml", "homebot.yml"},
		{"HomeBot.YML", "homebot.yml"},
		{"decisions.yml", "decisions.yml"},
		{".gitignore", ".gitignore"},
		{".GitIgnore", ".gitignore"},
		{".gitattributes", ".gitattributes"},
		{"x/../furniture", "furniture"},
	}
	for _, c := range cases {
		t.Run(c.out, func(t *testing.T) {
			yaml := "exports:\n  - {name: a, out: '" + c.out + "', run: x}\n"
			_, err := Parse([]byte(yaml), "homebot.yml")
			var ue *fail.UserError
			if !errors.As(err, &ue) {
				t.Fatalf("Parse: %v, want a *fail.UserError", err)
			}
			id, word := "config.error.export_out_managed", " is "+c.managed+","
			if !strings.EqualFold(filepath.Clean(c.out), c.managed) {
				id, word = "config.error.export_out_managed_inside", " is inside "+c.managed+","
			}
			if text, ok := ue.Text(); !ok || text.ID() != id {
				t.Errorf("the error holds %s, want %s", text, id)
			}
			want := `homebot.yml:2:20: exports[0]: out ` + c.out + ` of export "a"` + word + ` which homebot keeps for itself; put exports under manifests/`
			if err.Error() != want {
				t.Errorf("error = %q, want %q", err, want)
			}
		})
	}
	for _, out := range []string{"manifests/Brewfile", "manifests/apt-repos", "manifests/x/y", "manifests/profiles.txt", "manifests/actionsx", "autolinks", "furniture.txt", "homebot.yml.bak", "decisions.yml.d/x", "x/autolink", "x/homebot.yml"} {
		t.Run("valid "+out, func(t *testing.T) {
			cfg := parse(t, "exports:\n  - {name: a, out: '"+out+"', run: x}\n")
			if cfg.Exports[0].Out != out {
				t.Errorf("out = %q, want %q", cfg.Exports[0].Out, out)
			}
		})
	}
}

// Load also resolves the symlinks among out's existing parent folders: a
// folder in the repo that links outside it, or into private/, is refused.
func TestLoadRefusesAnExportOutThroughASymlink(t *testing.T) {
	outside := t.TempDir()
	dir := t.TempDir()
	for _, d := range []string{"private/common", "manifests", "real"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := func(target, name string) {
		t.Helper()
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	link(outside, "away")
	link("private/common", "secretish")
	link(".", "self")
	link("real", "inside")
	link(filepath.Join(dir, "nowhere"), "dangling")
	load := func(out string) error {
		t.Helper()
		yaml := "exports:\n  - {name: a, out: '" + out + "', run: x}\n"
		if err := os.WriteFile(filepath.Join(dir, "homebot.yml"), []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Load(dir)
		return err
	}
	for _, out := range []string{"away/file", "away/deep/er/file", "secretish/x", "self/private/x", "self/furniture/x", "self/Manifests", "inside/../away/x"} {
		t.Run(out, func(t *testing.T) {
			err := load(out)
			var ue *fail.UserError
			if !errors.As(err, &ue) {
				t.Fatalf("Load: %v, want a *fail.UserError", err)
			}
			if text, ok := ue.Text(); !ok || text.ID() != "config.error.export_out_symlink" {
				t.Errorf("the error holds %s, want config.error.export_out_symlink", text)
			}
			if want := `out ` + out + ` of export "a" goes through a symlink to `; !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want it to contain %q", err, want)
			}
		})
	}
	// out itself may be a link: the export removes the link, not its target.
	for _, out := range []string{"manifests/Brewfile", "inside/x", "self/manifests/x", "away", "dangling/x", "new/folder/x"} {
		t.Run("valid "+out, func(t *testing.T) {
			if err := load(out); err != nil {
				t.Fatalf("Load: %v", err)
			}
		})
	}
}

func TestReservedKeysAreIgnored(t *testing.T) {
	const base = "autosave:\n  push: false\n"
	want := parse(t, base)
	for _, extra := range []string{"ai: {provider: x}\n", "ai: true\n"} {
		if diff := cmp.Diff(want, parse(t, base+extra)); diff != "" {
			t.Errorf("%q changed the Config (-want +got):\n%s", extra, diff)
		}
		if diff := cmp.Diff(Default(), parse(t, extra)); diff != "" {
			t.Errorf("%q alone changed the Config (-want +got):\n%s", extra, diff)
		}
	}
}

func TestReservedKeysAreLoggedInDebug(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "homebot.yml"), []byte("update: auto\nai: {provider: x}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	if _, err := LoadDebug(dir, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))); err != nil {
		t.Fatal(err)
	}
	for _, k := range ReservedKeys {
		if n := strings.Count(buf.String(), "key="+k+" "); n != 1 {
			t.Errorf("key %s logged %d times, want 1:\n%s", k, n, buf.String())
		}
	}
}

func TestReservedKeysOnlyAtTopLevel(t *testing.T) {
	for _, c := range []struct{ yaml, id string }{
		{"exports:\n  - {name: a, out: b, run: c, persona: x}\n", "config.error.unknown_key_under"},
		{"autosave:\n  persona: x\n", "config.error.unknown_key_under"},
		{"profiles:\n  name: zork\n", "config.error.unknown_key_under"},
		{"autosave:\n  update: auto\n", "config.error.unknown_key_under"},
		{"autosave:\n  ai: {provider: x}\n", "config.error.unknown_key_under"},
		{"updates: auto\n", "config.error.unknown_key"},
		{"Update: auto\n", "config.error.unknown_key"},
		{"AI: {provider: x}\n", "config.error.unknown_key"},
		{"personas: tester\n", "config.error.unknown_key"},
		{"Persona: tester\n", "config.error.unknown_key"},
		{"personaAlias: zork\n", "config.error.unknown_key"},
		{"Name: zork\n", "config.error.unknown_key"},
		{"persona: tester\ncolour: red\n", "config.error.unknown_key"},
	} {
		_, err := Parse([]byte(c.yaml), "homebot.yml")
		var ue *fail.UserError
		if !errors.As(err, &ue) {
			t.Errorf("%q: error %v is not a *fail.UserError", c.yaml, err)
			continue
		}
		if text, ok := ue.Text(); !ok || text.ID() != c.id {
			t.Errorf("%q: the error holds %s, want %s", c.yaml, text, c.id)
		}
	}
}
