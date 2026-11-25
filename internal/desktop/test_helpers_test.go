package desktop

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Replace PATH so probes and setters can only invoke test executables.
func fakeBackendCommands(t *testing.T, scripts map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("OMARCHY_PATH", "/test/Omarchy $(literal)")
}

// Isolate native cache paths too: macOS ignores XDG_CACHE_HOME.
func setTestCache(t *testing.T, root string) string {
	t.Helper()
	if runtime.GOOS == "darwin" {
		t.Setenv("HOME", root)
		return filepath.Join(root, "Library", "Caches")
	}
	t.Setenv("XDG_CACHE_HOME", root)
	return root
}

func requireMacOS(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("macOS backend is intentionally unavailable on other platforms")
	}
}

func fakeOmarchy(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "omarchy"), []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func fakeNoctalia(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "noctalia"), []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func writeTestImage(t *testing.T, path string, width, height int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encodeErr := png.Encode(f, image.NewRGBA(image.Rect(0, 0, width, height)))
	closeErr := f.Close()
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}

type fakeWallpaperBackend struct {
	probe func(context.Context) error
}

func (b *fakeWallpaperBackend) Probe(ctx context.Context) error {
	return b.probe(ctx)
}

func (*fakeWallpaperBackend) SetWallpaper(context.Context, string, DisplayOptions) error {
	return nil
}
