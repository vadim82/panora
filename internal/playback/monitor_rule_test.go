package playback

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/desktop"
)

func TestMonitorRuleResolvesBackingPixelsWithoutScalingAndRejectsAmbiguity(t *testing.T) {
	rule, err := ParseRatioRule("monitor", nil)
	if err != nil || rule.matches(ImageInfo{Width: 1920, Height: 1080}) {
		t.Fatal("unresolved monitor silently accepted an image", err)
	}
	for _, test := range []struct {
		name, backendState, geometryState, want string
		displays                                []desktop.DisplayGeometry
	}{
		{"single Retina", "ready", "known", "resolved", []desktop.DisplayGeometry{{Name: "Retina", Width: 3456, Height: 2234, Scale: 2}}},
		{"no display", "partial", "unavailable", "unavailable", nil},
		{"missing display in known geometry", "ready", "known", "unavailable", nil},
		{"multiple", "partial", "ambiguous", "ambiguous", []desktop.DisplayGeometry{{Width: 1920, Height: 1080, Scale: 1}, {Width: 1920, Height: 1080, Scale: 1}}},
		{"backend ambiguous", "ambiguous", "ambiguous", "ambiguous", nil},
		{"unsupported adapter", "partial", "unsupported", "unsupported", nil},
		{"rotated Hyprland", "partial", "known", "unsupported", []desktop.DisplayGeometry{{Width: 1920, Height: 1080, Scale: 1, Transform: 1}}},
		{"invalid dimensions", "ready", "known", "unsupported", []desktop.DisplayGeometry{{Width: 0, Height: 1080, Scale: 1}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolved, result := ResolveRatioRule(rule, desktop.Capabilities{State: test.backendState, Backend: "macos", Geometry: desktop.Geometry{State: test.geometryState, Displays: test.displays}})
			if result.State != test.want {
				t.Fatalf("resolution=%+v", result)
			}
			if test.want == "resolved" {
				if result.Display.Width != 3456 || result.Display.Height != 2234 || result.Ratio != "1728:1117" || !resolved.matches(ImageInfo{Width: 3456, Height: 2234}) {
					t.Fatalf("backing pixels were scaled or not resolved: %+v", result)
				}
			} else if resolved.matches(ImageInfo{Width: 1920, Height: 1080}) {
				t.Fatal("missing monitor geometry broadened to Any")
			}
		})
	}
}

func TestMonitorGeometryChangesRecheckSelectionHistoryAndKeepAcceptedImage(t *testing.T) {
	root := t.TempDir()
	writeScanImage(t, filepath.Join(root, "wide.png"), 64, 36)
	writeScanImage(t, filepath.Join(root, "square.png"), 64, 64)
	service, _, backend := configurationFixture(t, localTOML(root)+"[filters]\nmin_width=64\nmin_height=36\n")
	refreshForTest(t, service)
	geometry := desktop.Geometry{State: "known", Displays: []desktop.DisplayGeometry{{Name: "single", Width: 1920, Height: 1080, Scale: 2}}}
	service.request.InspectCapabilities = func(ctx context.Context, name string) (desktop.Capabilities, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("geometry query is not bounded")
		}
		caps := testDesktopCapabilities(name)
		caps.Geometry = geometry
		return caps, nil
	}
	before := settings(t, service)
	updated, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, Ratio: configValue("monitor")})
	if err != nil || updated.RuleResolution.State != "resolved" || updated.RuleResolution.Ratio != "16:9" || backend.calls.Load() != 0 {
		t.Fatalf("monitor save=%+v %v", updated, err)
	}
	act(t, service, "next")
	first := service.Status().Current
	if filepath.Base(service.currentOriginPath(t.Context())) != "wide.png" {
		t.Fatal("selection did not use resolved monitor ratio")
	}
	geometry = desktop.Geometry{State: "unavailable"}
	if _, err := service.Action(t.Context(), "next"); err == nil {
		t.Fatal("missing display was treated as Any")
	} else {
		requireConfigError(t, err, "rule_unavailable")
	}
	if got := settings(t, service); got.RuleResolution.State != "unavailable" || got.Revision != updated.Revision {
		t.Fatalf("missing-display feedback=%+v", got)
	}
	if service.Status().Current.ID != first.ID || backend.calls.Load() != 1 || service.Status().Uncertain {
		t.Fatal("geometry loss changed accepted wallpaper or history")
	}
	geometry = desktop.Geometry{State: "known", Displays: []desktop.DisplayGeometry{{Name: "single", Width: 2560, Height: 2560, Scale: 2}}}
	page, err := service.History(t.Context(), "", 10)
	if err != nil || page.Entries[0].Availability != "unavailable" || page.Entries[0].Reason != "filter_mismatch" {
		t.Fatalf("history did not share changed monitor rule: %+v %v", page, err)
	}
	if _, err := service.ReplayHistory(t.Context(), first.ID); err == nil || backend.calls.Load() != 1 {
		t.Fatal("replay ignored changed display geometry")
	}
	act(t, service, "next")
	if filepath.Base(service.currentOriginPath(t.Context())) != "square.png" || backend.calls.Load() != 2 || !service.Status().Paused {
		t.Fatal("next action did not follow changed geometry while preserving pause")
	}
	current := settings(t, service)
	if current.RuleResolution.Ratio != "1:1" || current.RuleResolution.Display.Width != 2560 {
		t.Fatalf("resolution feedback=%+v", current.RuleResolution)
	}
}

