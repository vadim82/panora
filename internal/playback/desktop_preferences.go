package playback

import (
	"context"
	"time"

	"github.com/vadim82/panora/internal/desktop"
)

type runtimeDesktopSettings struct {
	backend string
	options desktop.DisplayOptions
}

// Called while actionMu is held. Readiness and presentation validation happen
// before publication; they never prepare an image or call a wallpaper setter.
func (s *Service) prepareRuntimeDesktop(ctx context.Context, config Config, request UpdateConfigurationRequest) (*runtimeDesktopSettings, error) {
	if request.Backend == nil && request.Fit == nil && request.Upscale == nil {
		return nil, nil
	}
	if err := desktop.ValidateBackendName(config.Desktop.Backend); err != nil {
		return nil, &Error{Code: "invalid_settings", Message: err.Error()}
	}
	if err := config.Desktop.Options().Validate(); err != nil {
		return nil, &Error{Code: "invalid_settings", Message: err.Error()}
	}
	if s.request.InspectCapabilities == nil || s.request.ResolveBackend == nil {
		return nil, &Error{Code: "unsupported", Message: "runtime desktop settings are unavailable"}
	}
	// Omitted fields retain their active values, including temporary CLI overrides.
	next := runtimeDesktopSettings{backend: s.backend, options: s.request.Options}
	if request.Backend != nil {
		next.backend = config.Desktop.Backend
	}
	if request.Fit != nil {
		next.options.Fit = config.Desktop.Fit
	}
	if request.Upscale != nil {
		next.options.Upscale = config.Desktop.Upscale
	}
	if err := next.options.Validate(); err != nil {
		return nil, &Error{Code: "invalid_settings", Message: err.Error()}
	}
	operation, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	inspect := func(name string) (desktop.Capabilities, error) {
		capabilities, err := s.request.InspectCapabilities(operation, name)
		if operation.Err() != nil {
			return desktop.Capabilities{}, operation.Err()
		}
		if err != nil {
			return desktop.Capabilities{}, &Error{Code: "backend_unavailable", Message: "could not inspect the requested desktop backend"}
		}
		return capabilities, nil
	}
	capabilities, err := inspect(next.backend)
	if err != nil {
		return nil, err
	}
	if err := validateDesktopSettings(capabilities, next); err != nil {
		return nil, err
	}
	// Temporary overrides must not hide unsupported persisted combinations. The
	// saved settings must also work after restart, when those overrides disappear.
	saved := runtimeDesktopSettings{backend: config.Desktop.Backend, options: config.Desktop.Options()}
	if saved.backend != next.backend {
		capabilities, err = inspect(saved.backend)
		if err != nil {
			return nil, err
		}
	}
	if err := validateDesktopSettings(capabilities, saved); err != nil {
		return nil, err
	}
	return &next, nil
}

func validateDesktopSettings(capabilities desktop.Capabilities, settings runtimeDesktopSettings) error {
	if capabilities.State == "ambiguous" {
		return &Error{Code: "backend_ambiguous", Message: "multiple wallpaper backends are ready; choose an explicit backend"}
	}
	if (capabilities.State != "ready" && capabilities.State != "partial") || capabilities.Backend == "" {
		return &Error{Code: "backend_unavailable", Message: "the requested desktop backend is unavailable"}
	}
	if settings.backend != "auto" && capabilities.Backend != settings.backend {
		return &Error{Code: "backend_unavailable", Message: "the requested desktop backend is unavailable"}
	}
	fit := settings.options.Fit
	if fit == "" {
		fit = "auto"
	}
	for _, presentation := range capabilities.Presentations {
		if presentation.Fit == fit && presentation.Available && (!settings.options.Upscale || presentation.Upscale) {
			return nil
		}
	}
	return &Error{Code: "unsupported_presentation", Message: "the requested Fit and upscaling options are unavailable on this desktop"}
}

// Publication has already succeeded (or its directory durability is uncertain).
// This changes only the next action's inputs, preserving the accepted wallpaper,
// persisted history, pause state and the existing rotation deadline.
func (s *Service) applyRuntimeDesktop(next runtimeDesktopSettings) {
	s.backend = next.backend
	s.request.Options = next.options
	s.statusMu.Lock()
	s.effectiveSettings.Backend = next.backend
	s.effectiveSettings.Fit = next.options.Fit
	s.effectiveSettings.Upscale = next.options.Upscale
	s.statusMu.Unlock()
	s.publishStatus()
}
