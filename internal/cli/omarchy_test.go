package cli

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootOmarchyDefaultsToFit(t *testing.T) {
	cache := t.TempDir()
	cache = setTestCache(t, cache)
	record := filepath.Join(t.TempDir(), "arguments")
	t.Setenv("PANORA_TEST_ARGUMENTS", record)
	fakeBackendCommands(t, map[string]string{
		"omarchy":       `printf '%s\n' "$@" > "$PANORA_TEST_ARGUMENTS"`,
		"omarchy-shell": "echo ok\n",
		"qs":            "printf 'target background\n  function set(path: string): void\n'\n",
		"hyprctl": `test "$#" = 2 && test "$1" = monitors && test "$2" = -j || exit 8
printf '[{"name":"DP-2","width":8,"height":4,"scale":1}]'`,
	})
	path := filepath.Join(t.TempDir(), "source $(literal).png")
	writeScanImage(t, path, 2, 2)
	cmd := NewRootCommand()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"debug", "set", path, "--config", writeDesktopConfig(t, "auto")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(args)), "\n")
	if len(lines) != 4 || strings.Join(lines[:3], " ") != "theme bg set" || !strings.HasPrefix(lines[3], filepath.Join(cache, "panora", "rendered")+string(os.PathSeparator)) {
		t.Fatalf("arguments = %q, want prepared cache path", args)
	}
	f, err := os.Open(lines[3])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil || img.Bounds() != image.Rect(0, 0, 8, 4) {
		t.Fatalf("canvas = %v, error = %v", img, err)
	}
}
