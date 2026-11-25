package playback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/catalog"
)

func TestExclusionUndoRestoreAndRestartDoNotChangeDesktopOrSchedule(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	s, _ := testService(t, backend)
	first := act(t, s, "next")
	act(t, s, "resume")
	before := s.Status()
	original := s.currentOriginPath(t.Context())
	for range 2 {
		status, err := s.Action(t.Context(), "exclude")
		if err != nil || !status.CurrentExcluded || !status.ExclusionUndoAvailable {
			t.Fatalf("exclude: %+v %v", status, err)
		}
	}
	if status := s.Status(); backend.calls != 1 || status.HistoryLength != 1 || status.Paused || !status.NextAt.Equal(*before.NextAt) || status.Current.Path != first.Current.Path {
		t.Fatalf("exclude changed playback: %+v", status)
	}
	if _, err := os.Stat(original); err != nil {
		t.Fatal("source original removed")
	}
	page, err := s.Exclusions(t.Context(), 0, 10)
	if err != nil || len(page.Entries) != 1 || page.Entries[0].SourceID != "home" {
		t.Fatalf("list: %+v %v", page, err)
	}
	s.Close()
	restarted := openTestService(t, s.request, s.resolve, time.Hour, false)
	if status := restarted.Status(); !status.CurrentExcluded || backend.calls != 1 {
		t.Fatal("restart lost exclusion or changed desktop")
	}
	if _, err := os.Stat(original); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}
	if page, err := restarted.Exclusions(t.Context(), 0, 10); err != nil || len(page.Entries) != 1 {
		t.Fatal("missing original hid restore target")
	}
	if status, err := restarted.RestoreExclusion(t.Context(), page.Entries[0].ID); err != nil || status.CurrentExcluded || status.ExclusionUndoAvailable || backend.calls != 1 {
		t.Fatalf("restore: %+v %v", status, err)
	}
	if _, err := restarted.RestoreExclusion(t.Context(), page.Entries[0].ID); err == nil {
		t.Fatal("stale restore handle succeeded")
	}
}

func TestExcludeCurrentRejectsAnEntryThatChangedAfterReview(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	s, _ := testService(t, backend)
	reviewed := act(t, s, "next")
	current := act(t, s, "next")
	stale := HistoryReference{ID: reviewed.Current.ID, ImageID: reviewed.Current.ImageID}
	s.actionMu.Lock()
	_, busyErr := s.ExcludeCurrent(t.Context(), reviewed.InstanceID, stale)
	s.actionMu.Unlock()
	var busy *Error
	if !errors.As(busyErr, &busy) || busy.Code != "busy" {
		t.Fatalf("checked the target without owning playback: %v", busyErr)
	}
	for _, test := range []struct {
		instance string
		entry    HistoryReference
		code     string
	}{
		{reviewed.InstanceID, stale, "current_changed"},
		{current.InstanceID, HistoryReference{ID: reviewed.Current.ID, ImageID: current.Current.ImageID}, "current_changed"},
		{current.InstanceID, HistoryReference{ID: current.Current.ID, ImageID: reviewed.Current.ImageID}, "current_changed"},
		{"previous-instance", HistoryReference{ID: current.Current.ID, ImageID: current.Current.ImageID}, "instance_changed"},
	} {
		status, err := s.ExcludeCurrent(t.Context(), test.instance, test.entry)
		var failure *Error
		if !errors.As(err, &failure) || failure.Code != test.code || status.Current.ID != current.Current.ID || status.ExclusionUndoAvailable {
			t.Fatalf("expected %s without exclusion, got %+v, %v", test.code, status, err)
		}
	}
	page, err := s.Exclusions(t.Context(), 0, MaxExclusionsPageSize)
	if err != nil || len(page.Entries) != 0 {
		t.Fatalf("stale request wrote an exclusion: %+v, %v", page, err)
	}
	status, err := s.ExcludeCurrent(t.Context(), current.InstanceID, HistoryReference{ID: current.Current.ID, ImageID: current.Current.ImageID})
	if err != nil || !status.CurrentExcluded || status.Current.ID != current.Current.ID || backend.calls != 2 {
		t.Fatalf("matching request failed or changed wallpaper: %+v, %v", status, err)
	}
}

