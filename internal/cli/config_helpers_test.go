package cli

import (
	"github.com/pelletier/go-toml/v2"
	"github.com/vadim82/panora/internal/playback"
	"os"
	"path/filepath"
	"testing"
)

func writeNextConfig(t *testing.T, root string) string {
	t.Helper()
	config := playback.Config{Version: 1, Providers: []playback.ProviderConfig{{ID: "local", Enabled: true, Folders: []playback.FolderConfig{{ID: "home", Path: root}}}}}
	config.Desktop = playback.DesktopConfig{Backend: "auto", Fit: "auto"}
	config.Playback.Interval = "30m"
	config.Playback.RotationMode = playback.DefaultConfig().Playback.RotationMode
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
