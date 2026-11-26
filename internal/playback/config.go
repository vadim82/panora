package playback

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/source"
)

const maxConfigBytes = 1 << 20

type DesktopConfig struct {
	Backend string `toml:"backend"`
	Fit     string `toml:"fit"`
	Upscale bool   `toml:"upscale"`
}

func (c DesktopConfig) Options() desktop.DisplayOptions {
	return desktop.DisplayOptions{Fit: c.Fit, Upscale: c.Upscale}
}

type FolderConfig struct {
	ImageRules      *SourceImageRules `toml:"image_rules,omitempty" json:"image_rules,omitempty"`
	ID              string            `toml:"id"`
	Path            string            `toml:"path"`
	Name            string            `toml:"name,omitempty" json:"-"`
	Enabled         *bool             `toml:"enabled,omitempty"`
	MaxDepth        *int              `toml:"max_depth,omitempty"`
	RefreshInterval string            `toml:"refresh_interval" json:"-"`
}

type SearchConfig struct {
	ImageRules      *SourceImageRules `toml:"image_rules,omitempty" json:"image_rules,omitempty"`
	ID              string            `toml:"id"`
	Query           string            `toml:"query"`
	Name            string            `toml:"name,omitempty" json:"-"`
	Enabled         *bool             `toml:"enabled,omitempty"`
	Categories      string            `toml:"categories,omitempty"`
	Tags            []string          `toml:"tags,omitempty" json:"tags,omitempty"`
	TagMatch        string            `toml:"tag_match,omitempty" json:"tag_match,omitempty"`
	Colors          []string          `toml:"colors,omitempty" json:"colors,omitempty"`
	Sorting         string            `toml:"sorting,omitempty"`
	Order           string            `toml:"order,omitempty"`
	MaxPages        *int              `toml:"max_pages,omitempty"`
	TopRange        string            `toml:"top_range,omitempty"`
	RefreshInterval string            `toml:"refresh_interval" json:"-"`
}

type ProviderConfig struct {
	ID              string `toml:"id"`
	RefreshInterval string `toml:"refresh_interval"`
	Enabled         bool   `toml:"enabled"`
	// Preserve omitted and explicitly empty arrays across preference writes.
	// Existing discovery revisions distinguish their JSON representations.
	Folders  []FolderConfig        `toml:"folders,omitzero"`
	Searches []SearchConfig        `toml:"searches,omitzero"`
	Inputs   []ExternalInputConfig `toml:"inputs,omitempty"`
	// The provider-wide key from earlier catalogs is used only during upgrade.
	legacyRevision string
}

const (
	rotationBalanceSources   = "balance_sources"
	rotationShuffleAllImages = "shuffle_all_images"
)

type Config struct {
	OmarchyTheme *OmarchyThemeConfig `toml:"omarchy_theme,omitempty"`
	Desktop      DesktopConfig       `toml:"desktop"`
	Version      int                 `toml:"version"`
	Playback     struct {
		Interval     string `toml:"interval"`
		RotationMode string `toml:"rotation_mode"`
	} `toml:"playback"`
	Cache struct {
		MaxImages int   `toml:"max_images"`
		MaxBytes  int64 `toml:"max_bytes"`
	} `toml:"cache"`
	Providers []ProviderConfig `toml:"providers"`
	Filters   struct {
		MaxDepth             int      `toml:"max_depth"`
		MinWidth             int      `toml:"min_width"`
		MinHeight            int      `toml:"min_height"`
		Ratio                string   `toml:"ratio"`
		RatioPresets         []string `toml:"ratio_presets,omitempty"`
		RatioTolerancePixels *int     `toml:"ratio_tolerance_pixels,omitempty"`
	} `toml:"filters"`
}

func configPath(path string) (string, error) {
	if path != "" {
		if strings.ToLower(filepath.Ext(path)) != ".toml" {
			return "", fmt.Errorf("user configuration must be a .toml file; see docs/configuration.md")
		}
		return path, nil
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" && !filepath.IsAbs(xdg) {
		return "", fmt.Errorf("XDG_CONFIG_HOME must be absolute")
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "panora")
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err == nil {
		return "", fmt.Errorf("config.json is no longer supported; create config.toml and select it with --config, or remove the old file")
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("check old configuration: %w", err)
	}
	return filepath.Join(dir, "config.toml"), nil
}

// ConfigPath resolves the same TOML path used by playback without creating it.
func ConfigPath(path string) (string, error) {
	resolved, err := configPath(path)
	if err != nil {
		return "", err
	}
	return filepath.Abs(resolved)
}

