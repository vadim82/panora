//go:build unix

package omarchytheme

import (
	"fmt"
	"os"
	"syscall"
)

func directoryIdentity(info os.FileInfo) string {
	stat := info.Sys().(*syscall.Stat_t)
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
}
