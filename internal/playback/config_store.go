package playback

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/vadim82/panora/internal/filelock"
	"github.com/vadim82/panora/internal/omarchytheme"
)

// JSON escaping can expand strings sixfold. Saved TOML and folder summaries
// repeat a path and its basename, so allow three escaped copies plus envelope.
const MaxConfigurationMessageBytes = 18*maxConfigBytes + 65536

type EffectiveSettings struct {
	Backend        string `json:"backend"`
	Fit            string `json:"fit"`
	Upscale        bool   `json:"upscale"`
	Interval       string `json:"interval"`
	RotationMode   string `json:"rotation_mode"`
	CacheMaxImages int    `json:"cache_max_images"`
	CacheMaxBytes  int64  `json:"cache_max_bytes"`
}

type ConfiguredFolder struct {
	ImageRules        *SourceImageRules `json:"image_rules"`
	EffectiveRules    SourceImageRules  `json:"effective_rules"`
	RuleResolution    RuleResolution    `json:"rule_resolution"`
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Path              string            `json:"path"`
	Enabled           bool              `json:"enabled"`
	InputEnabled      bool              `json:"input_enabled"`
	MaxDepth          *int              `json:"max_depth"`
	EffectiveMaxDepth int               `json:"effective_max_depth"`
	RefreshInterval   string            `json:"refresh_interval"`
}

type ImageRules struct {
	ExactResolutions     []string `json:"exact_resolutions,omitempty"`
	MinWidth             int      `json:"min_width"`
	MinHeight            int      `json:"min_height"`
	MaxDepth             int      `json:"max_depth"`
	Ratio                string   `json:"ratio"`
	RatioPresets         []string `json:"ratio_presets"`
	RatioTolerancePixels *int     `json:"ratio_tolerance_pixels,omitempty"`
}

type Configuration struct {
	OmarchyThemeAvailable bool                    `json:"omarchy_theme_available,omitempty"`
	OmarchyTheme          *ConfiguredOmarchyTheme `json:"omarchy_theme,omitempty"`
	Version               int                     `json:"version"`
	Revision              string                  `json:"revision"`
	ConfigPath            string                  `json:"config_path,omitempty"`
	CachePath             string                  `json:"cache_path,omitempty"`
	TOML                  string                  `json:"toml"`
	Effective             EffectiveSettings       `json:"effective"`
	Saved                 EffectiveSettings       `json:"saved"`
	RestartRequired       []string                `json:"restart_required"`
	ProviderEditors       []ProviderEditor        `json:"provider_editors,omitempty"`
	Inputs                []ConfiguredInput       `json:"inputs,omitempty"`
	Folders               []ConfiguredFolder      `json:"folders"`
	Searches              []ConfiguredSearch      `json:"searches"`
	Rules                 ImageRules              `json:"rules"`
	RuleResolution        RuleResolution          `json:"rule_resolution"`
}

type AddFolderRequest struct {
	ImageRules      *SourceImageRules `json:"image_rules,omitempty"`
	InheritRules    *bool             `json:"inherit_rules,omitempty"`
	Revision        string            `json:"revision"`
	Path            string            `json:"path"`
	Name            string            `json:"name,omitempty"`
	Enabled         *bool             `json:"enabled,omitempty"`
	MaxDepth        *int              `json:"max_depth,omitempty"`
	RefreshInterval string            `json:"refresh_interval,omitempty"`
}

