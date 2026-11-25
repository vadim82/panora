package catalog

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryMigrationAssignsStableReferencesAndRetainsPending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// A genuine old catalog retains its current/history/pending foreign keys.
	_, err = db.Exec(schema + cacheSchema + retrySchema + exclusionSchema + `
 INSERT INTO images(id,provider_id,identity_scope,provider_image_id,metadata_json,first_seen_ms,last_seen_ms) VALUES(1,'local','home','one.png','{}',1,1);
 INSERT INTO input_images(provider_id,input_id,settings_revision,image_id,candidate_json,first_seen_ms,last_seen_ms) VALUES('local','home','r1',1,'{}',1,1);
 INSERT INTO playback_history(id,image_id,provider_id,input_id,settings_revision,path,accepted_at_ms) VALUES(7,1,'local','home','r1','/cache/one.png',1000);
 UPDATE playback_state SET history_id=7,last_action_ms=1000;
 INSERT INTO pending_apply(singleton,image_id,provider_id,input_id,settings_revision,path,history_id,started_at_ms) VALUES(1,1,'local','home','r1','/cache/one.png',7,2000);`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	store, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.LoadState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ref := state.History[0].Reference
	if len(ref) != 32 || state.Cursor != 0 || state.Pending == nil || state.Pending.HistoryIndex != 0 {
		t.Fatalf("migrated=%+v", state)
	}
	if err := store.SaveState(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	after, err := store.LoadState(t.Context())
	if err != nil || after.History[0].Reference != ref || after.Pending == nil {
		t.Fatalf("restart=%+v %v", after, err)
	}
}

func TestHistoryMigrationFailureRollsBackVersionAndReferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema + cacheSchema + retrySchema + exclusionSchema + `CREATE TABLE blocker(x);CREATE INDEX playback_history_by_reference ON blocker(x);`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if store, err := Open(t.Context(), path); err == nil {
		store.Close()
		t.Fatal("conflicting migration succeeded")
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 4 {
		t.Fatalf("version=%d %v", version, err)
	}
	if _, err := db.Exec("SELECT public_ref FROM playback_history"); err == nil {
		t.Fatal("failed migration retained a partial column")
	}
}

func TestHistoryReferenceSurvivesRewritesAndCannotDuplicate(t *testing.T) {
	store, _ := openTest(t)
	id, err := store.EnsureImage(t.Context(), "local", "home", "r1", "home", candidate("one"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	one := Record{Reference: "first-stable-handle", Identity: id, Path: "/cache/one.png", AcceptedAt: now}
	two := Record{Reference: "second-stable-handle", Identity: id, Path: "/cache/two.png", AcceptedAt: now}
	state := State{History: []Record{one, two}, Cursor: 0, LastActionAt: now}
	if err := store.SaveState(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	// Pruning renumbers private SQL rows but retains the public handle.
	state.History = []Record{two}
	if err := store.SaveState(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadState(t.Context())
	if err != nil || got.History[0].Reference != two.Reference {
		t.Fatalf("pruned=%+v %v", got, err)
	}
	state.History = []Record{two, two}
	if err := store.SaveState(t.Context(), state); err == nil {
		t.Fatal("duplicate handle accepted")
	}
	got, err = store.LoadState(t.Context())
	if err != nil || len(got.History) != 1 || got.History[0].Reference != two.Reference {
		t.Fatalf("rollback=%+v %v", got, err)
	}
}

func TestHistoryLoadRejectsDuplicateReferencesWithoutIndex(t *testing.T) {
	store, _ := openTest(t)
	id, err := store.EnsureImage(t.Context(), "local", "home", "r1", "home", candidate("one"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	state := State{History: []Record{{Reference: "stable-handle", Identity: id, Path: "/cache/image.png", AcceptedAt: now}}, Cursor: 0, LastActionAt: now}
	if err := store.SaveState(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`DROP INDEX playback_history_by_reference;
 INSERT INTO playback_history(id,public_ref,image_id,provider_id,input_id,settings_revision,path,accepted_at_ms)
 SELECT 2,public_ref,image_id,provider_id,input_id,settings_revision,path,accepted_at_ms FROM playback_history WHERE id=1;`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadState(t.Context()); err == nil {
		t.Fatal("ambiguous public references loaded")
	}
}
