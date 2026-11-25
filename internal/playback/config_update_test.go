package playback

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/desktop"
)

func configValue[T any](value T) *T { return &value }

func TestPreferenceEditsPreserveInputsAndExposeSavedEffectiveAndRestart(t *testing.T) {
	service, path, backend := configurationFixture(t, localTOML(t.TempDir())+"name='Saved folder'\nenabled=false\nmax_depth=1\nrefresh_interval='10s'\n[[providers]]\nid='wallhaven'\nenabled=false\n[[providers.searches]]\nid='saved-search'\nquery='a public search'\n")
	before := settings(t, service)
	original, _, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	edited, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{
		Revision: before.Revision, Backend: configValue("macos"), Fit: configValue("contain"), Upscale: configValue(true), Interval: configValue("2m"), CacheMaxImages: configValue(7), CacheMaxBytes: configValue(int64(32 << 20)), MinWidth: configValue(1920), MinHeight: configValue(1080), Ratio: configValue("16:9"), MaxDepth: configValue(0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if edited.Revision == before.Revision || edited.Saved.Interval != "2m" || edited.Saved.Fit != "contain" || !edited.Saved.Upscale || edited.Saved.CacheMaxImages != 7 || edited.Rules.MinWidth != 1920 || edited.Rules.MinHeight != 1080 || edited.Rules.Ratio != "16:9" || edited.Rules.MaxDepth != 0 {
		t.Fatalf("saved=%+v", edited)
	}
	expectedEffective := before.Effective
	expectedEffective.Interval = "2m0s"
	expectedEffective.CacheMaxImages, expectedEffective.CacheMaxBytes = 7, 32<<20
	expectedEffective.Backend, expectedEffective.Fit, expectedEffective.Upscale = "macos", "contain", true
	if edited.Effective != expectedEffective || service.Status().Interval != "2m0s" || !service.Status().Paused || backend.calls.Load() != 0 {
		t.Fatal("save changed unrelated startup state or desktop")
	}
	for _, field := range []string{"backend", "fit", "upscale"} {
		if slices.Contains(edited.RestartRequired, field) {
			t.Fatalf("runtime field %s requires restart: %v", field, edited.RestartRequired)
		}
	}
	saved, _, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved.Providers[0].Folders[0], original.Providers[0].Folders[0]) || !reflect.DeepEqual(saved.Providers[1].Searches[0], original.Providers[1].Searches[0]) || saved.Providers[1].Enabled {
		t.Fatal("preference edit altered inputs")
	}
	// false is an explicit update; it must not be mistaken for an omitted field.
	edited, err = service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: edited.Revision, Upscale: configValue(false)})
	if err != nil || edited.Saved.Upscale {
		t.Fatalf("false update=%+v %v", edited, err)
	}
	service.Close()
	request := service.request
	request.Options = saved.Desktop.Options()
	request.Options.Upscale = false
	restarted, err := OpenService(t.Context(), ServiceOptions{Request: request, Backend: edited.Saved.Backend}, func(context.Context) (desktop.WallpaperBackend, error) { return backend, nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Close)
	after := settings(t, restarted)
	if after.Effective.Interval != "2m0s" || after.Effective.Fit != "contain" || after.Effective.Upscale || after.Effective.CacheMaxImages != 7 || len(after.RestartRequired) != 0 || after.Revision != edited.Revision || backend.calls.Load() != 0 {
		t.Fatalf("restart=%+v", after)
	}
}

func TestPreferenceNoOpKeepsExactBytesAndDurationFeedbackIsSemantic(t *testing.T) {
	service, _, _ := configurationFixture(t, "# retain this comment and layout\nversion=1\n[playback]\ninterval='60m'\n")
	before := settings(t, service)
	if slices.Contains(before.RestartRequired, "interval") {
		t.Fatal("equivalent duration requires restart")
	}
	after, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, MinWidth: configValue(before.Rules.MinWidth), Upscale: configValue(false)})
	if err != nil || after.Revision != before.Revision || after.TOML != before.TOML {
		t.Fatalf("no-op changed document: %+v %v", after, err)
	}
	_, err = service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision})
	requireConfigError(t, err, "invalid_request")
}

