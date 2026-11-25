package playback

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/desktop"
)

const (
	firstNavigationToken  = "00000000000000000000000000000001"
	latestNavigationToken = "00000000000000000000000000000002"
)

type navigationResult struct {
	status Status
	err    error
}

func startNavigation(t *testing.T, service *Service, current Status, token string, offset int) <-chan navigationResult {
	t.Helper()
	result := make(chan navigationResult, 1)
	go func() {
		status, err := service.Navigate(t.Context(), NavigationRequest{
			Instance: current.InstanceID, Current: current.Current.ID, Token: token, Offset: offset,
		})
		result <- navigationResult{status, err}
	}()
	return result
}

func awaitNavigation(t *testing.T, result <-chan navigationResult) navigationResult {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("navigation did not finish")
		return navigationResult{}
	}
}

func requireNavigationCode(t *testing.T, err error, code string) {
	t.Helper()
	var failure *Error
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("navigation error = %v, want %s", err, code)
	}
}

func TestNavigationSupersedesPreparationWithoutCommittingHistory(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	service, _ := lookaheadFixture(t, backend)
	second := act(t, service, "next")
	third := act(t, service, "next")
	act(t, service, "previous")
	first := act(t, service, "previous")

	preparing := make(chan struct{})
	service.resolve = func(ctx context.Context) (desktop.WallpaperBackend, error) {
		close(preparing)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	result := startNavigation(t, service, first, firstNavigationToken, 1)
	select {
	case <-preparing:
	case <-time.After(3 * time.Second):
		t.Fatal("navigation did not reach backend preparation")
	}
	if err := service.CancelNavigation(first.InstanceID, firstNavigationToken); err != nil {
		t.Fatal(err)
	}
	superseded := awaitNavigation(t, result)
	requireNavigationCode(t, superseded.err, "navigation_superseded")
	if got := superseded.status; got.Current.ID != first.Current.ID || got.HistoryLength != 3 || got.PendingImageID != 0 || got.Uncertain || backend.calls != 5 {
		t.Fatalf("superseded preparation reached playback: %+v, calls=%d", got, backend.calls)
	}

	service.resolve = func(context.Context) (desktop.WallpaperBackend, error) { return backend, nil }
	latest := awaitNavigation(t, startNavigation(t, service, first, latestNavigationToken, 2))
	if got := latest.status; latest.err != nil || got.Current.ID != third.Current.ID || got.Current.ID == second.Current.ID || got.HistoryLength != 3 || backend.calls != 6 {
		t.Fatalf("latest destination did not skip the superseded target: %+v, calls=%d, err=%v", got, backend.calls, latest.err)
	}
}

func TestNavigationSerializesDispatchedWritesAndKeepsUncertainIntent(t *testing.T) {
	for _, tc := range []struct {
		name      string
		setterErr error
	}{
		{name: "acknowledged"},
		{name: "uncertain", setterErr: errors.New("lost setter acknowledgement")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &lookaheadTestBackend{started: make(chan struct{}), finish: make(chan error, 1)}
			service, first := lookaheadFixture(t, backend)
			result := startNavigation(t, service, first, firstNavigationToken, 1)
			select {
			case <-backend.started:
			case <-time.After(3 * time.Second):
				t.Fatal("navigation did not reach the desktop setter")
			}
			pending := service.Status()
			if !pending.Busy || pending.PendingImageID == 0 || pending.Current.ID != first.Current.ID || pending.HistoryLength != 1 {
				t.Fatalf("dispatch changed accepted history: %+v", pending)
			}
			if err := service.CancelNavigation(first.InstanceID, firstNavigationToken); err != nil {
				t.Fatal(err)
			}
			reversed := awaitNavigation(t, startNavigation(t, service, first, latestNavigationToken, -1))
			requireNavigationCode(t, reversed.err, "busy")
			backend.finish <- tc.setterErr
			accepted := awaitNavigation(t, result)
			got := accepted.status
			if tc.setterErr != nil {
				requireNavigationCode(t, accepted.err, "apply_uncertain")
				saved, err := LoadState(service.request.StatePath)
				if !got.Uncertain || !got.Paused || got.Current.ID != first.Current.ID || got.HistoryLength != 1 || err != nil || saved.Pending == nil {
					t.Fatalf("uncertain apply lost its intent: %+v, saved=%+v, err=%v", got, saved, err)
				}
			} else if accepted.err != nil || got.Uncertain || got.Current.ImageID != pending.PendingImageID || got.HistoryLength != 2 || got.PendingImageID != 0 {
				t.Fatalf("dispatched navigation did not commit on acknowledgement: %+v, %v", got, accepted.err)
			}
			if backend.calls.Load() != 2 {
				t.Fatalf("another desktop write ran during the apply: calls=%d", backend.calls.Load())
			}
			if tc.setterErr == nil {
				latest := startNavigation(t, service, got, latestNavigationToken, -1)
				select {
				case <-backend.started:
				case <-time.After(3 * time.Second):
					t.Fatal("reversed navigation did not reach the desktop after acknowledgement")
				}
				if status := service.Status(); status.Current.ID != got.Current.ID || status.PendingImageID != first.Current.ImageID {
					t.Fatalf("reversal used the wrong target or accepted cursor: %+v", status)
				}
				backend.finish <- nil
				reversal := awaitNavigation(t, latest)
				if status := reversal.status; reversal.err != nil || status.Current.ID != first.Current.ID || status.HistoryLength != 2 {
					t.Fatalf("reversal did not restore the prior accepted cursor: %+v, %v", status, reversal.err)
				}
			}
		})
	}
}
