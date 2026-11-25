package playback

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/desktop"
)

func testDesktopCapabilities(name string) desktop.Capabilities {
	backend := name
	if backend == "auto" {
		backend = "macos"
	}
	presentations := []desktop.PresentationCapability{{Fit: "auto", Available: true, Upscale: true}, {Fit: "contain", Available: true, Upscale: true}, {Fit: "crop", Available: true}}
	if backend == "noctalia" {
		presentations = []desktop.PresentationCapability{{Fit: "auto", Available: true}, {Fit: "contain"}, {Fit: "crop"}}
	}
	return desktop.Capabilities{Version: 1, RequestedBackend: name, Backend: backend, State: "partial", Presentations: presentations}
}

type optionsBackend struct {
	mu      sync.Mutex
	options []desktop.DisplayOptions
}

func (*optionsBackend) Probe(context.Context) error { return nil }
func (b *optionsBackend) SetWallpaper(_ context.Context, _ string, options desktop.DisplayOptions) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.options = append(b.options, options)
	return nil
}

func TestDesktopEditChangesOnlyNextActionAndPreservesPlayback(t *testing.T) {
	root := t.TempDir()
	writeScanImage(t, filepath.Join(root, "one.png"), 2, 2)
	writeScanImage(t, filepath.Join(root, "two.png"), 2, 2)
	service, _, oldBackend := configurationFixture(t, localTOML(root)+"[filters]\nmin_width=2\nmin_height=2\n")
	refreshForTest(t, service)
	act(t, service, "next")
	act(t, service, "resume")
	before := service.Status()
	state := service.state
	nextBackend := &optionsBackend{}
	resolves := 0
	service.request.ResolveBackend = func(_ context.Context, name string) (desktop.WallpaperBackend, error) {
		resolves++
		if name != "macos" {
			t.Errorf("resolved backend=%s", name)
		}
		return nextBackend, nil
	}
	settingsBefore := settings(t, service)
	saved, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: settingsBefore.Revision, Backend: configValue("macos"), Fit: configValue("contain"), Upscale: configValue(true)})
	if err != nil {
		t.Fatal(err)
	}
	after := service.Status()
	if resolves != 0 || oldBackend.calls.Load() != 1 || len(nextBackend.options) != 0 || !reflect.DeepEqual(service.state, state) || !reflect.DeepEqual(before.NextAt, after.NextAt) || before.Paused != after.Paused || after.Backend != "macos" {
		t.Fatalf("edit changed playback: before=%+v after=%+v", before, after)
	}
	if saved.Effective.Backend != "macos" || saved.Effective.Fit != "contain" || !saved.Effective.Upscale || len(saved.RestartRequired) != 1 || saved.RestartRequired[0] != "interval" {
		t.Fatalf("saved/effective feedback=%+v", saved)
	}
	capabilities, err := service.Capabilities(t.Context())
	if err != nil || capabilities.RequestedBackend != "macos" || capabilities.Backend != "macos" {
		t.Fatalf("runtime capabilities=%+v %v", capabilities, err)
	}
	act(t, service, "next")
	act(t, service, "pause")
	act(t, service, "previous")
	if resolves != 2 || oldBackend.calls.Load() != 1 || len(nextBackend.options) != 2 || nextBackend.options[0] != (desktop.DisplayOptions{Fit: "contain", Upscale: true}) || nextBackend.options[1] != nextBackend.options[0] || !service.Status().Paused || service.Status().Current.ID != before.Current.ID {
		t.Fatalf("next/history did not use new options: %+v %+v", nextBackend.options, service.Status())
	}
	service.Close()
	request := service.request
	config, _, err := LoadConfig(request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	request.Options = config.Desktop.Options()
	restarted, err := OpenService(t.Context(), ServiceOptions{Request: request, Backend: config.Desktop.Backend}, service.resolve)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if got := settings(t, restarted); got.Effective.Backend != "macos" || got.Effective.Fit != "contain" || !got.Effective.Upscale || len(got.RestartRequired) != 0 || !restarted.Status().Paused {
		t.Fatalf("restart=%+v", got)
	}
}

