package playback

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/vadim82/panora/internal/desktop"
)

type spaceBackend struct{ fakeWallpaperBackend }

func (*spaceBackend) ReapplyOnSpaceChange() bool { return true }

type controlledSpaceBackend struct {
	spaceBackend
	apply func(context.Context, string) error
}

func (b *controlledSpaceBackend) SetWallpaper(ctx context.Context, path string, options desktop.DisplayOptions) error {
	if b.apply != nil {
		return b.apply(ctx, path)
	}
	return b.spaceBackend.SetWallpaper(ctx, path, options)
}

func TestDesktopRefreshPreservesPlaybackAndOnlyUsesCurrentCache(t *testing.T) {
	for _, paused := range []bool{true, false} {
		backend := &spaceBackend{}
		service, root := testService(t, backend)
		if err := service.RefreshDesktop(t.Context()); err != nil || backend.calls != 0 {
			t.Fatalf("fresh setup applied: %v calls=%d", err, backend.calls)
		}
		act(t, service, "next")
		act(t, service, "next")
		if !paused {
			act(t, service, "resume")
		}
		before, deadline := service.state, service.nextAt
		persistedBefore, err := LoadState(service.request.StatePath)
		if err != nil {
			t.Fatal(err)
		}
		calls := backend.calls
		if err := os.RemoveAll(root); err != nil {
			t.Fatal(err)
		}
		old := time.Unix(123, 0)
		if err := os.Chtimes(before.CurrentPath(), old, old); err != nil {
			t.Fatal(err)
		}
		if err := service.RefreshDesktop(t.Context()); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(before.CurrentPath())
		if err != nil || !info.ModTime().Equal(old) {
			t.Fatalf("desk refresh touched accepted file recency: %v %v", info, err)
		}
		if !reflect.DeepEqual(before, service.state) || !service.nextAt.Equal(deadline) || backend.calls != calls+1 || backend.path != before.CurrentPath() {
			t.Fatalf("refresh changed playback: before=%+v after=%+v calls=%d", before, service.state, backend.calls)
		}
		persisted, err := LoadState(service.request.StatePath)
		if err != nil || !reflect.DeepEqual(persistedBefore, persisted) {
			t.Fatalf("persisted playback changed: %+v %v", persisted, err)
		}
	}
}

func TestDesktopRefreshBusyMissingFileAndUncertainApply(t *testing.T) {
	backend := &spaceBackend{}
	service, _ := testService(t, backend)
	act(t, service, "next")
	service.actionMu.Lock()
	err := service.RefreshDesktop(t.Context())
	service.actionMu.Unlock()
	var failure *Error
	if !errors.As(err, &failure) || failure.Code != "busy" {
		t.Fatalf("concurrent action was not gated: %v", err)
	}
	backend.err = errors.New("fake desktop failure")
	if err := service.RefreshDesktop(t.Context()); err == nil || !service.Status().Uncertain || !service.Status().Paused {
		t.Fatalf("failed apply did not preserve intent: %v %+v", err, service.Status())
	}
	calls := backend.calls
	if err := service.RefreshDesktop(t.Context()); err != nil || backend.calls != calls {
		t.Fatalf("uncertain apply was replayed: %v", err)
	}
	backend.err = nil
	act(t, service, "next")
	before := service.state
	if err := os.Remove(before.CurrentPath()); err != nil {
		t.Fatal(err)
	}
	calls = backend.calls
	if err := service.RefreshDesktop(t.Context()); err == nil || backend.calls != calls || !reflect.DeepEqual(before, service.state) {
		t.Fatalf("missing cache mutated playback: %v", err)
	}
}

func TestDesktopRefreshRejectsChangedAcceptedBytes(t *testing.T) {
	backend := &spaceBackend{}
	service, _ := testService(t, backend)
	act(t, service, "next")
	path := service.state.CurrentPath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 1
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	before := service.state
	if err := service.RefreshDesktop(t.Context()); err == nil || backend.calls != 1 || !reflect.DeepEqual(before, service.state) {
		t.Fatalf("hash mismatch reached the setter or changed accepted playback: %v", err)
	}
}