func TestExclusionsSkipHistoryAndFreshSelection(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	s, _ := testService(t, backend)
	first := act(t, s, "next")
	act(t, s, "exclude")
	second := act(t, s, "next")
	if second.Current.Path == first.Current.Path {
		t.Fatal("fresh selection chose excluded image")
	}
	if _, err := s.Action(t.Context(), "previous"); err == nil {
		t.Fatal("history replayed excluded image")
	}
	if status := s.Status(); status.HistoryCursor != 1 || backend.calls != 2 {
		t.Fatal("failed navigation moved history or desktop")
	}
	act(t, s, "undo-exclude")
	if previous := act(t, s, "previous"); previous.Current.Path != first.Current.Path {
		t.Fatal("Undo did not restore history eligibility")
	}
	// Exclusions are independent of the bounded history, even when its entries expire.
	act(t, s, "exclude")
	for range HistoryLimit + 1 {
		act(t, s, "next")
	}
	page, err := s.Exclusions(t.Context(), 0, 10)
	if err != nil || len(page.Entries) != 1 || s.Status().HistoryLength != HistoryLimit {
		t.Fatalf("history pruning lost exclusion: %+v %v", page, err)
	}
}

func TestUnknownDuplicateBytesAreExcludedBeforeApplication(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	s, root := testService(t, backend)
	first := act(t, s, "next")
	origin := s.currentOriginPath(t.Context())
	data, err := os.ReadFile(origin)
	if err != nil {
		t.Fatal(err)
	}
	aliasPath := filepath.Join(root, "new-alias.png")
	if err := os.WriteFile(aliasPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	act(t, s, "exclude")
	refreshForTest(t, s)
	config, _, _ := LoadConfig(s.request.ConfigPath)
	p := config.Providers[0]
	rows, err := s.store.Candidates(t.Context(), p.ID, "home", config.InputRevision(p.ID, "home"), maxCatalogCandidates)
	if err != nil {
		t.Fatal(err)
	}
	var alias catalog.Identity
	for _, row := range rows {
		if row.Candidate.ID == "new-alias.png" {
			alias = row.Identity
		}
	}
	if alias.ImageID == 0 {
		t.Fatal("missing alias fixture")
	}
	if _, err := s.materializeChoice(t.Context(), playbackChoice{identity: alias}, config); !errors.Is(err, errExcluded) {
		t.Fatalf("unknown alias was not excluded: %v", err)
	}
	if hash, err := s.store.ContentHash(t.Context(), alias.ImageID); err != nil || hash == "" || !s.exclusions.Contains(alias.ImageID, hash) {
		t.Fatal("validated alias association was not retained")
	}
	if backend.calls != 1 || s.Status().HistoryLength != 1 || s.Status().Current.Path != first.Current.Path {
		t.Fatal("duplicate changed desktop/history")
	}
	if retries, err := s.store.LoadRetries(t.Context()); err != nil || len(retries) != 0 {
		t.Fatal("exclusion was treated as a provider failure")
	}
	// A known duplicate from another provider is rejected before resolving bytes.
	remote := metadataCandidate("remote-alias")
	id, err := s.store.EnsureImage(t.Context(), "wallhaven", "nature", "r1", "", remote)
	if err != nil {
		t.Fatal(err)
	}
	cached, ok, err := s.store.CachedImage(t.Context(), alias.ImageID)
	if err != nil || !ok {
		t.Fatal("missing validated alias cache")
	}
	if err := s.store.SaveCachedImage(t.Context(), id.ImageID, cached); err != nil {
		t.Fatal(err)
	}
	for _, evicted := range []bool{false, true} {
		if evicted {
			if err := os.Remove(cached.Path); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.materializeChoice(t.Context(), playbackChoice{identity: id}, config); !errors.Is(err, errExcluded) {
			t.Fatalf("known provider alias was not rejected before I/O (evicted=%t): %v", evicted, err)
		}
	}
}

func TestExclusionIndependentMarksWithSameHashAndAllExcluded(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	s, root := testService(t, backend)
	act(t, s, "next")
	origin := s.currentOriginPath(t.Context())
	data, err := os.ReadFile(origin)
	if err != nil {
		t.Fatal(err)
	}
	aliasPath := filepath.Join(root, "alias.png")
	if err := os.WriteFile(aliasPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	refreshForTest(t, s)
	config, _, _ := LoadConfig(s.request.ConfigPath)
	p := config.Providers[0]
	rows, err := s.store.Candidates(t.Context(), p.ID, "home", config.InputRevision(p.ID, "home"), maxCatalogCandidates)
	if err != nil {
		t.Fatal(err)
	}
	var alias catalog.Identity
	for _, row := range rows {
		if row.Candidate.ID == "alias.png" {
			alias = row.Identity
		}
	}
	if _, err := s.materializeChoice(t.Context(), playbackChoice{identity: alias}, config); err != nil {
		t.Fatal(err)
	}
	act(t, s, "exclude")
	// Two separate exclusions can originate before two identities are known aliases.
	set, err := s.store.ExcludeImage(t.Context(), alias.ImageID)
	if err != nil {
		t.Fatal(err)
	}
	s.exclusions = set
	page, _ := s.Exclusions(t.Context(), 0, 10)
	if _, err := s.RestoreExclusion(t.Context(), page.Entries[0].ID); err != nil {
		t.Fatal(err)
	}
	if !s.Status().CurrentExcluded {
		t.Fatal("restoring one mark erased the other hash exclusion")
	}
	act(t, s, "undo-exclude")
	if s.Status().CurrentExcluded {
		t.Fatal("Undo failed to remove last hash exclusion")
	}
	for _, row := range rows {
		set, err = s.store.ExcludeImage(t.Context(), row.Identity.ImageID)
		if err != nil {
			t.Fatal(err)
		}
	}
	s.exclusions = set
	if _, err := s.Action(t.Context(), "next"); err == nil || backend.calls != 1 {
		t.Fatal("all-excluded selection relaxed rules")
	}
}

func TestExclusionErrorsBusyUncertainAndPersistenceFailure(t *testing.T) {
	s, _ := testService(t, &fakeWallpaperBackend{})
	for _, action := range []string{"exclude", "undo-exclude"} {
		if _, err := s.Action(t.Context(), action); err == nil {
			t.Fatalf("%s accepted without a target", action)
		}
	}
	act(t, s, "next")
	s.actionMu.Lock()
	_, err := s.Action(t.Context(), "exclude")
	s.actionMu.Unlock()
	if typed, ok := err.(*Error); !ok || typed.Code != "busy" {
		t.Fatalf("busy: %v", err)
	}
	s.state.Pending = &playbackIntent{Path: s.state.CurrentPath(), Identity: s.state.History[s.state.Cursor].Identity, StartedAt: time.Now(), HistoryIndex: -1}
	if _, err := s.Action(t.Context(), "exclude"); err == nil {
		t.Fatal("excluded an uncertain current image")
	}
	s.state.Pending = nil
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Action(ctx, "exclude"); err == nil {
		t.Fatal("canceled exclusion succeeded")
	}
	if err := s.store.Close(); err != nil {
		t.Fatal(err)
	}
	if status, err := s.Action(t.Context(), "exclude"); err == nil || status.CurrentExcluded || status.ExclusionUndoAvailable {
		t.Fatalf("failed write changed memory: %+v %v", status, err)
	}
}

func TestExclusionPageFitsSocketLimitWithEscapedProviderMetadata(t *testing.T) {
	s, _ := testService(t, &fakeWallpaperBackend{})
	for i := 0; i < MaxExclusionsPageSize; i++ {
		item := metadataCandidate(strings.Repeat("\x00", 4095) + fmt.Sprint(i))
		item.Content.URL = "https://example.org/valid.png"
		item.Attribution.Title = strings.Repeat("\x00", 4096)
		prefix := "https://example.org/"
		item.Attribution.PageURL = prefix + strings.Repeat("<", 4096-len(prefix))
		id, err := s.store.EnsureImage(t.Context(), "wallhaven", "nature", "r1", "", item)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.store.ExcludeImage(t.Context(), id.ImageID); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.Exclusions(t.Context(), 0, MaxExclusionsPageSize)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(page)
	if err != nil || len(raw) > 1<<20 || len(page.Entries) != MaxExclusionsPageSize {
		t.Fatalf("unbounded response: %d bytes %v", len(raw), err)
	}
}

func TestExcludedNewAliasDoesNotPreventOnlyEligibleRepeat(t *testing.T) {
	backend := &fakeWallpaperBackend{}
	s, root := testService(t, backend)
	s.draw = func(int) int { return 0 }
	first := act(t, s, "next")
	config, _, _ := LoadConfig(s.request.ConfigPath)
	p := config.Providers[0]
	rows, err := s.store.Candidates(t.Context(), p.ID, "home", config.InputRevision(p.ID, "home"), maxCatalogCandidates)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Candidate.ID == "b.png" {
			if _, err := s.materializeChoice(t.Context(), playbackChoice{identity: row.Identity}, config); err != nil {
				t.Fatal(err)
			}
			set, err := s.store.ExcludeImage(t.Context(), row.Identity.ImageID)
			if err != nil {
				t.Fatal(err)
			}
			s.exclusions = set
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "b.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "new-b-alias.png"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "c.png")); err != nil {
		t.Fatal(err)
	}
	second := act(t, s, "next")
	if second.Current.Path != first.Current.Path || backend.calls != 2 {
		t.Fatal("normal exclusion incorrectly blocked the only eligible repeat")
	}
}
