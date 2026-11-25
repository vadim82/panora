package playback

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/source"
)

func cacheStatus(t *testing.T, service *Service) CacheStatus {
	t.Helper()
	got, err := service.CacheStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSavedCacheBudgetsApplyLiveProtectCurrentPendingAndSurviveRestart(t *testing.T) {
	root := t.TempDir()
	for i, name := range []string{"a.png", "b.png", "c.png"} {
		writeScanImage(t, filepath.Join(root, name), i+2, 2)
	}
	service, _, backend := configurationFixture(t, localTOML(root)+"[filters]\nmin_width=2\nmin_height=2\n")
	refreshForTest(t, service)
	service.draw = func(int) int { return 0 }
	if _, err := service.Action(t.Context(), "next"); err != nil {
		t.Fatal(err)
	}
	first := service.state.History[0]
	if _, err := service.Action(t.Context(), "next"); err != nil {
		t.Fatal(err)
	}
	current := service.Status().Current
	disposable, err := service.cache.Materialize(t.Context(), source.Content{MediaKind: "image", Kind: "local", Path: filepath.Join(root, "c.png")}, current.Path, first.Path)
	if err != nil {
		t.Fatal(err)
	}
	service.state.Pending = &playbackIntent{Identity: first.Identity, Path: first.Path, HistoryIndex: 0, StartedAt: time.Now().UTC()}
	if err := service.saveState(service.state); err != nil {
		t.Fatal(err)
	}
	before := settings(t, service)
	saved, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, CacheMaxImages: configValue(1), CacheMaxBytes: configValue(int64(1))})
	if err != nil || saved.Effective.CacheMaxImages != 1 || saved.Effective.CacheMaxBytes != 1 || slices.Contains(saved.RestartRequired, "cache_max_images") || slices.Contains(saved.RestartRequired, "cache_max_bytes") {
		t.Fatalf("saved limits=%+v %v", saved, err)
	}
	got := cacheStatus(t, service)
	if got.Effective != got.Saved || got.Usage.State != "protected_over_budget" || got.Usage.Images != 2 || got.Usage.ProtectedImages != 2 || got.Usage.ProtectedBytes <= 1 || got.LastApplyError != nil {
		t.Fatalf("pressure=%+v", got)
	}
	if _, err := os.Stat(disposable.Path); !os.IsNotExist(err) {
		t.Fatalf("disposable file not evicted=%v", err)
	}
	for _, path := range []string{current.Path, first.Path, filepath.Join(root, "a.png"), filepath.Join(root, "b.png"), filepath.Join(root, "c.png")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	if backend.calls.Load() != 2 || service.Status().Current.ID != current.ID || service.Status().HistoryLength != 2 || !service.Status().Paused {
		t.Fatal("budget change altered playback")
	}
	if _, err := service.store.LookupCandidate(t.Context(), first.Identity); err != nil {
		t.Fatal("budget change removed catalog origin", err)
	}
	service.Close()
	restarted := openTestService(t, service.request, service.resolve, time.Hour, false)
	got = cacheStatus(t, restarted)
	if got.Effective != got.Saved || got.Effective.MaxImages != 1 || got.Usage.ProtectedImages != 2 || got.Usage.State != "protected_over_budget" || !restarted.Status().Uncertain {
		t.Fatalf("restart lost limits/protection=%+v status=%+v", got, restarted.Status())
	}
	saved, err = restarted.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: saved.Revision, CacheMaxImages: configValue(20), CacheMaxBytes: configValue(int64(512 << 20))})
	if err != nil || saved.Effective.CacheMaxImages != 20 {
		t.Fatalf("increase=%+v %v", saved, err)
	}
	if got := cacheStatus(t, restarted); got.Usage.State != "within_budget" || got.LastApplyError != nil || backend.calls.Load() != 2 {
		t.Fatalf("increase status=%+v", got)
	}
}

