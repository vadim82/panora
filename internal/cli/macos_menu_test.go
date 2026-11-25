//go:build darwin || linux

package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMacOSMenuOnlyResolvesFromBundledService(t *testing.T) {
	executable := "/Applications/Home Apps/Panora.app/Contents/MacOS/panora"
	if got := macOSMenuPath(executable); got != "/Applications/Home Apps/Panora.app/Contents/Helpers/Panora Menu.app/Contents/MacOS/panora-menu" {
		t.Fatalf("menu path = %q", got)
	}
	for _, path := range []string{"/usr/bin/panora", "/tmp/Some.app/panora", "/Applications/Panora.app/Contents/MacOS/tool"} {
		if got := macOSMenuPath(path); got != "" {
			t.Fatalf("selected a menu for %q", path)
		}
	}
}

func TestMacOSMenuSupervisionRestartsAndJoinsChild(t *testing.T) {
	root := t.TempDir()
	record := filepath.Join(root, "calls")
	t.Setenv("PANORA_META_MENU_TEST_RECORD", record)
	helper := filepath.Join(root, "menu with spaces")
	script := `#!/bin/sh
if [ "$#" -ne 4 ] || [ "$1" != "--socket" ] || [ "$3" != "--instance" ]; then exit 2; fi
if [ -f "$PANORA_META_MENU_TEST_RECORD" ]; then
    printf '%s\n' "$2|$4" >> "$PANORA_META_MENU_TEST_RECORD"
    exec /bin/sleep 30
fi
printf '%s\n' "$2|$4" > "$PANORA_META_MENU_TEST_RECORD"
exit 1
`
	if err := os.WriteFile(helper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		runMacOSMenu(ctx, helper, "/tmp/socket with spaces", "instance", io.Discard)
	}()
	deadline := time.Now().Add(4 * time.Second)
	for {
		raw, _ := os.ReadFile(record)
		if strings.Count(string(raw), "\n") == 2 {
			if strings.Count(string(raw), "/tmp/socket with spaces|instance\n") != 2 {
				t.Fatalf("unexpected launch args: %q", raw)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child was not restarted: %q", raw)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("parent did not join the menu child")
	}
}
