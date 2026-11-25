package catalog

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

func TestExclusionPersistencePaginationAndRestoreHandles(t *testing.T) {
	s, path := openTest(t)
	for i := 0; i < 13; i++ {
		id, err := s.EnsureImage(t.Context(), "local", "home", "r1", "home", candidate(fmt.Sprint(i)))
		if err != nil {
			t.Fatal(err)
		}
		set, err := s.ExcludeImage(t.Context(), id.ImageID)
		if err != nil || set.Count != i+1 || !set.Contains(id.ImageID, "") {
			t.Fatalf("exclude: %+v %v", set, err)
		}
		if _, err := s.ExcludeImage(t.Context(), id.ImageID); err != nil {
			t.Fatal(err)
		}
	}
	entries, next, err := s.ListExclusions(t.Context(), 0, 10)
	if err != nil || len(entries) != 10 || next != entries[9].ID || entries[0].InputID != "home" {
		t.Fatalf("page: %+v %d %v", entries, next, err)
	}
	older, more, err := s.ListExclusions(t.Context(), next, 10)
	if err != nil || len(older) != 3 || more != 0 || older[0].ID >= next {
		t.Fatalf("next page: %+v %d %v", older, more, err)
	}
	removed := entries[0].ID
	if set, err := s.RestoreExclusion(t.Context(), removed); err != nil || set.Count != 12 {
		t.Fatal("restore did not remove one mark")
	}
	id, err := s.EnsureImage(t.Context(), "local", "home", "r1", "home", candidate("new"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExcludeImage(t.Context(), id.ImageID); err != nil {
		t.Fatal(err)
	}
	entries, _, _ = s.ListExclusions(t.Context(), 0, 10)
	if entries[0].ID <= removed {
		t.Fatal("restored ID was reused")
	}
	if _, err := s.RestoreExclusion(t.Context(), removed); !errors.Is(err, ErrExclusionNotFound) {
		t.Fatal("old handle restored a newer mark")
	}
	if _, err := s.RestoreExclusion(t.Context(), 0); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if set, err := s.LoadExclusions(t.Context()); err != nil || set.Count != 12 {
		t.Fatalf("restart: %+v %v", set, err)
	}
	for _, limit := range []int{0, 11} {
		if _, _, err := s.ListExclusions(t.Context(), 0, limit); err == nil {
			t.Fatal("unbounded list accepted")
		}
	}
}

func TestExclusionMigrationRollbackAndWriteFailure(t *testing.T) {
	for _, version := range []int{1, 2, 3} {
		path := filepath.Join(t.TempDir(), "state.db")
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		ddl := schema
		if version >= 2 {
			ddl += cacheSchema
		}
		if version >= 3 {
			ddl += retrySchema
		}
		if _, err = db.Exec(ddl + "CREATE TABLE image_exclusions(keep TEXT);INSERT INTO image_exclusions VALUES('keep')"); err != nil {
			t.Fatal(err)
		}
		db.Close()
		if store, err := Open(t.Context(), path); err == nil {
			store.Close()
			t.Fatal("conflicting exclusion schema accepted")
		}
		db, err = sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		var got int
		var value string
		if err = db.QueryRow("PRAGMA user_version").Scan(&got); err != nil || got != version {
			t.Fatalf("partial upgrade: %d %v", got, err)
		}
		if err = db.QueryRow("SELECT keep FROM image_exclusions").Scan(&value); err != nil || value != "keep" {
			t.Fatal("upgrade lost existing data")
		}
		if version == 1 {
			if err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='cached_content'").Scan(&got); err != nil || got != 0 {
				t.Fatal("earlier migration escaped rollback")
			}
		}
		db.Close()
	}
	s, _ := openTest(t)
	id, err := s.EnsureImage(t.Context(), "local", "home", "r1", "home", candidate("target"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("CREATE TRIGGER reject_exclusion BEFORE INSERT ON image_exclusions BEGIN SELECT RAISE(ABORT,'fixture'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExcludeImage(t.Context(), id.ImageID); err == nil {
		t.Fatal("failed write reported success")
	}
	if set, err := s.LoadExclusions(t.Context()); err != nil || set.Count != 0 {
		t.Fatal("failed write left an exclusion")
	}
}

func TestFailedRestoreKeepsExclusion(t *testing.T) {
	s, _ := openTest(t)
	id, err := s.EnsureImage(t.Context(), "local", "home", "r1", "home", candidate("target"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExcludeImage(t.Context(), id.ImageID); err != nil {
		t.Fatal(err)
	}
	entries, _, err := s.ListExclusions(t.Context(), 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("CREATE TRIGGER reject_restore BEFORE DELETE ON image_exclusions BEGIN SELECT RAISE(ABORT,'fixture'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RestoreExclusion(t.Context(), entries[0].ID); err == nil {
		t.Fatal("failed restore reported success")
	}
	if set, err := s.LoadExclusions(t.Context()); err != nil || !set.Contains(id.ImageID, "") {
		t.Fatal("failed restore removed exclusion")
	}
}
