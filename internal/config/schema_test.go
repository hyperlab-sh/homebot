package config

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
)

func TestSchemaRowsDescribeTheLoader(t *testing.T) {
	defaults := Default()
	// A loader fallback that Default() does not carry: the OSes an export
	// runs on when it leaves `on` out. Read it from the loader, not a literal.
	bare, err := Parse([]byte("exports:\n  - name: probe\n    out: manifests/probe\n    run: echo\n"), "homebot.yml")
	if err != nil || len(bare.Exports) != 1 {
		t.Fatalf("probe export: cfg %+v, err %v", bare, err)
	}
	hosts := make([]string, 0, len(defaults.Profiles.Hosts))
	for host, profile := range defaults.Profiles.Hosts {
		hosts = append(hosts, host+": "+profile)
	}
	slices.Sort(hosts)
	names := make([]string, len(defaults.Exports))
	for i, export := range defaults.Exports {
		names[i] = export.Name
	}
	wantDefaults := map[string]string{
		"profiles.hosts":   "{" + strings.Join(hosts, ", ") + "}",
		"triage.watch":     "[" + strings.Join(defaults.Triage.Watch, ", ") + "]",
		"autosave.commit":  string(defaults.Autosave.Commit),
		"autosave.push":    strconv.FormatBool(defaults.Autosave.Push),
		"exports":          "[" + strings.Join(names, ", ") + "]",
		"exports[].on":     "[" + strings.Join(bare.Exports[0].On, ", ") + "]",
		"limits.maxFileMB": strconv.FormatFloat(defaults.Limits.MaxFileMB, 'f', -1, 64),
		"update":           string(defaults.Update),
	}
	// Every row that carries a default must be listed above, so a new
	// default cannot slip past the comparison.
	for _, key := range Keys {
		if _, ok := wantDefaults[key.Path]; !ok && key.Default != "" {
			t.Errorf("%s has default %q but the test derives none", key.Path, key.Default)
		}
	}
	seen := map[string]bool{}
	for _, key := range Keys {
		if seen[key.Path] {
			t.Errorf("duplicate schema path %s", key.Path)
		}
		seen[key.Path] = true
		if key.Kind == "" || key.DocID == "" || key.Added == "" {
			t.Errorf("incomplete schema row %+v", key)
		}
		if _, ok := msg.Lookup(key.DocID); !ok || Description(key).ID() != key.DocID {
			t.Errorf("%s has no catalog description %s", key.Path, key.DocID)
		}
		if want := wantDefaults[key.Path]; key.Default != want {
			t.Errorf("%s default %q, want %q", key.Path, key.Default, want)
		}
	}
	for parent, want := range map[string][]string{
		"":           {"profiles", "triage", "autosave", "exports", "limits", "name", "persona", "update"},
		"profiles":   {"default", "hosts"},
		"triage":     {"watch"},
		"autosave":   {"commit", "push"},
		"exports[0]": {"name", "on", "out", "run", "profiles"},
		"limits":     {"maxFileMB"},
	} {
		if got := children(parent); !slices.Equal(got, want) {
			t.Errorf("%s children = %q, want %q", parent, got, want)
		}
	}
}

func TestUpdateModesAndWarnings(t *testing.T) {
	for _, mode := range []UpdateMode{UpdateOff, UpdateNotify, UpdateAuto} {
		cfg, err := Parse([]byte("update: "+string(mode)+"\n"), "homebot.yml")
		if err != nil || cfg.Update != mode || len(cfg.Warnings) != 0 {
			t.Errorf("update %s: cfg %+v, err %v", mode, cfg, err)
		}
	}
	for _, src := range []string{"update: atuo\n", "update: true\n", "update: [auto]\n", "update: {mode: auto}\n"} {
		cfg, err := Parse([]byte(src), "homebot.yml")
		if err != nil || cfg.Update != UpdateOff || len(cfg.Warnings) != 1 {
			t.Fatalf("%q: cfg %+v, err %v", src, cfg, err)
		}
		line := fail.Say(cfg.Warnings[0]).Error()
		if !strings.Contains(line, "homebot.yml:1") || !strings.Contains(line, "using off") {
			t.Errorf("warning %q", line)
		}
	}
	cfg, err := Parse([]byte("update:\n"), "homebot.yml")
	if err != nil || cfg.Update != UpdateOff || len(cfg.Warnings) != 0 {
		t.Errorf("null update: cfg %+v, err %v", cfg, err)
	}
}

func TestSchemaAllowedSamplesLoad(t *testing.T) {
	for _, row := range Keys {
		for _, value := range row.Allowed {
			var src string
			switch row.Path {
			case "update":
				src = "update: " + value + "\n"
			case "autosave.commit":
				src = "autosave:\n  commit: " + value + "\n"
			case "exports[].on":
				src = "exports:\n  - name: sample\n    on: " + value + "\n    out: manifests/sample\n    run: 'true'\n"
			default:
				t.Errorf("no allowed-value fixture for %s", row.Path)
			}
			if _, err := Parse([]byte(src), "homebot.yml"); err != nil {
				t.Errorf("%s=%s: %v", row.Path, value, err)
			}
		}
	}
}
