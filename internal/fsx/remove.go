package fsx

import (
	"os"
	"path/filepath"
)

// RemoveAll removes path and everything in it, as os.RemoveAll does. A
// folder without write permission (Go's module cache has them, and so may
// what a command writes) holds up os.RemoveAll, so when it fails the folders
// get u+rwx and it tries once more.
func RemoveAll(path string) error { return removeAllWith(path, os.RemoveAll) }

func removeAllWith(path string, rm func(string) error) error {
	if err := rm(path); err == nil {
		return nil
	}
	MakeWritable(path)
	return rm(path)
}

// MakeWritable gives path and every folder under it u+rwx. It never follows
// a link, and ignores what it cannot change.
func MakeWritable(path string) {
	st, err := os.Lstat(path)
	if err != nil || !st.IsDir() {
		return
	}
	_ = os.Chmod(path, st.Mode().Perm()|0o700)
	entries, _ := os.ReadDir(path)
	for _, e := range entries {
		MakeWritable(filepath.Join(path, e.Name()))
	}
}