func TestDesktopValidationRejectsUnavailableAmbiguousAndUnsupportedBeforeSaving(t *testing.T) {
	for _, test := range []struct {
		name    string
		backend string
		fit     string
		upscale bool
		change  func(*desktop.Capabilities)
		code    string
	}{
		{name: "auto has no ready adapters", backend: "auto", fit: "auto", change: func(c *desktop.Capabilities) { c.State, c.Backend = "unavailable", "" }, code: "backend_unavailable"},
		{name: "auto has multiple ready adapters", backend: "auto", fit: "auto", change: func(c *desktop.Capabilities) { c.State, c.Backend = "ambiguous", "" }, code: "backend_ambiguous"},
		{name: "explicit unavailable", backend: "macos", fit: "auto", change: func(c *desktop.Capabilities) { c.State, c.Backend = "unavailable", "" }, code: "backend_unavailable"},
		{name: "explicit never falls back", backend: "macos", fit: "auto", change: func(c *desktop.Capabilities) { c.Backend = "noctalia" }, code: "backend_unavailable"},
		{name: "unsupported containment", backend: "noctalia", fit: "contain", code: "unsupported_presentation"},
		{name: "unsupported upscale", backend: "noctalia", fit: "auto", upscale: true, code: "unsupported_presentation"},
		{name: "Omarchy geometry unavailable", backend: "omarchy", fit: "contain", change: func(c *desktop.Capabilities) { c.Presentations[1].Available = false }, code: "unsupported_presentation"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, _, backend := configurationFixture(t, "version=1\n")
			before := settings(t, service)
			deadline := service.nextAt
			service.request.InspectCapabilities = func(ctx context.Context, name string) (desktop.Capabilities, error) {
				if _, exists := ctx.Deadline(); !exists {
					t.Fatal("probe is not bounded")
				}
				result := testDesktopCapabilities(name)
				if test.change != nil {
					test.change(&result)
				}
				return result, nil
			}
			_, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, Backend: &test.backend, Fit: &test.fit, Upscale: &test.upscale, Interval: configValue("2m")})
			requireConfigError(t, err, test.code)
			after := settings(t, service)
			if after.TOML != before.TOML || after.Revision != before.Revision || after.Effective != before.Effective || service.nextAt != deadline || backend.calls.Load() != 0 {
				t.Fatal("rejected desktop patch partially saved or applied preferences")
			}
		})
	}
}

func TestDesktopExplicitEditsReplaceOnlyTheirTemporaryOverrides(t *testing.T) {
	service, _, _ := configurationFixture(t, "# preserve exact bytes\nversion=1\n[desktop]\nbackend='macos'\n")
	service.backend = "macos"
	service.request.Options = desktop.DisplayOptions{Fit: "contain", Upscale: true}
	service.effectiveSettings.Backend, service.effectiveSettings.Fit, service.effectiveSettings.Upscale = "macos", "contain", true
	before := settings(t, service)
	updated, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, MinWidth: configValue(1920)})
	if err != nil || updated.Effective.Fit != "contain" || !updated.Effective.Upscale {
		t.Fatalf("unrelated save replaced overrides: %+v %v", updated, err)
	}
	// Same-as-saved values explicitly supersede an override without rewriting TOML.
	noOp, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: updated.Revision, Fit: configValue("auto")})
	if err != nil || noOp.TOML != updated.TOML || noOp.Effective.Fit != "auto" || !noOp.Effective.Upscale || slices.Contains(noOp.RestartRequired, "fit") || !slices.Contains(noOp.RestartRequired, "upscale") {
		t.Fatalf("explicit Fit no-op=%+v %v", noOp, err)
	}
	// A crop edit must not silently discard a still-active upscaling override.
	_, err = service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: noOp.Revision, Fit: configValue("crop")})
	requireConfigError(t, err, "invalid_settings")
	final, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: noOp.Revision, Fit: configValue("crop"), Upscale: configValue(false)})
	if err != nil || final.Effective.Fit != "crop" || final.Effective.Upscale || len(final.RestartRequired) != 1 || final.RestartRequired[0] != "interval" {
		t.Fatalf("explicit combined edit=%+v %v", final, err)
	}
}

func TestDesktopOverridesCannotHideUnsupportedSavedSettings(t *testing.T) {
	for _, test := range []struct {
		name  string
		saved string
	}{
		{name: "contain hidden by auto override", saved: "fit='contain'\n"},
		{name: "upscaling hidden by false override", saved: "upscale=true\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, path, backend := configurationFixture(t, "# keep exact bytes\nversion=1\n[desktop]\nbackend='macos'\n"+test.saved)
			// Model serve --fit auto --upscale=false against saved presentation
			// preferences. Noctalia supports these active options, but not the
			// omitted saved preference once the temporary flag is gone.
			service.backend = "macos"
			service.effectiveSettings.Backend = "macos"
			before := settings(t, service)
			deadline := service.nextAt
			_, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, Backend: configValue("noctalia")})
			requireConfigError(t, err, "unsupported_presentation")
			after := settings(t, service)
			data, err := os.ReadFile(path)
			if err != nil || string(data) != before.TOML || after.Revision != before.Revision || after.Effective != before.Effective || service.nextAt != deadline || backend.calls.Load() != 0 {
				t.Fatalf("override hid unsupported saved settings: after=%+v read=%v", after, err)
			}
			// An explicit compatible saved combination is accepted atomically.
			updated, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, Backend: configValue("noctalia"), Fit: configValue("auto"), Upscale: configValue(false)})
			if err != nil || updated.Saved.Backend != "noctalia" || updated.Saved.Fit != "auto" || updated.Saved.Upscale || updated.Effective.Backend != "noctalia" || updated.Effective.Fit != "auto" || updated.Effective.Upscale || backend.calls.Load() != 0 {
				t.Fatalf("compatible combined edit=%+v %v", updated, err)
			}
		})
	}
}

