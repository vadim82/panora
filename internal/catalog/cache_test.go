package catalog

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVersionOneMigrationRetainsPlaybackAndOrigin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO images(id,provider_id,identity_scope,provider_image_id,metadata_json,first_seen_ms,last_seen_ms) VALUES(7,'local','home','a','{}',1,2);
 INSERT INTO input_images(provider_id,input_id,settings_revision,image_id,candidate_json,first_seen_ms,last_seen_ms) VALUES('local','home','r1',7,'{}',1,2);
 INSERT INTO playback_history(id,image_id,provider_id,input_id,settings_revision,path,accepted_at_ms) VALUES(9,7,'local','home','r1','/a.png',3);
 UPDATE playback_state SET paused=1,history_id=9,last_action_ms=3;
 INSERT INTO pending_apply(singleton,image_id,provider_id,input_id,settings_revision,path,history_id,started_at_ms) VALUES(1,7,'local','home','r1','/a.png',9,4);`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	store, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var version int
	if err = store.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 6 {
		t.Fatalf("version %d: %v", version, err)
	}
	state, err := store.LoadState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(state.History) != 1 || state.History[0].Identity.ImageID != 7 || state.Pending == nil || !state.Paused || state.Cursor != 0 {
		t.Fatalf("migration lost playback: %+v", state)
	}
}

func TestCacheContentDedupAndMissingFileRecovery(t *testing.T) {
	store, _ := openTest(t)
	ctx := t.Context()
	a, err := store.EnsureImage(ctx, "local", "home", "r1", "home", candidate("a"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.EnsureImage(ctx, "wallhaven", "mountains", "r1", "", candidate("b"))
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("published bytes")
	path := filepath.Join(t.TempDir(), "published.png")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	file := CachedFile{Hash: fmt.Sprintf("%x", sha256.Sum256(data)), Path: path, Bytes: int64(len(data)), Width: 12, Height: 9}
	for _, id := range []Identity{a, b} {
		if err = store.SaveCachedImage(ctx, id.ImageID, file); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = store.db.QueryRow("SELECT count(*) FROM cached_content").Scan(&count); err != nil || count != 1 {
		t.Fatalf("content rows %d: %v", count, err)
	}
	cached, ok, err := store.CachedImage(ctx, a.ImageID)
	if err != nil || !ok || cached != file {
		t.Fatalf("lookup: %+v %t %v", cached, ok, err)
	}
	now := time.Now().UTC()
	state := State{Cursor: 0, History: []Record{{Reference: "fixture-reference", Path: path, AcceptedAt: now, Identity: a}}, LastActionAt: now}
	if err = store.SaveState(ctx, state); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, ok, err = store.CachedImage(ctx, a.ImageID); err != nil || ok {
		t.Fatalf("missing file: %t %v", ok, err)
	}
	origin, err := store.LookupCandidate(ctx, a)
	if err != nil || origin.ID != "a" {
		t.Fatalf("origin lost: %+v %v", origin, err)
	}
	loaded, err := store.LoadState(ctx)
	if err != nil || len(loaded.History) != 1 {
		t.Fatalf("history lost: %+v %v", loaded, err)
	}
	if _, err = store.LookupCandidate(ctx, Identity{ImageID: a.ImageID, ProviderID: "wallhaven", InputID: "home", Revision: "r1"}); err == nil {
		t.Fatal("cross-provider identity returned origin")
	}
}

func TestCacheMetadataRollbackLeavesPublishedFile(t *testing.T) {
	store, _ := openTest(t)
	data := []byte("orphan published bytes")
	path := filepath.Join(t.TempDir(), "orphan.png")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	file := CachedFile{Hash: fmt.Sprintf("%x", sha256.Sum256(data)), Path: path, Bytes: int64(len(data)), Width: 2, Height: 3}
	if err := store.SaveCachedImage(t.Context(), 999, file); err == nil {
		t.Fatal("unknown image accepted")
	}
	var count int
	if err := store.db.QueryRow("SELECT count(*) FROM cached_content").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed transaction retained content: %d %v", count, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("failed metadata transaction removed published file")
	}
}

func TestCacheMigrationFailureRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE image_content(unrelated TEXT)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if store, err := Open(t.Context(), path); err == nil {
		store.Close()
		t.Fatal("conflicting upgrade accepted")
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, count int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 1 {
		t.Fatalf("failed upgrade changed version: %d %v", version, err)
	}
	if err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='cached_content'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed upgrade retained table: %d %v", count, err)
	}
}

func TestKnownContentHashSurvivesEviction(t *testing.T) {
	store, _ := openTest(t)
	ctx := t.Context()
	id, err := store.EnsureImage(ctx, "local", "home", "r1", "home", candidate("a"))
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("published image bytes")
	path := filepath.Join(t.TempDir(), "content.png")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	file := CachedFile{Hash: fmt.Sprintf("%x", sha256.Sum256(data)), Path: path, Bytes: int64(len(data)), Width: 3, Height: 2}
	if err = store.SaveCachedImage(ctx, id.ImageID, file); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.CachedImage(ctx, id.ImageID); err != nil || ok {
		t.Fatalf("evicted cache lookup: %t %v", ok, err)
	}
	hash, err := store.ContentHash(ctx, id.ImageID)
	if err != nil || hash != file.Hash {
		t.Fatalf("known hash lost: %q %v", hash, err)
	}
	candidates, err := store.Candidates(ctx, "local", "home", "r1", 10)
	if err != nil || len(candidates) != 1 {
		t.Fatal(err)
	}
	if candidates[0].Hash != file.Hash {
		t.Fatalf("bulk known metadata: %+v", candidates[0])
	}
	origin, err := store.LookupCandidate(ctx, id)
	if err != nil || origin.ID != "a" {
		t.Fatal("origin lost")
	}
	if hash, err = store.ContentHash(ctx, 999); err != nil || hash != "" {
		t.Fatalf("unknown content hash: %q %v", hash, err)
	}
}

func TestCorruptVersionOneDoesNotCommitUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("PRAGMA foreign_keys=OFF;UPDATE playback_state SET history_id=999 WHERE singleton=1"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if store, err := Open(t.Context(), path); err == nil {
		store.Close()
		t.Fatal("invalid foreign key accepted")
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, count int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 1 {
		t.Fatalf("corrupt state was upgraded: %d %v", version, err)
	}
	if err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='cached_content'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("corrupt state gained cache schema: %d %v", count, err)
	}
}
