package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacOSReadinessAndSelection(t *testing.T) {
	requireMacOS(t)
	fakeBackendCommands(t, map[string]string{"panora-macos-wallpaper": `test "$#" = 1 && test "$1" = probe || exit 9
printf 'ready\n'`})
	for _, name := range []string{"auto", "macos"} {
		cmd := NewRootCommand()
		var output bytes.Buffer
		cmd.SetOut(&output)
		cmd.SetArgs([]string{"doctor", "--config", writeDesktopConfig(t, name), "--socket", shortSocket(t)})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Backend: macos", "AppKit desktop query available", "current Spaces only", "no upscale by default"} {
			if !strings.Contains(output.String(), want) {
				t.Fatalf("output = %q, missing %q", output.String(), want)
			}
		}
	}
}

func TestRootSetSelectsMacOS(t *testing.T) {
	requireMacOS(t)
	record := filepath.Join(t.TempDir(), "args")
	t.Setenv("PANORA_TEST_ARGUMENTS", record)
	fakeBackendCommands(t, map[string]string{"panora-macos-wallpaper": `case "$1" in
probe) printf 'ready\n' ;;
set) printf '%s\n' "$@" > "$PANORA_TEST_ARGUMENTS"; printf 'ok\n' ;;
*) exit 9 ;;
esac`})
	path := filepath.Join(t.TempDir(), "wallpaper.png")
	writeScanImage(t, path, 2, 2)
	cmd := NewRootCommand()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"debug", "set", path, "--config", writeDesktopConfig(t, "auto")})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if want := "set\n" + path + "\ncontain\nfalse\n"; string(got) != want {
		t.Fatalf("arguments = %q, want %q", got, want)
	}
}
