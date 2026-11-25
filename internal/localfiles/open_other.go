//go:build !darwin && !linux

package localfiles

import (
	"fmt"
	"os"
)

func OpenRegular(string) (*os.File, error) {
	return nil, fmt.Errorf("bounded local image inspection is supported on macOS and Linux only")
}
