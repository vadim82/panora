package appservice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"syscall"

	"github.com/vadim82/panora/internal/control"
)

const archPackageMarker = "/var/lib/panora-package-lifetime"

// ArchPackageContext binds only the process launched by the stock unit to the
// installed package. Drop-ins and foreground instances retain their own lifetime.
func ArchPackageContext(ctx context.Context) (context.Context, func(), error) {
	if os.Getenv("PANORA_ARCH_UNIT") != systemdFragment || os.Getenv("SYSTEMD_EXEC_PID") != strconv.Itoa(os.Getpid()) {
		return ctx, func() {}, nil
	}
	socket, err := control.SocketPath("")
	if err != nil {
		return nil, nil, err
	}
	manager, err := NewSystemd(socket)
	if err != nil {
		return nil, nil, err
	}
	job, err := manager.job(ctx)
	if errors.Is(err, errCustomizedUnit) {
		return ctx, func() {}, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("verify installed Panora process: %w", err)
	}
	if job.pid != os.Getpid() {
		return nil, nil, errors.New("systemd did not identify this process as the packaged Panora service")
	}
	return watchPackageMarker(ctx, archPackageMarker, 0)
}

func watchPackageMarker(ctx context.Context, path string, owner uint32) (context.Context, func(), error) {
	marker, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	fd := int(marker.Fd())
	if err := syscall.Flock(fd, syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		marker.Close()
		return nil, nil, err
	}
	inotify, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)
	if err != nil {
		marker.Close()
		return nil, nil, err
	}
	events := os.NewFile(uintptr(inotify), "Panora package lifetime")
	closeFiles := func() {
		events.Close()
		marker.Close()
	}
	// Pin the watch to the same inode as the lock, including during reinstall.
	_, err = syscall.InotifyAddWatch(inotify, "/proc/self/fd/"+strconv.Itoa(fd), syscall.IN_ATTRIB)
	if err != nil {
		closeFiles()
		return nil, nil, err
	}
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		closeFiles()
		return nil, nil, err
	}
	if stat.Nlink == 0 {
		closeFiles()
		return nil, nil, os.ErrNotExist
	}
	if stat.Uid != owner || stat.Mode != syscall.S_IFREG|0644 || stat.Nlink != 1 {
		closeFiles()
		return nil, nil, errors.New("invalid Panora package lifetime file owner, mode or link count")
	}
	lifetime, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		var buffer [4096]byte
		for {
			if _, err := events.Read(buffer[:]); err != nil {
				return
			}
			if err := syscall.Fstat(fd, &stat); err != nil || stat.Nlink != 1 {
				return
			}
		}
	}()
	return lifetime, func() {
		events.Close()
		<-done
		marker.Close()
	}, nil
}
