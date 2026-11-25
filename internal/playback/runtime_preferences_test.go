package playback

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestMixedRuntimePreferencesKeepProtectedPreviewAndRejectInvalidSave(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "image.png")
	writeScanImage(t, original, 2, 2)
	service, _, backend := configurationFixture(t, localTOML(root)+"[filters]\nmin_width=2\nmin_height=2\n")
	refreshForTest(t, service)
	if _, err := service.Action(t.Context(), "next"); err != nil {
		t.Fatal(err)
	}
	current := service.Status().Current
	known, present, err := service.store.KnownCachedImage(t.Context(), current.ImageID)
	if err != nil || !present || known.Bytes < 2 {
		t.Fatalf("known image=%+v %v", known, err)
	}
	preview, err := service.Preview(t.Context(), current.ImageID)
	if err != nil || !preview.Available {
		t.Fatalf("initial preview=%+v %v", preview, err)
	}
	before := settings(t, service)
	initialCache := cacheStatus(t, service)
	_, err = service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, Interval: configValue("2m"), CacheMaxImages: configValue(1), CacheMaxBytes: configValue(int64(0))})
	requireConfigError(t, err, "invalid_settings")
	unchanged := settings(t, service)
	if unchanged.Revision != before.Revision || unchanged.TOML != before.TOML || unchanged.Effective != before.Effective || service.Status().Interval != before.Effective.Interval {
		t.Fatal("invalid mixed patch partially applied settings")
	}
	if got := cacheStatus(t, service); got.Effective != initialCache.Effective || got.Usage != initialCache.Usage {
		t.Fatalf("invalid mixed patch changed cache=%+v", got)
	}

	reducedBytes := known.Bytes - 1
	saved, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, Interval: configValue("2m"), CacheMaxImages: configValue(1), CacheMaxBytes: &reducedBytes})
	if err != nil || saved.Effective.Interval != "2m0s" || saved.Effective.CacheMaxImages != 1 || saved.Effective.CacheMaxBytes != reducedBytes {
		t.Fatalf("mixed runtime patch=%+v %v", saved, err)
	}
	for _, field := range []string{"interval", "cache_max_images", "cache_max_bytes"} {
		if slices.Contains(saved.RestartRequired, field) {
			t.Fatalf("runtime field requires restart=%s", field)
		}
	}
	pressure := cacheStatus(t, service)
	if pressure.Effective != pressure.Saved || pressure.Usage.State != "protected_over_budget" || pressure.Usage.Images != 1 || pressure.Usage.ProtectedImages != 1 || pressure.Usage.ProtectedBytes != known.Bytes || pressure.LastApplyError != nil {
		t.Fatalf("protected pressure=%+v", pressure)
	}
	after, err := service.Preview(t.Context(), current.ImageID)
	if err != nil || !after.Available || !bytes.Equal(after.Data, preview.Data) {
		t.Fatalf("budget reduction invalidated protected preview=%+v %v", after, err)
	}
	for _, path := range []string{original, current.Path} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	state := service.Status()
	if backend.calls.Load() != 1 || state.Current.ID != current.ID || state.HistoryLength != 1 || !state.Paused || state.Interval != "2m0s" {
		t.Fatalf("settings or preview changed playback=%+v calls=%d", state, backend.calls.Load())
	}
}
