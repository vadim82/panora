package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorCommandIsReadOnly(t *testing.T) {
	fakeBackendCommands(t, map[string]string{
		"omarchy":       "exit 9 # the setter must not be called\n",
		"omarchy-shell": "echo ok\n",
		"qs":            "printf 'target background\n  function set(path: string): void\n'\n",
	})
	cmd := NewRootCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"doctor", "--config", writeDesktopConfig(t, "auto"), "--socket", shortSocket(t)})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Backend: omarchy", "Scope: all monitors", "crop-to-fill"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, missing %q", output.String(), want)
		}
	}
}

func TestDebugSetSelectsOmarchy(t *testing.T) {
	record := filepath.Join(t.TempDir(), "arguments")
	t.Setenv("PANORA_TEST_ARGUMENTS", record)
	fakeBackendCommands(t, map[string]string{
		"omarchy":       `printf '%s\n' "$@" > "$PANORA_TEST_ARGUMENTS"`,
		"omarchy-shell": "echo ok\n",
		"qs":            "printf 'target background\n  function set(path: string): void\n'\n",
	})
	path := filepath.Join(t.TempDir(), "wallpaper $(literal).png")
	writeScanImage(t, path, 2, 2)
	cmd := NewRootCommand()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"debug", "set", path, "--config", writeDesktopConfig(t, "omarchy"), "--fit", "crop"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if want := "theme\nbg\nset\n" + path + "\n"; string(got) != want {
		t.Fatalf("arguments = %q, want %q", got, want)
	}
}

func TestDebugSetSelectsNoctalia(t *testing.T) {
	record := filepath.Join(t.TempDir(), "arguments")
	t.Setenv("PANORA_TEST_ARGUMENTS", record)
	t.Setenv("XDG_CURRENT_DESKTOP", "niri")
	fakeBackendCommands(t, map[string]string{"noctalia": `case "$*" in
"msg wallpaper-get") printf '/current.png\n' ;;
"config export full") printf '[wallpaper]\nenabled = true\n' ;;
*) printf '%s\n' "$@" > "$PANORA_TEST_ARGUMENTS" ;;
esac`})
	path := filepath.Join(t.TempDir(), "wallpaper $(literal).png")
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
	if want := "msg\nwallpaper-set\n" + path + "\n"; string(got) != want {
		t.Fatalf("arguments = %q, want %q", got, want)
	}
}

func TestDebugSetRejectsUnavailableOrAmbiguousBackends(t *testing.T) {
	for _, request := range []string{"auto", "noctalia", "other"} {
		t.Run(request, func(t *testing.T) {
			record := filepath.Join(t.TempDir(), "called")
			t.Setenv("PANORA_TEST_MARKER", record)
			t.Setenv("XDG_CURRENT_DESKTOP", "niri")
			// Both shells are available. Explicit Noctalia fails its query; auto
			// sees both ready and must not choose one based on the desktop name.
			t.Setenv("PANORA_TEST_REQUEST", request)
			fakeBackendCommands(t, map[string]string{
				"omarchy":       `printf called > "$PANORA_TEST_MARKER"`,
				"omarchy-shell": "echo ok\n",
				"qs":            "printf 'target background\n  function set(path: string): void\n'\n",
				"noctalia": `case "$*" in
"msg wallpaper-get")
  test "$PANORA_TEST_REQUEST" != noctalia || exit 7
  printf '/current.png\n' ;;
"config export full") printf '[wallpaper]\nenabled = true\n' ;;
*) printf called > "$PANORA_TEST_MARKER" ;;
esac`,
			})
			path := filepath.Join(t.TempDir(), "wallpaper.png")
			writeScanImage(t, path, 2, 2)
			cmd := NewRootCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetArgs([]string{"debug", "set", path, "--config", writeDesktopConfig(t, request)})
			if err := cmd.Execute(); err == nil {
				t.Fatal("expected backend selection error")
			}
			if output.Len() != 0 {
				t.Fatalf("unexpected success output: %q", output.String())
			}
			if _, err := os.Stat(record); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("setter invoked or marker check failed: %v", err)
			}
		})
	}
}

func TestDesktopNameDoesNotImplyBackendReadiness(t *testing.T) {
	t.Setenv("XDG_CURRENT_DESKTOP", "niri")
	fakeBackendCommands(t, nil)
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"doctor", "--config", writeDesktopConfig(t, "auto"), "--socket", shortSocket(t)})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "setup checks failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScanAndHelpDoNotRequireDesktop(t *testing.T) {
	fakeBackendCommands(t, nil)
	for _, args := range [][]string{{"--help"}, {"debug", "set", "--help"}, {"scan", t.TempDir(), "--config", writeDesktopConfig(t, "auto")}} {
		cmd := NewRootCommand()
		cmd.SetOut(new(bytes.Buffer))
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("args %v: %v", args, err)
		}
	}
}

func writeDesktopConfig(t *testing.T, backend string) string {
	t.Helper()
	path := writeNextConfig(t, t.TempDir())
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(string(data), "backend = 'auto'", fmt.Sprintf("backend = %q", backend), 1)
	if body == string(data) {
		body = strings.Replace(string(data), `backend = "auto"`, fmt.Sprintf("backend = %q", backend), 1)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
