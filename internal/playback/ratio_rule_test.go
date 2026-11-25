package playback

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSelectedPresetsUseExactIntegerToleranceAndRoundedAliases(t *testing.T) {
	rule, err := ParseRatioRule("presets", []string{"16:9", "21:9", "1:1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, image := range []ImageInfo{{Width: 1920, Height: 1080}, {Width: 2560, Height: 1080}, {Width: 3440, Height: 1440}, {Width: 5120, Height: 2160}, {Width: 2520, Height: 1080}, {Width: 100, Height: 100}, {Width: 1921, Height: 1080}} {
		if !rule.matches(image) {
			t.Errorf("selected preset rejected %+v", image)
		}
	}
	for _, image := range []ImageInfo{{Width: 1922, Height: 1080}, {Width: 1600, Height: 1000}, {Width: 1080, Height: 1920}, {Width: 0, Height: 0}} {
		if rule.matches(image) {
			t.Errorf("selected preset broadened to %+v", image)
		}
	}
	custom, err := ParseRatioRule("21:9", nil)
	if err != nil || custom.matches(ImageInfo{Width: 3440, Height: 1440}) || !custom.matches(ImageInfo{Width: 2520, Height: 1080}) {
		t.Fatalf("legacy custom ratio changed: %+v %v", custom, err)
	}
	maxInt := int(^uint(0) >> 1)
	if rule.matches(ImageInfo{Width: maxInt, Height: 1}) {
		t.Fatal("extreme integer product wrapped into a match")
	}
	boundary, err := ParseRatioRule(fmt.Sprintf("%d:1", maxInt), nil)
	if err != nil || !boundary.matches(ImageInfo{Width: maxInt, Height: 1}) {
		t.Fatalf("exact boundary ratio failed: %v", err)
	}
}

func TestPresetConfigValidationAndCompatibility(t *testing.T) {
	for _, suffix := range []string{
		"ratio='presets'", "ratio='presets'\nratio_presets=['unknown']", "ratio='presets'\nratio_presets=['16:9','16:9']", "ratio='any'\nratio_presets=['16:9']", "ratio='16:9'\nratio_presets=['16:9']", "ratio='presets'\nratio_presets='16:9'",
	} {
		if _, _, err := ParseConfig([]byte("version=1\n[filters]\n" + suffix)); err == nil {
			t.Fatalf("accepted invalid presets: %s", suffix)
		}
	}
	for _, suffix := range []string{"ratio='any'", "ratio='32:9'", "ratio='presets'\nratio_presets=['16:9','21:9']"} {
		if _, _, err := ParseConfig([]byte("version=1\n[filters]\n" + suffix)); err != nil {
			t.Fatalf("rejected compatible config %s: %v", suffix, err)
		}
	}
}

func TestPresetEditsShareScanningPlaybackHistoryAndStrictMinima(t *testing.T) {
	root := t.TempDir()
	writeScanImage(t, filepath.Join(root, "wide.png"), 64, 36)
	writeScanImage(t, filepath.Join(root, "square.png"), 64, 64)
	service, _, backend := configurationFixture(t, localTOML(root)+"[filters]\nmin_width=64\nmin_height=36\n")
	refreshForTest(t, service)
	act(t, service, "next")
	first := service.Status().Current
	act(t, service, "next")
	before := settings(t, service)
	edited, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, Ratio: configValue("presets"), RatioPresets: configValue([]string{"16:9", "1:1"})})
	if err != nil || len(edited.Rules.RatioPresets) != 2 || backend.calls.Load() != 2 {
		t.Fatalf("preset save=%+v %v", edited, err)
	}
	act(t, service, "previous")
	if service.Status().Current.ID != first.ID {
		t.Fatal("history did not share selected presets")
	}
	_, rule, err := LoadConfig(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	passed := 0
	err = ScanDirectoryContext(t.Context(), root, 0, 64, 36, rule, func(result ScanResult) error {
		if result.Err == nil && len(result.Reasons) == 0 {
			passed++
		}
		return nil
	})
	if err != nil || passed != 2 {
		t.Fatalf("scan matcher=%d %v", passed, err)
	}
	// Ratio tolerance must not make an independently undersized image eligible.
	updated, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: edited.Revision, MinWidth: configValue(65)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Action(t.Context(), "next"); err == nil || backend.calls.Load() != 3 {
		t.Fatal("presets relaxed minimum dimensions or reapplied a wallpaper")
	}
	_, err = service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: updated.Revision, RatioPresets: configValue([]string{})})
	requireConfigError(t, err, "invalid_settings")
	if got := settings(t, service); got.Revision != updated.Revision {
		t.Fatal("invalid empty selection changed settings")
	}
	for _, name := range []string{"wide.png", "square.png"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal("original removed", err)
		}
	}
}
