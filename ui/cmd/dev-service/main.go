// dev-service runs the real service with disposable state and a fake desktop
// setter for native client acceptance. It cannot change the desktop wallpaper.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/playback"
	"github.com/vadim82/panora/internal/service"
	"github.com/vadim82/panora/internal/source"
	"github.com/vadim82/panora/internal/sources/local"
	"github.com/vadim82/panora/internal/sources/wallhaven"
)

type fixtureDesktop struct{}

func (fixtureDesktop) Probe(context.Context) error { return nil }
func (fixtureDesktop) SetWallpaper(_ context.Context, path string, _ desktop.DisplayOptions) error {
	fmt.Printf("Fake desktop accepted %s\n", filepath.Base(path))
	return nil
}

func run(ctx context.Context, images string, interval time.Duration, display string) error {
	if images != "" && !filepath.IsAbs(images) {
		return fmt.Errorf("--images must be an absolute folder path")
	}
	if interval <= 0 {
		return fmt.Errorf("--interval must be positive")
	}
	geometry := desktop.Geometry{State: "unsupported", Displays: []desktop.DisplayGeometry{}}
	if display != "" {
		widthText, heightText, found := strings.Cut(display, "x")
		width, widthErr := strconv.Atoi(widthText)
		height, heightErr := strconv.Atoi(heightText)
		if !found || widthErr != nil || heightErr != nil || width < 1 || height < 1 || width > 16384 || height > 16384 {
			return fmt.Errorf("--display must be WIDTHxHEIGHT backing pixels, each between 1 and 16384")
		}
		geometry = desktop.Geometry{State: "known", Displays: []desktop.DisplayGeometry{{Name: "Review display", Width: width, Height: height, Scale: 1}}}
	}
	directory, err := os.MkdirTemp("/tmp", "panora-client-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	config := playback.DefaultConfig()
	config.Filters.MinWidth, config.Filters.MinHeight = 1, 1
	config.Playback.Interval = interval.String()
	if images != "" {
		config.Providers = []playback.ProviderConfig{{ID: "local", Enabled: true, Folders: []playback.FolderConfig{{ID: "review", Path: images}}}}
	}
	// Encode through the existing configuration serializer, never a user path.
	data, err := toml.Marshal(config)
	if err != nil {
		return err
	}
	configPath := filepath.Join(directory, "config.toml")
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return err
	}
	socket := filepath.Join(directory, "control.sock")
	fmt.Printf("Disposable native review service; all wallpaper calls use a fake setter.\nDisplay geometry is an explicit review fixture, not a screen measurement.\nSocket: %s\nState: %s\n", socket, directory)
	inspect := func(ctx context.Context, name string) (desktop.Capabilities, error) {
		if err := ctx.Err(); err != nil {
			return desktop.Capabilities{}, err
		}
		result := desktop.Capabilities{Version: 1, RequestedBackend: name, State: "unavailable", Backends: []desktop.BackendReadiness{{Name: "macos", Ready: true}, {Name: "omarchy"}, {Name: "noctalia"}}, Presentations: []desktop.PresentationCapability{}, Geometry: geometry}
		if name == "auto" || name == "macos" {
			result.Backend, result.State, result.AssignmentScope = "macos", "partial", "all_connected_screens_current_spaces"
			result.Presentations = []desktop.PresentationCapability{{Fit: "auto", Available: true, Upscale: true}, {Fit: "contain", Available: true, Upscale: true}, {Fit: "crop", Available: true}}
		}
		return result, nil
	}
	resolve := func(ctx context.Context, name string) (desktop.WallpaperBackend, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if name != "auto" && name != "macos" {
			return nil, fmt.Errorf("the review fixture supports only auto/macOS")
		}
		return fixtureDesktop{}, nil
	}
	options := playback.ServiceOptions{Request: playback.Request{ConfigPath: configPath, StatePath: filepath.Join(directory, "state.db"), CachePath: filepath.Join(directory, "cache"), Source: local.Provider{}, Providers: map[string]source.Provider{"wallhaven": wallhaven.New()}, Options: config.Desktop.Options(), InspectCapabilities: inspect, ResolveBackend: resolve}, Paused: true, Backend: config.Desktop.Backend}
	return service.Run(ctx, options, func(context.Context) (desktop.WallpaperBackend, error) { return fixtureDesktop{}, nil }, socket, os.Stdout, os.Stderr, service.Hooks{})
}

func main() {
	images := flag.String("images", "", "optional absolute sample-image folder; omit to review first-run setup")
	interval := flag.Duration("interval", 30*time.Second, "automatic rotation interval after Resume")
	display := flag.String("display", "", "optional WIDTHxHEIGHT backing-pixel geometry fixture; never queries or changes the real display")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, *images, *interval, *display); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