func TestDesktopAutoRetainsRequestedChoiceAndUsesResolverOnEachAction(t *testing.T) {
	root := t.TempDir()
	writeScanImage(t, filepath.Join(root, "one.png"), 2, 2)
	service, _, _ := configurationFixture(t, localTOML(root)+"[filters]\nmin_width=2\nmin_height=2\n")
	refreshForTest(t, service)
	before := settings(t, service)
	updated, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, Backend: configValue("auto")})
	if err != nil || updated.Effective.Backend != "auto" || slices.Contains(updated.RestartRequired, "backend") {
		t.Fatalf("auto=%+v %v", updated, err)
	}
	capabilities, err := service.Capabilities(t.Context())
	if err != nil || capabilities.Backend != "macos" || capabilities.RequestedBackend != "auto" {
		t.Fatalf("auto resolution=%+v %v", capabilities, err)
	}
	// Readiness can change after a valid edit. An explicit action reports the new
	// failure without accepting an intent or falling back to an old backend.
	service.request.ResolveBackend = func(_ context.Context, name string) (desktop.WallpaperBackend, error) {
		if name != "auto" {
			t.Fatalf("resolved %s instead of auto", name)
		}
		return nil, errors.New("desktop went away")
	}
	_, err = service.Action(t.Context(), "next")
	requireConfigError(t, err, "backend_unavailable")
	if status := service.Status(); status.Uncertain || status.Current != nil || status.HistoryLength != 0 {
		t.Fatalf("unavailable action changed accepted state=%+v", status)
	}
}

func TestDesktopCanceledAndFailedPublicationDoNotChangeEffectiveSettings(t *testing.T) {
	service, path, _ := configurationFixture(t, "version=1\n")
	before := settings(t, service)
	ctx, cancel := context.WithCancel(t.Context())
	service.request.InspectCapabilities = func(context.Context, string) (desktop.Capabilities, error) {
		cancel()
		return testDesktopCapabilities("macos"), nil
	}
	_, err := service.UpdateConfiguration(ctx, UpdateConfigurationRequest{Revision: before.Revision, Backend: configValue("macos")})
	if !errors.Is(err, context.Canceled) || settings(t, service).Effective != before.Effective || settings(t, service).TOML != before.TOML {
		t.Fatalf("canceled validation published settings=%v", err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	service.request.InspectCapabilities = func(_ context.Context, name string) (desktop.Capabilities, error) {
		return testDesktopCapabilities(name), nil
	}
	if err := os.Chmod(filepath.Dir(path), 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(path), 0700) })
	_, err = service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, Backend: configValue("macos")})
	requireConfigError(t, err, "config_write_failed")
	if got := settings(t, service); got.Effective != before.Effective || got.TOML != before.TOML {
		t.Fatal("failed publication changed running desktop settings")
	}
}

func TestCapabilitiesReadsRaceWithDesktopEdits(t *testing.T) {
	root := t.TempDir()
	writeScanImage(t, filepath.Join(root, "one.png"), 2, 2)
	service, _, _ := configurationFixture(t, localTOML(root)+"[filters]\nmin_width=2\nmin_height=2\n")
	service.StartDiscovery()
	awaitCondition(t, func() bool {
		status := service.Status()
		return len(status.Providers) == 1 && len(status.Providers[0].Inputs) == 1 && status.Providers[0].Inputs[0].State == "ready"
	})
	var readers sync.WaitGroup
	stop := make(chan struct{})
	readers.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
				if _, err := service.Capabilities(t.Context()); err != nil {
					t.Errorf("capabilities=%v", err)
					return
				}
			}
		}
	})
	defer func() { close(stop); readers.Wait() }()
	for _, name := range []string{"macos", "auto", "noctalia", "macos"} {
		before := settings(t, service)
		// Changing an image-rule input revision also reconciles provider jobs,
		// exercising discovery provider lookups alongside active-options writes.
		depth := before.Rules.MaxDepth + 1
		if _, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, Backend: &name, MaxDepth: &depth}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDesktopProbeStopsWithService(t *testing.T) {
	service, _, _ := configurationFixture(t, "version=1\n")
	before := settings(t, service)
	entered := make(chan struct{})
	service.request.InspectCapabilities = func(ctx context.Context, _ string) (desktop.Capabilities, error) {
		close(entered)
		<-ctx.Done()
		return desktop.Capabilities{}, ctx.Err()
	}
	done := make(chan error, 1)
	go func() {
		_, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, Backend: configValue("macos")})
		done <- err
	}()
	<-entered
	service.Close()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("stopped probe=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("desktop validation continued after service shutdown")
	}
}
