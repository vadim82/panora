package playback

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/vadim82/panora/internal/source"
)

type AlphaRequest struct {
	Action    string            `json:"action"`
	Key       string            `json:"key,omitempty"`
	Prefix    string            `json:"prefix,omitempty"`
	Settings  json.RawMessage   `json:"settings,omitempty"`
	Rules     *SourceImageRules `json:"image_rules,omitempty"`
	Thumbnail string            `json:"thumbnail,omitempty"`
}

type AlphaResult struct {
	Connected bool               `json:"connected"`
	Tags      []source.AlphaTag  `json:"tags,omitempty"`
	Check     *source.AlphaCheck `json:"check,omitempty"`
	Thumbnail string             `json:"thumbnail,omitempty"`
}

func (s *Service) AlphaCoders(ctx context.Context, req AlphaRequest) (AlphaResult, error) {
	provider, ok := s.request.provider(source.AlphaCodersID).(source.AlphaEditor)
	if !ok {
		return AlphaResult{}, &Error{Code: "unsupported", Message: "Alpha Coders is unavailable"}
	}
	var result AlphaResult
	var err error
	switch req.Action {
	case "status":
		result.Connected = provider.Connected()
	case "connect", "remove":
		err = provider.SetKey(ctx, req.Key, req.Action == "remove")
		if err == nil {
			result.Connected = provider.Connected()
			if result.Connected {
				s.retryMu.Lock()
				for key, state := range s.retries {
					if key.ProviderID == source.AlphaCodersID && (state.Code == "auth_required" || state.Code == "auth_rejected") {
						s.deleteRetryLocked(key)
					}
				}
				s.retryMu.Unlock()
				s.wakeDiscovery()
			}
		}
	case "tags":
		result.Tags, err = provider.Tags(ctx, req.Prefix)
	case "thumbnail":
		result.Thumbnail, err = provider.Thumbnail(ctx, req.Thumbnail)
	case "check":
		config, _, _, readErr := ReadConfig(s.request.ConfigPath)
		if readErr != nil {
			return result, invalidSavedConfiguration()
		}
		if err = validateSourceRules(req.Rules); err != nil {
			break
		}
		rules := effectiveSourceRules(config, req.Rules)
		s.statusMu.RLock()
		backend := s.effectiveSettings.Backend
		s.statusMu.RUnlock()
		ratio, resolution := s.resolveRatioRule(ctx, rules.ratioRule(config.RatioTolerancePixels()), backend)
		if resolution.State != "resolved" {
			return result, unresolvedRuleError(resolution)
		}
		description, _ := s.request.provider(source.AlphaCodersID).Describe(ctx)
		filter := discoveryFilter(rules, ratio, description.FilterHints)
		var check source.AlphaCheck
		check, err = provider.Check(ctx, req.Settings, filter, func(d source.Dimensions) bool {
			return rules.matches(ImageInfo{Width: d.Width, Height: d.Height}, ratio)
		})
		if err == nil {
			result.Check = &check
		}
	default:
		return result, &Error{Code: "invalid_request", Message: "unknown Alpha Coders action"}
	}
	if err == nil {
		return result, nil
	}
	var failure *source.Error
	if errors.As(err, &failure) {
		return result, &Error{Code: failure.Code, Message: failure.Message}
	}
	return result, &Error{Code: "unavailable", Message: "Alpha Coders could not complete the operation"}
}
