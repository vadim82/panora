package playback

import (
	"context"
	"testing"
)

func TestSelectionCancellationDoesNotApply(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	service, _ := testService(t, backend)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Action(ctx, "next"); err == nil {
		t.Fatal("accepted canceled selection")
	}
	if backend.calls != 0 || service.Status().HistoryLength != 0 {
		t.Fatal("canceled selection applied image")
	}
}
