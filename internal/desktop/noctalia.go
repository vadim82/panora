package desktop

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type NoctaliaBackend struct{}

// Probe checks readiness without changing the desktop.
func (NoctaliaBackend) Probe(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	output, err := runBackendCommand(ctx, "noctalia", "msg", "wallpaper-get")
	if err != nil {
		return err
	}
	path := strings.TrimSpace(output)
	// An empty path is valid for a fresh configuration. An IPC error or unknown
	// command response must not be mistaken for wallpaper state, even at exit 0.
	if path != "" && !filepath.IsAbs(path) && !strings.HasPrefix(path, "color:#") {
		return fmt.Errorf("Noctalia's wallpaper query did not return wallpaper state")
	}
	output, err = runBackendCommand(ctx, "noctalia", "config", "export", "full")
	if err != nil {
		return err
	}
	var config struct {
		Wallpaper struct {
			Enabled *bool `toml:"enabled"`
		} `toml:"wallpaper"`
	}
	if err := toml.Unmarshal([]byte(output), &config); err != nil {
		return fmt.Errorf("decode Noctalia wallpaper configuration: %w", err)
	}
	if config.Wallpaper.Enabled == nil {
		return fmt.Errorf("Noctalia's effective configuration does not report wallpaper.enabled")
	}
	if !*config.Wallpaper.Enabled {
		return fmt.Errorf("wallpaper rendering is disabled in Noctalia")
	}
	return nil
}

// SetWallpaper delegates rendering to the running Noctalia session.
func (NoctaliaBackend) SetWallpaper(ctx context.Context, path string, options DisplayOptions) error {
	if err := options.Validate(); err != nil {
		return err
	}
	if options.Fit != "" && options.Fit != "auto" || options.Upscale {
		return fmt.Errorf("Noctalia does not support Panora fit/upscale options; use --fit auto and configure Noctalia's display settings")
	}
	executable, err := exec.LookPath("noctalia")
	if err != nil {
		return fmt.Errorf("find Noctalia executable: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if _, err := runBackendCommand(ctx, executable, "msg", "wallpaper-set", path); err != nil {
		return fmt.Errorf("set wallpaper through Noctalia: %w", err)
	}
	return nil
}