// InitializeDefaultConfig publishes complete private defaults without replacing
// an existing file, including another starter's simultaneous publication.
func InitializeDefaultConfig(path string, omarchySession bool) (bool, error) {
	if _, err := os.Lstat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	config := DefaultConfig()
	if omarchySession {
		config.OmarchyTheme = newOmarchyThemeConfig(true, nil)
	}
	data, err := toml.Marshal(config)
	if err != nil {
		return false, err
	}
	name, err := stageConfig(path, append([]byte("# Created by Panora.\n"), data...))
	if err != nil {
		return false, err
	}
	defer os.Remove(name)
	if err := os.Link(name, path); errors.Is(err, os.ErrExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, syncConfigDirectory(path)
}

func configRevision(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func (s *Service) Configuration(ctx context.Context) (Configuration, error) {
	if err := ctx.Err(); err != nil {
		return Configuration{}, err
	}
	// Keep saved bytes and active values from straddling a runtime edit.
	s.configMu.RLock()
	config, ratio, data, err := ReadConfig(s.request.ConfigPath)
	if err != nil {
		s.configMu.RUnlock()
		return Configuration{}, invalidSavedConfiguration()
	}
	result := s.configuration(config, data)
	s.configMu.RUnlock()
	_, result.RuleResolution = s.resolveRatioRule(ctx, ratio, result.Effective.Backend)
	return result, nil
}

func (s *Service) configuration(config Config, data []byte) Configuration {
	folders := make([]ConfiguredFolder, 0)
	for _, provider := range config.Providers {
		for _, folder := range provider.Folders {
			folders = append(folders, ConfiguredFolder{
				ID: folder.ID, Name: folder.label(), Path: folder.Path,
				ImageRules: folder.ImageRules, EffectiveRules: effectiveSourceRules(config, folder.ImageRules), RuleResolution: staticRuleResolution(effectiveSourceRules(config, folder.ImageRules).Ratio),
				Enabled: provider.Enabled && folder.enabled(), InputEnabled: folder.enabled(),
				MaxDepth: folder.MaxDepth, EffectiveMaxDepth: folder.depth(config.Filters.MaxDepth),
				RefreshInterval: refreshInterval(provider.RefreshInterval, folder.RefreshInterval).String(),
			})
		}
	}
	s.statusMu.RLock()
	effective := s.effectiveSettings
	s.statusMu.RUnlock()
	saved := savedSettings(config)
	// Fresh playback reads the saved mode at the start of each action.
	effective.RotationMode = saved.RotationMode
	available := false
	if s.theme.home != "" {
		_, err := omarchytheme.ReadActive(s.theme.home)
		available = err == nil
	}
	configPath, _ := ConfigPath(s.request.ConfigPath)
	cachePath, _ := filepath.Abs(s.request.CachePath)
	return Configuration{OmarchyThemeAvailable: available, OmarchyTheme: configuredOmarchyTheme(config), Version: StatusVersion, Revision: configRevision(data), ConfigPath: configPath, CachePath: cachePath, TOML: string(data), Effective: effective, Saved: saved, RestartRequired: restartRequired(saved, effective), Folders: folders, Searches: configuredSearches(config),
		Rules: imageRules(config), RuleResolution: staticRuleResolution(config.Filters.Ratio),
		ProviderEditors: s.providerEditors(config), Inputs: s.configuredInputs(config)}
}

func imageRules(config Config) ImageRules {
	tolerance := config.RatioTolerancePixels()
	return ImageRules{MinWidth: config.Filters.MinWidth, MinHeight: config.Filters.MinHeight, MaxDepth: config.Filters.MaxDepth, Ratio: config.Filters.Ratio, RatioPresets: append([]string{}, config.Filters.RatioPresets...), RatioTolerancePixels: &tolerance}
}

// AddFolder enrolls a local input through the shared configuration writer.
// Clients provide a reviewed revision; IDs are generated independently of labels.
func (s *Service) AddFolder(ctx context.Context, request AddFolderRequest) (Configuration, error) {
	if err := ctx.Err(); err != nil {
		return Configuration{}, err
	}
	if !validFolderPath(request.Path) {
		return Configuration{}, &Error{Code: "invalid_request", Message: "folder path must be absolute, contain no NUL and be at most 4096 bytes"}
	}
	return s.editConfiguration(ctx, request.Revision, func(config *Config) (bool, error) {
		rules, rulesErr := addedSourceRules(request.ImageRules, request.InheritRules)
		if rulesErr != nil {
			return false, rulesErr
		}
		root := filepath.Clean(request.Path)
		local := -1
		for i, provider := range config.Providers {
			if provider.ID == "local" {
				local = i
				if !provider.Enabled && len(provider.Folders) > 0 {
					return false, &Error{Code: "provider_disabled", Message: "local folders are disabled; enable the provider before adding a folder to rotation"}
				}
				for _, folder := range provider.Folders {
					if folder.Path == root {
						return false, nil
					}
				}
			}
		}
		if local < 0 {
			config.Providers = append(config.Providers, ProviderConfig{ID: "local", Enabled: true, RefreshInterval: "5m"})
			local = len(config.Providers) - 1
		}
		var identity [16]byte
		if _, err := rand.Read(identity[:]); err != nil {
			return false, &Error{Code: "config_write_failed", Message: "could not generate a folder identity"}
		}
		provider := &config.Providers[local]
		provider.Enabled = true
		provider.Folders = append(provider.Folders, FolderConfig{ImageRules: rules, ID: "folder-" + hex.EncodeToString(identity[:]), Path: root, Name: strings.TrimSpace(request.Name), Enabled: request.Enabled, MaxDepth: request.MaxDepth, RefreshInterval: request.RefreshInterval})

		return true, nil
	})
}

// editConfiguration serializes all API writers. Mutations are applied to a
// fresh validated snapshot, never to stale client TOML or effective overrides.
func (s *Service) editConfiguration(ctx context.Context, revision string, edit func(*Config) (bool, error), afterSave ...func(Config)) (Configuration, error) {
	if err := ctx.Err(); err != nil {
		return Configuration{}, err
	}
	if revision == "" {
		return Configuration{}, &Error{Code: "invalid_request", Message: "provide the reviewed settings revision"}
	}
	operation, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer cancel()
	ctx = operation
	if !s.actionMu.TryLock() {
		return Configuration{}, &Error{Code: "busy", Message: "another playback operation is running; try again after it finishes"}
	}
	defer s.actionMu.Unlock()
	if s.ctx.Err() != nil {
		return Configuration{}, &Error{Code: "canceled", Message: "service stopping"}
	}
	path := s.request.ConfigPath
	release, err := filelock.Acquire(ctx, path+".lock")
	if err != nil {
		return Configuration{}, &Error{Code: "config_write_failed", Message: err.Error()}
	}
	defer release()
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return Configuration{}, &Error{Code: "config_write_failed", Message: "configuration edits require an existing regular file"}
	}
	// Page commits hold the read side across their configuration check and SQL
	// transaction, so an API save cannot race an obsolete page into the catalog.
	s.configMu.Lock()
	defer s.configMu.Unlock()
	config, _, original, err := ReadConfig(path)
	if err != nil {
		return Configuration{}, invalidSavedConfiguration()
	}
	if revision != configRevision(original) {
		return Configuration{}, &Error{Code: "config_conflict", Message: "settings changed; reload them and review before saving again"}
	}
	changed, err := edit(&config)
	if err != nil {
		return Configuration{}, err
	}
	// A reviewed no-op can still explicitly replace a temporary runtime override.
	apply := func() {
		for _, callback := range afterSave {
			callback(config)
		}
	}
	if !changed {
		if err := ctx.Err(); err != nil {
			return Configuration{}, err
		}
		apply()
		return s.configuration(config, original), nil
	}
	data, err := toml.Marshal(config)
	if err == nil {
		config, _, err = ParseConfig(data)
	}
	if err != nil {
		return Configuration{}, &Error{Code: "invalid_settings", Message: err.Error()}
	}
	if err := replaceConfig(ctx, path, revision, data); err != nil {
		var typed *Error
		if errors.As(err, &typed) {
			if typed.Code == "config_write_uncertain" {
				apply()
			}
			return Configuration{}, typed
		}
		return Configuration{}, &Error{Code: "config_write_failed", Message: err.Error()}
	}
	s.stopLookaheadWork()
	s.finishLookahead(false, false)
	s.publishStatus()
	s.wakeLookahead()
	apply()
	// Existing coordinator reloads and reconciles jobs; it alone changes setup
	// readiness. No second service or rotation timer is created.
	select {
	case s.discoveryWake <- struct{}{}:
	default:
	}
	return s.configuration(config, data), nil
}

func stageConfig(path string, data []byte) (name string, result error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return "", err
	}
	name = file.Name()
	defer func() {
		file.Close()
		if result != nil {
			os.Remove(name)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return name, err
	}
	if err := file.Sync(); err != nil {
		return name, err
	}
	return name, file.Close()
}

// Cooperating writers hold the same lock. Direct external edits should be made
// with the service stopped; a final revision check also detects observed changes.
func replaceConfig(ctx context.Context, path, revision string, data []byte) error {
	name, err := stageConfig(path, data)
	if err != nil {
		return err
	}
	defer os.Remove(name)
	_, _, current, err := ReadConfig(path)
	if err != nil || configRevision(current) != revision {
		return &Error{Code: "config_conflict", Message: "settings changed while saving; reload them before trying again"}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	if err := syncConfigDirectory(path); err != nil {
		return &Error{Code: "config_write_uncertain", Message: "settings were published but directory sync failed; reload before retrying"}
	}
	return nil
}

func syncConfigDirectory(path string) error {
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open settings directory: %w", err)
	}
	defer directory.Close()
	return directory.Sync()
}

func invalidSavedConfiguration() *Error {
	// Unsupported saved fields may contain credentials from a newer schema.
	// Preserve the document and avoid echoing parser context into API responses.
	return &Error{Code: "config_invalid", Message: "saved settings are invalid or unsupported; run panora config check before editing"}
}

func savedSettings(config Config) EffectiveSettings {
	return EffectiveSettings{Backend: config.Desktop.Backend, Fit: config.Desktop.Fit, Upscale: config.Desktop.Upscale, Interval: config.Playback.Interval, RotationMode: config.Playback.RotationMode, CacheMaxImages: config.Cache.MaxImages, CacheMaxBytes: config.Cache.MaxBytes}
}

func restartRequired(saved, effective EffectiveSettings) []string {
	fields := make([]string, 0)
	if saved.Backend != effective.Backend {
		fields = append(fields, "backend")
	}
	if saved.Fit != effective.Fit {
		fields = append(fields, "fit")
	}
	if saved.Upscale != effective.Upscale {
		fields = append(fields, "upscale")
	}
	savedInterval, _ := time.ParseDuration(saved.Interval)
	effectiveInterval, _ := time.ParseDuration(effective.Interval)
	if savedInterval != effectiveInterval {
		fields = append(fields, "interval")
	}
	if saved.CacheMaxImages != effective.CacheMaxImages {
		fields = append(fields, "cache_max_images")
	}
	if saved.CacheMaxBytes != effective.CacheMaxBytes {
		fields = append(fields, "cache_max_bytes")
	}
	return fields
}
