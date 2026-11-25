package playback

import (
	"context"
	"fmt"
	"slices"

	"github.com/vadim82/panora/internal/catalog"
	"github.com/vadim82/panora/internal/source"
)

// SourceImageRules are complete image eligibility rules for one configured
// folder or search. A nil source block explicitly inherits legacy global rules.
// Scan depth remains a local-folder setting rather than an image property.
type SourceImageRules struct {
	MinWidth         int      `toml:"min_width" json:"min_width"`
	MinHeight        int      `toml:"min_height" json:"min_height"`
	Ratio            string   `toml:"ratio" json:"ratio"`
	RatioPresets     []string `toml:"ratio_presets,omitempty" json:"ratio_presets"`
	ExactResolutions []string `toml:"exact_resolutions,omitempty" json:"exact_resolutions,omitempty"`
}

func sourceDefaultRules(config Config) SourceImageRules {
	return SourceImageRules{MinWidth: config.Filters.MinWidth, MinHeight: config.Filters.MinHeight, Ratio: config.Filters.Ratio, RatioPresets: append([]string{}, config.Filters.RatioPresets...)}
}

func effectiveSourceRules(config Config, configured *SourceImageRules) SourceImageRules {
	if configured == nil {
		return sourceDefaultRules(config)
	}
	result := *configured
	result.RatioPresets = append([]string{}, configured.RatioPresets...)
	result.ExactResolutions = slices.Clone(configured.ExactResolutions)
	return result
}

// New inputs begin permissively even when an older profile has restrictive
// legacy defaults. Existing inputs with no block retain their old inheritance.
func newSourceRules(request *SourceImageRules) *SourceImageRules {
	if request == nil {
		return &SourceImageRules{MinWidth: 1, MinHeight: 1, Ratio: "any", RatioPresets: []string{}}
	}
	result := *request
	result.RatioPresets = slices.Clone(request.RatioPresets)
	result.ExactResolutions = slices.Clone(request.ExactResolutions)
	return &result
}

func validateSourceRules(rules *SourceImageRules) error {
	if rules == nil {
		return nil
	}
	if rules.MinWidth < 1 || rules.MinHeight < 1 {
		return fmt.Errorf("image_rules requires positive minimum dimensions")
	}
	if len(rules.ExactResolutions) > source.MaxExactResolutions {
		return fmt.Errorf("image_rules supports up to %d exact resolutions", source.MaxExactResolutions)
	}
	for i, value := range rules.ExactResolutions {
		if _, valid := source.ParseExactResolution(value); !valid || slices.Contains(rules.ExactResolutions[:i], value) {
			return fmt.Errorf("image_rules requires distinct exact resolutions in WxH format")
		}
	}
	if _, err := ParseRatioRule(rules.Ratio, rules.RatioPresets); err != nil {
		return fmt.Errorf("image_rules: %w", err)
	}
	return nil
}

func addedSourceRules(rules *SourceImageRules, inherit *bool) (*SourceImageRules, error) {
	if inherit != nil {
		if !*inherit || rules != nil {
			return nil, &Error{Code: "invalid_settings", Message: "inherit_rules must be true and cannot accompany image_rules"}
		}
		return nil, nil
	}
	result := newSourceRules(rules)
	if err := validateSourceRules(result); err != nil {
		return nil, &Error{Code: "invalid_settings", Message: err.Error()}
	}
	return result, nil
}

func editSourceRules(saved **SourceImageRules, rules *SourceImageRules, inherit *bool) (bool, error) {
	if inherit != nil && (!*inherit || rules != nil) {
		return false, &Error{Code: "invalid_settings", Message: "inherit_rules must be true and cannot accompany image_rules"}
	}
	if inherit != nil {
		changed := *saved != nil
		*saved = nil
		return changed, nil
	}
	if rules == nil {
		return false, nil
	}
	if err := validateSourceRules(rules); err != nil {
		return false, &Error{Code: "invalid_settings", Message: err.Error()}
	}
	if *saved != nil && (*saved).MinWidth == rules.MinWidth && (*saved).MinHeight == rules.MinHeight && (*saved).Ratio == rules.Ratio && slices.Equal((*saved).RatioPresets, rules.RatioPresets) && slices.Equal((*saved).ExactResolutions, rules.ExactResolutions) {
		return false, nil
	}
	*saved = newSourceRules(rules)
	return true, nil
}

