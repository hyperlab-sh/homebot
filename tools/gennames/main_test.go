package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/config"
)

func TestGenerateAndCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reserved-names.json")
	if err := run(false, path); err != nil {
		t.Fatal(err)
	}
	if err := run(true, path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value names
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	if value.Syntax.Pattern == "" || value.Syntax.MinLength != 2 || value.Syntax.MaxLength != 32 {
		t.Fatalf("missing name syntax: %+v", value.Syntax)
	}
	pattern, err := regexp.Compile(value.Syntax.Pattern)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "ab", "zork", "z-2", "z-", "2z", "Zork", "a_b", strings.Repeat("a", 33)} {
		valid := len(name) >= value.Syntax.MinLength && len(name) <= value.Syntax.MaxLength && pattern.MatchString(name)
		if valid != config.ValidName(name) {
			t.Errorf("browser syntax for %q = %v, Go = %v", name, valid, config.ValidName(name))
		}
	}
	for group, values := range map[string][]string{"homebot": value.Homebot, "shell": value.Shell, "tools": value.Tools} {
		if !slices.IsSorted(values) || len(values) == 0 {
			t.Errorf("%s = %q", group, values)
		}
	}
	if !slices.Contains(value.Homebot, "name") {
		t.Error("the new command is missing")
	}
	data[0] ^= 1
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(true, path); err == nil {
		t.Error("one-byte change was not detected")
	}
}
