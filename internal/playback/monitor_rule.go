package playback

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/vadim82/panora/internal/desktop"
)

type RuleResolution struct {
	State   string                   `json:"state"`
	Ratio   string                   `json:"ratio,omitempty"`
	Reason  string                   `json:"reason,omitempty"`
	Backend string                   `json:"backend,omitempty"`
	Display *desktop.DisplayGeometry `json:"display,omitempty"`
}

func staticRuleResolution(mode string) RuleResolution {
	if mode == "monitor" || mode == "monitor-similar" {
		return RuleResolution{State: "unavailable", Reason: "monitor_geometry_not_inspected"}
	}
	result := RuleResolution{State: "resolved"}
	if mode != "any" && mode != "presets" {
		result.Ratio = mode
	}
	return result
}

// ResolveRatioRule resolves one shared rule from one unambiguous display.
// Reported dimensions are pixels, not logical points, and are never multiplied
// by scale. Transformed Hyprland outputs remain unsupported; AppKit reports an
// already oriented backing frame with transform zero.
func ResolveRatioRule(rule *RatioRule, capabilities desktop.Capabilities) (*RatioRule, RuleResolution) {
	if rule == nil || !rule.monitor {
		resolution := RuleResolution{State: "resolved"}
		if rule != nil && len(rule.ratios) == 1 {
			resolution.Ratio = rule.label
		}
		return rule, resolution
	}
	result := RuleResolution{State: capabilities.Geometry.State, Backend: capabilities.Backend, Reason: "monitor_geometry_" + capabilities.Geometry.State}
	if capabilities.State == "ambiguous" {
		result.State, result.Reason = "ambiguous", "backend_ambiguous"
		return rule, result
	}
	if capabilities.State != "ready" && capabilities.State != "partial" {
		result.State, result.Reason = "unavailable", "backend_unavailable"
		return rule, result
	}
	if capabilities.Geometry.State != "known" || len(capabilities.Geometry.Displays) != 1 {
		if capabilities.Geometry.State == "known" {
			result.State, result.Reason = "unavailable", "monitor_geometry_unavailable"
			if len(capabilities.Geometry.Displays) > 1 {
				result.State, result.Reason = "ambiguous", "monitor_geometry_ambiguous"
			}
		}
		if result.State != "ambiguous" && result.State != "unsupported" {
			result.State = "unavailable"
		}
		return rule, result
	}
	display := capabilities.Geometry.Displays[0]
	if display.Width < 1 || display.Height < 1 || display.Transform != 0 || display.Scale <= 0 || math.IsNaN(display.Scale) || math.IsInf(display.Scale, 0) {
		result.State, result.Reason = "unsupported", "monitor_geometry_unsupported"
		return rule, result
	}
	a, b := display.Width, display.Height
	for b != 0 {
		a, b = b, a%b
	}
	ratio := AspectRatio{Width: display.Width / a, Height: display.Height / a}
	result.State, result.Reason, result.Ratio, result.Display = "resolved", "", fmt.Sprintf("%d:%d", ratio.Width, ratio.Height), &display
	resolved := &RatioRule{ratios: []AspectRatio{ratio}, label: result.Ratio, similar: rule.similar, tolerancePixels: rule.tolerancePixels}
	if rule.similar {
		resolved.label += " ±5%"
	}
	return resolved, result
}

func (s *Service) resolveRatioRule(ctx context.Context, rule *RatioRule, backend string) (*RatioRule, RuleResolution) {
	if rule == nil || !rule.monitor {
		return ResolveRatioRule(rule, desktop.Capabilities{})
	}
	if s.request.InspectCapabilities == nil {
		return rule, RuleResolution{State: "unsupported", Reason: "desktop_capabilities_unavailable"}
	}
	operation, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	capabilities, err := s.request.InspectCapabilities(operation, backend)
	if err != nil || operation.Err() != nil {
		return rule, RuleResolution{State: "unavailable", Reason: "monitor_geometry_query_failed"}
	}
	return ResolveRatioRule(rule, capabilities)
}

func unresolvedRuleError(resolution RuleResolution) *Error {
	return &Error{Code: "rule_unavailable", Message: "Match monitor is unavailable (" + resolution.Reason + "); reconnect a single supported display or choose an explicit image ratio"}
}
