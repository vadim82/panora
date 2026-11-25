package catalog

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestRetryMigrationAndDurableScopes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(schema + cacheSchema); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	next := time.Now().UTC().Truncate(time.Millisecond).Add(time.Minute)
	for _, key := range []RetryKey{{ProviderID: "wallhaven", Scope: "provider_metadata"}, {ProviderID: "wallhaven", InputID: "nature", Revision: "r1", Scope: "discovery"}, {ProviderID: "wallhaven", InputID: "nature", Revision: "r1", Scope: "materialization", ImageID: 7}} {
		if err := s.SaveRetry(t.Context(), RetryState{RetryKey: key, Attempts: 2, NextAt: next, Stage: "resolve", Code: "rate_limited"}); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	states, err := s.LoadRetries(t.Context())
	if err != nil || len(states) != 3 {
		t.Fatalf("states=%+v err=%v", states, err)
	}
	for _, state := range states {
		if state.Attempts != 2 || !state.NextAt.Equal(next) {
			t.Fatalf("retry changed on restart: %+v", state)
		}
	}
	if err := s.DeleteRetry(t.Context(), states[0].RetryKey); err != nil {
		t.Fatal(err)
	}
	states, err = s.LoadRetries(t.Context())
	if err != nil || len(states) != 2 {
		t.Fatal("deleting one scope affected others")
	}
}

func TestRetryMigrationRollbackPreservesPriorSchema(t *testing.T) {
	for _, version := range []int{1, 2} {
		path := filepath.Join(t.TempDir(), "state.db")
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		ddl := schema
		if version == 2 {
			ddl += cacheSchema
		}
		if _, err = db.Exec(ddl + "CREATE TABLE provider_retries(keep TEXT);INSERT INTO provider_retries VALUES('keep')"); err != nil {
			t.Fatal(err)
		}
		db.Close()
		if store, err := Open(t.Context(), path); err == nil {
			store.Close()
			t.Fatal("conflicting retry schema accepted")
		}
		db, err = sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		var got int
		var value string
		if err = db.QueryRow("PRAGMA user_version").Scan(&got); err != nil || got != version {
			t.Fatalf("partial migration changed schema version: %d %v", got, err)
		}
		if err = db.QueryRow("SELECT keep FROM provider_retries").Scan(&value); err != nil || value != "keep" {
			t.Fatal("failed migration lost prior data")
		}
		if version == 1 {
			if err = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='cached_content'").Scan(&got); err != nil || got != 0 {
				t.Fatal("v2 migration escaped v3 rollback")
			}
		}
		db.Close()
	}
}

func TestRelatedRetryLimitsCommitAtomically(t *testing.T) {
	s, _ := openTest(t)
	key := RetryKey{ProviderID: "wallhaven", InputID: "nature", Revision: "r1", Scope: "discovery"}
	state := RetryState{RetryKey: key, Attempts: 1, NextAt: time.Now().Add(time.Minute), Stage: "discovery", Code: "rate_limited"}
	invalid := state
	invalid.RetryKey = RetryKey{ProviderID: "wallhaven", Scope: "provider_metadata"}
	invalid.Attempts = 17
	if err := s.SaveRetries(t.Context(), []RetryState{state, invalid}); err == nil {
		t.Fatal("invalid related limit accepted")
	}
	if states, err := s.LoadRetries(t.Context()); err != nil || len(states) != 0 {
		t.Fatalf("partial retry limits committed: %+v %v", states, err)
	}
}