func TestNeighborPreviewReleasesPlaybackDuringGeometryAndRejectsStaleResult(t *testing.T) {
	root := t.TempDir()
	writeScanImage(t, filepath.Join(root, "first.png"), 64, 36)
	writeScanImage(t, filepath.Join(root, "second.png"), 128, 72)
	service, _, _ := configurationFixture(t, localTOML(root))
	refreshForTest(t, service)
	first := act(t, service, "next").Current
	act(t, service, "next")
	service.request.InspectCapabilities = func(_ context.Context, name string) (desktop.Capabilities, error) {
		return knownMonitorCapabilities(name, 1920, 1080), nil
	}
	before := settings(t, service)
	if _, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, Ratio: configValue("monitor")}); err != nil {
		t.Fatal(err)
	}
	inspecting := make(chan struct{})
	resume := make(chan struct{})
	service.request.InspectCapabilities = func(ctx context.Context, name string) (desktop.Capabilities, error) {
		close(inspecting)
		select {
		case <-resume:
			return knownMonitorCapabilities(name, 1920, 1080), nil
		case <-ctx.Done():
			return desktop.Capabilities{}, ctx.Err()
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	type result struct {
		preview ImagePreview
		err     error
	}
	done := make(chan result, 1)
	go func() {
		preview, err := service.PreviewNeighbor(ctx, first.ImageID, first.ID)
		done <- result{preview, err}
	}()
	select {
	case <-inspecting:
	case <-ctx.Done():
		t.Fatal("navigation preview did not inspect geometry")
	}
	if _, err := service.Action(ctx, "exclude"); err != nil {
		t.Fatalf("preview held playback ownership during geometry inspection: %v", err)
	}
	close(resume)
	select {
	case got := <-done:
		if got.err != nil || got.preview.Available || got.preview.Reason != "history_unavailable" {
			t.Fatalf("stale preview: available=%t reason=%q %v", got.preview.Available, got.preview.Reason, got.err)
		}
	case <-ctx.Done():
		t.Fatal("navigation preview did not finish")
	}
}

func TestSimilarMonitorRatioBoundariesAndIndependentMinimum(t *testing.T) {
	rules := SourceImageRules{MinWidth: 3024, MinHeight: 1964, Ratio: "monitor-similar"}
	rule, err := ParseRatioRule(rules.Ratio, nil)
	if err != nil || rule.matches(ImageInfo{Width: 3840, Height: 2400}) {
		t.Fatalf("unresolved similar rule accepted an image: %v", err)
	}
	caps := knownMonitorCapabilities("macos", 3024, 1964)
	ratio, resolution := ResolveRatioRule(rule, caps)
	if resolution.State != "resolved" || resolution.Ratio != "756:491" {
		t.Fatalf("similar monitor resolution: %+v", resolution)
	}
	for _, tc := range []struct {
		width, height int
		want          bool
	}{
		{3333, 2165, true},
		{5120, 3328, true},
		{3600, 2400, true},
		{3840, 2400, true},
		{3840, 2160, false},
		{3000, 2000, false},
		{7181, 4910, false},
		{7182, 4910, true},
		{7938, 4910, true},
		{7939, 4910, false},
	} {
		image := ImageInfo{Width: tc.width, Height: tc.height}
		if got := rules.matches(image, ratio); got != tc.want {
			t.Errorf("%dx%d matches = %v, want %v", tc.width, tc.height, got, tc.want)
		}
	}
	if !ratio.matches(ImageInfo{Width: 3000, Height: 2000}) {
		t.Fatal("3000x2000 should fail minimum width only")
	}
	for _, mode := range []string{"monitor", "756:491"} {
		exact, _ := ParseRatioRule(mode, nil)
		exact, _ = ResolveRatioRule(exact, caps)
		near := ImageInfo{Width: 5120, Height: 3328}
		if !exact.matches(near) || exact.WithTolerancePixels(0).matches(near) {
			t.Errorf("%s did not use the configured pixel tolerance for a near match", mode)
		}
		if exact.matches(ImageInfo{Width: 3600, Height: 2400}) {
			t.Errorf("%s used the similar screen percentage for a pixel rule", mode)
		}
	}
}
