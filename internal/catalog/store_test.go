package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vadim82/panora/internal/source"
)

func openTest(t *testing.T) (*Store, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, p
}
func candidate(id string) source.Candidate {
	return source.Candidate{ID: id, MediaKind: "image", Ref: json.RawMessage(`{"file":"x"}`), Content: &source.Content{Kind: "https", MediaKind: "image", URL: "https://example.com/image.png"}}
}

func TestIdentityScopesAndMemberships(t *testing.T) {
	s, _ := openTest(t)
	ctx := t.Context()
	a, err := s.EnsureImage(ctx, "local", "home", "r1", "home", candidate("mountain.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.EnsureImage(ctx, "local", "work", "r1", "work", candidate("mountain.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	if a.ImageID == b.ImageID {
		t.Fatal("local scopes collided")
	}
	c := candidate("xyz")
	c.Attribution = &source.Attribution{Author: "Known"}
	first, err := s.EnsureImage(ctx, "wallhaven", "mountains", "r1", "", c)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.EnsureImage(ctx, "wallhaven", "nature", "r2", "", candidate("xyz"))
	if err != nil {
		t.Fatal(err)
	}
	if first.ImageID != second.ImageID {
		t.Fatal("cross-search identity changed")
	}
	var metadata string
	if err = s.db.QueryRow("SELECT metadata_json FROM images WHERE id=?", first.ImageID).Scan(&metadata); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(metadata, "Known") {
		t.Fatal("missing fields erased known attribution")
	}
	for _, input := range []string{"mountains", "nature"} {
		var n int
		if err = s.db.QueryRow("SELECT count(*) FROM input_images WHERE provider_id='wallhaven' AND input_id=?", input).Scan(&n); err != nil || n != 1 {
			t.Fatalf("membership %s: %d %v", input, n, err)
		}
	}
}

func TestScanAtomicResumeAndStaleCommits(t *testing.T) {
	s, path := openTest(t)
	ctx := t.Context()
	scan, err := s.StartScan(ctx, "wallhaven", "nature", "v1", false)
	if err != nil {
		t.Fatal(err)
	}
	page := source.Page{Candidates: []source.Candidate{candidate("a")}, NextCursor: json.RawMessage(`{"page":2}`)}
	ids, err := s.CommitPage(ctx, scan, "", page)
	if err != nil || len(ids) != 1 {
		t.Fatalf("commit: %v", err)
	}
	if _, err = s.CommitPage(ctx, scan, "", page); !errors.Is(err, ErrStaleScan) {
		t.Fatalf("replay: %v", err)
	}
	s.Close()
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	resumed, err := s.StartScan(ctx, "wallhaven", "nature", "v1", false)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.ScanID != scan.ScanID || string(resumed.Cursor) != `{"page":2}` {
		t.Fatalf("did not resume committed cursor: %+v", resumed)
	}
	invalid := candidate("bad")
	invalid.MediaKind = "video"
	_, err = s.CommitPage(ctx, resumed, "", source.Page{Candidates: []source.Candidate{candidate("b"), invalid}, NextCursor: json.RawMessage("null")})
	if err == nil {
		t.Fatal("invalid page committed")
	}
	after, err := s.StartScan(ctx, "wallhaven", "nature", "v1", false)
	if err != nil {
		t.Fatal(err)
	}
	if after.Complete || string(after.Cursor) != string(resumed.Cursor) {
		t.Fatal("failed transaction changed checkpoint")
	}
	candidates, err := s.Candidates(ctx, "wallhaven", "nature", "v1", 10)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("failed transaction left images: %d %v", len(candidates), err)
	}
	fresh, err := s.StartScan(ctx, "wallhaven", "nature", "v2", false)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.ScanID == resumed.ScanID || !source.IsNull(fresh.Cursor) {
		t.Fatal("revision did not reset scan")
	}
	if _, err = s.CommitPage(ctx, resumed, "", page); !errors.Is(err, ErrStaleScan) {
		t.Fatalf("old revision accepted: %v", err)
	}
}

func TestPlaybackTransactionsAndPendingRecovery(t *testing.T) {
	s, path := openTest(t)
	ctx := t.Context()
	id, err := s.EnsureImage(ctx, "local", "home", "r1", "home", candidate("a"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	state := State{Cursor: 0, Paused: true, LastActionAt: now, History: []Record{{Reference: "fixture-reference", Path: "/wallpapers/a.png", AcceptedAt: now, Identity: id}}, Pending: &Intent{Path: "/wallpapers/a.png", StartedAt: now, Identity: id, HistoryIndex: 0}}
	if err = s.SaveState(ctx, state); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	loaded, err := s.LoadState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.History) != 1 || loaded.Cursor != 0 || !loaded.Paused || loaded.Pending == nil || loaded.Pending.Identity != id || !loaded.LastActionAt.Equal(now) {
		t.Fatalf("bad restart state: %+v", loaded)
	}
	broken := state
	broken.History = []Record{{Reference: "fixture-reference", Path: "/x", AcceptedAt: now, Identity: Identity{ImageID: 999, ProviderID: "other", InputID: "wrong", Revision: "r"}}}
	if err = s.SaveState(ctx, broken); err == nil {
		t.Fatal("invalid foreign key accepted")
	}
	loaded, err = s.LoadState(ctx)
	if err != nil || loaded.History[0].Path != "/wallpapers/a.png" || loaded.Pending == nil {
		t.Fatalf("rollback failed: %+v %v", loaded, err)
	}
	state.Pending = nil
	if err = s.SaveState(ctx, state); err != nil {
		t.Fatal(err)
	}
	loaded, err = s.LoadState(ctx)
	if err != nil || loaded.Pending != nil {
		t.Fatal("accepted state did not clear intent")
	}
}

func TestRejectUnsupportedDatabasesWithoutReset(t *testing.T) {
	for _, version := range []int{0, 7} {
		t.Run(string(rune('0'+version)), func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "state.db")
			db, err := sql.Open("sqlite", p)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec("CREATE TABLE unrelated(value TEXT);INSERT INTO unrelated VALUES('keep')"); err != nil {
				t.Fatal(err)
			}
			if version == 7 {
				if _, err := db.Exec("PRAGMA user_version=7"); err != nil {
					t.Fatal(err)
				}
			}
			db.Close()
			before, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if s, err := Open(t.Context(), p); err == nil {
				s.Close()
				t.Fatal("unsupported database accepted")
			}
			after, _ := os.ReadFile(p)
			if string(before) != string(after) {
				t.Fatal("unsupported database modified")
			}
		})
	}
	p := filepath.Join(t.TempDir(), "legacy.db")
	os.WriteFile(p, []byte(" {\"version\":2}"), 0600)
	if s, err := Open(t.Context(), p); err == nil {
		s.Close()
		t.Fatal("JSON accepted")
	}
	if s, err := Open(t.Context(), filepath.Join(t.TempDir(), "state.json")); err == nil {
		s.Close()
		t.Fatal("JSON extension accepted")
	}
}

