package playback

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/desktop"
	"github.com/vadim82/panora/internal/filelock"
	"github.com/vadim82/panora/internal/sources/local"
)

func testService(t *testing.T, backend desktop.WallpaperBackend) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"a.png", "b.png", "c.png"} {
		writeScanImage(t, filepath.Join(root, name), 2, 2)
	}
	request := Request{ConfigPath: writeNextConfig(t, root), StatePath: filepath.Join(t.TempDir(), "state.db"), CachePath: t.TempDir(), Source: local.Provider{}}
	service := openTestService(t, request, func(context.Context) (desktop.WallpaperBackend, error) { return backend, nil }, time.Hour, true)
	return service, root
}

func act(t *testing.T, service *Service, action string) Status {
	t.Helper()
	// Fixtures run an explicit discovery pass to prepare catalog candidates.
	// Production runs these passes independently through StartDiscovery.
	if action == "next" && service.state.Cursor+1 >= len(service.state.History) {
		refreshForTest(t, service)
	}
	status, err := service.Action(context.Background(), action)
	if err != nil {
		t.Fatal(err)
	}
	return status
}

func TestServiceHistoryNavigationAndPause(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	service, _ := testService(t, backend)
	first := act(t, service, "next")
	second := act(t, service, "next")
	if first.Current.Path == second.Current.Path || second.HistoryLength != 2 || !second.Paused {
		t.Fatalf("unexpected selections: first=%+v second=%+v", first, second)
	}
	previous := act(t, service, "previous")
	if previous.Current.Path != first.Current.Path || previous.HistoryCursor != 0 || previous.HistoryLength != 2 {
		t.Fatalf("previous did not move through history: %+v", previous)
	}
	forward := act(t, service, "next")
	if forward.Current.Path != second.Current.Path || forward.HistoryCursor != 1 || forward.HistoryLength != 2 {
		t.Fatalf("next did not follow forward history: %+v", forward)
	}
	resumed := act(t, service, "resume")
	if resumed.Paused || resumed.NextAt == nil || time.Until(*resumed.NextAt) < 59*time.Minute {
		t.Fatalf("resume did not start a fresh interval: %+v", resumed)
	}
	deadline := *resumed.NextAt
	if repeated := act(t, service, "resume"); !repeated.NextAt.Equal(deadline) {
		t.Fatal("repeated resume changed the deadline")
	}
	paused := act(t, service, "pause")
	act(t, service, "pause")
	if !paused.Paused || paused.NextAt != nil || backend.calls != 4 {
		t.Fatalf("pause unexpectedly applied an image: %+v calls=%d", paused, backend.calls)
	}
	state, err := LoadState(service.request.StatePath)
	if err != nil || !state.Paused || state.CurrentPath() != second.Current.Path {
		t.Fatalf("unexpected saved state: %+v %v", state, err)
	}
	service.Close()
	restarted := openTestService(t, service.request, service.resolve, 0, false)
	if status := restarted.Status(); !status.Paused || status.Interval != "30m0s" || status.Current.Path != second.Current.Path || backend.calls != 4 {
		t.Fatalf("restart lost settings/history or reapplied: %+v", status)
	}
}

func TestServiceSkipsUnavailableOrIneligibleHistory(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	service, root := testService(t, backend)
	for _, name := range []string{"a.png", "b.png", "c.png"} {
		service.state.Record(filepath.Join(root, name), "home")
	}
	if err := saveFixtureState(t, &service.state, service.request.StatePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "b.png")); err != nil {
		t.Fatal(err)
	}
	previous := act(t, service, "previous")
	if service.currentOriginPath(context.Background()) != filepath.Join(root, "a.png") || len(previous.Warnings) != 1 {
		t.Fatalf("missing entry not skipped: %+v", previous)
	}
	forward := act(t, service, "next")
	if service.currentOriginPath(context.Background()) != filepath.Join(root, "c.png") || forward.HistoryLength != 3 || len(forward.Warnings) != 1 {
		t.Fatalf("forward navigation did not skip missing entry: %+v", forward)
	}
	// Existing history must be checked against edited filters before rendering.
	data, err := os.ReadFile(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), `min_width = 2`, `min_width = 3`, 1))
	if err := os.WriteFile(service.request.ConfigPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	before := backend.calls
	if _, err := service.Action(context.Background(), "previous"); err == nil {
		t.Fatal("accepted ineligible history")
	}
	if status := service.Status(); status.HistoryCursor != 2 || backend.calls != before || len(status.Warnings) != 2 {
		t.Fatalf("failed previous changed history or omitted warnings: %+v", status)
	}
}

type blockingServiceBackend struct {
	started chan struct{}
	finish  chan struct{}
	calls   atomic.Int32
}

