package fsx

import (
	"errors"
	"io/fs"
	"os"
)

// LstatIfExists is os.Lstat that returns nil info and no error when nothing
// is at path. Any other error is returned.
func LstatIfExists(path string) (fs.FileInfo, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return info, err
}

// IsDir reports whether path is a directory, following symlinks. Any error
// counts as no.
func IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
