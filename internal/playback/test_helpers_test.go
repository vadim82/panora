package playback

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/source"
)

func openTestService(t *testing.T, request Request, resolve Resolver, interval time.Duration, paused bool) *Service {
	t.Helper()
	s, err := OpenService(t.Context(), ServiceOptions{
		Request: request, Backend: "fake", Paused: paused,
		Interval: interval, IntervalChanged: interval != 0,
	}, resolve)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func writeNextConfig(t *testing.T, root string) string {
	t.Helper()
	config := Config{Version: 1, Providers: []ProviderConfig{{ID: "local", Enabled: true, Folders: []FolderConfig{{ID: "home", Path: root}}}}}
	config.Desktop = DesktopConfig{Backend: "auto", Fit: "auto"}
	config.Playback.Interval = "30m"
	config.Playback.RotationMode = rotationBalanceSources
	config.Cache.MaxImages = 20
	config.Cache.MaxBytes = 512 << 20
	config.Filters.MinWidth = 2
	config.Filters.MinHeight = 2
	config.Filters.Ratio = "any"
	data, err := toml.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

type fakeWallpaperBackend struct {
	calls   int
	ctx     context.Context
	path    string
	options desktop.DisplayOptions
	err     error
}

func (*fakeWallpaperBackend) Probe(context.Context) error { return nil }

func (b *fakeWallpaperBackend) SetWallpaper(ctx context.Context, path string, options desktop.DisplayOptions) error {
	b.calls++
	b.ctx = ctx
	b.path = path
	b.options = options
	return b.err
}

func (s *Service) currentOriginPath(ctx context.Context) string {
	index := s.state.Cursor
	if index < 0 || index >= len(s.state.History) {
		return ""
	}
	candidate, err := s.store.LookupCandidate(ctx, s.state.History[index].Identity)
	if err == nil && candidate.Content != nil && candidate.Content.Kind == "local" {
		return candidate.Content.Path
	}
	return ""
}

func (c Config) localFolders() ([]FolderConfig, string) {
	for _, p := range c.Providers {
		if p.Enabled && p.ID == "local" {
			return p.Folders, c.InputRevision("local", p.Folders[0].ID)
		}
	}
	return nil, ""
}

func discoverMetadata(ctx context.Context, store *catalog.Store, provider source.Provider, providerID, input, revision, scope string, config json.RawMessage) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return discoverMetadataWithCommit(ctx, store, provider, providerID, input, revision, scope, config, func(ctx context.Context, scan catalog.Scan, scope string, page source.Page) error {
		_, err := store.CommitPage(ctx, scan, scope, page)
		return err
	}, maxDiscoveryPages)
}

// refreshForTest prepares a catalog without coupling discovery to Action.
func refreshForTest(t *testing.T, s *Service) {
	t.Helper()
	config, _, err := LoadConfig(s.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	warnings := s.refreshCandidates(t.Context(), config)
	s.statusMu.Lock()
	s.status.Refreshing = false
	s.status.RefreshWarnings = warnings
	s.statusMu.Unlock()
}
