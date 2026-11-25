package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/control"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/playback"
)

func TestServeCreatesDefaultSettingsButReadOnlyCommandsDoNot(t *testing.T) {
	options, socket := serviceFixture(t) // Isolated config/state/cache home.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := playback.ConfigPath("")
	if err != nil {
		t.Fatal(err)
	}
	check := NewRootCommand()
	check.SetOut(io.Discard)
	check.SetErr(io.Discard)
	check.SetArgs([]string{"config", "check"})
	if err := check.Execute(); err == nil {
		t.Fatal("read-only check accepted a missing file")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("read-only command created settings")
	}
	var output bytes.Buffer
	var probes atomic.Int32
	cmd := newServeCommand(func(context.Context) (desktop.WallpaperBackend, error) {
		probes.Add(1)
		return &fakeWallpaperBackend{}, nil
	})
	cmd.SetOut(&output)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--socket", socket, "--state-file", options.Request.StatePath})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- cmd.ExecuteContext(ctx) }()
	t.Cleanup(cancel)
	status := awaitService(t, socket)
	if !status.SetupRequired || status.NextAt != nil || status.Current != nil {
		t.Fatalf("first startup = %+v", status)
	}
	if _, err := control.Request(t.Context(), socket, "next"); err == nil || !strings.Contains(err.Error(), "add or enable") {
		t.Fatalf("unconfigured action = %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first-run service did not stop")
	}
	if probes.Load() != 0 || !strings.Contains(output.String(), "Created default settings") {
		t.Fatalf("unexpected startup probes/output: %d %s", probes.Load(), output.String())
	}
	if config, _, err := playback.LoadConfig(path); err != nil || config.HasEnabledInputs() {
		t.Fatalf("default config = %+v %v", config, err)
	}
}

func TestConfigurationEditsPreserveSavedPreferencesUnderCLIOverrides(t *testing.T) {
	options, socket := serviceFixture(t)
	cmd := newServeCommand(func(context.Context) (desktop.WallpaperBackend, error) { return &fakeWallpaperBackend{}, nil })
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--config", options.Request.ConfigPath, "--state-file", options.Request.StatePath, "--socket", socket, "--paused", "--interval", "1h", "--fit", "contain", "--upscale"})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- cmd.ExecuteContext(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("shutdown=%v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("serve did not stop")
		}
	})
	awaitService(t, socket)
	before, err := control.ReadConfiguration(t.Context(), socket)
	if err != nil {
		t.Fatal(err)
	}
	if before.Saved.Interval != "30m" || before.Saved.Fit != "auto" || before.Saved.Upscale || before.Effective.Interval != "1h0m0s" || before.Effective.Fit != "contain" || !before.Effective.Upscale {
		t.Fatalf("overrides=%+v", before)
	}
	width := 1920
	after, err := control.UpdateConfiguration(t.Context(), socket, playback.UpdateConfigurationRequest{Revision: before.Revision, MinWidth: &width})
	if err != nil {
		t.Fatal(err)
	}
	if after.Saved != before.Saved || after.Effective != before.Effective || after.Rules.MinWidth != 1920 {
		t.Fatalf("rule edit persisted overrides: %+v", after)
	}
	saved, _, err := playback.LoadConfig(options.Request.ConfigPath)
	if err != nil || saved.Playback.Interval != "30m" || saved.Desktop.Fit != "auto" || saved.Desktop.Upscale {
		t.Fatalf("saved overrides=%+v %v", saved, err)
	}
}
