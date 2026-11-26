package source

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

type Dimensions struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type Attribution struct {
	SourceURL   string `json:"source_url,omitempty"`
	Title       string `json:"title,omitempty"`
	PageURL     string `json:"page_url,omitempty"`
	Author      string `json:"author,omitempty"`
	AuthorURL   string `json:"author_url,omitempty"`
	LicenseURL  string `json:"license_url,omitempty"`
	LicenseName string `json:"license_name,omitempty"`
}

// FilterHints are optional provider-side optimizations, never eligibility proof.
type FilterHints struct {
	MinWidth         int          `json:"min_width,omitempty"`
	MinHeight        int          `json:"min_height,omitempty"`
	Ratio            *Dimensions  `json:"ratio,omitempty"`
	Ratios           []Dimensions `json:"ratios,omitempty"`
	ExactResolutions []string     `json:"exact_resolutions,omitempty"`
}

func validDimensions(d *Dimensions) bool { return d == nil || d.Width > 0 && d.Height > 0 }

// Locations are public, credential-free HTTPS URLs. Download-time address,
// redirect, size, and decoded-image checks belong to the future downloader.
func validURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && len(value) <= 4096 && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && !strings.ContainsAny(value, "\r\n\t ")
}

func validateMetadata(d *Dimensions, a *Attribution, requirements []string) error {
	if !validDimensions(d) || len(requirements) != 0 {
		return fmt.Errorf("unsupported provider dimensions or requirements")
	}
	if a != nil {
		if len(a.Title) > 4096 || len(a.Author) > 4096 || len(a.LicenseName) > 128 {
			return fmt.Errorf("provider attribution is too long")
		}
		for _, value := range []string{a.PageURL, a.AuthorURL, a.LicenseURL, a.SourceURL} {
			if value != "" && !validURL(value) {
				return fmt.Errorf("invalid public attribution URL")
			}
		}
	}
	return nil
}

func validHexDigest(value string, wantLength int) bool {
	if len(value) != wantLength || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validateContent(c Content) error {
	if c.MediaKind != "image" || c.MIMEType != "" && c.MIMEType != "image/jpeg" && c.MIMEType != "image/png" {
		return fmt.Errorf("unsupported provider media")
	}
	if c.SHA256 != "" && !validHexDigest(c.SHA256, 64) {
		return fmt.Errorf("invalid original SHA-256 digest")
	}
	if c.RevisionSHA1 != "" && (c.MIMEType == "" || !validHexDigest(c.RevisionSHA1, 40)) {
		return fmt.Errorf("invalid original revision SHA-1")
	}
	switch c.Kind {
	case "local":
		if !filepath.IsAbs(c.Path) || len(c.Path) > 4096 || c.URL != "" {
			return fmt.Errorf("invalid local image location")
		}
	case "https":
		if c.Path != "" || !validURL(c.URL) {
			return fmt.Errorf("invalid public HTTPS image location")
		}
	default:
		return fmt.Errorf("unsupported provider content")
	}
	return nil
}

func validatePage(page Page, limit int) error {
	if page.Candidates == nil || limit < 1 || limit > MaxPageSize || len(page.Candidates) > limit || len(page.NextCursor) == 0 || !json.Valid(page.NextCursor) || len(page.NextCursor) > MaxCursorBytes {
		return fmt.Errorf("provider returned an invalid candidate page")
	}
	seen := make(map[string]bool, len(page.Candidates))
	for _, c := range page.Candidates {
		if seen[c.ID] {
			return fmt.Errorf("provider repeated a candidate ID")
		}
		seen[c.ID] = true
		if err := c.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (c Candidate) Validate() error {
	if c.ID == "" || len(c.ID) > 4096 || c.MediaKind != "image" || !json.Valid(c.Ref) || IsNull(c.Ref) {
		return fmt.Errorf("provider returned an invalid image candidate")
	}
	if err := validateMetadata(c.Dimensions, c.Attribution, c.Requirements); err != nil {
		return err
	}
	if c.Content != nil {
		return validateContent(*c.Content)
	}
	return nil
}

func (result Resolved) Validate() error {
	if err := validateMetadata(result.Dimensions, result.Attribution, result.Requirements); err != nil {
		return err
	}
	return validateContent(result.Content)
}
