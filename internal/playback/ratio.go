package playback

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// RatioRule is the shared image matcher. A nil rule accepts any aspect ratio;
// each selected ratio retains the exact integer matcher's one-pixel tolerance.
type RatioRule struct {
	ratios  []AspectRatio
	label   string
	monitor bool
	similar bool
}

var ratioPresets = map[string][]AspectRatio{
	"16:9": {{16, 9}}, "16:10": {{16, 10}}, "21:9": {{21, 9}, {64, 27}, {43, 18}},
	"32:9": {{32, 9}}, "4:3": {{4, 3}}, "3:2": {{3, 2}}, "1:1": {{1, 1}}, "9:16": {{9, 16}},
}

// ParseRatioRule keeps literal custom ratios exact. The rounded 21:9 preset
// explicitly includes common 2560x1080 and 3440x1440 families, without a broad
// percentage tolerance that would weaken arbitrary custom integer ratios.
func ParseRatioRule(value string, presets []string) (*RatioRule, error) {
	if value != "presets" && len(presets) != 0 {
		return nil, fmt.Errorf("ratio_presets must be empty unless ratio is presets")
	}
	if value == "any" {
		return nil, nil
	}
	if value == "monitor" || value == "monitor-similar" {
		return &RatioRule{label: value, monitor: true, similar: value == "monitor-similar"}, nil
	}
	if value != "presets" {
		ratio, err := ParseAspectRatio(value)
		if err != nil {
			return nil, err
		}
		return &RatioRule{ratios: []AspectRatio{ratio}, label: value}, nil
	}
	if len(presets) == 0 || len(presets) > 12 {
		return nil, fmt.Errorf("choose between 1 and 12 aspect ratios")
	}
	rule := &RatioRule{label: strings.Join(presets, ", ")}
	seen := map[string]bool{}
	for _, name := range presets {
		ratios, ok := ratioPresets[name]
		if seen[name] {
			return nil, fmt.Errorf("aspect ratio %q is duplicated", name)
		}
		if !ok {
			ratio, err := ParseAspectRatio(name)
			if err != nil {
				return nil, err
			}
			ratios = []AspectRatio{ratio}
		}
		seen[name] = true
		for _, ratio := range ratios {
			if !slices.Contains(rule.ratios, ratio) {
				rule.ratios = append(rule.ratios, ratio)
			}
		}
	}
	return rule, nil
}

func (r *RatioRule) matches(image ImageInfo) bool {
	if r == nil {
		return true
	}
	if r.similar && len(r.ratios) == 1 {
		return image.matchesRatio(r.ratios[0], true)
	}
	for _, ratio := range r.ratios {
		if image.matchesAspectRatio(ratio) {
			return true
		}
	}
	return false
}

func singleRatioRule(ratio *AspectRatio) *RatioRule {
	if ratio == nil {
		return nil
	}
	return &RatioRule{ratios: []AspectRatio{*ratio}, label: fmt.Sprintf("%d:%d", ratio.Width, ratio.Height)}
}

func ParseAspectRatio(value string) (AspectRatio, error) {
	widthText, heightText, found := strings.Cut(value, ":")
	if !found {
		return AspectRatio{}, fmt.Errorf("invalid aspect ratio %q: expected width:height (for example, 32:9)", value)
	}

	width, err := strconv.Atoi(widthText)
	if err != nil {
		return AspectRatio{}, fmt.Errorf("invalid aspect ratio %q: width must be an integer: %w", value, err)
	}
	height, err := strconv.Atoi(heightText)
	if err != nil {
		return AspectRatio{}, fmt.Errorf("invalid aspect ratio %q: height must be an integer: %w", value, err)
	}
	if width <= 0 || height <= 0 {
		return AspectRatio{}, fmt.Errorf("invalid aspect ratio %q: width and height must be positive", value)
	}

	return AspectRatio{Width: width, Height: height}, nil
}
