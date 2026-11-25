//go:build linux || darwin

package control

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/vadim82/panora/internal/filelock"
)

func SocketPath(override string) (string, error) {
	if override != "" {
		if !filepath.IsAbs(override) {
			return "", fmt.Errorf("socket path must be absolute")
		}
		return filepath.Clean(override), nil
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" && runtime.GOOS == "darwin" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(cache, "panora", "run", "control.sock"), nil
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("XDG_RUNTIME_DIR must be an absolute, private session directory; alternatively use --socket in a private directory")
	}
	if err := privateDirectory(dir); err != nil {
		return "", fmt.Errorf("unsafe XDG_RUNTIME_DIR: %w", err)
	}
	return filepath.Join(dir, "panora", "control.sock"), nil
}

func owned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func privateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || !owned(info) || info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("%q must be an owned directory with mode 0700 and no symlink", path)
	}
	return nil
}

func validateSocket(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("socket path must be absolute")
	}
	if err := privateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 || !owned(info) || info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("%q must be an owned private socket", path)
	}
	return nil
}

// Listener keeps a separate lock while cleaning up and owning the socket path.
type Listener struct {
	net.Listener
	closeOnce sync.Once
	closeErr  error
	path      string
	info      os.FileInfo
	release   func()
}

func Listen(path string) (*Listener, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("socket path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err := privateDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	release, err := filelock.Try(path + ".lock")
	if err != nil {
		return nil, fmt.Errorf("another service may own the control socket: %w", err)
	}
	success := false
	defer func() {
		if !success {
			release()
		}
	}()
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSocket == 0 || !owned(info) {
			return nil, fmt.Errorf("refusing to replace non-socket or unowned path %q", path)
		}
		connection, dialErr := net.DialTimeout("unix", path, 200*time.Millisecond)
		if dialErr == nil {
			connection.Close()
			return nil, fmt.Errorf("a live service already owns %q", path)
		}
		if !errors.Is(dialErr, syscall.ECONNREFUSED) && !errors.Is(dialErr, os.ErrNotExist) {
			return nil, fmt.Errorf("cannot establish that socket is stale: %w", dialErr)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	listener.SetUnlinkOnClose(false)
	if err := os.Chmod(path, 0600); err != nil {
		listener.Close()
		os.Remove(path)
		return nil, err
	}
	info, err = os.Lstat(path)
	if err != nil {
		listener.Close()
		return nil, err
	}
	success = true
	return &Listener{Listener: listener, path: path, info: info, release: release}, nil
}

func (l *Listener) Close() error {
	l.closeOnce.Do(func() {
		l.closeErr = l.Listener.Close()
		if info, err := os.Lstat(l.path); err == nil && os.SameFile(l.info, info) {
			if err := os.Remove(l.path); err != nil && l.closeErr == nil {
				l.closeErr = err
			}
		}
		l.release()
	})
	return l.closeErr
}
