//go:build darwin || linux

package cache

import (
	"path/filepath"
	"syscall"
	"testing"

	"github.com/vadim82/panora/internal/source"
)

func TestMaterializeRejectsFIFO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "original")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	c := newCache(t, 0, 0)
	if _, err := c.Materialize(t.Context(), source.Content{MediaKind: "image", Kind: "local", Path: path}); err == nil {
		t.Fatal("FIFO accepted as a local original")
	}
}