func configuredInputRules(config Config, providerID, inputID string) (SourceImageRules, bool) {
	if providerID == omarchyProvider && config.OmarchyTheme != nil && config.OmarchyTheme.Enabled && config.OmarchyTheme.ID == inputID {
		return effectiveSourceRules(config, config.OmarchyTheme.ImageRules), true
	}
	for _, p := range config.Providers {
		if p.ID != providerID || !p.Enabled {
			continue
		}
		if rules, enabled := p.enabledInputRules(inputID); enabled {
			return effectiveSourceRules(config, rules), true
		}
	}
	return SourceImageRules{}, false
}

// The boolean distinguishes inherited rules from a missing or disabled input.
func (p ProviderConfig) enabledInputRules(inputID string) (*SourceImageRules, bool) {
	for _, folder := range p.Folders {
		if folder.ID == inputID && folder.enabled() {
			return folder.ImageRules, true
		}
	}
	for _, search := range p.Searches {
		if search.ID == inputID && search.enabled() {
			return search.ImageRules, true
		}
	}
	for _, input := range p.Inputs {
		if input.ID == inputID && input.enabled() {
			return input.ImageRules, true
		}
	}
	return nil, false
}

func (s *Service) resolveSourceRules(ctx context.Context, config Config, identity catalog.Identity) (SourceImageRules, *RatioRule, RuleResolution) {
	return s.resolveSourceRulesForBackend(ctx, config, identity, s.backend)
}

func (s *Service) resolveSourceRulesForBackend(ctx context.Context, config Config, identity catalog.Identity, backend string) (SourceImageRules, *RatioRule, RuleResolution) {
	rules, ok := configuredInputRules(config, identity.ProviderID, identity.InputID)
	if !ok {
		return rules, nil, RuleResolution{State: "unavailable", Reason: "outside_enabled_inputs"}
	}
	ratio, _ := ParseRatioRule(rules.Ratio, rules.RatioPresets) // Config was validated.
	ratio, resolution := s.resolveRatioRule(ctx, ratio, backend)
	return rules, ratio, resolution
}

// Request-local resolution avoids repeating native geometry inspection for every
// history entry or every source that uses the same monitor rule.
func (s *Service) sourceRuleResolver(ctx context.Context, config Config) func(catalog.Identity) (SourceImageRules, *RatioRule, RuleResolution) {
	return s.sourceRuleResolverForBackend(ctx, config, s.backend)
}

func (s *Service) sourceRuleResolverForBackend(ctx context.Context, config Config, backend string) func(catalog.Identity) (SourceImageRules, *RatioRule, RuleResolution) {
	type resolved struct {
		rules      SourceImageRules
		ratio      *RatioRule
		resolution RuleResolution
	}
	inputs := map[string]resolved{}
	monitors := map[string]resolved{}
	return func(identity catalog.Identity) (SourceImageRules, *RatioRule, RuleResolution) {
		key := discoveryKey(identity.ProviderID, identity.InputID)
		if found, ok := inputs[key]; ok {
			return found.rules, found.ratio, found.resolution
		}
		rules, ok := configuredInputRules(config, identity.ProviderID, identity.InputID)
		value := resolved{rules: rules, resolution: RuleResolution{State: "unavailable", Reason: "outside_enabled_inputs"}}
		if ok {
			ratio, _ := ParseRatioRule(rules.Ratio, rules.RatioPresets)
			if monitor, found := monitors[rules.Ratio]; found {
				value.ratio, value.resolution = monitor.ratio, monitor.resolution
			} else {
				value.ratio, value.resolution = s.resolveRatioRule(ctx, ratio, backend)
				if ratio != nil && ratio.monitor {
					monitors[rules.Ratio] = value
				}
			}
		}
		inputs[key] = value
		return value.rules, value.ratio, value.resolution
	}
}

