package msg_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Command examples in Go code must use the effective name. The guard's shim
// and the shell integration deliberately invoke the stable homebot binary.
func TestGoCommandLiteralsUseSelf(t *testing.T) {
	if !literalCommand.MatchString("homebot pack") || literalCommand.MatchString("{self} pack") {
		t.Fatal("the Go literal detector does not distinguish its fixture")
	}
	root := filepath.Clean("..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == filepath.Join(root, "msg") {
				return filepath.SkipDir // catalog messages are checked separately
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(lit.Value)
			if path == filepath.Join(root, "inbox", "inbox.go") && value == "homebot " {
				return true // FixText is stored and must stay machine-neutral
			}
			if path == filepath.Join(root, "cli", "setup_steps.go") && value == `eval "$(homebot shell zsh)"` {
				return true // .zshrc must invoke the stable command before the named link is on PATH
			}
			if err == nil && literalCommand.MatchString(value) {
				t.Errorf("%s: literal command %q", fset.Position(lit.Pos()), value)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
