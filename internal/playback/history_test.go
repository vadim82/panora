package playback

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryReferencesPagingReplayAndRestart(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	service, _ := testService(t, backend)
	firstStatus := act(t, service, "next")
	first := firstStatus.Current.ID
	firstImage := firstStatus.Current.ImageID
	firstDetails := firstStatus.CurrentImage
	if firstDetails == nil || firstDetails.OriginalPath == firstStatus.Current.Path || firstDetails.Dimensions == nil || firstDetails.Dimensions.Width != 2 {
		t.Fatalf("first-image projection: %+v", firstDetails)
	}
	act(t, service, "next")
	current := service.Status()
	second := current.Current.ID
	if current.PreviousHistory == nil || *current.PreviousHistory != (HistoryReference{ID: first, ImageID: firstImage, ContentRevision: previewContentRevision(firstStatus.Current.Path)}) || current.ForwardHistory != nil {
		t.Fatalf("history edge: previous=%+v forward=%+v", current.PreviousHistory, current.ForwardHistory)
	}
	if first == "" || first == second || firstImage <= 0 {
		t.Fatal("history IDs are not distinct stable handles")
	}
	page, err := service.History(t.Context(), "", 1)
	if err != nil || len(page.Entries) != 1 || page.Entries[0].ID != second || !page.Entries[0].Current || page.CurrentID != second || page.NextBefore != second || page.Entries[0].Dimensions == nil || page.Entries[0].Availability != "cached" {
		t.Fatalf("page=%+v %v", page, err)
	}
	older, err := service.History(t.Context(), page.NextBefore, 1)
	if err != nil || len(older.Entries) != 1 || older.Entries[0].ID != first || older.Entries[0].ImageID != firstImage || older.NextBefore != "" {
		t.Fatalf("older=%+v %v", older, err)
	}
	status, err := service.ReplayHistory(t.Context(), first)
	if err != nil || status.Current.ID != first || status.HistoryCursor != 0 || status.HistoryLength != 2 || backend.calls != 3 {
		t.Fatalf("replay=%+v %v", status, err)
	}
	if status.CurrentImage == nil || status.CurrentImage.Dimensions == nil || status.CurrentImage.OriginalPath != firstDetails.OriginalPath {
		t.Fatalf("projection did not follow replay: %+v", status.CurrentImage)
	}
	status.CurrentImage.Dimensions.Width = 0
	if service.Status().CurrentImage.Dimensions.Width != 2 {
		t.Fatal("status returned mutable service metadata")
	}
	if status.PreviousHistory != nil || status.ForwardHistory == nil || *status.ForwardHistory != (HistoryReference{ID: second, ImageID: current.Current.ImageID, ContentRevision: previewContentRevision(current.Current.Path)}) {
		t.Fatalf("replay: previous=%+v forward=%+v", status.PreviousHistory, status.ForwardHistory)
	}
	// A subsequent pause/state write cannot invalidate the previously saved ID.
	act(t, service, "pause")
	service.Close()
	restarted := openTestService(t, service.request, service.resolve, time.Hour, false)
	status = restarted.Status()
	if status.Current.ID != first || status.CurrentImage == nil || status.CurrentImage.OriginalPath != firstDetails.OriginalPath || status.CurrentImage.Dimensions == nil || status.CurrentImage.Dimensions.Width != 2 {
		t.Fatalf("restart lost accepted-image details: %+v", status.CurrentImage)
	}
	status, err = restarted.ReplayHistory(t.Context(), second)
	if err != nil || status.Current.ID != second || status.HistoryCursor != 1 || backend.calls != 4 {
		t.Fatalf("restart replay=%+v %v", status, err)
	}
}

