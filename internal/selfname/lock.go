package selfname

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"

	"github.com/hyperlab-sh/homebot/internal/app"
)

// transactionLock serializes the whole record and link transaction across
// independent homebot processes. The file stays in place; the kernel releases
// the flock when the process exits, including after a crash.
func transactionLock(c *app.Context) (*os.File, error) {
	if err := os.MkdirAll(c.StateDir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(c.StateDir, "name.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}
