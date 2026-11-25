package source

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

const (
	EditorVersion     = 1
	SuggestMethod     = "suggest"
	MaxSuggestions    = 10
	MaxSuggestionText = 128
)

var editorKey = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,47}$`)
var swatchValue = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// EditorManifest describes host controls. Playback settings such as image rules
// and rotation participation are deliberately outside this schema.
type EditorManifest struct {
	Version int           `json:"version"`
	Groups  []EditorGroup `json:"groups"`
}

type EditorGroup struct {
	ID     string        `json:"id"`
	Label  string        `json:"label,omitempty"`
	Fields []EditorField `json:"fields"`
}

type EditorField struct {
	Key         string           `json:"key"`
	Label       string           `json:"label"`
	Control     string           `json:"control"`
	Help        string           `json:"help,omitempty"`
	Placeholder string           `json:"placeholder,omitempty"`
	Default     json.RawMessage  `json:"default,omitempty"`
	Options     []EditorOption   `json:"options,omitempty"`
	VisibleWhen *EditorCondition `json:"visible_when,omitempty"`
	Suggest     string           `json:"suggest,omitempty"`
	Required    bool             `json:"required,omitempty"`
	Min         *int             `json:"min,omitempty"`
	Max         *int             `json:"max,omitempty"`
}

type EditorOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type EditorCondition struct {
	Key    string          `json:"key"`
	Equals json.RawMessage `json:"equals"`
}

func DecodeEditorManifest(raw json.RawMessage) (EditorManifest, error) {
	var manifest EditorManifest
	if len(raw) > MaxRequestBytes || Decode(raw, &manifest) != nil {
		return manifest, fmt.Errorf("invalid editor manifest; install a provider with a valid editor")
	}
	if manifest.Version != EditorVersion {
		return manifest, fmt.Errorf("unsupported editor version %d; update Panora or install a provider with editor version %d", manifest.Version, EditorVersion)
	}
	if manifest.Groups == nil || len(manifest.Groups) > 16 {
		return manifest, fmt.Errorf("editor must declare an array of up to 16 groups")
	}
	groups := make(map[string]bool)
	fields := make(map[string]EditorField)
	for _, group := range manifest.Groups {
		if !editorKey.MatchString(group.ID) || groups[group.ID] || !editorText(group.Label, 128, true) || group.Fields == nil {
			return manifest, fmt.Errorf("editor has an invalid or duplicate group")
		}
		groups[group.ID] = true
		for _, field := range group.Fields {
			if len(fields) >= 48 || fields[field.Key].Key != "" || !field.valid() {
				return manifest, fmt.Errorf("editor has an invalid or duplicate field")
			}
			fields[field.Key] = field
		}
	}
	for _, field := range fields {
		if when := field.VisibleWhen; when != nil {
			dependency := fields[when.Key]
			if when.Key == field.Key || !slices.Contains([]string{"text", "integer", "checkbox", "select"}, dependency.Control) || !dependency.validValue(when.Equals) {
				return manifest, fmt.Errorf("editor has an invalid visibility condition")
			}
		}
	}
	return manifest, nil
}

func editorText(value string, max int, empty bool) bool {
	return len(value) <= max && (empty || strings.TrimSpace(value) != "") && !strings.ContainsFunc(value, unicode.IsControl)
}

func (f EditorField) valid() bool {
	if !editorKey.MatchString(f.Key) || slices.Contains([]string{"__proto__", "constructor", "prototype"}, f.Key) ||
		!editorText(f.Label, 128, false) || !editorText(f.Help, 512, true) || !editorText(f.Placeholder, 128, true) ||
		(f.Suggest != "" && (f.Control != "multivalue" || f.Suggest != SuggestMethod)) {
		return false
	}
	switch f.Control {
	case "select", "swatches", "bitmask":
		if len(f.Options) == 0 || len(f.Options) > 32 {
			return false
		}
	case "text", "checkbox", "integer", "multivalue":
		if len(f.Options) != 0 {
			return false
		}
	default:
		return false
	}
	if (f.Min != nil || f.Max != nil) && f.Control != "integer" ||
		f.Min != nil && f.Max != nil && *f.Min > *f.Max {
		return false
	}
	seen := make(map[string]bool)
	for _, option := range f.Options {
		if !editorText(option.Label, 128, false) || !editorText(option.Value, 128, false) || seen[option.Value] ||
			f.Control == "swatches" && !swatchValue.MatchString(option.Value) {
			return false
		}
		seen[option.Value] = true
	}
	return len(f.Default) == 0 || f.validValue(f.Default)
}

func (f EditorField) validValue(raw json.RawMessage) bool {
	if len(raw) == 0 || IsNull(raw) {
		return false
	}
	switch f.Control {
	case "integer":
		var value int64
		return json.Unmarshal(raw, &value) == nil && value >= -(1<<53-1) && value <= 1<<53-1 &&
			(f.Min == nil || value >= int64(*f.Min)) && (f.Max == nil || value <= int64(*f.Max))
	case "checkbox":
		var value bool
		return json.Unmarshal(raw, &value) == nil
	case "multivalue", "swatches":
		var values []string
		if json.Unmarshal(raw, &values) != nil || len(values) > 32 {
			return false
		}
		for _, value := range values {
			if !editorText(value, 128, false) || f.Control == "swatches" && !swatchValue.MatchString(value) {
				return false
			}
		}
		return true
	default:
		var value string
		if json.Unmarshal(raw, &value) != nil || !editorText(value, 2048, !f.Required) {
			return false
		}
		switch f.Control {
		case "select":
			return slices.ContainsFunc(f.Options, func(option EditorOption) bool { return option.Value == value })
		case "bitmask":
			return len(value) == len(f.Options) && strings.Trim(value, "01") == ""
		default:
			return true
		}
	}
}

type SuggestParams struct {
	Field  string `json:"field"`
	Prefix string `json:"prefix"`
	Limit  int    `json:"limit"`
}

type suggestionResult struct {
	Values []string `json:"values"`
}

type Suggester interface {
	Suggest(context.Context, SuggestParams) ([]string, error)
}

func (p SuggestParams) Valid() bool {
	return editorKey.MatchString(p.Field) && editorText(p.Prefix, MaxSuggestionText, false) && p.Limit > 0 && p.Limit <= MaxSuggestions
}

func (r suggestionResult) valid(limit int) bool {
	if r.Values == nil || len(r.Values) > limit {
		return false
	}
	for _, value := range r.Values {
		if !editorText(value, MaxSuggestionText, false) {
			return false
		}
	}
	return true
}