func TestHistoryReplayExcludedUnavailableAndFailedApplyKeepCursor(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	service, _ := testService(t, backend)
	act(t, service, "next")
	first := service.Status().Current.ID
	act(t, service, "exclude")
	secondStatus := act(t, service, "next")
	second := secondStatus.Current.ID
	_, err := service.ReplayHistory(t.Context(), first)
	requireConfigError(t, err, "history_excluded")
	if service.Status().Current.ID != second || backend.calls != 2 {
		t.Fatal("excluded replay changed current")
	}
	page, err := service.History(t.Context(), "", 10)
	if err != nil || page.Entries[1].Reason != "excluded" {
		t.Fatalf("excluded list=%+v %v", page, err)
	}
	act(t, service, "undo-exclude")
	backend.err = errors.New("simulated rejection")
	status, err := service.ReplayHistory(t.Context(), first)
	requireConfigError(t, err, "apply_uncertain")
	if status.Current.ID != second || status.HistoryCursor != 1 || !status.Uncertain || !status.Paused {
		t.Fatalf("failed replay=%+v", status)
	}
	if status.CurrentImage == nil || secondStatus.CurrentImage == nil || status.CurrentImage.OriginalPath != secondStatus.CurrentImage.OriginalPath {
		t.Fatalf("unconfirmed image replaced accepted-image details: %+v", status.CurrentImage)
	}
	backend.err = nil
	status, err = service.ReplayHistory(t.Context(), first)
	if err != nil || status.Current.ID != first || status.Uncertain {
		t.Fatalf("recovery=%+v %v", status, err)
	}
	before := settings(t, service)
	_, err = service.UpdateFolder(t.Context(), UpdateFolderRequest{Revision: before.Revision, ID: "home", Enabled: configValue(false)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ReplayHistory(t.Context(), second)
	requireConfigError(t, err, "history_unavailable")
	if service.Status().Current.ID != first || backend.calls != 4 {
		t.Fatal("unavailable replay changed current")
	}
	_, err = service.ReplayHistory(t.Context(), "pruned-or-absent")
	requireConfigError(t, err, "history_not_found")
}

func TestHistoryPruningRetainsNewerSavedHandles(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	service, root := testService(t, backend)
	// Populate accepted records without costly repeated image applications.
	act(t, service, "next")
	original := service.state.History[0]
	for i := 0; i < HistoryLimit; i++ {
		service.state.Record(filepath.Join(root, "a.png"), "home")
		service.state.History[len(service.state.History)-1].Identity = original.Identity
	}
	if err := service.saveState(service.state); err != nil {
		t.Fatal(err)
	}
	retained := service.state.History[0].ID
	_, err := service.ReplayHistory(t.Context(), original.ID)
	requireConfigError(t, err, "history_not_found")
	status, err := service.ReplayHistory(t.Context(), retained)
	if err != nil || status.Current.ID != retained || status.HistoryLength != HistoryLimit {
		t.Fatalf("retained handle=%+v %v", status, err)
	}
}

func TestHistoryAvailabilityRetainsDimensionsAfterEviction(t *testing.T) {
	service, _ := testService(t, &fakeWallpaperBackend{})
	act(t, service, "next")
	entry := service.state.History[0]
	if err := os.Remove(entry.Path); err != nil {
		t.Fatal(err)
	}
	before := settings(t, service)
	if _, err := service.UpdateConfiguration(t.Context(), UpdateConfigurationRequest{Revision: before.Revision, MinWidth: configValue(100000)}); err != nil {
		t.Fatal(err)
	}
	page, err := service.History(t.Context(), "", 10)
	if err != nil || page.Entries[0].Dimensions == nil || page.Entries[0].Availability != "unverified" || page.Entries[0].Reason != "recheck_on_replay" {
		t.Fatalf("known dimensions lost=%+v %v", page, err)
	}
}

func TestHistoryReadBoundsAndBusy(t *testing.T) {
	service, _ := testService(t, &fakeWallpaperBackend{})
	for _, limit := range []int{0, 11} {
		_, err := service.History(t.Context(), "", limit)
		requireConfigError(t, err, "invalid_request")
	}
	_, err := service.History(t.Context(), "not-retained", 10)
	requireConfigError(t, err, "history_not_found")
	service.actionMu.Lock()
	_, err = service.History(t.Context(), "", 10)
	requireConfigError(t, err, "busy")
	service.actionMu.Unlock()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = service.History(ctx, "", 10)
	requireConfigError(t, err, "canceled")
}
