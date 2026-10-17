package persona

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Names are data owned by builtin/, so replacing a built-in does not require
// hunting through Go code or test fixtures for a character's name.
func TestNamesStayInBuiltin(t *testing.T) {
	root := filepath.Join("..", "..")
	patterns := denylist(t)
	for _, id := range Builtins() {
		if id != Default {
			patterns = append(patterns, regexp.MustCompile(`(?i)\b`+regexp.QuoteMeta(id)+`\b`))
		}
	}
	err := filepath.WalkDir(root, func(path string, item os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if item.IsDir() {
			if item.Name() == ".git" || item.Name() == ".worktrees" || item.Name() == "vendor" || rel == filepath.Join("internal", "persona", "builtin") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") && !strings.Contains(filepath.ToSlash(rel), "/testdata/") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, pattern := range patterns {
			if pattern.Match(data) {
				t.Errorf("%s contains a built-in name or forbidden phrase (%s)", rel, pattern.String())
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