func TestInvalidPreferenceEditsAreAtomic(t *testing.T) {
	service, path, backend := configurationFixture(t, "version=1\n")
	before := settings(t, service)
	for _, edit := range []UpdateConfigurationRequest{
		{MinWidth: configValue(0)}, {MinHeight: configValue(-1)}, {MaxDepth: configValue(-1)}, {Ratio: configValue("0:9")}, {Ratio: configValue("bad")}, {Interval: configValue("0s")}, {Interval: configValue("bad")}, {Backend: configValue("unknown")}, {Fit: configValue("stretch")}, {Fit: configValue("crop"), Upscale: configValue(true)}, {CacheMaxImages: configValue(0)}, {CacheMaxBytes: configValue(int64(-1))},
	} {
		edit.Revision = before.Revision
		_, err := service.UpdateConfiguration(t.Context(), edit)
		requireConfigError(t, err, "invalid_settings")
		data, err := os.ReadFile(path)
		if err != nil || string(data) != before.TOML || settings(t, service).Effective != before.Effective || backend.calls.Load() != 0 {
			t.Fatal("invalid edit changed saved/running settings")
		}
	}
}

func TestPreferenceEditsRejectChangedOrUnsupportedDocumentsWithoutEchoingSecrets(t *testing.T) {
	service, path, _ := configurationFixture(t, "version=1\n")
	before := settings(t, service)
	external := "version=1\n[cache]\nmax_images=8\n"
	if err := os.WriteFile(path, []byte(external), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, MinWidth: configValue(1920)})
	requireConfigError(t, err, "config_conflict")
	reloaded := settings(t, service)
	edited, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: reloaded.Revision, MinWidth: configValue(1920)})
	if err != nil || edited.Saved.CacheMaxImages != 8 {
		t.Fatalf("reloaded edit=%+v %v", edited, err)
	}
	for _, body := range []string{"version=99\nsecret='private-marker'\n", "version=1\ncredentials='private-marker'\n", "version=1\nbroken=['private-marker'\n", "version=1\n#private-marker" + strings.Repeat("x", maxConfigBytes)} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := service.Configuration(t.Context())
		requireConfigError(t, err, "config_invalid")
		if strings.Contains(err.Error(), "private-marker") {
			t.Fatal("read error echoed unsupported credentials")
		}
		_, err = service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: edited.Revision, MinWidth: configValue(1280)})
		requireConfigError(t, err, "config_invalid")
		if strings.Contains(err.Error(), "private-marker") {
			t.Fatal("write error echoed unsupported credentials")
		}
		data, _ := os.ReadFile(path)
		if string(data) != body {
			t.Fatal("unsupported document was rewritten")
		}
	}
}

func TestPreferenceEditsUseSharedWriterLockWithFolderEnrollment(t *testing.T) {
	service, _, _ := configurationFixture(t, "version=1\n")
	request := service.request
	request.StatePath = filepath.Join(t.TempDir(), "state.db")
	request.CachePath = t.TempDir()
	other := openTestService(t, request, service.resolve, time.Hour, true)
	before := settings(t, service)
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	workers.Go(func() {
		<-start
		_, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, MinWidth: configValue(1920)})
		results <- err
	})
	workers.Go(func() {
		<-start
		_, err := other.AddFolder(t.Context(), AddFolderRequest{Revision: before.Revision, Path: t.TempDir()})
		results <- err
	})
	close(start)
	workers.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			requireConfigError(t, err, "config_conflict")
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestPreferenceWriteFailureLeavesDocumentAndStartupStateIntact(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	service, path, backend := configurationFixture(t, "version=1\n")
	before := settings(t, service)
	if err := os.Chmod(filepath.Dir(path), 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(path), 0700) })
	_, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, Interval: configValue("5m")})
	requireConfigError(t, err, "config_write_failed")
	after := settings(t, service)
	if after.Revision != before.Revision || after.Effective != before.Effective || service.Status().Interval != "1h0m0s" || backend.calls.Load() != 0 {
		t.Fatal("failed save changed settings")
	}
}

func TestPreferenceEditCancellationDoesNotPublish(t *testing.T) {
	service, _, _ := configurationFixture(t, "version=1\n")
	before := settings(t, service)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := service.UpdateConfiguration(ctx, UpdateConfigurationRequest{Revision: before.Revision, MinWidth: configValue(1920)})
	if err != context.Canceled || settings(t, service).Revision != before.Revision {
		t.Fatalf("canceled edit=%v", err)
	}
	service.Close()
	_, err = service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, MinWidth: configValue(1920)})
	requireConfigError(t, err, "canceled")
}
