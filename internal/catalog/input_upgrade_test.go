package catalog

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/source"
)

func TestInputRevisionUpgradePreservesPlaybackAndRetry(t *testing.T) {
	store, path := openTest(t)
	ctx := t.Context()
	scan, err := store.StartScan(ctx, "wallhaven", "nature", "old", false)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := store.CommitPage(ctx, scan, "", source.Page{Candidates: []source.Candidate{candidate("wall")}, NextCursor: json.RawMessage(`{"page":2}`)})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	record := Record{Reference: "retained", Path: "/cache/wall.jpg", AcceptedAt: now, Identity: ids[0]}
	state := State{History: []Record{record}, Cursor: 0, Paused: true, LastActionAt: now, Pending: &Intent{Identity: ids[0], Path: record.Path, StartedAt: now, HistoryIndex: 0}}
	if err := store.SaveState(ctx, state); err != nil {
		t.Fatal(err)
	}
	retry := RetryState{RetryKey: RetryKey{ProviderID: "wallhaven", InputID: "nature", Revision: "old", Scope: "discovery"}, Attempts: 2, NextAt: now.Add(time.Hour), Stage: "discovery", Code: "rate_limited"}
	if err := store.SaveRetry(ctx, retry); err != nil {
		t.Fatal(err)
	}
	upgrades := []InputRevisionUpgrade{{InputRevision: InputRevision{ProviderID: "wallhaven", InputID: "nature", Revision: "new"}, LegacyRevision: "old"}}
	if err := store.UpgradeInputRevisions(ctx, upgrades); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.LoadState(ctx)
	if err != nil || len(got.History) != 1 || got.Pending == nil || !got.Paused || got.Cursor != 0 {
		t.Fatalf("lost state: %+v, %v", got, err)
	}
	newIdentity := ids[0]
	newIdentity.Revision = "new"
	if got.History[0].Identity != newIdentity || got.History[0].Reference != record.Reference || got.Pending.Identity != newIdentity {
		t.Fatalf("lost committed provenance: %+v", got)
	}
	if c, err := store.LookupCandidate(ctx, newIdentity); err != nil || c.ID != "wall" {
		t.Fatalf("candidate = %+v, %v", c, err)
	}
	resumed, err := store.StartScan(ctx, "wallhaven", "nature", "new", false)
	if err != nil || !source.IsNull(resumed.Cursor) {
		t.Fatalf("retained incompatible cursor: %+v, %v", resumed, err)
	}
	retry.Revision = "new"
	retries, err := store.LoadRetries(ctx)
	if err != nil || len(retries) != 1 || retries[0] != retry {
		t.Fatalf("lost retry deadline: %+v, %v", retries, err)
	}
	if err := store.DeleteRetry(ctx, retry.RetryKey); err != nil {
		t.Fatal(err)
	}
	if err := store.UpgradeInputRevisions(ctx, upgrades); err != nil {
		t.Fatal(err)
	}
	if retries, err := store.LoadRetries(ctx); err != nil || len(retries) != 0 {
		t.Fatalf("migration replay restored retry: %+v, %v", retries, err)
	}
}

func TestLocalScanCompletionFencesStaleCandidates(t *testing.T) {
	store, path := openTest(t)
	ctx := t.Context()
	start := func() Scan {
		t.Helper()
		scan, err := store.StartScan(ctx, "local", "home", "rev", false)
		if err != nil {
			t.Fatal(err)
		}
		return scan
	}
	populate := func(scan Scan, c source.Candidate, cursor string) Identity {
		t.Helper()
		ids, err := store.CommitPage(ctx, scan, "home", source.Page{Candidates: []source.Candidate{c}, NextCursor: json.RawMessage(cursor)})
		if err != nil {
			t.Fatal(err)
		}
		return ids[0]
	}
	old := populate(start(), candidate("old"), "null")
	populate(start(), candidate("new-a"), `{"page":2}`)
	if rows, err := store.Candidates(ctx, "local", "home", "rev", 10); err != nil || len(rows) != 2 {
		t.Fatalf("partial scan hid prior candidates: %v, %v", rows, err)
	}
	store.Close()
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	store = reopened
	populate(start(), candidate("new-b"), "null")
	if rows, err := store.Candidates(ctx, "local", "home", "rev", 10); err != nil || len(rows) != 2 || rows[0].Candidate.ID != "new-a" || rows[1].Candidate.ID != "new-b" {
		t.Fatalf("completed scan retained the stale candidate: %v, %v", rows, err)
	}
	if c, err := store.LookupCandidate(ctx, old); err != nil || c.ID != "old" {
		t.Fatalf("history lookup lost stale origin: %v, %v", c, err)
	}
}
