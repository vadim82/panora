package source

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	WallhavenTagMatchAny = "any"
	WallhavenTagMatchAll = "all"
	MaxWallhavenTags     = 16
	MaxWallhavenColors   = 6
	MaxExactResolutions  = 12
)

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
var resolutionSyntax = regexp.MustCompile(`^[1-9][0-9]*x[1-9][0-9]*$`)
var standaloneTagQuery = regexp.MustCompile(`(?i)(^|[[:space:]])id:[0-9]+([[:space:]]|$)`)

// WallhavenConfig is the bundled provider's wire settings. Host-side saved edits
// share this validation without depending on a provider implementation or HTTP.
type WallhavenConfig struct {
	Query      string   `json:"query"`
	Categories string   `json:"categories,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	TagMatch   string   `json:"tag_match,omitempty"`
	Colors     []string `json:"colors,omitempty"`
	Sorting    string   `json:"sorting,omitempty"`
	Order      string   `json:"order,omitempty"`
	MaxPages   int      `json:"max_pages,omitempty"`
	Revision   string   `json:"revision,omitempty"`
	TopRange   string   `json:"top_range,omitempty"`
}

func DefaultWallhavenConfig() WallhavenConfig {
	return WallhavenConfig{Categories: "100", Sorting: "relevance", Order: "desc", MaxPages: 20, TopRange: "1M"}
}

func (c WallhavenConfig) EffectiveTagMatch() string {
	if c.TagMatch == "" {
		return WallhavenTagMatchAll
	}
	return c.TagMatch
}

func (c WallhavenConfig) Validate() error {
	if mode := c.EffectiveTagMatch(); mode != WallhavenTagMatchAny && mode != WallhavenTagMatchAll {
		return Failure("invalid_config", "tag_match must be any or all")
	}
	if len(c.Query) > 2048 || strings.ContainsAny(c.Query, "\r\n\x00") || len(c.Revision) > 128 {
		return Failure("invalid_config", "query must be at most 2048 bytes without NUL or newlines; revision at most 128 bytes")
	}
	if c.Categories != "" && (len(c.Categories) != 3 || strings.Trim(c.Categories, "01") != "") {
		return Failure("invalid_config", "categories must be a three-bit mask")
	}
	if len(c.Tags) > MaxWallhavenTags || len(c.Tags) > 0 && standaloneTagQuery.MatchString(c.Query) {
		return Failure("invalid_config", "use up to 16 tags, or use a standalone id: search without tags")
	}
	for i, tag := range c.Tags {
		// Structured tags are literals, so they cannot add a Wallhaven operator.
		if tag != strings.TrimSpace(tag) || len(tag) == 0 || len(tag) > 128 ||
			strings.ContainsAny(tag, "\\\"\r\n\t\x00") || strings.HasPrefix(tag, "+") ||
			strings.HasPrefix(tag, "-") || strings.HasPrefix(tag, "@") || strings.Contains(tag, ":") ||
			slices.ContainsFunc(c.Tags[:i], func(previous string) bool { return strings.EqualFold(previous, tag) }) {
			return Failure("invalid_config", "tags must be distinct plain names of at most 128 bytes")
		}
	}
	if len(c.Colors) > MaxWallhavenColors {
		return Failure("invalid_config", "choose up to 6 colors")
	}
	for i, color := range c.Colors {
		if !IsHexColor(color) || slices.ContainsFunc(c.Colors[:i], func(previous string) bool { return strings.EqualFold(previous, color) }) {
			return Failure("invalid_config", "colors must be distinct #RRGGBB values")
		}
	}
	switch c.Sorting {
	case "date_added", "relevance", "random", "views", "favorites", "toplist":
	default:
		return Failure("invalid_config", "unsupported Wallhaven sorting")
	}
	if c.Order != "asc" && c.Order != "desc" || c.MaxPages < 1 || c.MaxPages > 100 {
		return Failure("invalid_config", "order must be asc/desc and max_pages between 1 and 100")
	}
	switch c.TopRange {
	case "1d", "3d", "1w", "1M", "3M", "6M", "1y":
	default:
		return Failure("invalid_config", "unsupported toplist period")
	}
	return nil
}

func IsHexColor(value string) bool { return hexColor.MatchString(value) }

func ParseExactResolution(value string) (Dimensions, bool) {
	if !resolutionSyntax.MatchString(value) {
		return Dimensions{}, false
	}
	widthText, heightText, _ := strings.Cut(value, "x")
	width, widthErr := strconv.Atoi(widthText)
	height, heightErr := strconv.Atoi(heightText)
	return Dimensions{Width: width, Height: height}, widthErr == nil && heightErr == nil
}