func (*blockingServiceBackend) Probe(context.Context) error { return nil }
func (b *blockingServiceBackend) SetWallpaper(ctx context.Context, _ string, _ desktop.DisplayOptions) error {
	b.calls.Add(1)
	close(b.started)
	select {
	case <-b.finish:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestServiceStatusResponsiveAndConcurrentMutationsRejected(t *testing.T) {
	backend := &blockingServiceBackend{started: make(chan struct{}), finish: make(chan struct{})}
	service, _ := testService(t, backend)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	refreshForTest(t, service)
	done := make(chan error, 1)
	go func() { _, err := service.Action(ctx, "next"); done <- err }()
	select {
	case <-backend.started:
	case <-time.After(time.Second):
		t.Fatal("backend did not start")
	}
	if status := service.Status(); !status.Busy || status.HistoryLength != 0 || !status.Uncertain {
		t.Fatalf("status failed during rendering: %+v", status)
	}
	for _, action := range []string{"next", "previous", "pause", "resume"} {
		_, err := service.Action(context.Background(), action)
		var apiErr *Error
		if !errors.As(err, &apiErr) || apiErr.Code != "busy" {
			t.Fatalf("%s accepted during rendering: %v", action, err)
		}
	}
	intent, err := LoadState(service.request.StatePath)
	if err != nil || intent.Pending == nil || len(intent.History) != 0 {
		t.Fatalf("apply intent not durable before backend completion: %+v %v", intent, err)
	}
	close(backend.finish)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if status := service.Status(); status.Busy || status.Uncertain || status.HistoryLength != 1 || backend.calls.Load() != 1 {
		t.Fatalf("unexpected completion: %+v", status)
	}
}

func TestServiceFailedApplyPreservesHistoryAndRequiresExplicitRecovery(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	service, _ := testService(t, backend)
	first := act(t, service, "next")
	backend.err = errors.New("desktop connection lost")
	if _, err := service.Action(context.Background(), "next"); err == nil {
		t.Fatal("accepted failed backend")
	}
	failed := service.Status()
	if !failed.Paused || !failed.Uncertain || failed.HistoryLength != 1 || failed.Current.Path != first.Current.Path || failed.LastError.Code != "apply_uncertain" {
		t.Fatalf("failed apply lost accepted history: %+v", failed)
	}
	pending := service.state.Pending
	resolve := service.resolve
	service.resolve = func(context.Context) (desktop.WallpaperBackend, error) { return definiteThemeRejection{}, nil }
	status, err := service.Action(t.Context(), "next")
	requireConfigError(t, err, "theme_changed")
	if !status.Uncertain || service.state.Pending != pending {
		t.Fatalf("pre-dispatch rejection lost the earlier uncertain attempt: %+v", status)
	}
	service.Close()
	restarted := openTestService(t, service.request, resolve, time.Hour, false)
	if status := restarted.Status(); !status.Uncertain || !status.Paused || backend.calls != 2 {
		t.Fatalf("restart replayed an uncertain operation: %+v", status)
	}
	if recovered := restarted.state.Pending; recovered.Path != pending.Path || recovered.Identity != pending.Identity {
		t.Fatalf("wrong intent restored after definite rejection: %+v", recovered)
	}
	if _, err := restarted.Action(context.Background(), "resume"); err == nil {
		t.Fatal("resume ignored uncertain apply")
	}
	backend.err = nil
	recovered := act(t, restarted, "next")
	if recovered.Uncertain || !recovered.Paused || recovered.HistoryLength != 2 || recovered.LastError != nil {
		t.Fatalf("explicit recovery failed: %+v", recovered)
	}
	act(t, restarted, "resume")
}

type callbackServiceBackend struct{ apply func() error }

func (callbackServiceBackend) Probe(context.Context) error { return nil }
func (b callbackServiceBackend) SetWallpaper(context.Context, string, desktop.DisplayOptions) error {
	return b.apply()
}

func TestServiceSaveFailureAfterApplyLeavesRecoverableIntent(t *testing.T) {
	var service *Service
	backend := callbackServiceBackend{apply: func() error {
		// Simulate loss of the persistence connection after intent commit.
		return service.store.Close()
	}}
	service, _ = testService(t, backend)
	statePath := service.request.StatePath
	refreshForTest(t, service)

	_, err := service.Action(context.Background(), "next")
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Code != "state_save_after_apply" {
		t.Fatalf("unexpected error: %v", err)
	}
	select {
	case <-service.fatal:
	default:
		t.Fatal("service did not request shutdown after post-apply save failure")
	}
	intent, err := LoadState(statePath)
	if err != nil || intent.Pending == nil || len(intent.History) != 0 || !service.Status().Uncertain {
		t.Fatalf("lost interrupted apply intent: %+v %v", intent, err)
	}
	service.Close()
	restarted := openTestService(t, service.request, service.resolve, time.Hour, false)
	if status := restarted.Status(); !status.Paused || !status.Uncertain {
		t.Fatalf("restart did not expose interrupted apply: %+v", status)
	}
}

func TestServiceCloseCancelsApplyBeforeReleasingOwnership(t *testing.T) {
	backend := &blockingServiceBackend{started: make(chan struct{}), finish: make(chan struct{})}
	fixture, _ := testService(t, backend)
	fixture.Close()
	options := ServiceOptions{Request: fixture.request, Interval: time.Hour, Paused: true, Backend: "fake"}
	service, err := OpenService(context.Background(), options, fixture.resolve)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	refreshForTest(t, service)
	// This checks cancellation and ownership, not backend startup latency on
	// shared CI runners. Report early action failures without hiding them behind
	// the startup wait.
	done := make(chan error, 1)
	go func() { _, err := service.Action(context.Background(), "next"); done <- err }()
	select {
	case <-backend.started:
	case err := <-done:
		t.Fatalf("action ended before apply started: %v; status=%+v", err, service.Status())
	case <-time.After(5 * time.Second):
		t.Fatalf("apply did not start within the fixture budget; status=%+v", service.Status())
	}
	closed := make(chan struct{})
	go func() { service.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("close did not cancel the application")
	}
	if err := <-done; err == nil {
		t.Fatal("canceled application reported success")
	}
	for _, path := range []string{fixture.request.StatePath + ".lock", fixture.request.StatePath + ".rotation.lock"} {
		release, err := filelock.Try(path)
		if err != nil {
			t.Fatal(err)
		}
		release()
	}
	restarted, err := OpenService(context.Background(), options, fixture.resolve)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if status := restarted.Status(); !status.Paused || !status.Uncertain || status.HistoryLength != 0 {
		t.Fatalf("lost canceled apply on restart: %+v", status)
	}
}

func TestStatusSnapshotCannotMutateService(t *testing.T) {
	service, _ := testService(t, &fakeWallpaperBackend{})
	act(t, service, "next")
	act(t, service, "resume")
	_, _ = service.Action(context.Background(), "unknown")
	snapshot := service.Status()
	originalPath, originalTime, originalCode := snapshot.Current.Path, *snapshot.NextAt, snapshot.LastError.Code
	snapshot.Current.Path = "changed by caller"
	*snapshot.NextAt = time.Time{}
	snapshot.LastError.Code = "changed by caller"
	actual := service.Status()
	if actual.Current.Path != originalPath || !actual.NextAt.Equal(originalTime) || actual.LastError.Code != originalCode {
		t.Fatal("status snapshot shared mutable state with the service")
	}
}

func TestServiceNavigatesMultipleFoldersAndPreservesRemovedInputHistory(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	service, root := testService(t, backend)
	first := act(t, service, "next")
	firstOrigin := service.currentOriginPath(context.Background())
	work := t.TempDir()
	workImage := filepath.Join(work, "same.png")
	writeScanImage(t, workImage, 2, 2)
	body := localTOML(root) + fmt.Sprintf("[[providers.folders]]\nid='work'\npath=%q\n[filters]\nmin_width=2\nmin_height=2\n", work)
	if err := os.WriteFile(service.request.ConfigPath, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	// Keep only the already accepted image in the first folder.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if path != firstOrigin {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}
	}
	second := act(t, service, "next")
	if service.currentOriginPath(context.Background()) != workImage || second.Current.SourceID != "work" || second.HistoryLength != 2 {
		t.Fatalf("second folder missing: %+v", second)
	}
	previous := act(t, service, "previous")
	if previous.Current.Path != first.Current.Path {
		t.Fatalf("history crossed to wrong folder: %+v", previous)
	}
	body = localTOML(root) + "[filters]\nmin_width=2\nmin_height=2\n"
	if err := os.WriteFile(service.request.ConfigPath, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	refreshForTest(t, service)
	next := act(t, service, "next")
	if next.Current.SourceID != "home" || len(next.Warnings) != 1 || service.state.History[1].SourceID != "work" {
		t.Fatalf("removed folder history mishandled: %+v", next)
	}
	if _, err := os.Stat(workImage); err != nil {
		t.Fatal("removing folder configuration deleted its image")
	}
}

func TestExtremeRatioCannotReachWallpaperBackend(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	service, root := testService(t, backend)
	body := localTOML(root) + fmt.Sprintf("[filters]\nmin_width=2\nmin_height=2\nratio='%d:1'\nratio_tolerance_pixels=1\n", int(^uint(0)>>1))
	if err := os.WriteFile(service.request.ConfigPath, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	refreshForTest(t, service)
	status, err := service.Action(context.Background(), "next")
	if err == nil || status.HistoryLength != 0 || backend.calls != 0 {
		t.Fatalf("extreme ratio reached desktop: %+v, error=%v, calls=%d", status, err, backend.calls)
	}
}
