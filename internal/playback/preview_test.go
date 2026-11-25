package playback

import (
	"bytes"
	"context"
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/vadim82/panora/internal/cache"
)

func TestPreviewUsesStableImageIDWithoutPlaybackOrProviderEffects(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	service, root := testService(t, backend)
	current := act(t, service, "next")
	imageID := current.Current.ImageID
	// A preview remains inspectable after exclusion/disable and original removal;
	// it is not a request to replay or revalidate playback eligibility.
	act(t, service, "exclude")
	before := settings(t, service)
	if _, err := service.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: before.Revision, ID: "home", Enabled: configValue(false)}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.png", "b.png", "c.png"} {
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	state := service.Status()
	preview, err := service.Preview(t.Context(), imageID)
	if err != nil || !preview.Available || preview.Version != StatusVersion || preview.ImageID != imageID || preview.ContentRevision == "" || preview.ContentRevision != current.CurrentPreviewContent || preview.MIMEType != "image/png" || preview.Width != 2 || preview.Height != 2 || len(preview.Data) == 0 || len(preview.Data) > cache.MaxPreviewBytes {
		t.Fatalf("preview=%+v %v", preview, err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(preview.Data))
	if err != nil || decoded.Bounds().Dx() != 2 {
		t.Fatalf("preview bytes=%v %v", decoded, err)
	}
	after := service.Status()
	if backend.calls != 1 || after.Current.ID != state.Current.ID || after.HistoryLength != state.HistoryLength || after.Paused != state.Paused || after.CurrentExcluded != state.CurrentExcluded {
		t.Fatal("preview changed playback")
	}
	// No action ownership is needed: a read can proceed while a client owns the
	// playback action mutex (it will report cache busy if file work overlaps).
	service.actionMu.Lock()
	_, err = service.Preview(t.Context(), imageID)
	service.actionMu.Unlock()
	if err != nil {
		t.Fatalf("preview depended on action lock=%v", err)
	}
}

func TestNeighborPreviewSuppressesExcludedAndDisabledHistory(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	service, _ := testService(t, backend)
	first := act(t, service, "next").Current
	second := act(t, service, "next").Current
	check := func(available bool) ImagePreview {
		t.Helper()
		got, err := service.PreviewNeighbor(t.Context(), first.ImageID, first.ID)
		if err != nil || got.Available != available || (!available && got.Reason != "history_unavailable") {
			t.Fatalf("neighbor: available=%t reason=%q %v", got.Available, got.Reason, err)
		}
		return got
	}
	firstPreview := check(true)
	for range 2 {
		history, err := service.Preview(t.Context(), first.ImageID)
		now := check(true)
		if err != nil || !history.Available || !bytes.Equal(firstPreview.Data, history.Data) || !bytes.Equal(firstPreview.Data, now.Data) {
			t.Fatalf("revisited History and Now returned different bytes: %v", err)
		}
	}
	if status := service.Status(); status.HistoryLength != 2 || status.Current.ID != second.ID || backend.calls != 2 {
		t.Fatal("revisiting previews changed history or applied a wallpaper")
	}
	got, err := service.PreviewNeighbor(t.Context(), second.ImageID, first.ID)
	if err != nil || got.Available || got.Reason != "history_unavailable" {
		t.Fatalf("mismatched identity: available=%t reason=%q %v", got.Available, got.Reason, err)
	}
	act(t, service, "previous")
	before := service.Status().HistoryPreviewRevision
	act(t, service, "exclude")
	current := act(t, service, "next")
	if current.Current.ID != second.ID || current.HistoryPreviewRevision == before {
		t.Fatal("exclusion did not invalidate neighboring previews")
	}
	check(false)
	act(t, service, "undo-exclude")
	check(true)
	config := settings(t, service)
	if _, err := service.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: config.Revision, ID: "home", Enabled: configValue(false)}); err != nil {
		t.Fatal(err)
	}
	check(false)
	got, err = service.Preview(t.Context(), first.ImageID)
	if err != nil || !got.Available || backend.calls != 4 {
		t.Fatalf("ordinary preview: available=%t applies=%d %v", got.Available, backend.calls, err)
	}
}

func TestPreviewUnknownUncachedEvictedAndCorruptImages(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	service, _ := testService(t, backend)
	_, err := service.Preview(t.Context(), 0)
	requireConfigError(t, err, "invalid_request")
	_, err = service.Preview(t.Context(), -1)
	requireConfigError(t, err, "invalid_request")
	_, err = service.Preview(t.Context(), 999999)
	requireConfigError(t, err, "preview_not_found")
	refreshForTest(t, service)
	config, _, err := LoadConfig(service.request.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := service.store.Candidates(t.Context(), "local", "home", config.InputRevision("local", "home"), maxCatalogCandidates)
	if err != nil || len(candidates) == 0 {
		t.Fatalf("candidates=%v %v", candidates, err)
	}
	preview, err := service.Preview(t.Context(), candidates[0].Identity.ImageID)
	if err != nil || preview.Available || preview.Reason != "not_cached" || len(preview.Data) != 0 || backend.calls != 0 {
		t.Fatalf("uncached=%+v %v", preview, err)
	}
	current := act(t, service, "next")
	imageID := current.Current.ImageID
	// A size-preserving corruption must be rejected rather than returned as a
	// trusted preview merely because the catalog once validated the original.
	known, ok, err := service.store.KnownCachedImage(t.Context(), imageID)
	if err != nil || !ok {
		t.Fatalf("metadata=%+v %v", known, err)
	}
	if err := os.WriteFile(known.Path, bytes.Repeat([]byte{'x'}, int(known.Bytes)), 0600); err != nil {
		t.Fatal(err)
	}
	preview, err = service.Preview(t.Context(), imageID)
	if err != nil || preview.Available || preview.Reason != "content_unavailable" || len(preview.Data) != 0 {
		t.Fatalf("corrupt=%+v %v", preview, err)
	}
	if err := os.Remove(known.Path); err != nil {
		t.Fatal(err)
	}
	preview, err = service.Preview(t.Context(), imageID)
	if err != nil || preview.Available || preview.Reason != "content_unavailable" {
		t.Fatalf("evicted=%+v %v", preview, err)
	}
	if backend.calls != 1 || service.Status().HistoryLength != 1 {
		t.Fatal("unavailable preview attempted playback")
	}
}

func TestPreviewCancellationAndServiceShutdown(t *testing.T) {
	service, _ := testService(t, &fakeWallpaperBackend{})
	current := act(t, service, "next")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := service.Preview(ctx, current.Current.ImageID)
	requireConfigError(t, err, "canceled")
	service.Close()
	_, err = service.Preview(t.Context(), current.Current.ImageID)
	requireConfigError(t, err, "canceled")
}
