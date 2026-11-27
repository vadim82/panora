package playback

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/vadim82/panora/internal/source"
)

const maxProviderValues = 12 << 10

// ExternalInputConfig stores opaque provider settings as one TOML string. JSON
// keeps booleans, arrays and numbers stable across TOML round trips.
type ExternalInputConfig struct {
	ImageRules *SourceImageRules `toml:"image_rules,omitempty"`
	ID         string            `toml:"id"`
	Name       string            `toml:"name,omitempty"`
	Values     string            `toml:"settings_json"`
	Enabled    *bool             `toml:"enabled,omitempty"`
}

func (c ExternalInputConfig) enabled() bool { return c.Enabled == nil || *c.Enabled }

func (c ExternalInputConfig) validate() error {
	if len(c.Name) > 256 || strings.ContainsAny(c.Name, "\x00\n\r") {
		return fmt.Errorf("source name must be at most 256 bytes without NUL or newlines")
	}
	var values map[string]json.RawMessage
	if len(c.Values) > maxProviderValues || json.Unmarshal([]byte(c.Values), &values) != nil || values == nil {
		return fmt.Errorf("provider settings must be a JSON object of at most 12 KiB")
	}
	return validateSourceRules(c.ImageRules)
}

type ProviderEditor struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Editor      *source.EditorManifest `json:"editor,omitempty"`
	EditorError string                 `json:"editor_error,omitempty"`
}

type ConfiguredInput struct {
	ProviderID     string            `json:"provider_id"`
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	InputEnabled   bool              `json:"input_enabled"`
	Enabled        bool              `json:"enabled"`
	Values         json.RawMessage   `json:"values"`
	ImageRules     *SourceImageRules `json:"image_rules"`
	EffectiveRules SourceImageRules  `json:"effective_rules"`
	RuleResolution RuleResolution    `json:"rule_resolution"`
}

type ProviderInputEdit struct {
	ProviderID   string            `json:"provider_id"`
	ID           string            `json:"id,omitempty"`
	Revision     string            `json:"revision"`
	Name         *string           `json:"name,omitempty"`
	Enabled      *bool             `json:"enabled,omitempty"`
	Values       json.RawMessage   `json:"values,omitempty"`
	ImageRules   *SourceImageRules `json:"image_rules,omitempty"`
	InheritRules *bool             `json:"inherit_rules,omitempty"`
}

func (s *Service) providerEditors(config Config) []ProviderEditor {
	editors := slices.Clone(s.request.ProviderEditors)
	for _, p := range config.Providers {
		if source.ValidInputProviderID(p.ID) && editorIndex(editors, p.ID) < 0 {
			editors = append(editors, ProviderEditor{ID: p.ID, Name: p.ID, EditorError: "Install this provider and restart Panora’s service."})
		}
	}
	return editors
}

func editorIndex(editors []ProviderEditor, id string) int {
	return slices.IndexFunc(editors, func(editor ProviderEditor) bool { return editor.ID == id })
}

func (s *Service) configuredInputs(config Config) []ConfiguredInput {
	var result []ConfiguredInput
	for _, p := range config.Providers {
		for _, input := range p.Inputs {
			effective := effectiveSourceRules(config, input.ImageRules)
			result = append(result, ConfiguredInput{
				ProviderID: p.ID, ID: input.ID, Name: input.Name, Values: json.RawMessage(input.Values),
				InputEnabled: input.enabled(), Enabled: p.Enabled && input.enabled() && s.request.inputProviderAvailable(p.ID),
				ImageRules: input.ImageRules, EffectiveRules: effective, RuleResolution: staticRuleResolution(effective.Ratio),
			})
		}
	}
	return result
}

func (s *Service) providerEditor(id string) (*source.EditorManifest, error) {
	i := editorIndex(s.request.ProviderEditors, id)
	if i < 0 || s.request.provider(id) == nil {
		return nil, &Error{Code: "unsupported", Message: "install this provider and restart Panora’s service"}
	}
	if editor := s.request.ProviderEditors[i]; editor.Editor != nil {
		return editor.Editor, nil
	}
	return nil, &Error{Code: "unsupported", Message: "this provider’s editor is unavailable; check the installed manifest"}
}

func (s *Service) validateProviderSettings(ctx context.Context, id string, input ExternalInputConfig) error {
	if err := input.validate(); err != nil {
		return &Error{Code: "invalid_settings", Message: err.Error()}
	}
	if _, err := s.providerEditor(id); err != nil {
		return err
	}
	operation, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	if err := s.request.provider(id).Validate(operation, json.RawMessage(input.Values)); err != nil {
		return &Error{Code: "invalid_settings", Message: "provider could not validate these settings; check the values and try again"}
	}
	return nil
}

