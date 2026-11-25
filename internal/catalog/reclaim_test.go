package catalog

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/source"
)

func TestReclaimKeepsReferencedMembershipsAndRetryGates(t *testing.T) {
	s, path := openTest(t)
	ctx := t.Context()
	seed := func(provider, input, revision, id string) Identity {
		t.Helper()
		identity, err := s.EnsureImage(ctx, provider, input, revision, "", candidate(id))
		if err != nil {
			t.Fatal(err)
		}
		return identity
	}
	active := seed("wallhaven", "saved", "current", "active")
	historical := seed("wallhaven", "saved", "old", "history")
	pending := seed("wallhaven", "removed", "old", "pending")
	excluded := seed("local", "former", "old", "excluded")
	excludedAlias := seed("local", "former", "older", "excluded")
	retired := seed("wallhaven", "saved", "old", "retired")
	newer := seed("wallhaven", "removed", "old", "recent")
	if _, err := s.ExcludeImage(ctx, excluded.ImageID); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	cutoff := now.Add(time.Minute)
	imagePath := filepath.Join(t.TempDir(), "image.png")
	state := State{Cursor: 0, Paused: true,
		History: []Record{{Reference: "accepted", Path: imagePath, AcceptedAt: now, Identity: historical}},
		Pending: &Intent{Path: imagePath, StartedAt: now, Identity: pending, HistoryIndex: -1}}
	if err := s.SaveState(ctx, state); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE input_images SET last_seen_ms=? WHERE image_id=?", cutoff.Add(time.Minute).UnixMilli(), newer.ImageID); err != nil {
		t.Fatal(err)
	}
	for _, scan := range []InputRevision{{"wallhaven", "gone", "retired"}, {"local", "former", "older"}} {
		if _, err := s.StartScan(ctx, scan.ProviderID, scan.InputID, scan.Revision, false); err != nil {
			t.Fatal(err)
		}
	}
	staleRetry := RetryKey{ProviderID: "wallhaven", InputID: "removed", Revision: "old", Scope: "discovery"}
	keepRetries := []RetryState{
		{RetryKey: RetryKey{ProviderID: "wallhaven", Scope: "provider_metadata"}, Attempts: 2, NextAt: now, Stage: "discovery", Code: "rate_limited"},
		{RetryKey: RetryKey{ProviderID: "wallhaven", InputID: "saved", Revision: "current", Scope: "discovery"}, Attempts: 2, NextAt: now, Stage: "discovery", Code: "unavailable"},
		{RetryKey: RetryKey{ProviderID: "wallhaven", InputID: "removed", Revision: "old", Scope: "materialization", ImageID: pending.ImageID}, Attempts: 2, NextAt: cutoff.Add(time.Hour), Stage: "download", Code: "unavailable"},
	}
	if err := s.SaveRetries(ctx, append(keepRetries, RetryState{RetryKey: staleRetry, Attempts: 1, NextAt: now, Stage: "discovery", Code: "unavailable"})); err != nil {
		t.Fatal(err)
	}
	_, pruned, err := s.ReclaimObsolete(ctx, []InputRevision{{"wallhaven", "saved", "current"}}, cutoff, ReclaimCursor{})
	if err != nil || !reflect.DeepEqual(pruned, []RetryKey{staleRetry}) {
		t.Fatalf("reclaimed retries=%+v: %v", pruned, err)
	}
	for _, id := range []Identity{retired, excludedAlias} {
		if _, err := s.LookupCandidate(ctx, id); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("obsolete membership survived: %+v, %v", id, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []Identity{active, historical, pending, excluded, newer} {
		if _, err := s.LookupCandidate(ctx, id); err != nil {
			t.Fatalf("protected membership lost: %+v, %v", id, err)
		}
	}
	entries, _, err := s.ListExclusions(ctx, 0, 1)
	if err != nil || len(entries) != 1 || entries[0].InputID != "former" {
		t.Fatalf("exclusion origin lost: %+v, %v", entries, err)
	}
	var progressCount int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM discovery_progress WHERE provider_id='local' AND input_id='former'").Scan(&progressCount); err != nil || progressCount != 1 {
		t.Fatalf("local scan epoch lost while a membership survives: %d, %v", progressCount, err)
	}
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM discovery_progress WHERE input_id='gone'").Scan(&progressCount); err != nil || progressCount != 0 {
		t.Fatalf("obsolete checkpoint retained: %d, %v", progressCount, err)
	}
	got, err := s.LoadRetries(ctx)
	if err != nil || len(got) != len(keepRetries) {
		t.Fatalf("retained retries=%+v: %v", got, err)
	}
	if returned := seed("wallhaven", "saved", "returning", "retired"); returned.ImageID != retired.ImageID {
		t.Fatalf("rediscovery replaced image identity: %+v -> %+v", retired, returned)
	}
}

func TestReclaimSweepContinuesPastCurrentRows(t *testing.T) {
	s, _ := openTest(t)
	ctx := t.Context()
	scan, err := s.StartScan(ctx, "local", "folder", "revision", false)
	if err != nil {
		t.Fatal(err)
	}
	for i := range reclaimBatchSize {
		if _, err := s.CommitPage(ctx, scan, "folder", source.Page{
			Candidates: []source.Candidate{candidate(fmt.Sprintf("image-%d", i))}, NextCursor: json.RawMessage(`{"page":2}`),
		}); err != nil {
			t.Fatal(err)
		}
		scan.Cursor = json.RawMessage(`{"page":2}`)
	}
	if _, err := s.CommitPage(ctx, scan, "folder", source.Page{Candidates: []source.Candidate{}, NextCursor: json.RawMessage(`null`)}); err != nil {
		t.Fatal(err)
	}
	stale, err := s.EnsureImage(ctx, "local", "folder", "revision", "folder", candidate("stale"))
	if err != nil {
		t.Fatal(err)
	}
	inputs := []InputRevision{{"local", "folder", "revision"}}
	cutoff := time.Now().Add(time.Minute)
	cursor, _, err := s.ReclaimObsolete(ctx, inputs, cutoff, ReclaimCursor{})
	if err != nil || cursor.memberships == 0 {
		t.Fatalf("first batch did not advance: %+v, %v", cursor, err)
	}
	if _, err := s.LookupCandidate(ctx, stale); err != nil {
		t.Fatalf("first batch passed the scan limit: %v", err)
	}
	if _, _, err := s.ReclaimObsolete(ctx, inputs, cutoff, cursor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LookupCandidate(ctx, stale); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unseen local membership retained after complete scan: %v", err)
	}
}
