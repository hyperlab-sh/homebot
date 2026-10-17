// Command gencatalog turns internal/msg/catalog.yaml into typed accessors,
// one per message id, so an unknown id or a wrong argument fails to compile.
// It runs from internal/msg through go generate and writes, next to the
// catalog:
//
//   - messages_gen.go: the message table and the accessors;
//   - samples_gen_test.go: every accessor called with sample values, which
//     the catalog snapshot test renders.
//
// It refuses a catalog whose texts, placeholders or ids are inconsistent.
// Files whose content would not change are left alone.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/hyperlab-sh/homebot/internal/msg/catalog"
)

func main() {
	dir := flag.String("dir", ".", "the directory holding catalog.yaml, where the files are written")
	flag.Parse()
	if err := run(*dir); err != nil {
		fmt.Fprintln(os.Stderr, "gencatalog:", err)
		os.Exit(1)
	}
}

func run(dir string) error {
	files, err := generate(dir)
	if err != nil {
		return err
	}
	for name, content := range files {
		if err := writeIfChanged(filepath.Join(dir, name), content); err != nil {
			return err
		}
	}
	return nil
}

// generate reads dir/catalog.yaml and returns the generated files by name.
func generate(dir string) (map[string][]byte, error) {
	data, err := os.ReadFile(filepath.Join(dir, "catalog.yaml"))
	if err != nil {
		return nil, err
	}
	f, err := catalog.Load(data)
	if err != nil {
		return nil, err
	}
	as, err := accessors(f)
	if err != nil {
		return nil, fmt.Errorf("catalog.yaml:\n%w", err)
	}
	messages, err := messagesFile(as, newSampler(f))
	if err != nil {
		return nil, err
	}
	samples, err := samplesFile(as)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{"messages_gen.go": messages, "samples_gen_test.go": samples}, nil
}

func writeIfChanged(path string, content []byte) error {
	old, err := os.ReadFile(path)
	if err == nil && bytes.Equal(old, content) {
		return nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}
