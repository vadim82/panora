package playback

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/vadim82/panora/internal/source"
)

func (s SearchConfig) enabled() bool { return s.Enabled == nil || *s.Enabled }
func (s SearchConfig) label() string {
	if s.Name != "" {
		return s.Name
	}
	if strings.TrimSpace(s.Query) != "" {
		return s.Query
	}
	return "Wallhaven"
}
func (s SearchConfig) settings(revision string) source.WallhavenConfig {
	c := source.DefaultWallhavenConfig()
	c.Query, c.Revision = s.Query, revision
	c.Tags, c.TagMatch, c.Colors = s.Tags, s.TagMatch, s.Colors
	if s.Categories != "" {
		c.Categories = s.Categories
	}
	if s.Sorting != "" {
		c.Sorting = s.Sorting
	}
	if s.Order != "" {
		c.Order = s.Order
	}
	if s.MaxPages != nil {
		c.MaxPages = *s.MaxPages
	}
	if s.TopRange != "" {
		c.TopRange = s.TopRange
	}
	return c
}

type ConfiguredSearch struct {
	ImageRules      *SourceImageRules `json:"image_rules"`
	EffectiveRules  SourceImageRules  `json:"effective_rules"`
	RuleResolution  RuleResolution    `json:"rule_resolution"`
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	CustomName      string            `json:"custom_name"`
	Tags            []string          `json:"tags"`
	TagMatch        string            `json:"tag_match"`
	Colors          []string          `json:"colors"`
	Query           string            `json:"query"`
	Enabled         bool              `json:"enabled"`
	InputEnabled    bool              `json:"input_enabled"`
	Categories      string            `json:"categories"`
	Sorting         string            `json:"sorting"`
	Order           string            `json:"order"`
	MaxPages        int               `json:"max_pages"`
	TopRange        string            `json:"top_range"`
	Purity          string            `json:"purity"`
	RefreshInterval string            `json:"refresh_interval"`
}

func configuredSearches(config Config) []ConfiguredSearch {
	result := make([]ConfiguredSearch, 0)
	for _, p := range config.Providers {
		for _, search := range p.Searches {
			c := search.settings("")
			result = append(result, ConfiguredSearch{
				ID: search.ID, Name: search.label(), CustomName: search.Name, Query: search.Query,
				Tags: append([]string{}, c.Tags...), TagMatch: c.EffectiveTagMatch(), Colors: append([]string{}, c.Colors...),
				ImageRules: search.ImageRules, EffectiveRules: effectiveSourceRules(config, search.ImageRules), RuleResolution: staticRuleResolution(effectiveSourceRules(config, search.ImageRules).Ratio),
				Enabled: p.Enabled && search.enabled(), InputEnabled: search.enabled(),
				Categories: c.Categories, Sorting: c.Sorting, Order: c.Order,
				MaxPages: c.MaxPages, TopRange: c.TopRange, Purity: "sfw",
				RefreshInterval: refreshInterval(p.RefreshInterval, search.RefreshInterval).String(),
			})
		}
	}
	return result
}

type AddSearchRequest struct {
	ImageRules      *SourceImageRules `json:"image_rules,omitempty"`
	InheritRules    *bool             `json:"inherit_rules,omitempty"`
	Revision        string            `json:"revision"`
	Name            string            `json:"name,omitempty"`
	Query           string            `json:"query"`
	Tags            []string          `json:"tags,omitempty"`
	TagMatch        string            `json:"tag_match,omitempty"`
	Colors          []string          `json:"colors,omitempty"`
	Enabled         *bool             `json:"enabled,omitempty"`
	Categories      string            `json:"categories,omitempty"`
	Sorting         string            `json:"sorting,omitempty"`
	Order           string            `json:"order,omitempty"`
	MaxPages        *int              `json:"max_pages,omitempty"`
	TopRange        string            `json:"top_range,omitempty"`
	RefreshInterval string            `json:"refresh_interval,omitempty"`
}

type UpdateSearchRequest struct {
	ImageRules      *SourceImageRules `json:"image_rules,omitempty"`
	InheritRules    *bool             `json:"inherit_rules,omitempty"`
	Revision        string            `json:"revision"`
	ID              string            `json:"id"`
	Name            *string           `json:"name,omitempty"`
	Query           *string           `json:"query,omitempty"`
	Tags            *[]string         `json:"tags,omitempty"`
	TagMatch        *string           `json:"tag_match,omitempty"`
	Colors          *[]string         `json:"colors,omitempty"`
	Enabled         *bool             `json:"enabled,omitempty"`
	Categories      *string           `json:"categories,omitempty"`
	Sorting         *string           `json:"sorting,omitempty"`
	Order           *string           `json:"order,omitempty"`
	MaxPages        *int              `json:"max_pages,omitempty"`
	TopRange        *string           `json:"top_range,omitempty"`
	RefreshInterval *string           `json:"refresh_interval,omitempty"`
}

type RemoveSearchRequest struct {
	Revision string `json:"revision"`
	ID       string `json:"id"`
}

