package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/goccy/go-yaml"
	"github.com/rogpeppe/go-internal/txtar"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/persona"
)

func templateTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, marker := range map[string]string{
		"commands": "commands", "config": "config", "personas": "personas",
	} {
		path := filepath.Join(root, "docs/pages/reference", name+".md")
		content := "prose before\n<!-- gendocs:start " + marker + " -->\nold\n<!-- gendocs:end -->\nprose after\n"
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestDocsDeterminismAndCheck(t *testing.T) {
	root := templateTree(t)
	if err := writeDocs(root, true); err == nil || !strings.Contains(err.Error(), "commands.md") {
		t.Fatalf("-check before generation: %v", err)
	}
	if err := writeDocs(root, false); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		"docs/pages/reference/commands.md", "docs/pages/reference/config.md",
		"docs/pages/reference/messages.md", "docs/pages/reference/personas.md",
		"docs/landing/schema/homebot.json", "docs/landing/schema/persona.json",
	}
	first := make(map[string][]byte, len(paths))
	for _, path := range paths {
		first[path], _ = os.ReadFile(filepath.Join(root, path))
	}
	if err := writeDocs(root, false); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		got, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || !bytes.Equal(got, first[path]) {
			t.Fatalf("%s was not deterministic: %v", path, err)
		}
	}
	if err := writeDocs(root, true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, paths[0])
	changed := bytes.Replace(first[paths[0]], []byte("Generated"), []byte("Xenerated"), 1)
	if err := os.WriteFile(path, changed, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeDocs(root, true); err == nil || !strings.Contains(err.Error(), paths[0]) {
		t.Fatalf("-check missed one-byte change: %v", err)
	}
}

func TestDocsMarkersAndProse(t *testing.T) {
	before := []byte("front matter\nprose before\n<!-- gendocs:start config -->\nold\n<!-- gendocs:end -->\nprose after\n")
	got, err := replaceSection("config.md", before, "config", []byte("new\n"))
	if err != nil || !bytes.HasPrefix(got, []byte("front matter\nprose before\n")) || !bytes.HasSuffix(got, []byte("<!-- gendocs:end -->\nprose after\n")) {
		t.Fatalf("prose changed: %q, %v", got, err)
	}
	if _, err := replaceSection("config.md", []byte("unmarked"), "config", nil); err == nil || !strings.Contains(err.Error(), "config.md") {
		t.Fatalf("missing marker: %v", err)
	}
}