// DefaultConfig contains preferences without assuming any folder or search.
func DefaultConfig() Config {
	var config Config
	config.Version = 1
	config.Desktop = DesktopConfig{Backend: "auto", Fit: "auto"}
	config.Playback.Interval = "30m"
	config.Playback.RotationMode = rotationBalanceSources
	config.Cache.MaxImages = 20
	config.Cache.MaxBytes = 512 << 20
	config.Filters.MaxDepth = 2
	config.Filters.MinWidth = 1
	config.Filters.MinHeight = 1
	config.Filters.Ratio = "any"
	return config
}

// RatioTolerancePixels applies to all aspect ratios matched in pixels.
func (c Config) RatioTolerancePixels() int {
	if c.Filters.RatioTolerancePixels != nil {
		return *c.Filters.RatioTolerancePixels
	}
	return defaultRatioTolerancePixels
}

func (c Config) HasEnabledInputs() bool {
	return c.OmarchyTheme != nil && c.OmarchyTheme.Enabled || c.hasEnabledProviderInputs()
}

func (c Config) hasEnabledProviderInputs() bool {
	for _, provider := range c.Providers {
		if !provider.Enabled {
			continue
		}
		for _, input := range provider.Inputs {
			if input.enabled() {
				return true
			}
		}
		for _, search := range provider.Searches {
			if search.enabled() {
				return true
			}
		}
		for _, folder := range provider.Folders {
			if folder.enabled() {
				return true
			}
		}
	}
	return false
}

func LoadConfig(path string) (Config, *RatioRule, error) {
	config, ratio, _, err := ReadConfig(path)
	return config, ratio, err
}

