//go:build !darwin && !linux

package filelock

import "fmt"

func Try(path string) (func(), error) {
	return nil, fmt.Errorf("process locking is currently supported on macOS and Linux only")
}