// Saves validate syntax offline. Discovery reports availability separately, so
// an outage never prevents users from saving a search for later verification.
func (s *Service) AddSearch(ctx context.Context, request AddSearchRequest) (Configuration, error) {
	return s.editConfiguration(ctx, request.Revision, func(config *Config) (bool, error) {
		rules, rulesErr := addedSourceRules(request.ImageRules, request.InheritRules)
		if rulesErr != nil {
			return false, rulesErr
		}
		var provider *ProviderConfig
		for i := range config.Providers {
			if config.Providers[i].ID == "wallhaven" {
				provider = &config.Providers[i]
				break
			}
		}
		if provider != nil && !provider.Enabled && len(provider.Searches) > 0 {
			return false, &Error{Code: "provider_disabled", Message: "Wallhaven is disabled; enable the provider before adding searches"}
		}
		if provider == nil {
			config.Providers = append(config.Providers, ProviderConfig{ID: "wallhaven", Enabled: true, RefreshInterval: "5m"})
			provider = &config.Providers[len(config.Providers)-1]
		}
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return false, &Error{Code: "config_write_failed", Message: "could not generate a search identity"}
		}
		provider.Enabled = true
		categories := request.Categories
		if categories == "" {
			categories = "000"
		}
		tagMatch := request.TagMatch
		if tagMatch == "" {
			tagMatch = source.WallhavenTagMatchAny
		}
		provider.Searches = append(provider.Searches, SearchConfig{
			ImageRules: rules, ID: "search-" + hex.EncodeToString(id[:]), Name: strings.TrimSpace(request.Name),
			Query: request.Query, Enabled: request.Enabled, Categories: categories,
			Tags: slices.Clone(request.Tags), TagMatch: tagMatch, Colors: slices.Clone(request.Colors),
			Sorting: request.Sorting, Order: request.Order, MaxPages: request.MaxPages,
			TopRange: request.TopRange, RefreshInterval: request.RefreshInterval,
		})
		return true, nil
	})
}

func (s *Service) UpdateSearch(ctx context.Context, request UpdateSearchRequest) (Configuration, error) {
	if request.Name == nil && request.Query == nil && request.Enabled == nil && request.Categories == nil && request.Sorting == nil && request.Order == nil && request.MaxPages == nil && request.TopRange == nil && request.RefreshInterval == nil && request.ImageRules == nil && request.InheritRules == nil && request.Tags == nil && request.TagMatch == nil && request.Colors == nil {
		return Configuration{}, &Error{Code: "invalid_request", Message: "provide at least one search setting to update"}
	}
	return s.editConfiguration(ctx, request.Revision, func(config *Config) (bool, error) {
		p, i := findSearch(config, request.ID)
		if i < 0 {
			return false, searchNotFound()
		}
		search := &p.Searches[i]
		changed, rulesErr := editSourceRules(&search.ImageRules, request.ImageRules, request.InheritRules)
		if rulesErr != nil {
			return false, rulesErr
		}
		changed = setPreference(&search.Query, request.Query) || changed
		if request.Name != nil {
			name := strings.TrimSpace(*request.Name)
			changed = setPreference(&search.Name, &name) || changed
		}
		if request.Enabled != nil && search.enabled() != *request.Enabled {
			search.Enabled = request.Enabled
			changed = true
		}
		changed = setPreference(&search.Categories, request.Categories) || changed
		changed = setPreference(&search.TagMatch, request.TagMatch) || changed
		for _, field := range []struct{ saved, supplied *[]string }{{&search.Tags, request.Tags}, {&search.Colors, request.Colors}} {
			if field.supplied != nil && !slices.Equal(*field.saved, *field.supplied) {
				*field.saved = slices.Clone(*field.supplied)
				changed = true
			}
		}
		changed = setPreference(&search.Sorting, request.Sorting) || changed
		changed = setPreference(&search.Order, request.Order) || changed
		if request.MaxPages != nil && (search.MaxPages == nil || *search.MaxPages != *request.MaxPages) {
			search.MaxPages = request.MaxPages
			changed = true
		}
		changed = setPreference(&search.TopRange, request.TopRange) || changed
		changed = setPreference(&search.RefreshInterval, request.RefreshInterval) || changed
		return changed, nil
	})
}

func (s *Service) RemoveSearch(ctx context.Context, request RemoveSearchRequest) (Configuration, error) {
	return s.editConfiguration(ctx, request.Revision, func(config *Config) (bool, error) {
		p, i := findSearch(config, request.ID)
		if i < 0 {
			return false, searchNotFound()
		}
		p.Searches = append(p.Searches[:i], p.Searches[i+1:]...)
		return true, nil
	})
}

func findSearch(config *Config, id string) (*ProviderConfig, int) {
	for i := range config.Providers {
		p := &config.Providers[i]
		if p.ID != "wallhaven" {
			continue
		}
		for j, search := range p.Searches {
			if search.ID == id {
				return p, j
			}
		}
	}
	return nil, -1
}
func searchNotFound() *Error {
	return &Error{Code: "search_not_found", Message: "Wallhaven search no longer exists; reload settings"}
}
