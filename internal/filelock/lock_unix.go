//go:build darwin || linux

package filelock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Try holds a lock until release or process exit. Never unlink the lock file:
// separate inodes would allow two processes to believe they own the same lock.
func Try(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, fmt.Errorf("%s: %w", path, ErrBusy)
		}
		return nil, err
	}
	return func() { f.Close() }, nil
}
