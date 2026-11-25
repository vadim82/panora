package playback

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/vadim82/panora/internal/source"
)

type externalFixture struct{ path string }

func (f externalFixture) Describe(context.Context) (source.Description, error) {
	return source.Description{Protocol: source.Protocol, ID: "fixture", MediaKinds: []string{"image"},
		ContentKinds: []string{"local"}, Operations: []string{"describe", "validate", "discover", "resolve"}}, nil
}

func (f externalFixture) Validate(_ context.Context, raw json.RawMessage) error {
	var config struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(raw, &config); err != nil || config.Mode != "square" {
		return errors.New("invalid mode")
	}
	return nil
}

func (f externalFixture) file() source.Content {
	return source.Content{Kind: "local", MediaKind: "image", Path: f.path}
}

func (f externalFixture) Discover(context.Context, source.DiscoverParams) (source.Page, error) {
	file := f.file()
	return source.Page{NextCursor: json.RawMessage("null"), Candidates: []source.Candidate{
		{ID: "still", MediaKind: "image", Ref: json.RawMessage(`{}`), Content: &file},
	}}, nil
}

func (f externalFixture) Resolve(context.Context, source.ResolveParams) (source.Resolved, error) {
	return source.Resolved{Content: f.file()}, nil
}

func TestProviderInputRulesAndRejectedEdits(t *testing.T) {
	s, _, backend := configurationFixture(t, localTOML(t.TempDir())+"[filters]\nmin_width=100\nmin_height=60\nratio='16:9'\n")
	image := filepath.Join(t.TempDir(), "square.png")
	writeScanImage(t, image, 64, 64)
	s.request.Providers = map[string]source.Provider{"fixture": externalFixture{path: image}}
	s.request.ProviderEditors = []ProviderEditor{{ID: "fixture", Name: "Fixture", Editor: &source.EditorManifest{
		Version: source.EditorVersion,
		Groups:  []source.EditorGroup{},
	}}}
	edit := ProviderInputEdit{ProviderID: "fixture", Revision: settings(t, s).Revision,
		Name: configValue("Square"), Values: json.RawMessage(`{"mode":"square"}`), InheritRules: configValue(true)}
	result, err := s.AddProviderInput(t.Context(), edit)
	if err != nil {
		t.Fatal(err)
	}
	got := result.Inputs[0]
	if got.EffectiveRules.MinWidth != 100 || got.ImageRules != nil || !got.Enabled {
		t.Fatalf("inherited input=%+v", got)
	}
	refreshForTest(t, s)
	if _, err := s.Action(t.Context(), "next"); err == nil || backend.calls.Load() != 0 {
		t.Fatalf("external discovery bypassed decoded rules: %v", err)
	}
	edit.ID, edit.Revision = got.ID, result.Revision
	edit.Values = json.RawMessage(`{"mode":"wrong"}`)
	_, err = s.UpdateProviderInput(t.Context(), edit)
	requireConfigError(t, err, "invalid_settings")
	edit.Revision = "stale"
	_, err = s.UpdateProviderInput(t.Context(), edit)
	requireConfigError(t, err, "config_conflict")
	if settings(t, s).TOML != result.TOML {
		t.Fatal("invalid edit changed saved settings")
	}

	edit.Revision, edit.InheritRules, edit.Values = result.Revision, nil, nil
	edit.ImageRules = &SourceImageRules{MinWidth: 1, MinHeight: 1, Ratio: "any"}
	result, err = s.UpdateProviderInput(t.Context(), edit)
	if err != nil {
		t.Fatal(err)
	}
	if result.Folders[0].EffectiveRules.MinWidth != 100 || result.Inputs[0].EffectiveRules.Ratio != "any" {
		t.Fatalf("override leaked to shared rules: %+v", result)
	}
	refreshForTest(t, s)
	if _, err := s.Action(t.Context(), "next"); err != nil || backend.calls.Load() != 1 {
		t.Fatalf("qualifying provider image did not play: %v, status=%+v", err, s.Status())
	}
	edit.Revision, edit.InheritRules, edit.ImageRules = result.Revision, configValue(true), nil
	result, err = s.UpdateProviderInput(t.Context(), edit)
	if err != nil {
		t.Fatal(err)
	}
	if saved := settings(t, s); result.Revision != saved.Revision || saved.Inputs[0].ImageRules != nil || saved.Inputs[0].EffectiveRules.Ratio != "16:9" {
		t.Fatalf("inheritance did not persist: %+v", saved.Inputs)
	}
}