func compiledSchema(t *testing.T, doc schema) *jsonschema.Schema {
	t.Helper()
	data, err := jsonDoc(doc)
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	meta, err := compiler.Compile(schemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err := meta.Validate(value); err != nil {
		t.Fatalf("schema fails its 2020-12 metaschema: %v", err)
	}
	url := doc["$id"].(string)
	if err := compiler.AddResource(url, value); err != nil {
		t.Fatal(err)
	}
	s, err := compiler.Compile(url)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func jsonValue(t *testing.T, v any) any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestHomebotSchemaMatchesLoader(t *testing.T) {
	s := compiledSchema(t, homebotSchema())
	valid := []string{
		"", "profiles:\n  hosts:\n    laptop: workstation\n",
		"triage:\n  watch: '~/.config/*'\n",
		"triage:\n  watch: null\n",
		"autosave:\n  commit: daily\n  push: true\n",
		"exports:\n  - name: packages\n    out: manifests/linux/packages.txt\n    run: 'printf packages'\n    on: linux\n    profiles: [workstation]\n",
		"exports:\n  - name: packages\n    out: manifests/linux/packages.txt\n    run: 'printf packages'\n    on: [linux, macos]\n",
		"limits:\n  maxFileMB: 12.5\nname: tester\npersona: none\nupdate: auto\nai:\n  future: [true, 3]\n",
	}
	for _, src := range valid {
		cfg, err := config.Parse([]byte(src), "homebot.yml")
		if err != nil || len(cfg.Warnings) != 0 {
			t.Fatalf("loader rejected valid %q: %v, warnings %v", src, err, cfg.Warnings)
		}
		var value any
		if err := yaml.Unmarshal([]byte(src), &value); err != nil {
			t.Fatal(err)
		}
		if err := s.Validate(jsonValue(t, value)); err != nil {
			t.Errorf("schema rejected valid %q: %v", src, err)
		}
	}
	invalid := []string{
		"updates: auto\n", "autosave:\n  commit: sometimes\n",
		"autosave:\n  push: maybe\n", "profiles:\n  hosts:\n    laptop: 2\n",
		"name: x\n", "persona: ../x\n", "limits:\n  maxFileMB: -1\n",
		"exports:\n  - name: packages\n    out: manifests/linux/packages.txt\n    run: echo\n    on: windows\n",
	}
	for _, src := range invalid {
		if _, err := config.Parse([]byte(src), "homebot.yml"); err == nil {
			t.Errorf("loader accepted invalid %q", src)
		}
		var value any
		if err := yaml.Unmarshal([]byte(src), &value); err != nil {
			t.Fatal(err)
		}
		if err := s.Validate(jsonValue(t, value)); err == nil {
			t.Errorf("schema accepted invalid %q", src)
		}
	}
	if cfg, err := config.Parse([]byte("update: unexpected\n"), "homebot.yml"); err != nil || len(cfg.Warnings) != 1 {
		t.Fatalf("unknown update loader result: %+v, %v", cfg, err)
	}
	if err := s.Validate(map[string]any{"update": "unexpected"}); err == nil {
		t.Error("schema accepted an unknown update mode")
	}
}

func TestHomebotSchemaAcceptsNoWarningScriptFixtures(t *testing.T) {
	s := compiledSchema(t, homebotSchema())
	files, err := filepath.Glob("../../testdata/script/*.txtar")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, path := range files {
		archive, err := txtar.ParseFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range archive.Files {
			if filepath.Base(f.Name) != "homebot.yml" {
				continue
			}
			cfg, err := config.Parse(f.Data, "homebot.yml")
			if err != nil || len(cfg.Warnings) > 0 {
				continue
			}
			var value any
			if err := yaml.Unmarshal(f.Data, &value); err != nil {
				t.Fatal(err)
			}
			if err := s.Validate(jsonValue(t, value)); err != nil {
				t.Errorf("%s:%s: %v", path, f.Name, err)
			}
			count++
		}
	}
	if count == 0 {
		t.Fatal("no clean homebot.yml script fixtures checked")
	}
}

func TestPersonaSchemaAndBuiltins(t *testing.T) {
	s := compiledSchema(t, personaSchema())
	ids := persona.Builtins()
	if len(ids) == 0 {
		t.Fatal("no built-in personas")
	}
	for _, id := range ids {
		path := filepath.Join("../../internal/persona/builtin", id, "persona.toml")
		var value map[string]any
		if _, err := toml.DecodeFile(path, &value); err != nil {
			t.Fatal(err)
		}
		if err := s.Validate(jsonValue(t, value)); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
	var numericColour map[string]any
	if _, err := toml.Decode("schema = 1\nname = 'Tester'\nemoji = '🧪'\n[colors]\naccent = { light = '245', dark = 248 }\n", &numericColour); err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(jsonValue(t, numericColour)); err != nil {
		t.Errorf("schema rejected a string ANSI index accepted by the loader: %v", err)
	}
	bad := []byte("schema = 1\nname = 'Tester'\nemoji = '🧪'\n[lines]\n'not.a.voice.id' = ['hello']\n")
	f, ds := persona.Parse("tester.toml", bad)
	if f == nil {
		t.Fatalf("parse failed: %v", ds)
	}
	if ds := persona.Validate(nil, f, persona.ValidateOptions{}); !slices.ContainsFunc(ds, func(d persona.Diagnostic) bool { return d.Severity == persona.Error }) {
		t.Fatalf("validator accepted unknown line: %v", ds)
	}
	var value map[string]any
	if _, err := toml.Decode(string(bad), &value); err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(jsonValue(t, value)); err == nil {
		t.Fatal("schema accepted unknown line")
	}
}
