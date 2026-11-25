//go:build darwin || linux

package localfiles

import (
	"fmt"
	"os"
	"syscall"
)

// OpenRegular avoids hanging if a previously enumerated image becomes a FIFO.
// Validate the opened descriptor, not only an earlier pathname stat.
func OpenRegular(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("image must be a readable regular file")
	}
	return file, nil
}
