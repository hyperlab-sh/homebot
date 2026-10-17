package fsx

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
)

// tempName is the name WriteFileAtomic and PublishFile give their temp
// files: the target's name, ".tmp-" and a random base-36 number.
var tempName = regexp.MustCompile(`\.tmp-[0-9a-z]{1,7}$`)

// IsTempName reports whether name looks like one of this package's temp
// files, which a crash can leave behind.
func IsTempName(name string) bool {
	return tempName.MatchString(name)
}

// link is os.Link. Tests replace it to play a filesystem without hard
// links.
var link = os.Link

// PublishFile writes data to a new file at path, as WriteFileAtomic does,
// but never replaces anything: it fails with fs.ErrExist when path exists,
// even as a dangling symlink. It creates the parent directories.
//
// The temp file is published with link(2), which checks and creates the
// name in one step. A filesystem without hard links (FAT, exFAT, some
// network and FUSE mounts) refuses the link; there PublishFile checks that
// path is free and renames the temp file onto it. That leaves a small race:
// a file created at path between the check and the rename is replaced.
func PublishFile(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return err
	}
	f, err := createTemp(path, perm)
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = publish(tmp, path)
	}
	if err := errors.Join(err, removeIfExists(tmp)); err != nil {
		return err
	}
	return syncDir(dir)
}

// publish gives the file tmp the new name path, with a hard link where the
// filesystem has them, else with a rename after checking path is free.
func publish(tmp, path string) error {
	err := link(tmp, path)
	if !noHardLinks(err) {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return &fs.PathError{Op: "publish", Path: path, Err: fs.ErrExist}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.Rename(tmp, path)
}

// noHardLinks reports whether err is link(2) refusing because the
// filesystem makes no hard links there: EPERM (Linux on FAT), ENOTSUP or
// EOPNOTSUPP (macOS on FAT and exFAT, some FUSE mounts). EXDEV and EMLINK
// count too: they cannot be about a fresh temp file in the same folder, so
// they mean the same.
func noHardLinks(err error) bool {
	for _, errno := range []syscall.Errno{syscall.EPERM, syscall.ENOTSUP, syscall.EOPNOTSUPP, syscall.EXDEV, syscall.EMLINK} {
		if errors.Is(err, errno) {
			return true
		}
	}
	return false
}
