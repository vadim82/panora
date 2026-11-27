package playback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/vadim82/panora/internal/omarchytheme"
)

const (
	MaxOmarchyThemePageSize     = 12
	MaxOmarchyThemeMessageBytes = 2 << 20
)

type OmarchySwatch struct {
	Label string `json:"label"`
	Color string `json:"color"`
}

type OmarchyStillPreview struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Eligible bool   `json:"eligible"`
	JPEG     []byte `json:"jpeg"`
}

type OmarchyThemePreview struct {
	Version  int                   `json:"version"`
	Theme    string                `json:"theme,omitempty"`
	Revision string                `json:"revision,omitempty"`
	State    string                `json:"state"`
	Warning  string                `json:"warning,omitempty"`
	Total    int                   `json:"total"`
	Eligible *int                  `json:"eligible"`
	Page     int                   `json:"page"`
	Palette  []OmarchySwatch       `json:"palette"`
	Stills   []OmarchyStillPreview `json:"stills"`
}

// OmarchyThemePreview only reads discovery-prepared thumbnails and configured
// image rules. It cannot supply a path for decoding nor request provider work.
func (s *Service) OmarchyThemePreview(ctx context.Context, page, limit int) (OmarchyThemePreview, error) {
	if page < 1 || page > omarchytheme.MaxStills || limit < 0 || limit > MaxOmarchyThemePageSize || limit == 0 && page != 1 {
		return OmarchyThemePreview{}, &Error{Code: "invalid_request", Message: "provide a page within the theme collection and thumbnail limit 0 through 12"}
	}
	if err := ctx.Err(); err != nil {
		return OmarchyThemePreview{}, err
	}
	config, _, err := LoadConfig(s.request.ConfigPath)
	if err != nil {
		return OmarchyThemePreview{}, invalidSavedConfiguration()
	}
	result := OmarchyThemePreview{Version: StatusVersion, State: "unavailable", Page: 1, Stills: []OmarchyStillPreview{}, Palette: []OmarchySwatch{}}
	if config.OmarchyTheme == nil {
		return result, nil
	}
	active, _ := omarchytheme.ReadActive(s.theme.home)
	if active.Root == "" || s.theme.home == "" {
		result.Warning = "The active Omarchy theme is unavailable."
		return result, nil
	}
	result.Theme = active.Label()
	collection := s.theme.collection.Load()
	if collection == nil || collection.active != active || collection.sourceID != config.OmarchyTheme.ID {
		result.State = "loading"
		return result, nil
	}
	result.State = "ready"
	result.Warning = collection.warning
	result.Total = len(collection.stills)
	palette := collection.palette
	for _, pair := range []OmarchySwatch{
		{"Accent", palette.Accent}, {"Background", palette.Background}, {"Text", palette.Foreground},
		{"Blue", palette.Blue}, {"Green", palette.Green}, {"Magenta", palette.Magenta},
	} {
		if pair.Color != "" {
			result.Palette = append(result.Palette, pair)
		}
	}
	rules := effectiveSourceRules(config, config.OmarchyTheme.ImageRules)
	ratio := rules.ratioRule(config.RatioTolerancePixels())
	s.statusMu.RLock()
	backend := s.effectiveSettings.Backend
	s.statusMu.RUnlock()
	ratio, resolution := s.resolveRatioRule(ctx, ratio, backend)
	digest := sha256.New()
	fmt.Fprint(digest, active.Generation, active.ID, collection.warning, palette, rules, resolution, config.RatioTolerancePixels())
	if limit != 0 {
		result.Page = min(page, max(1, (result.Total+limit-1)/limit))
	}
	start := (result.Page - 1) * limit
	eligible := 0
	for i, still := range collection.stills {
		matches := resolution.State == "resolved" && rules.matches(ImageInfo{Width: still.thumb.Width, Height: still.thumb.Height}, ratio)
		if matches {
			eligible++
		}
		fmt.Fprint(digest, still.candidate.Identity.ImageID, matches)
		if i >= start && i < start+limit {
			result.Stills = append(result.Stills, OmarchyStillPreview{ID: still.candidate.Identity.ImageID, Name: still.file.Name,
				Width: still.thumb.Width, Height: still.thumb.Height, Eligible: matches, JPEG: still.thumb.Data})
		}
	}
	if resolution.State == "resolved" {
		result.Eligible = &eligible
	}
	result.Revision = hex.EncodeToString(digest.Sum(nil))
	return result, nil
}

// Use geometry already decoded by theme discovery. Rules reads never inspect
// originals, even when a draft differs from the saved theme source rules.
func (s *Service) addOmarchyRulePreview(result *RulePreview, config Config, rules SourceImageRules, ratio *RatioRule) {
	collection := s.currentThemeCollection()
	if collection == nil || config.OmarchyTheme == nil || collection.sourceID != config.OmarchyTheme.ID {
		result.Complete = false
		result.Reason = "discovery_incomplete"
		return
	}
	if result.ProviderID == omarchyProvider {
		result.Validation = "discovery_decoded_dimensions"
	}
	for _, still := range collection.stills {
		if result.Inspected >= result.Limit {
			result.Complete = false
			result.Reason = "image_limit"
			return
		}
		result.Inspected++
		geometry := ImageInfo{Width: still.thumb.Width, Height: still.thumb.Height}
		resolutionMatches, shapeMatches := rules.matchesResolution(geometry), ratio.matches(geometry)
		if resolutionMatches && shapeMatches {
			result.Matched++
			continue
		}
		result.Rejected++
		if !resolutionMatches {
			result.Reasons.BelowMinimum++
		}
		if !shapeMatches {
			result.Reasons.RatioMismatch++
		}
	}
}