func (r SourceImageRules) matches(image ImageInfo, ratio *RatioRule) bool {
	return r.matchesResolution(image) && ratio.matches(image)
}

func (r SourceImageRules) matchesResolution(image ImageInfo) bool {
	if len(r.ExactResolutions) == 0 {
		return image.meetsMinResolution(r.MinWidth, r.MinHeight)
	}
	return slices.Contains(r.ExactResolutions, fmt.Sprintf("%dx%d", image.Width, image.Height))
}

// Push hints supported by the provider. Playback still checks the final decoded bytes.
func discoveryFilter(rules SourceImageRules, ratio *RatioRule, supported []string) *source.FilterHints {
	result := &source.FilterHints{}
	if slices.Contains(supported, "min_width") {
		result.MinWidth = rules.MinWidth
	}
	if slices.Contains(supported, "min_height") {
		result.MinHeight = rules.MinHeight
	}
	if len(rules.ExactResolutions) > 0 {
		result.MinWidth, result.MinHeight = 0, 0
		if slices.Contains(supported, "exact_resolutions") {
			result.ExactResolutions = rules.ExactResolutions
		}
	}
	if ratio != nil && !ratio.monitor && !ratio.similar {
		if len(ratio.ratios) == 1 && slices.Contains(supported, "ratio") {
			result.Ratio = &source.Dimensions{Width: ratio.ratios[0].Width, Height: ratio.ratios[0].Height}
		} else if slices.Contains(supported, "ratios") {
			for _, shape := range ratio.ratios {
				result.Ratios = append(result.Ratios, source.Dimensions{Width: shape.Width, Height: shape.Height})
			}
		}
	}
	if result.MinWidth == 0 && result.MinHeight == 0 && result.Ratio == nil && len(result.Ratios) == 0 && len(result.ExactResolutions) == 0 {
		return nil
	}
	return result
}

// A geometry problem in one source must not prevent another source from playing.
func (s *Service) availableSourceRules(ctx context.Context, config Config) RuleResolution {
	last := RuleResolution{State: "unavailable", Reason: "outside_enabled_inputs"}
	resolveRules := s.sourceRuleResolver(ctx, config)
	for _, p := range config.Providers {
		if !p.Enabled {
			continue
		}
		for _, input := range providerInputs(p, config.Filters.MaxDepth, sourceDefaultRules(config)) {
			if input.disabled {
				continue
			}
			_, _, resolution := resolveRules(catalog.Identity{ProviderID: p.ID, InputID: input.id})
			if resolution.State == "resolved" {
				return resolution
			}
			last = resolution
		}
	}
	if config.OmarchyTheme != nil && config.OmarchyTheme.Enabled {
		_, _, returnValue := resolveRules(catalog.Identity{ProviderID: omarchyProvider, InputID: config.OmarchyTheme.ID})
		return returnValue
	}

	return last
}

type discoveryHintsProvider struct {
	source.Provider
	filter       *source.FilterHints
	rules        SourceImageRules
	resolveRatio func(context.Context, *RatioRule) *RatioRule
}

func (p *discoveryHintsProvider) Describe(ctx context.Context) (source.Description, error) {
	description, err := p.Provider.Describe(ctx)
	if err == nil {
		ratio, _ := ParseRatioRule(p.rules.Ratio, p.rules.RatioPresets)
		if ratio != nil && ratio.monitor && !ratio.similar && slices.Contains(description.FilterHints, "ratio") {
			ratio = p.resolveRatio(ctx, ratio)
		}
		p.filter = discoveryFilter(p.rules, ratio, description.FilterHints)
	}
	return description, err
}
func (p *discoveryHintsProvider) Discover(ctx context.Context, params source.DiscoverParams) (source.Page, error) {
	params.Filter = p.filter
	return p.Provider.Discover(ctx, params)
}