// ReadConfig returns validated preferences and the exact bytes used for revisions.
func ReadConfig(path string) (Config, *RatioRule, []byte, error) {
	resolved, err := ConfigPath(path)
	if err != nil {
		return Config{}, nil, nil, err
	}
	file, err := os.Open(resolved)
	if err != nil {
		return Config{}, nil, nil, fmt.Errorf("read configuration %q (run panora serve for first-run defaults, or pass an existing --config file): %w", resolved, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return Config{}, nil, nil, err
	}
	config, ratio, err := ParseConfig(data)
	return config, ratio, data, err
}

// ParseConfig shares saved-file validation with service-owned edits.
func ParseConfig(data []byte) (Config, *RatioRule, error) {
	config := DefaultConfig()
	config.Version = 0
	if len(data) > maxConfigBytes {
		return config, nil, fmt.Errorf("configuration exceeds 1 MiB")
	}
	if err := toml.NewDecoder(strings.NewReader(string(data))).DisallowUnknownFields().Decode(&config); err != nil {
		return config, nil, fmt.Errorf("parse TOML configuration: %w", err)
	}
	if config.Version != 1 {
		return config, nil, fmt.Errorf("unsupported configuration version %d; expected 1", config.Version)
	}
	if err := desktop.ValidateBackendName(config.Desktop.Backend); err != nil {
		return config, nil, fmt.Errorf("desktop.backend: %w", err)
	}
	if err := config.Desktop.Options().Validate(); err != nil {
		return config, nil, fmt.Errorf("desktop: %w", err)
	}
	if config.Cache.MaxImages < 1 || config.Cache.MaxBytes < 1 {
		return config, nil, fmt.Errorf("cache.max_images and cache.max_bytes must be positive")
	}
	if duration, err := time.ParseDuration(config.Playback.Interval); err != nil || duration <= 0 {
		return config, nil, fmt.Errorf("playback.interval must be a positive duration")
	}
	switch config.Playback.RotationMode {
	case rotationBalanceSources, rotationShuffleAllImages:
	default:
		return config, nil, fmt.Errorf("playback.rotation_mode must be balance_sources or shuffle_all_images")
	}
	if config.Filters.MaxDepth < 0 || config.Filters.MinWidth <= 0 || config.Filters.MinHeight <= 0 {
		return config, nil, fmt.Errorf("filters require non-negative max_depth and positive minimum dimensions")
	}
	if !validRatioTolerance(config.RatioTolerancePixels()) {
		return config, nil, fmt.Errorf("filters.ratio_tolerance_pixels: %s", ratioToleranceError)
	}
	ratio, err := parseRatioRule(config.Filters.Ratio, config.Filters.RatioPresets, config.RatioTolerancePixels())
	if err != nil {
		return config, nil, err
	}
	providerIDs := map[string]bool{}
	inputIDs := map[string]bool{}
	for i := range config.Providers {
		p := &config.Providers[i]
		if !validConfigID(p.ID) || providerIDs[p.ID] {
			return config, nil, fmt.Errorf("provider IDs must be nonblank and unique")
		}
		providerIDs[p.ID] = true
		if p.RefreshInterval == "" {
			p.RefreshInterval = "5m"
		}
		if err := validateRefreshInterval(p.RefreshInterval); err != nil {
			return config, nil, err
		}
		for _, f := range p.Folders {
			if f.ImageRules != nil {
				f.ImageRules.RatioPresets = append([]string{}, f.ImageRules.RatioPresets...)
			}
			if err := validateSourceRules(f.ImageRules); err != nil {
				return config, nil, fmt.Errorf("folder %q: %w", f.ID, err)
			}
			if f.RefreshInterval != "" {
				if err := validateRefreshInterval(f.RefreshInterval); err != nil {
					return config, nil, err
				}
			}
		}
		for _, search := range p.Searches {
			if search.ImageRules != nil {
				search.ImageRules.RatioPresets = append([]string{}, search.ImageRules.RatioPresets...)
			}
			if err := validateSourceRules(search.ImageRules); err != nil {
				return config, nil, fmt.Errorf("search %q: %w", search.ID, err)
			}
			if search.RefreshInterval != "" {
				if err := validateRefreshInterval(search.RefreshInterval); err != nil {
					return config, nil, err
				}
			}
		}
		if !source.SupportsProviderInputs(p.ID) && len(p.Inputs) > 0 {
			return config, nil, fmt.Errorf("bundled provider %q does not accept plugin inputs", p.ID)
		}
		switch p.ID {
		case "local":
			if len(p.Searches) != 0 {
				return config, nil, fmt.Errorf("local provider does not accept searches")
			}
			for j := range p.Folders {
				f := &p.Folders[j]
				if !validConfigID(f.ID) || inputIDs[f.ID] {
					return config, nil, fmt.Errorf("folder/search IDs must be nonblank and globally unique")
				}
				inputIDs[f.ID] = true
				if f.MaxDepth != nil && *f.MaxDepth < 0 {
					return config, nil, fmt.Errorf("folder %q requires non-negative max_depth", f.ID)
				}
				if len(f.Name) > 256 || strings.ContainsAny(f.Name, "\x00\r\n") {
					return config, nil, fmt.Errorf("folder name must be at most 256 bytes without NUL or newlines")
				}
				if !validFolderPath(f.Path) {
					return config, nil, fmt.Errorf("folder %q requires an absolute path (no ~ expansion)", f.ID)
				}
				f.Path = filepath.Clean(f.Path)
			}
		case "wallhaven":
			if len(p.Folders) != 0 {
				return config, nil, fmt.Errorf("Wallhaven provider does not accept folders")
			}
			for _, search := range p.Searches {
				if !validConfigID(search.ID) || inputIDs[search.ID] {
					return config, nil, fmt.Errorf("folder/search IDs must be nonblank and globally unique")
				}
				inputIDs[search.ID] = true
				if err := search.settings("").Validate(); err != nil {
					return config, nil, fmt.Errorf("search %q: %w", search.ID, err)
				}
				if len(search.Name) > 256 || strings.ContainsAny(search.Name, "\x00\r\n") {
					return config, nil, fmt.Errorf("search name must be at most 256 bytes without NUL or newlines")
				}
			}

		default:
			if !source.SupportsProviderInputs(p.ID) || len(p.Folders) > 0 || len(p.Searches) > 0 {
				return config, nil, fmt.Errorf("unknown provider or unsupported inputs for %q", p.ID)
			}
			for _, input := range p.Inputs {
				if !validConfigID(input.ID) || inputIDs[input.ID] {
					return config, nil, fmt.Errorf("source IDs must be nonblank and globally unique")
				}
				inputIDs[input.ID] = true
				if err := input.validate(); err != nil {
					return config, nil, fmt.Errorf("provider %q input %q: %w", p.ID, input.ID, err)
				}
			}
		}
		settings, _ := json.Marshal(struct {
			Provider string
			Folders  []FolderConfig
			Searches []SearchConfig
			Inputs   []ExternalInputConfig `json:",omitempty"`
			MaxDepth int
			Rules    SourceImageRules
		}{p.ID, p.Folders, p.Searches, p.Inputs, config.Filters.MaxDepth, sourceDefaultRules(config)})
		digest := sha256.Sum256(settings)
		p.legacyRevision = hex.EncodeToString(digest[:])
	}
	if c := config.OmarchyTheme; c != nil {
		if !validConfigID(c.ID) || inputIDs[c.ID] {
			return config, nil, fmt.Errorf("Omarchy theme source ID must be nonblank and globally unique")
		}
		if err := validateSourceRules(c.ImageRules); err != nil {
			return config, nil, fmt.Errorf("Omarchy theme source: %w", err)
		}
	}

	return config, ratio, nil
}

func validConfigID(id string) bool {
	return strings.TrimSpace(id) != "" && strings.TrimSpace(id) == id && len(id) <= 128
}

func validateRefreshInterval(value string) error {
	duration, err := time.ParseDuration(value)
	if err != nil || duration < time.Second {
		return fmt.Errorf("refresh_interval must be a duration of at least 1s")
	}
	return nil
}
