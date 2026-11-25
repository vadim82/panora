package playback

import (
	"context"
	"crypto/rand"
)

const omarchyProvider = "omarchy"

type OmarchyThemeConfig struct {
	ID         string            `toml:"id"`
	Enabled    bool              `toml:"enabled"`
	ImageRules *SourceImageRules `toml:"image_rules,omitempty"`
}

func newOmarchyThemeConfig(enabled bool, rules *SourceImageRules) *OmarchyThemeConfig {
	return &OmarchyThemeConfig{ID: "omarchy-" + rand.Text(), Enabled: enabled, ImageRules: rules}
}

type ConfiguredOmarchyTheme struct {
	ID             string            `json:"id"`
	Enabled        bool              `json:"enabled"`
	InputEnabled   bool              `json:"input_enabled"`
	ImageRules     *SourceImageRules `json:"image_rules"`
	EffectiveRules SourceImageRules  `json:"effective_rules"`
	RuleResolution RuleResolution    `json:"rule_resolution"`
}

func configuredOmarchyTheme(config Config) *ConfiguredOmarchyTheme {
	if config.OmarchyTheme == nil {
		return nil
	}
	entry := config.OmarchyTheme
	rules := effectiveSourceRules(config, entry.ImageRules)
	return &ConfiguredOmarchyTheme{ID: entry.ID, Enabled: entry.Enabled, InputEnabled: entry.Enabled, ImageRules: entry.ImageRules,
		EffectiveRules: rules, RuleResolution: staticRuleResolution(rules.Ratio)}
}

type EditOmarchyThemeRequest struct {
	Revision     string            `json:"revision"`
	ID           string            `json:"id,omitempty"`
	Enabled      *bool             `json:"enabled,omitempty"`
	ImageRules   *SourceImageRules `json:"image_rules,omitempty"`
	InheritRules *bool             `json:"inherit_rules,omitempty"`
}

func (s *Service) EditOmarchyTheme(ctx context.Context, action string, request EditOmarchyThemeRequest) (Configuration, error) {
	return s.editConfiguration(ctx, request.Revision, func(config *Config) (bool, error) {
		saved := config.OmarchyTheme
		switch action {
		case "add":
			if request.ID != "" {
				return false, &Error{Code: "invalid_request", Message: "new sources receive a generated ID"}
			}
			if saved != nil {
				return false, &Error{Code: "invalid_request", Message: "an Omarchy theme source already exists"}
			}
			if request.ImageRules == nil && request.InheritRules == nil {
				inherit := true
				request.InheritRules = &inherit
			}
			rules, err := addedSourceRules(request.ImageRules, request.InheritRules)
			if err != nil {
				return false, err
			}
			config.OmarchyTheme = newOmarchyThemeConfig(request.Enabled == nil || *request.Enabled, rules)
			return true, nil
		case "update", "remove":
			if saved == nil || saved.ID != request.ID {
				return false, &Error{Code: "invalid_request", Message: "Omarchy theme source does not exist"}
			}
			if action == "remove" {
				if request.Enabled != nil || request.ImageRules != nil || request.InheritRules != nil {
					return false, &Error{Code: "invalid_request", Message: "removing the Omarchy theme source takes only revision and ID"}
				}
				config.OmarchyTheme = nil
				return true, nil
			}
			changed, err := editSourceRules(&saved.ImageRules, request.ImageRules, request.InheritRules)
			if err != nil {
				return false, err
			}
			if request.Enabled != nil && saved.Enabled != *request.Enabled {
				saved.Enabled = *request.Enabled
				changed = true
			}
			return changed, nil
		default:
			return false, &Error{Code: "invalid_request", Message: "unknown Omarchy theme source operation"}
		}
	})
}
