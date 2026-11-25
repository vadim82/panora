package playback

import (
	"context"
	"slices"
)

// UpdateConfigurationRequest patches preferences only. Providers and input
// identities have dedicated operations; clients cannot replace raw TOML.
// Pointers distinguish omitted fields from explicit false and zero values.
type UpdateConfigurationRequest struct {
	Revision             string    `json:"revision"`
	Backend              *string   `json:"backend,omitempty"`
	Fit                  *string   `json:"fit,omitempty"`
	Upscale              *bool     `json:"upscale,omitempty"`
	Interval             *string   `json:"interval,omitempty"`
	RotationMode         *string   `json:"rotation_mode,omitempty"`
	CacheMaxImages       *int      `json:"cache_max_images,omitempty"`
	CacheMaxBytes        *int64    `json:"cache_max_bytes,omitempty"`
	MinWidth             *int      `json:"min_width,omitempty"`
	MinHeight            *int      `json:"min_height,omitempty"`
	Ratio                *string   `json:"ratio,omitempty"`
	RatioPresets         *[]string `json:"ratio_presets,omitempty"`
	RatioTolerancePixels *int      `json:"ratio_tolerance_pixels,omitempty"`
	MaxDepth             *int      `json:"max_depth,omitempty"`
}

func (s *Service) UpdateConfiguration(ctx context.Context, request UpdateConfigurationRequest) (Configuration, error) {
	if request.Backend == nil && request.Fit == nil && request.Upscale == nil && request.Interval == nil && request.RotationMode == nil && request.CacheMaxImages == nil && request.CacheMaxBytes == nil && request.MinWidth == nil && request.MinHeight == nil && request.Ratio == nil && request.RatioPresets == nil && request.RatioTolerancePixels == nil && request.MaxDepth == nil {
		return Configuration{}, &Error{Code: "invalid_request", Message: "provide at least one setting to update"}
	}
	var desktopUpdate *runtimeDesktopSettings
	result, err := s.editConfiguration(ctx, request.Revision, func(config *Config) (bool, error) {
		changed := setPreference(&config.Desktop.Backend, request.Backend)
		changed = setPreference(&config.Desktop.Fit, request.Fit) || changed
		changed = setPreference(&config.Desktop.Upscale, request.Upscale) || changed
		changed = setPreference(&config.Playback.Interval, request.Interval) || changed
		changed = setPreference(&config.Playback.RotationMode, request.RotationMode) || changed
		changed = setPreference(&config.Cache.MaxImages, request.CacheMaxImages) || changed
		changed = setPreference(&config.Cache.MaxBytes, request.CacheMaxBytes) || changed
		changed = setPreference(&config.Filters.MinWidth, request.MinWidth) || changed
		changed = setPreference(&config.Filters.MinHeight, request.MinHeight) || changed
		changed = setPreference(&config.Filters.Ratio, request.Ratio) || changed
		if request.RatioPresets != nil && !slices.Equal(config.Filters.RatioPresets, *request.RatioPresets) {
			config.Filters.RatioPresets = slices.Clone(*request.RatioPresets)
			changed = true
		}
		if pixels := request.RatioTolerancePixels; pixels != nil && *pixels != config.RatioTolerancePixels() {
			value := *pixels
			config.Filters.RatioTolerancePixels = &value
			changed = true
		}
		changed = setPreference(&config.Filters.MaxDepth, request.MaxDepth) || changed
		var err error
		desktopUpdate, err = s.prepareRuntimeDesktop(ctx, *config, request)
		if err != nil {
			return false, err
		}
		return changed, nil
	}, func(config Config) {
		if desktopUpdate != nil {
			s.applyRuntimeDesktop(*desktopUpdate)
		}
		s.applyRuntimePreferences(config, request)
	})
	if err == nil {
		ratio, _ := ParseRatioRule(result.Rules.Ratio, result.Rules.RatioPresets)
		_, result.RuleResolution = s.resolveRatioRule(ctx, ratio, result.Effective.Backend)
	}
	return result, err
}

func setPreference[T comparable](saved *T, update *T) bool {
	if update == nil || *saved == *update {
		return false
	}
	*saved = *update
	return true
}