func TestCanceledOperations(t *testing.T) {
	s, _ := openTest(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.StartScan(ctx, "local", "home", "r1", false); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled scan: %v", err)
	}
}

func TestReplacedConnectionRetainsSafetyPragmas(t *testing.T) {
	s, _ := openTest(t)
	s.db.SetMaxIdleConns(0)
	for _, check := range []struct {
		pragma string
		want   int
	}{{"foreign_keys", 1}, {"busy_timeout", 1000}, {"synchronous", 2}} {
		var got int
		if err := s.db.QueryRowContext(t.Context(), "PRAGMA "+check.pragma).Scan(&got); err != nil || got != check.want {
			t.Fatalf("new connection %s = %d, %v", check.pragma, got, err)
		}
	}
}

func TestIncompleteVersionOneRejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "broken.db")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("PRAGMA user_version=1;CREATE TABLE images(id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if s, err := Open(t.Context(), p); err == nil {
		s.Close()
		t.Fatal("incomplete version one accepted")
	} else if !strings.Contains(err.Error(), "incomplete SQLite schema") {
		t.Fatalf("unclear schema error: %v", err)
	}
}

func TestOptionalMembershipAttributionSurvivesRefresh(t *testing.T) {
	s, _ := openTest(t)
	c := candidate("a")
	c.Attribution = &source.Attribution{Author: "Known", Title: "Mountain"}
	if _, err := s.EnsureImage(t.Context(), "wallhaven", "search", "r1", "", c); err != nil {
		t.Fatal(err)
	}
	next := candidate("a")
	next.Ref = json.RawMessage(`{"new":true}`)
	next.Attribution = &source.Attribution{Title: "New title"}
	if _, err := s.EnsureImage(t.Context(), "wallhaven", "search", "r1", "", next); err != nil {
		t.Fatal(err)
	}
	found, err := s.Candidates(t.Context(), "wallhaven", "search", "r1", 10)
	if err != nil || len(found) != 1 {
		t.Fatal(err)
	}
	if found[0].Candidate.Attribution.Author != "Known" || found[0].Candidate.Attribution.Title != "New title" || string(found[0].Candidate.Ref) != `{"new":true}` {
		t.Fatalf("metadata or reference changed incorrectly: %+v", found[0].Candidate)
	}
	if next.Attribution.Author != "" {
		t.Fatal("catalog mutated caller-owned metadata")
	}
}

func TestBusyTimeoutAndCancellation(t *testing.T) {
	s, path := openTest(t)
	blocker, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	blocker.SetMaxOpenConns(1)
	if _, err = blocker.Exec("BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	defer blocker.Exec("ROLLBACK")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = s.StartScan(ctx, "local", "home", "r1", false)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("locked database operation succeeded")
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("busy cancellation exceeded bound: %s", elapsed)
	}
	if _, err = blocker.Exec("ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.db.QueryRowContext(t.Context(), "SELECT count(*) FROM discovery_progress").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("canceled operation changed discovery state")
	}
}

func TestCorruptDatabaseRejectedWithoutRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	original := []byte("This is deliberately corrupt SQLite content\x00\x01")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(t.Context(), path); err == nil {
		s.Close()
		t.Fatal("corrupt database accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatal("corrupt database was rewritten")
	}
}
