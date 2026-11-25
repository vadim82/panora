//go:build linux

package cli

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/playback"
)

func omarchySessionFixture(t *testing.T) string {
	t.Helper()
	runtime := t.TempDir()
	if err := os.Chmod(runtime, 0700); err != nil {
		t.Fatal(err)
	}
	display := "wayland-test"
	listener, err := net.Listen("unix", filepath.Join(runtime, display))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	t.Setenv("WAYLAND_DISPLAY", display)
	t.Setenv("OMARCHY_PATH", runtime)
	t.Setenv("PATH", runtime)
	readyFile := filepath.Join(runtime, "ready")
	t.Setenv("PANORA_TEST_READY_FILE", readyFile)

	for name, script := range map[string]string{
		"omarchy":       "exit 1",
		"omarchy-shell": `[ "$*" = "shell ping" ] && printf 'ok\n'`,
		"qs":            `[ -f "$PANORA_TEST_READY_FILE" ] && printf 'target background\nfunction set(path: string): void\n'`,
	} {
		if err := os.WriteFile(filepath.Join(runtime, name), []byte("#!/bin/sh\n"+script+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return readyFile
}

func TestOmarchySessionWaitsForHandler(t *testing.T) {
	readyFile := omarchySessionFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := checkOmarchySession(ctx); err == nil || !strings.Contains(err.Error(), "background IPC handler") {
		t.Fatalf("missing handler: %v", err)
	}
	published := make(chan error, 1)
	timer := time.AfterFunc(250*time.Millisecond, func() {
		published <- os.WriteFile(readyFile, nil, 0600)
	})
	defer timer.Stop()
	if err := waitForOmarchySession(ctx, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := <-published; err != nil {
		t.Fatal(err)
	}
}

func TestOmarchySessionMissingEnvironment(t *testing.T) {
	runtime := t.TempDir()
	if err := os.Chmod(runtime, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	t.Setenv("OMARCHY_PATH", "")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := waitForOmarchySession(ctx, 30*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "OMARCHY_PATH") {
		t.Fatalf("readiness: %v", err)
	}
	cancel()
	if err := waitForOmarchySession(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled readiness: %v", err)
	}
}

func TestSystemdStartupNotification(t *testing.T) {
	readyFile := omarchySessionFixture(t)
	if err := os.WriteFile(readyFile, nil, 0600); err != nil {
		t.Fatal(err)
	}
	options, socket := serviceFixture(t)
	path := filepath.Join(t.TempDir(), "notify.sock")
	notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer notify.Close()
	t.Setenv("NOTIFY_SOCKET", path)
	if err := announceSystemdReady(t.Context(), socket); err == nil {
		t.Fatal("readiness announced with no control API")
	}
	if err := notify.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	command := newServeCommand(func(context.Context) (desktop.WallpaperBackend, error) { return &fakeWallpaperBackend{}, nil })
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"--omarchy-session", "--paused", "--config", options.Request.ConfigPath,
		"--socket", socket, "--state-file", options.Request.StatePath})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- command.ExecuteContext(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("managed service did not stop")
		}
	})
	var message [128]byte
	n, _, err := notify.ReadFromUnix(message[:])
	if err != nil || string(message[:n]) != "READY=1" {
		t.Fatalf("notification: %q, %v", message[:n], err)
	}
	if status, err := control.Request(t.Context(), socket, ""); err != nil || status.InstanceID == "" {
		t.Fatalf("control API after READY: %+v, %v", status, err)
	}
}

func TestOmarchyFirstRunStartsWithTheActiveTheme(t *testing.T) {
	readyFile := omarchySessionFixture(t)
	if err := os.WriteFile(readyFile, nil, 0600); err != nil {
		t.Fatal(err)
	}
	options, socket := serviceFixture(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("NOTIFY_SOCKET", "")
	t.Setenv("PANORA_ARCH_UNIT", "")
	introReady := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "intro-ready")
	t.Setenv("PANORA_TEST_INTRO_READY", introReady)
	shell := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "omarchy-shell")
	if err := os.WriteFile(shell, []byte(`#!/bin/sh
case "$2" in
  ping) printf 'ok\n' ;;
  themeIntroStatus) [ -f "$PANORA_TEST_INTRO_READY" ] && printf 'ready\n' ;;
esac
`), 0700); err != nil {
		t.Fatal(err)
	}
	path, err := playback.ConfigPath("")
	if err != nil {
		t.Fatal(err)
	}
	launch := func(t *testing.T) context.CancelFunc {
		t.Helper()
		command := newServeCommand(func(context.Context) (desktop.WallpaperBackend, error) { return &fakeWallpaperBackend{}, nil })
		command.SetOut(io.Discard)
		command.SetErr(io.Discard)
		command.SetArgs([]string{"--omarchy-session", "--pause-for-setup", "--socket", socket, "--state-file", options.Request.StatePath})
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- command.ExecuteContext(ctx) }()
		t.Cleanup(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(5 * time.Second):
				t.Error("managed service did not stop")
			}
		})
		return cancel
	}
	await := func(t *testing.T, ready func(control.Status) bool) control.Status {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		ticker := time.NewTicker(15 * time.Millisecond)
		defer ticker.Stop()
		for {
			status, err := control.Request(ctx, socket, "")
			if err == nil && ready(status) {
				return status
			}
			select {
			case <-ctx.Done():
				t.Fatalf("service status did not meet expectation: %+v, %v", status, err)
			case <-ticker.C:
			}
		}
	}
	t.Run("automatically accepts one theme still after the native intro", func(t *testing.T) {
		launch(t)
		status := awaitService(t, socket)
		if status.SetupRequired || status.Paused || status.Current != nil {
			t.Fatalf("fresh startup without an active theme: %+v", status)
		}
		config, _, err := playback.LoadConfig(path)
		if err != nil || config.OmarchyTheme == nil || !config.OmarchyTheme.Enabled || config.OmarchyTheme.ImageRules != nil || len(config.Providers) != 0 {
			t.Fatalf("fresh Omarchy settings: %+v, %v", config, err)
		}
		current := filepath.Join(os.Getenv("HOME"), ".local", "state", "omarchy", "current")
		backgrounds := filepath.Join(current, "theme", "backgrounds")
		if err := os.MkdirAll(backgrounds, 0700); err != nil {
			t.Fatal(err)
		}
		writeScanImage(t, filepath.Join(backgrounds, "theme.png"), 16, 9)
		if err := os.WriteFile(filepath.Join(current, "theme.name"), []byte("first-theme\n"), 0600); err != nil {
			t.Fatal(err)
		}
		status = await(t, func(status control.Status) bool { return status.OmarchyThemeName == "First Theme" })
		if status.Current != nil || status.HistoryLength != 0 {
			t.Fatalf("playback started before the native intro finished: %+v", status)
		}
		if err := os.WriteFile(introReady, nil, 0600); err != nil {
			t.Fatal(err)
		}
		status = await(t, func(status control.Status) bool { return status.Current != nil })
		if status.Current.SourceID != config.OmarchyTheme.ID || status.HistoryLength != 1 || status.Paused || status.NextAt == nil || time.Until(*status.NextAt) < 29*time.Minute {
			t.Fatalf("first automatic theme wallpaper: %+v", status)
		}
	})
	t.Run("preserves theme removal on restart", func(t *testing.T) {
		const saved = "version = 1\n# Existing profile without a theme source.\n"
		if err := os.WriteFile(path, []byte(saved), 0600); err != nil {
			t.Fatal(err)
		}
		launch(t)
		status := awaitService(t, socket)
		if !status.SetupRequired || !status.Paused || status.HistoryLength != 1 {
			t.Fatalf("restart with an existing configuration: %+v", status)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != saved {
			t.Fatalf("existing settings changed: %q, %v", got, err)
		}
	})
}
