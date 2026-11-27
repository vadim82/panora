package playback

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSelectedPresetsUseExactIntegerToleranceAndRoundedAliases(t *testing.T) {
	rule, err := parseRatioRule("presets", []string{"16:9", "21:9", "1:1"}, 1)
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

func TestPixelRatioToleranceDefaultAndBoundaries(t *testing.T) {
	for _, tc := range []struct {
		setting string
		pixels  int
	}{
		{pixels: 10},
		{setting: "ratio_tolerance_pixels=0", pixels: 0},
		{setting: "ratio_tolerance_pixels=4", pixels: 4},
	} {
		config, ratio, err := ParseConfig([]byte("version=1\n[filters]\nratio='16:9'\n" + tc.setting))
		if err != nil || config.RatioTolerancePixels() != tc.pixels {
			t.Fatalf("setting %q: got tolerance %d, err %v", tc.setting, config.RatioTolerancePixels(), err)
		}
		if !ratio.matches(ImageInfo{Width: 1600, Height: 900 + tc.pixels}) ||
			ratio.matches(ImageInfo{Width: 1600, Height: 901 + tc.pixels}) {
			t.Fatalf("setting %q did not honor the %d-pixel boundary", tc.setting, tc.pixels)
		}
	}
}

func TestPixelRatioToleranceEditRechecksCustomSourceWithoutRediscovery(t *testing.T) {
	root := t.TempDir()
	writeScanImage(t, filepath.Join(root, "almost-wide.png"), 320, 185)
	service, path, backend := configurationFixture(t, localTOML(root)+
		"[providers.folders.image_rules]\nmin_width=320\nmin_height=180\nratio='presets'\nratio_presets=['16:9']\n")
	refreshForTest(t, service)
	before := settings(t, service)
	config, _, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	inputRevision := config.InputRevision("local", "home")
	request := RulePreviewRequest{Revision: before.Revision, ProviderID: "local", InputID: "home",
		Rules: ImageRules{MinWidth: 320, MinHeight: 180, MaxDepth: 2, Ratio: "presets", RatioPresets: []string{"16:9"}}}
	preview, err := service.PreviewRules(t.Context(), request)
	if err != nil || preview.Matched != 1 {
		t.Fatalf("default tolerance preview=%+v %v", preview, err)
	}
	act(t, service, "next")
	request.Rules.RatioTolerancePixels = configValue(0)
	preview, err = service.PreviewRules(t.Context(), request)
	if err != nil || preview.Reasons.RatioMismatch != 1 || backend.calls.Load() != 1 {
		t.Fatalf("draft tolerance preview=%+v %v", preview, err)
	}
	_, err = service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{
		Revision: before.Revision, RatioTolerancePixels: configValue(-1),
	})
	requireConfigError(t, err, "invalid_settings")
	edited, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{
		Revision: before.Revision, RatioTolerancePixels: configValue(0),
	})
	if err != nil || edited.Revision == before.Revision || edited.Rules.RatioTolerancePixels == nil || *edited.Rules.RatioTolerancePixels != 0 {
		t.Fatalf("saved zero tolerance=%+v %v", edited.Rules, err)
	}
	config, _, err = LoadConfig(path)
	if err != nil || config.RatioTolerancePixels() != 0 || config.InputRevision("local", "home") != inputRevision {
		t.Fatalf("persisted tolerance=%d revision=%s err=%v", config.RatioTolerancePixels(), config.InputRevision("local", "home"), err)
	}
	_, err = service.PreviewRules(t.Context(), request)
	requireConfigError(t, err, "config_conflict")
	request.Revision, request.Rules.RatioTolerancePixels = edited.Revision, nil
	preview, err = service.PreviewRules(t.Context(), request)
	if err != nil || preview.Reasons.RatioMismatch != 1 {
		t.Fatalf("saved zero tolerance preview=%+v %v", preview, err)
	}
	if _, err = service.Action(t.Context(), "next"); err == nil || backend.calls.Load() != 1 {
		t.Fatalf("after saving zero tolerance next returned %v and called desktop %d times", err, backend.calls.Load())
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

func TestSimilarScreenToleranceAppliesToDiscoveryAndPlayback(t *testing.T) {
	percent := 10
	rules := SourceImageRules{MinWidth: 2560, MinHeight: 720, Ratio: "monitor-similar", RatioTolerancePercent: &percent}
	if err := validateSourceRules(&rules); err != nil {
		t.Fatal(err)
	}
	ratio := rules.ratioRule(10)
	ratio.monitor = false // The display was resolved.
	ratio.ratios = []AspectRatio{{Width: 32, Height: 9}}
	filter := discoveryFilter(rules, ratio, []string{"min_width", "min_height", "ratio_range"})
	if filter.RatioRange == nil || (filter.RatioRange.Min < 3.1999999 || filter.RatioRange.Min > 3.2000001) || !rules.matches(ImageInfo{Width: 3840, Height: 1200}, ratio) ||
		rules.matches(ImageInfo{Width: 3839, Height: 1200}, ratio) {
		t.Fatalf("upstream and local rules disagree: %+v", filter)
	}
	percent = 0
	if rules.matches(ImageInfo{Width: 3840, Height: 1200}, ratio) ||
		!rules.matches(ImageInfo{Width: 3840, Height: 1080}, ratio) {
		t.Fatal("zero tolerance did not require the exact shape")
	}
}
