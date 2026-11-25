package source

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFixtureEditorManifests(t *testing.T) {
	base := filepath.Join("..", "..", "docs", "providers", "examples", "editor")
	for _, name := range []string{"fixture-images", "fixture-album"} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(base, name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var plugin Manifest
			if err := Decode(body, &plugin); err != nil {
				t.Fatal(err)
			}
			editor, err := DecodeEditorManifest(plugin.Editor)
			if err != nil || editor.Version != 1 || len(editor.Groups) != 2 {
				t.Fatalf("editor=%+v err=%v", editor, err)
			}
		})
	}
}

func TestRejectMalformedEditorIndependentlyOfPlugin(t *testing.T) {
	labels := map[string]string{
		"version":                      `{"version":2,"groups":[]}`,
		"null groups":                  `{"version":1,"groups":null}`,
		"unknown control":              `{"version":1,"groups":[{"id":"first","fields":[{"key":"x","label":"X","control":"html"}]}]}`,
		"mismatched default":           `{"version":1,"groups":[{"id":"first","fields":[{"key":"x","label":"X","control":"checkbox","default":"true"}]}]}`,
		"unsafe swatch":                `{"version":1,"groups":[{"id":"first","fields":[{"key":"x","label":"X","control":"swatches","options":[{"value":"red","label":"Red"}]}]}]}`,
		"missing condition dependency": `{"version":1,"groups":[{"id":"first","fields":[{"key":"x","label":"X","control":"text","visible_when":{"key":"missing","equals":"yes"}}]}]}`,
	}
	for name, editor := range labels {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(Manifest{ID: "fixture", Editor: json.RawMessage(editor)})
			if err != nil {
				t.Fatal(err)
			}
			var plugin Manifest
			if err := Decode(raw, &plugin); err != nil {
				t.Fatalf("editor failure should be isolated from provider registration: %v", err)
			}
			if _, err := DecodeEditorManifest(plugin.Editor); err == nil {
				t.Fatal("invalid editor accepted")
			}
		})
	}
}

func TestSuggestionBounds(t *testing.T) {
	valid := SuggestParams{Field: "tag", Prefix: "fo", Limit: 10}
	if !valid.Valid() ||
		(SuggestParams{Field: "tag", Prefix: "  ", Limit: 10}).Valid() ||
		(SuggestParams{Field: "tag", Prefix: strings.Repeat("é", 65), Limit: 10}).Valid() ||
		(SuggestParams{Field: "tag", Prefix: "fo", Limit: 11}).Valid() {
		t.Fatal("unexpected suggestion request validation")
	}
	if !(suggestionResult{Values: []string{"forest"}}).valid(1) ||
		(suggestionResult{Values: []string{"forest", "fox"}}).valid(1) ||
		(suggestionResult{Values: []string{"bad\nname"}}).valid(1) ||
		(suggestionResult{}).valid(1) {
		t.Fatal("unexpected suggestion result validation")
	}
}
