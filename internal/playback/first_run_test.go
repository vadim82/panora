package playback

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/sources/local"
)

type firstRunBackend struct{ calls atomic.Int32 }

func (*firstRunBackend) Probe(context.Context) error { return nil }
func (b *firstRunBackend) SetWallpaper(context.Context, string, desktop.DisplayOptions) error {
	b.calls.Add(1)
	return nil
}

func TestUnconfiguredServiceIdlesAndEnrollsWithoutRestart(t *testing.T) {
	root := t.TempDir()
	writeScanImage(t, filepath.Join(root, "wallpaper.png"), 2, 2)
	path := writeConfigBody(t, "version=1\n")
	backend := &firstRunBackend{}
	var resolves atomic.Int32
	request := Request{ConfigPath: path, StatePath: filepath.Join(t.TempDir(), "state.db"), CachePath: t.TempDir(), Source: local.Provider{}}
	service := openTestService(t, request, func(context.Context) (desktop.WallpaperBackend, error) {
		resolves.Add(1)
		return backend, nil
	}, 10*time.Millisecond, false)
	var reported atomic.Int32
	done := make(chan struct{})
	service.StartDiscovery()
	go func() { service.Run(func(error) { reported.Add(1) }); close(done) }()
	t.Cleanup(func() { service.Close(); <-done })
	time.Sleep(80 * time.Millisecond)
	status := service.Status()
	if !status.SetupRequired || status.NextAt != nil || status.Current != nil || status.Busy || status.LastError != nil || reported.Load() != 0 || resolves.Load() != 0 {
		t.Fatalf("empty setup attempted playback: %+v reports=%d resolves=%d", status, reported.Load(), resolves.Load())
	}
	if _, err := service.Action(t.Context(), "next"); err == nil || err.(*Error).Code != "setup_required" {
		t.Fatalf("manual Next did not explain setup: %v", err)
	}
	// Unavailable enrollment is retained as configuration, not erased or reset.
	unavailable := localTOML(filepath.Join(root, "unavailable")) + "[filters]\nmin_width=2\nmin_height=2\n"
	if err := os.WriteFile(path, []byte(unavailable), 0600); err != nil {
		t.Fatal(err)
	}
	awaitCondition(t, func() bool {
		s := service.Status()
		return !s.SetupRequired && len(s.Providers) == 1 && len(s.Providers[0].Inputs) == 1 && s.Providers[0].Inputs[0].Failure != nil
	})
	if backend.calls.Load() != 0 {
		t.Fatal("unavailable folder changed wallpaper")
	}
	configured := localTOML(root) + "[filters]\nmin_width=2\nmin_height=2\n"
	if err := os.WriteFile(path, []byte(configured), 0600); err != nil {
		t.Fatal(err)
	}
	awaitCondition(t, func() bool { return service.Status().Current != nil })
	if backend.calls.Load() == 0 || service.Status().SetupRequired {
		t.Fatal("existing scheduler did not enter playback after enrollment")
	}
	disabled := strings.Replace(configured, "enabled = true", "enabled = false", 1)
	if err := os.WriteFile(path, []byte(disabled), 0600); err != nil {
		t.Fatal(err)
	}
	awaitCondition(t, func() bool { s := service.Status(); return s.SetupRequired && !s.Busy })
	before := backend.calls.Load()
	time.Sleep(80 * time.Millisecond)
	status = service.Status()
	if backend.calls.Load() != before || status.Current == nil || status.HistoryLength == 0 || status.NextAt != nil {
		t.Fatalf("disabled inputs did not idle while retaining history: %+v", status)
	}
}

func TestZeroInputsAndDisabledProvidersAreValidSetup(t *testing.T) {
	for _, body := range []string{
		"version=1\n",
		"version=1\nproviders=[]\n",
		"version=1\n[[providers]]\nid='local'\nenabled=true\n",
		"version=1\n[[providers]]\nid='wallhaven'\nenabled=true\n",
		strings.Replace(localTOML(t.TempDir()), "enabled = true", "enabled = false", 1),
	} {
		config, _, err := LoadConfig(writeConfigBody(t, body))
		if err != nil || config.HasEnabledInputs() {
			t.Fatalf("unconfigured settings rejected: %s\n%+v %v", body, config, err)
		}
	}
}

func TestFreshDefaultsPlaySmallPortraitAndLandscapeWithoutFilterEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if created, err := InitializeDefaultConfig(path, false); err != nil || !created {
		t.Fatalf("initialize fresh settings: created=%v error=%v", created, err)
	}
	root := t.TempDir()
	writeScanImage(t, filepath.Join(root, "portrait.png"), 1, 2)
	writeScanImage(t, filepath.Join(root, "landscape.png"), 3, 1)
	backend := &firstRunBackend{}
	request := Request{ConfigPath: path, StatePath: filepath.Join(t.TempDir(), "state.db"), CachePath: t.TempDir(), Source: local.Provider{}, ResolveBackend: func(context.Context, string) (desktop.WallpaperBackend, error) { return backend, nil }}
	service := openTestService(t, request, func(context.Context) (desktop.WallpaperBackend, error) { return backend, nil }, time.Hour, true)
	service.StartDiscovery()
	config := settings(t, service)
	enrolled, err := service.AddFolder(t.Context(), AddFolderRequest{Revision: config.Revision, Path: root})
	if err != nil {
		t.Fatal(err)
	}
	if enrolled.Rules.MinWidth != 1 || enrolled.Rules.MinHeight != 1 || enrolled.Rules.Ratio != "any" {
		t.Fatalf("enrollment changed fresh image rules: %+v", enrolled.Rules)
	}
	awaitCondition(t, func() bool {
		status := service.Status()
		return len(status.Providers) == 1 && len(status.Providers[0].Inputs) == 1 && status.Providers[0].Inputs[0].State == "ready"
	})
	for range 2 {
		if _, err := service.Action(t.Context(), "next"); err != nil {
			t.Fatalf("default image rule rejected a small image: %v", err)
		}
	}
	history, err := service.History(t.Context(), "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Entries) != 2 || history.Entries[0].ImageID == history.Entries[1].ImageID || backend.calls.Load() != 2 {
		t.Fatalf("small images did not both play: %+v backend calls=%d", history, backend.calls.Load())
	}
	for _, entry := range history.Entries {
		if entry.Dimensions == nil || entry.Dimensions.Width > 3 || entry.Dimensions.Height > 2 {
			t.Fatalf("unexpected accepted image: %+v", entry)
		}
	}
}