func TestDesktopRefreshBoundsUnavailableSessionWithoutChangingAcceptedState(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		backend := &spaceBackend{}
		service, _ := testService(t, backend)
		act(t, service, "next")
		before := act(t, service, "resume")
		persisted, err := LoadState(service.request.StatePath)
		if err != nil {
			t.Fatal(err)
		}
		service.resolve = func(ctx context.Context) (desktop.WallpaperBackend, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		started := time.Now()
		if err := service.RefreshDesktop(t.Context()); err == nil || !strings.Contains(err.Error(), "deadline exceeded") || time.Since(started) > 8*time.Second {
			t.Fatalf("unavailable session was not reported within the refresh deadline: %v", err)
		}
		after := service.Status()
		if backend.calls != 1 || !reflect.DeepEqual(before.Current, after.Current) || before.HistoryCursor != after.HistoryCursor || before.HistoryLength != after.HistoryLength || after.Paused || after.Uncertain || after.NextAt == nil || !after.NextAt.Equal(*before.NextAt) || after.LastError == nil || after.LastError.Code != "desktop_refresh_failed" {
			t.Fatalf("session failure changed playback or omitted the warning: before=%+v after=%+v", before, after)
		}
		if actual, err := LoadState(service.request.StatePath); err != nil || !reflect.DeepEqual(persisted, actual) {
			t.Fatalf("session failure changed stored playback: %+v %v", actual, err)
		}
	})
}

func TestOtherDesktopBackendDoesNotFollowMacSpaces(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	service, _ := testService(t, backend)
	act(t, service, "next")
	if err := service.RefreshDesktop(t.Context()); err != nil || backend.calls != 1 {
		t.Fatalf("non-Mac backend reapplied: %v calls=%d", err, backend.calls)
	}
}

func TestDesktopRefreshReadsAcceptedPathAfterConcurrentPrevious(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		backend := &controlledSpaceBackend{}
		service, _ := testService(t, backend)
		first := act(t, service, "next")
		act(t, service, "next")
		paths := make(chan string, 4)
		release := make(chan struct{})
		backend.apply = func(ctx context.Context, path string) error {
			paths <- path
			if path == first.Current.Path {
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
			return ctx.Err()
		}
		done := make(chan error, 1)
		go func() { _, err := service.Action(t.Context(), "previous"); done <- err }()
		synctest.Wait()
		if len(paths) != 1 || <-paths != first.Current.Path {
			t.Fatal("previous did not start applying the earlier history entry")
		}
		var failure *Error
		if err := service.RefreshDesktop(t.Context()); !errors.As(err, &failure) || failure.Code != "busy" || len(paths) != 0 {
			t.Fatalf("Space refresh reached the backend during navigation: %v", err)
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if err := service.RefreshDesktop(t.Context()); err != nil {
			t.Fatal(err)
		}
		if status := service.Status(); len(paths) != 1 || <-paths != first.Current.Path || status.Current.Path != first.Current.Path || status.HistoryCursor != 0 || status.HistoryLength != 2 || !status.Paused {
			t.Fatalf("refresh reapplied a stale image or changed history: %+v", status)
		}
	})
}

func TestScheduledRotationWinsAfterConcurrentDesktopRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		backend := &controlledSpaceBackend{}
		service, _ := testService(t, backend)
		first := act(t, service, "next")
		second := act(t, service, "next")
		act(t, service, "previous")
		service.interval = time.Second
		deadline := *act(t, service, "resume").NextAt
		paths := make(chan string, 4)
		release := make(chan struct{})
		backend.apply = func(ctx context.Context, path string) error {
			paths <- path
			if path == first.Current.Path {
				select {
				case <-release:
				case <-ctx.Done():
				}
			}
			return ctx.Err()
		}
		schedulerDone := make(chan struct{})
		go func() {
			service.Run(func(err error) { t.Errorf("scheduled rotation failed: %v", err) })
			close(schedulerDone)
		}()
		defer func() {
			service.Stop()
			<-schedulerDone
		}()
		refreshDone := make(chan error, 1)
		go func() { refreshDone <- service.RefreshDesktop(t.Context()) }()
		synctest.Wait()
		if len(paths) != 1 || <-paths != first.Current.Path {
			t.Fatal("Space refresh did not start applying the accepted image")
		}
		time.Sleep(time.Until(deadline))
		synctest.Wait()
		if len(paths) != 0 {
			t.Fatal("rotation reached the backend while the older image was still being reapplied")
		}
		close(release)
		if err := <-refreshDone; err != nil {
			t.Fatal(err)
		}
		select {
		case path := <-paths:
			if path != second.Current.Path {
				t.Fatalf("scheduled rotation applied %s; want %s", path, second.Current.Path)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("scheduled rotation did not retry after the Space refresh")
		}
		synctest.Wait()
		if status := service.Status(); len(paths) != 0 || status.Current.Path != second.Current.Path || status.HistoryCursor != 1 || status.HistoryLength != 2 || status.Paused || status.NextAt == nil || !status.NextAt.After(deadline) {
			t.Fatalf("scheduled rotation did not win after the refresh: %+v", status)
		}
	})
}
