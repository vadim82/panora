package cli

import (
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
