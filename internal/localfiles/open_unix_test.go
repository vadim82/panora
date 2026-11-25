//go:build darwin || linux

package localfiles

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestOpenRegularRejectsFIFOWithoutWaitingForWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "replaced.png")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		file, err := OpenRegular(path)
		if file != nil {
			file.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("opened a FIFO as a regular image")
		}
	case <-time.After(time.Second):
		t.Fatal("image inspection waited for a FIFO writer")
	}
}