func (s *Service) AddProviderInput(ctx context.Context, request ProviderInputEdit) (Configuration, error) {
	return s.editConfiguration(ctx, request.Revision, func(config *Config) (bool, error) {
		var bytes [16]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			return false, &Error{Code: "config_write_failed", Message: "could not generate a source identity"}
		}
		rules, err := addedSourceRules(request.ImageRules, request.InheritRules)
		if err != nil {
			return false, err
		}
		input := ExternalInputConfig{ID: "input-" + hex.EncodeToString(bytes[:]), Enabled: request.Enabled, ImageRules: rules, Values: string(request.Values)}
		if request.Name != nil {
			input.Name = strings.TrimSpace(*request.Name)
		}
		if err := s.validateProviderSettings(ctx, request.ProviderID, input); err != nil {
			return false, err
		}
		index := slices.IndexFunc(config.Providers, func(p ProviderConfig) bool { return p.ID == request.ProviderID })
		if index < 0 {
			config.Providers = append(config.Providers, ProviderConfig{ID: request.ProviderID, Enabled: true})
			index = len(config.Providers) - 1
		}
		p := &config.Providers[index]
		if !p.Enabled {
			return false, &Error{Code: "provider_disabled", Message: "enable this provider before adding sources"}
		}
		p.Inputs = append(p.Inputs, input)
		return true, nil
	})
}

func (s *Service) UpdateProviderInput(ctx context.Context, request ProviderInputEdit) (Configuration, error) {
	return s.editConfiguration(ctx, request.Revision, func(config *Config) (bool, error) {
		p, i := findProviderInput(config, request.ProviderID, request.ID)
		if i < 0 {
			return false, providerInputNotFound()
		}
		input := &p.Inputs[i]
		changed, err := editSourceRules(&input.ImageRules, request.ImageRules, request.InheritRules)
		if err != nil {
			return false, err
		}
		if request.Name != nil {
			name := strings.TrimSpace(*request.Name)
			changed = setPreference(&input.Name, &name) || changed
		}
		if request.Values != nil && string(request.Values) != input.Values {
			input.Values = string(request.Values)
			if err := s.validateProviderSettings(ctx, request.ProviderID, *input); err != nil {
				return false, err
			}
			changed = true
		}
		if request.Enabled != nil && input.enabled() != *request.Enabled {
			if *request.Enabled && s.request.provider(request.ProviderID) == nil {
				return false, &Error{Code: "unsupported", Message: "install this provider and restart Panora’s service"}
			}
			input.Enabled = request.Enabled
			changed = true
		}
		return changed, nil
	})
}

func (s *Service) RemoveProviderInput(ctx context.Context, request ProviderInputEdit) (Configuration, error) {
	return s.editConfiguration(ctx, request.Revision, func(config *Config) (bool, error) {
		p, i := findProviderInput(config, request.ProviderID, request.ID)
		if i < 0 {
			return false, providerInputNotFound()
		}
		p.Inputs = slices.Delete(p.Inputs, i, i+1)
		return true, nil
	})
}

func findProviderInput(config *Config, providerID, id string) (*ProviderConfig, int) {
	for i := range config.Providers {
		p := &config.Providers[i]
		if p.ID == providerID {
			return p, slices.IndexFunc(p.Inputs, func(input ExternalInputConfig) bool { return input.ID == id })
		}
	}
	return nil, -1
}

func providerInputNotFound() *Error {
	return &Error{Code: "invalid_request", Message: "provider source no longer exists; reload settings"}
}

func (s *Service) SuggestProviderInput(ctx context.Context, id string, params source.SuggestParams) ([]string, error) {
	editor, err := s.providerEditor(id)
	if err != nil {
		return nil, err
	}
	for _, group := range editor.Groups {
		for _, field := range group.Fields {
			if field.Key == params.Field && field.Suggest == source.SuggestMethod {
				provider, ok := s.request.provider(id).(source.Suggester)
				if !ok {
					break
				}
				operation, cancel := context.WithTimeout(ctx, 2*time.Second)
				defer cancel()
				stop := context.AfterFunc(s.ctx, cancel)
				defer stop()
				if values, err := provider.Suggest(operation, params); err == nil {
					return values, nil
				}
				return nil, &Error{Code: "unsupported", Message: "suggestions are unavailable; enter a value manually"}
			}
		}
	}
	return nil, &Error{Code: "unsupported", Message: "this field does not offer suggestions"}
}
