package msg_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every catalog entry is spoken by some Go code, or documents text that lives
// outside Go. An entry nobody calls is a promise the catalog cannot keep:
// editing it changes nothing, and the code it duplicates keeps its own words
// (fsutil.error.bad_json once sat unused while fsx hard-coded the sentence).
//
// script.* and shell.* are the text of the bundled action scripts and the zsh
// integration, which echo their own words; the catalog keeps them as the
// reference for those files.
func TestEveryCatalogEntryHasACaller(t *testing.T) {
	gen, err := os.ReadFile("messages_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	accessors := regexp.MustCompile(`(?m)^// (\w+) is ([a-z_0-9.]+):`).FindAllStringSubmatch(string(gen), -1)
	if len(accessors) == 0 {
		t.Fatal("no accessors found in messages_gen.go")
	}

	var code strings.Builder
	err = filepath.WalkDir(filepath.Join("..", ".."), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".worktrees", "node_modules", "src", "test":
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "messages_gen.go" {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		code.Write(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	text := code.String()

	for _, a := range accessors {
		name, id := a[1], a[2]
		if strings.HasPrefix(id, "script.") || strings.HasPrefix(id, "shell.") {
			continue
		}
		if !regexp.MustCompile(`\b` + name + `\b`).MatchString(text) {
			t.Errorf("%s (%s) has no caller: wire it in where the words are said, or delete the entry", name, id)
		}
	}
}
