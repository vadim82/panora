package cli

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/playback"
)

func TestAppLaunchPausesFreshSetupAndWritesPrivateLog(t *testing.T) {
	options, socket := serviceFixture(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if _, err := playback.InitializeDefaultConfig(path, false); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "logs", "service.log")
	command := newServeCommand(func(context.Context) (desktop.WallpaperBackend, error) {
		t.Error("fresh setup probed desktop")
		return &fakeWallpaperBackend{}, nil
	})
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"--config", path, "--socket", socket, "--state-file", options.Request.StatePath, "--pause-for-setup", "--log-file", logPath})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- command.ExecuteContext(ctx) }()
	status := awaitService(t, socket)
	if !status.SetupRequired || !status.Paused || status.NextAt != nil {
		t.Fatalf("fresh app startup: %+v", status)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("service did not stop")
	}
	log, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(log), "Panora service listening") {
		t.Fatalf("log: %q %v", log, err)
	}
	if info, err := os.Stat(logPath); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private log: %v %v", info, err)
	}
}

func TestAppLaunchKeepsConfiguredPauseAcrossRestart(t *testing.T) {
	for _, paused := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "paused"}[paused], func(t *testing.T) {
			options, socket := serviceFixture(t)
			for attempt := range 2 {
				command := newServeCommand(func(context.Context) (desktop.WallpaperBackend, error) { return &fakeWallpaperBackend{}, nil })
				command.SetOut(io.Discard)
				command.SetErr(io.Discard)
				args := []string{"--config", options.Request.ConfigPath, "--socket", socket, "--state-file", options.Request.StatePath, "--pause-for-setup"}
				if attempt == 0 && paused {
					args = append(args, "--paused")
				}
				command.SetArgs(args)
				ctx, cancel := context.WithCancel(t.Context())
				done := make(chan error, 1)
				go func() { done <- command.ExecuteContext(ctx) }()
				status := awaitService(t, socket)
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("shutdown timed out")
				}
				if status.SetupRequired || status.Paused != paused {
					t.Fatalf("configured launch %d: %+v", attempt, status)
				}
			}
		})
	}
}

func TestInvalidAppSettingsRemainUntouchedAndSecretsStayOutOfLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	before := []byte("version = [\nsecret-test-value")
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "service.log")
	command := newServeCommand(nil)
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	command.SetArgs([]string{"--config", path, "--pause-for-setup", "--log-file", logPath})
	if err := command.ExecuteContext(t.Context()); err == nil {
		t.Fatal("invalid settings accepted")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatal("invalid settings replaced")
	}
	log, err := os.ReadFile(logPath)
	if err != nil || strings.Contains(string(log), "secret-test-value") || !strings.Contains(string(log), "Existing settings were kept") {
		t.Fatalf("unsafe/unhelpful log %q %v", log, err)
	}
}
