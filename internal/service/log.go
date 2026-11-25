package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const LogLimit = 1 << 20

// Log keeps at most the current and previous 1 MiB files. It never logs provider wire data.
type Log struct {
	mu   sync.Mutex
	file *os.File
	path string
	size int64
}

func OpenLog(path string) (*Log, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("log path must be absolute")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0) {
		return nil, fmt.Errorf("existing service log must be a private regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("service log is not a regular file")
	}
	if info.Size() > LogLimit {
		tail := make([]byte, LogLimit)
		if _, err := file.ReadAt(tail, info.Size()-LogLimit); err != nil {
			file.Close()
			return nil, err
		}
		if err := file.Truncate(0); err != nil {
			file.Close()
			return nil, err
		}
		if _, err := file.Write(tail); err != nil {
			file.Close()
			return nil, err
		}
		info, err = file.Stat()
		if err != nil {
			file.Close()
			return nil, err
		}
	}
	return &Log{file: file, path: path, size: info.Size()}, nil
}
func (l *Log) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	total := len(p)
	for len(p) > 0 {
		if l.size >= LogLimit {
			if err := l.file.Close(); err != nil {
				return total - len(p), err
			}
			if err := os.Rename(l.path, l.path+".1"); err != nil {
				return total - len(p), err
			}
			file, err := os.OpenFile(l.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
			if err != nil {
				return total - len(p), err
			}
			l.file = file
			l.size = 0
		}
		count := min(int64(len(p)), LogLimit-l.size)
		n, err := l.file.Write(p[:count])
		l.size += int64(n)
		p = p[n:]
		if err != nil {
			return total - len(p), err
		}
	}
	return total, nil
}
func (l *Log) Close() error { l.mu.Lock(); defer l.mu.Unlock(); return l.file.Close() }
