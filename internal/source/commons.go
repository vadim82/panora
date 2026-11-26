package source

import (
	"encoding/json"
	"strings"
	"unicode"
)

const (
	CommonsFeatured      = "featured-landscapes"
	CommonsLive          = "search"
	CommonsMaxPicks      = 36
	CommonsMaxOffset     = 200
	CommonsMaxCategories = 8
)

type CommonsSearch struct {
	Query       string   `json:"query,omitempty"`
	Category    string   `json:"category,omitempty"`
	Categories  []string `json:"categories,omitempty"`
	Format      string   `json:"format,omitempty"`
	MinWidth    int      `json:"min_width,omitempty"`
	MinHeight   int      `json:"min_height,omitempty"`
	ExactWidth  int      `json:"exact_width,omitempty"`
	ExactHeight int      `json:"exact_height,omitempty"`
	Offset      int      `json:"offset,omitempty"`
}

func ValidCommonsCategory(name string) bool {
	return validCommonsCategory(name, MaxSuggestionText)
}

func validCommonsCategory(name string, max int) bool {
	trimmed := strings.TrimSpace(name)
	return editorText(name, max, false) && !strings.ContainsAny(name, `"\|`) &&
		!strings.HasPrefix(strings.ToLower(trimmed), "category:")
}

func (q CommonsSearch) CategoryNames() []string {
	if legacy := strings.TrimSpace(q.Category); legacy != "" {
		return []string{legacy}
	}
	return q.Categories
}

func (q CommonsSearch) Validate() error {
	if strings.TrimSpace(q.Query) == "" && len(q.CategoryNames()) == 0 {
		return Failure("invalid_config", "enter Commons search terms or a category")
	}
	if len(q.Query) > 256 || strings.ContainsAny(q.Query, `"\|:`) || strings.ContainsFunc(q.Query, unicode.IsControl) {
		return Failure("invalid_config", "enter plain Commons search terms")
	}
	if len(q.Categories) > CommonsMaxCategories || q.Category != "" && len(q.Categories) > 0 {
		return Failure("invalid_config", "choose up to eight Commons categories")
	}
	if q.Category != "" && !validCommonsCategory(q.Category, 256) {
		return Failure("invalid_config", "enter a Commons category name without a prefix")
	}
	seen := make(map[string]bool, len(q.Categories))
	for _, name := range q.Categories {
		folded := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), "_", " "))
		if !ValidCommonsCategory(name) || seen[folded] {
			return Failure("invalid_config", "enter distinct Commons category names without the Category prefix")
		}
		seen[folded] = true
	}
	if q.Format != "" && q.Format != "jpeg" && q.Format != "png" {
		return Failure("invalid_config", "choose JPEG or PNG for the Commons file format")
	}
	if (q.ExactWidth == 0) != (q.ExactHeight == 0) {
		return Failure("invalid_config", "provide both exact image dimensions")
	}
	for _, n := range []int{q.MinWidth, q.MinHeight, q.ExactWidth, q.ExactHeight} {
		if n < 0 || n > 65535 {
			return Failure("invalid_config", "Commons dimensions must be between 1 and 65535 pixels when specified")
		}
	}
	if q.Offset < 0 || q.Offset > CommonsMaxOffset {
		return Failure("invalid_config", "Commons search offset exceeds the result limit")
	}
	return nil
}

func (q CommonsSearch) Matches(width, height int, mimeType string) bool {
	if q.Format != "" && mimeType != "image/"+q.Format {
		return false
	}
	if q.ExactWidth > 0 {
		return width == q.ExactWidth && height == q.ExactHeight
	}
	return width >= q.MinWidth && height >= q.MinHeight
}

type CommonsApproval struct {
	PageID      int    `json:"page_id"`
	Revision    string `json:"revision"`
	TermsDigest string `json:"terms_digest"`
}

func (a CommonsApproval) Valid() bool {
	return a.PageID > 0 && validHexDigest(a.Revision, 40) && validHexDigest(a.TermsDigest, 64)
}

type CommonsSearchResult struct {
	CommonsApproval
	Title       string `json:"title"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Author      string `json:"author"`
	LicenseName string `json:"license_name"`
	LicenseURL  string `json:"license_url"`
	PageURL     string `json:"page_url"`
	Thumbnail   string `json:"thumbnail,omitempty"`
}

type CommonsSearchPage struct {
	Version    int                   `json:"version"`
	Results    []CommonsSearchResult `json:"results"`
	NextOffset int                   `json:"next_offset,omitempty"`
}

type CommonsConfig struct {
	CommonsSearch
	Collection string            `json:"collection"`
	Approved   []CommonsApproval `json:"approved,omitempty"`
}

func DecodeCommonsConfig(raw json.RawMessage) (CommonsConfig, error) {
	var c CommonsConfig
	if err := Decode(raw, &c); err != nil {
		return c, Failure("invalid_config", "invalid Commons source settings")
	}
	if c.Collection == CommonsFeatured && len(c.Approved) == 0 &&
		c.Query == "" && c.Category == "" && len(c.Categories) == 0 && c.Format == "" && c.Offset == 0 &&
		c.MinWidth == 0 && c.MinHeight == 0 && c.ExactWidth == 0 && c.ExactHeight == 0 {
		return c, nil
	}
	if c.Collection != CommonsLive || c.Offset != 0 || len(c.Approved) < 1 || len(c.Approved) > CommonsMaxPicks {
		return c, Failure("invalid_config", "choose Featured landscapes or approve between 1 and 36 Commons search results")
	}
	if err := c.CommonsSearch.Validate(); err != nil {
		return c, err
	}
	seen := make(map[int]bool, len(c.Approved))
	for _, a := range c.Approved {
		if !a.Valid() || seen[a.PageID] {
			return c, Failure("invalid_config", "Commons approvals require distinct file revisions and reuse terms")
		}
		seen[a.PageID] = true
	}
	return c, nil
}

func (c CommonsConfig) Allows(candidate Candidate) bool {
	if candidate.Content == nil {
		return false
	}
	if c.Collection == CommonsFeatured {
		return candidate.Content.SHA256 != "" && candidate.Content.RevisionSHA1 == ""
	}
	var approval CommonsApproval
	if Decode(candidate.Ref, &approval) != nil || !approval.Valid() ||
		candidate.Content.RevisionSHA1 != approval.Revision || candidate.Dimensions == nil ||
		!c.Matches(candidate.Dimensions.Width, candidate.Dimensions.Height, candidate.Content.MIMEType) {
		return false
	}
	for _, saved := range c.Approved {
		if saved == approval {
			return true
		}
	}
	return false
}