func TestFailedCacheConfigSaveDoesNotActivateOrEvict(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	service, path, _ := configurationFixture(t, "version=1\n")
	root := t.TempDir()
	for i, name := range []string{"a.png", "b.png"} {
		image := filepath.Join(root, name)
		writeScanImage(t, image, i+2, 2)
		if _, err := service.cache.Materialize(t.Context(), source.Content{MediaKind: "image", Kind: "local", Path: image}); err != nil {
			t.Fatal(err)
		}
	}
	before := settings(t, service)
	if err := os.Chmod(filepath.Dir(path), 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(path), 0700) })
	_, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, CacheMaxImages: configValue(1)})
	requireConfigError(t, err, "config_write_failed")
	after := cacheStatus(t, service)
	if after.Saved.MaxImages != 20 || after.Effective.MaxImages != 20 || after.Usage.Images != 2 || settings(t, service).Revision != before.Revision {
		t.Fatalf("failed save affected active cache=%+v", after)
	}
}

func TestCacheCleanupFailureIsSeparateFromSuccessfulSaveAndNoOpRetries(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	service, _, _ := configurationFixture(t, "version=1\n")
	root := t.TempDir()
	for i, name := range []string{"a.png", "b.png"} {
		image := filepath.Join(root, name)
		writeScanImage(t, image, i+2, 2)
		if _, err := service.cache.Materialize(t.Context(), source.Content{MediaKind: "image", Kind: "local", Path: image}); err != nil {
			t.Fatal(err)
		}
	}
	before := settings(t, service)
	if err := os.Chmod(service.request.CachePath, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(service.request.CachePath, 0700) })
	saved, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, CacheMaxImages: configValue(1)})
	if err != nil || saved.Saved.CacheMaxImages != 1 || saved.Effective.CacheMaxImages != 1 {
		t.Fatalf("published save mislabeled=%+v %v", saved, err)
	}
	got := cacheStatus(t, service)
	if got.Usage.State != "over_budget" || got.LastApplyError == nil || got.LastApplyError.Code != "cache_cleanup_failed" || got.Effective != got.Saved {
		t.Fatalf("cleanup failure hidden=%+v", got)
	}
	if err := os.Chmod(service.request.CachePath, 0700); err != nil {
		t.Fatal(err)
	}
	retried, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: saved.Revision, CacheMaxImages: configValue(1)})
	if err != nil || retried.Revision != saved.Revision {
		t.Fatalf("no-op retry=%+v %v", retried, err)
	}
	if got := cacheStatus(t, service); got.Usage.State != "within_budget" || got.Usage.Images != 1 || got.LastApplyError != nil {
		t.Fatalf("retry cleanup=%+v", got)
	}
}

func TestCacheUsageBusyAndCanceledRequests(t *testing.T) {
	service, _, _ := configurationFixture(t, "version=1\n")
	service.actionMu.Lock()
	_, err := service.CacheStatus(t.Context())
	requireConfigError(t, err, "busy")
	service.actionMu.Unlock()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = service.CacheStatus(ctx)
	requireConfigError(t, err, "canceled")
}

func TestCacheUsageInspectionFailureIsExplicit(t *testing.T) {
	service, _, _ := configurationFixture(t, "version=1\n")
	managed := filepath.Join(service.request.CachePath, strings.Repeat("a", 64)+".png")
	if err := os.Mkdir(managed, 0700); err != nil {
		t.Fatal(err)
	}
	got := cacheStatus(t, service)
	if got.Usage.State != "unknown" || got.UsageError == nil || got.UsageError.Code != "cache_usage_unavailable" || got.Effective.MaxImages != 20 {
		t.Fatalf("usage error looked like an empty cache=%+v", got)
	}
}

func TestCacheStatusReturnsAnIndependentErrorSnapshot(t *testing.T) {
	service, _, _ := configurationFixture(t, "version=1\n")
	service.cacheApplyError = &Error{Code: "cache_cleanup_failed", Message: "fixture cleanup error"}
	got := cacheStatus(t, service)
	got.LastApplyError.Code = "caller edit"
	got.LastApplyError.Message = "caller edit"
	next := cacheStatus(t, service)
	if next.LastApplyError.Code != "cache_cleanup_failed" || next.LastApplyError.Message != "fixture cleanup error" {
		t.Fatalf("caller changed service error=%+v", next.LastApplyError)
	}
}
