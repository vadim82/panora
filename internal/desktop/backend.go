package desktop

import (
	"context"
	"fmt"
)

// WallpaperBackend checks readiness and applies images through a desktop integration.
type WallpaperBackend interface {
	// Probe checks readiness without changing the desktop. Nil means ready.
	Probe(ctx context.Context) error
	SetWallpaper(ctx context.Context, path string, options DisplayOptions) error
}

// DisplayOptions controls image fit and optional enlargement.
// Auto uses centered containment on Omarchy/macOS and the existing settings on Noctalia.
type DisplayOptions struct {
	Fit     string
	Upscale bool
}

// Validate checks supported fit modes and option combinations.
func (options DisplayOptions) Validate() error {
	switch options.Fit {
	case "", "auto", "contain", "crop":
	default:
		return fmt.Errorf("unknown fit mode %q; choose auto, contain, or crop", options.Fit)
	}
	if options.Fit == "crop" && options.Upscale {
		return fmt.Errorf("upscaling applies to containment; crop controls its own scaling")
	}
	return nil
}
