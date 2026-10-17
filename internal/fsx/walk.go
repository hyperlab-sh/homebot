package fsx

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// WalkFiles returns every regular file below dir as a slash-separated path
// relative to dir, sorted. It skips anything named .git, does not follow
// symlinks below dir, and returns nothing for a missing dir.
func WalkFiles(dir string) ([]string, error) {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var out []string
	var walk func(rel string) error
	walk = func(rel string) error {
		entries, err := os.ReadDir(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.Name() == ".git" {
				continue
			}
			child := e.Name()
			if rel != "" {
				child = rel + "/" + e.Name()
			}
			switch {
			case e.IsDir():
				if err := walk(child); err != nil {
					return err
				}
			case e.Type().IsRegular():
				out = append(out, child)
			}
		}
		return nil
	}
	if err := walk(""); err != nil {
		return nil, err
	}
	slices.Sort(out)
	return out, nil
}

// TreeSizeLimit is how many entries TreeSize looks at before it gives up.
const TreeSizeLimit = 20000

// TreeSize returns the size in bytes of a file or directory tree. It gives up
// after limit entries so a huge browser profile does not stall the UI; capped
// then says the size is a lower bound. Symlinks count as themselves, and
// unreadable directories count as empty.
func TreeSize(path string, limit int) (size int64, capped bool) {
	seen := 0
	stack := []string{path}
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		info, err := os.Lstat(p)
		if err != nil {
			continue
		}
		seen++
		if seen > limit {
			return size, true
		}
		if !info.IsDir() {
			size += info.Size()
			continue
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			continue
		}
		for _, e := range entries {
			stack = append(stack, filepath.Join(p, e.Name()))
		}
	}
	return size, false
}
