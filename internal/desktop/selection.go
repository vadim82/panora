package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Selection describes a backend and its display capabilities.
type Selection struct {
	Name      string
	Backend   WallpaperBackend
	Fit       string
	Readiness string
	Scope     string
}

// Select probes the registered backends and resolves an auto or explicit choice.
func Select(ctx context.Context, name string) (Selection, error) {
	return selectWallpaperBackend(ctx, name, wallpaperBackends())
}

// ValidateBackendName validates a saved choice without probing the desktop.
func ValidateBackendName(name string) error {
	switch name {
	case "auto", "omarchy", "noctalia", "macos":
		return nil
	default:
		return fmt.Errorf("unknown backend %q; choose auto, omarchy, noctalia, or macos", name)
	}
}

func wallpaperBackends() []Selection {
	return []Selection{
		{
			Name:      "omarchy",
			Backend:   OmarchyBackend{},
			Fit:       "centered Fit on black padding (requires one unrotated monitor; no upscale by default); --fit crop uses native global crop-to-fill",
			Readiness: "IPC available",
			Scope:     "all monitors",
		},
		{
			Name:      "macos",
			Backend:   MacOSBackend{},
			Fit:       "native containment on black padding (auto/contain), no upscale by default; crop fills the screen; visual verification pending",
			Readiness: "AppKit desktop query available",
			Scope:     "all connected screens; current Spaces only (other Spaces unverified)",
		},
		{
			Name:      "noctalia",
			Backend:   NoctaliaBackend{},
			Fit:       "controlled by Noctalia settings; visual verification pending",
			Readiness: "IPC available",
			Scope:     "all monitors",
		},
	}
}

func selectWallpaperBackend(ctx context.Context, name string, options []Selection) (Selection, error) {
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	var ready []Selection
	var failures []error
	for _, option := range options {
		if name != "auto" && option.Name != name {
			continue
		}
		if err := ctx.Err(); err != nil {
			return Selection{}, err
		}
		if err := option.Backend.Probe(ctx); err != nil {
			if name != "auto" {
				return Selection{}, fmt.Errorf("%s backend is not ready: %w", name, err)
			}
			failures = append(failures, fmt.Errorf("%s: %w", option.Name, err))
		} else {
			ready = append(ready, option)
		}
		if name != "auto" {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	switch len(ready) {
	case 1:
		return ready[0], nil
	case 0:
		if name != "auto" {
			return Selection{}, fmt.Errorf("unknown backend %q; choose auto, omarchy, noctalia, or macos", name)
		}
		// Join the underlying errors so callers can still inspect probe failures.
		return Selection{}, fmt.Errorf("no wallpaper backend is ready: %w", errors.Join(failures...))
	default:
		var names []string
		for _, option := range ready {
			names = append(names, option.Name)
		}
		return Selection{}, fmt.Errorf("multiple wallpaper backends are ready (%s); set desktop.backend in config.toml to an explicit choice", strings.Join(names, ", "))
	}
}
