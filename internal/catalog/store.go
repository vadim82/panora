// Package catalog owns SQLite persistence for image discovery and playback.
package catalog

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

//go:embed cache_schema.sql
var cacheSchema string

//go:embed retry_schema.sql
var retrySchema string

//go:embed exclusion_schema.sql
var exclusionSchema string

//go:embed history_schema.sql
var historySchema string

//go:embed local_scan_schema.sql
var localScanSchema string

var ErrStaleScan = errors.New("discovery scan has changed")

type Store struct{ db *sql.DB }
type Identity struct {
	ImageID                       int64
	ProviderID, InputID, Revision string
}
type Record struct {
	Reference  string
	Path       string
	AcceptedAt time.Time
	Identity   Identity
}
type Intent struct {
	Path         string
	StartedAt    time.Time
	Identity     Identity
	HistoryIndex int
}
type State struct {
	History      []Record
	Cursor       int
	Paused       bool
	LastActionAt time.Time
	Pending      *Intent
}

func Open(ctx context.Context, path string) (*Store, error) {
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return nil, fmt.Errorf("legacy JSON playback state is unsupported; select a fresh SQLite state path")
	}
	f, readErr := os.Open(path)
	if readErr == nil {
		var header [32]byte
		n, _ := io.ReadFull(f, header[:])
		f.Close()
		trimmed := strings.TrimSpace(string(header[:n]))
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			return nil, fmt.Errorf("legacy JSON playback state is unsupported; select a fresh SQLite state path")
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, fmt.Errorf("read catalog: %w", readErr)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: absolute}
	// These connection-local settings are reapplied if database/sql replaces a
	// connection after cancellation. journal_mode is set only after version checks.
	query := url.Values{}
	for _, pragma := range []string{"foreign_keys(1)", "busy_timeout(1000)", "synchronous(2)"} {
		query.Add("_pragma", pragma)
	}
	u.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	fail := func(err error) (*Store, error) { db.Close(); return nil, fmt.Errorf("open catalog: %w", err) }
	var version int
	if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fail(err)
	}
	if version < 0 || version > 6 {
		return fail(fmt.Errorf("unsupported SQLite schema version %d", version))
	}
	if version == 0 {
		var count int
		if err = db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE name NOT LIKE 'sqlite_%'").Scan(&count); err != nil {
			return fail(err)
		}
		if count != 0 {
			return fail(fmt.Errorf("unrecognized SQLite schema version 0"))
		}
	}
	verifySchema := []string{
		`SELECT i.id,i.provider_id,i.identity_scope,i.provider_image_id,i.metadata_json,i.first_seen_ms,i.last_seen_ms,
   d.provider_id,d.input_id,d.settings_revision,d.scan_id,d.cursor_json,d.complete,d.updated_at_ms,
   m.provider_id,m.input_id,m.settings_revision,m.image_id,m.candidate_json,m.first_seen_ms,m.last_seen_ms,
   h.id,h.image_id,h.provider_id,h.input_id,h.settings_revision,h.path,h.accepted_at_ms,
   p.singleton,p.paused,p.history_id,p.last_action_ms,
   a.singleton,a.image_id,a.provider_id,a.input_id,a.settings_revision,a.path,a.history_id,a.started_at_ms
   FROM images i,discovery_progress d,input_images m,playback_history h,playback_state p,pending_apply a LIMIT 0`,
		"SELECT c.hash,c.path,c.bytes,c.width,c.height,c.last_used_ms,m.image_id,m.hash FROM cached_content c,image_content m LIMIT 0",
		"SELECT provider_id,input_id,settings_revision,scope,image_id,attempts,next_at_ms,failure_stage,failure_code FROM provider_retries LIMIT 0",
		"SELECT id,image_id,content_hash,excluded_at_ms FROM image_exclusions LIMIT 0",
		"SELECT public_ref FROM playback_history LIMIT 0",
		"SELECT m.seen_epoch,d.scan_epoch,d.complete_epoch FROM input_images m,discovery_progress d LIMIT 0",
	}
	for i, query := range verifySchema[:version] {
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return fail(fmt.Errorf("incomplete SQLite schema version %d: %w", i+1, err))
		}
		rows.Close()
	}
	// Validate existing data before any schema upgrade can commit.
	if version > 0 {
		if err := checkIntegrity(ctx, db); err != nil {
			return fail(err)
		}
	}
	for _, p := range []struct {
		set, read string
		expected  any
	}{
		{"PRAGMA foreign_keys=ON", "PRAGMA foreign_keys", 1},
		{"PRAGMA busy_timeout=1000", "PRAGMA busy_timeout", 1000},
		{"PRAGMA journal_mode=DELETE", "PRAGMA journal_mode", "delete"},
		{"PRAGMA synchronous=FULL", "PRAGMA synchronous", 2},
	} {
		if _, err = db.ExecContext(ctx, p.set); err != nil {
			return fail(err)
		}
		switch expected := p.expected.(type) {
		case int:
			var got int
			err = db.QueryRowContext(ctx, p.read).Scan(&got)
			if err == nil && got != expected {
				err = fmt.Errorf("could not set %s", p.read)
			}
		case string:
			var got string
			err = db.QueryRowContext(ctx, p.read).Scan(&got)
			if err == nil && got != expected {
				err = fmt.Errorf("could not set %s", p.read)
			}
		}
		if err != nil {
			return fail(err)
		}
	}
	var valid int
	if err = db.QueryRowContext(ctx, `SELECT json_valid('{}')`).Scan(&valid); err != nil || valid != 1 {
		if err == nil {
			err = fmt.Errorf("SQLite JSON support unavailable")
		}
		return fail(err)
	}
	if version == 0 {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fail(err)
		}
		if _, err = tx.ExecContext(ctx, schema); err != nil {
			tx.Rollback()
			return fail(err)
		}
		if err = tx.Commit(); err != nil {
			return fail(err)
		}
	}
	upgrades := []string{cacheSchema, retrySchema, exclusionSchema, historySchema, localScanSchema}
	from := max(version, 1)
	if from-1 < len(upgrades) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fail(err)
		}
		for i, script := range upgrades[from-1:] {
			if _, err := tx.ExecContext(ctx, script); err != nil {
				tx.Rollback()
				return fail(fmt.Errorf("upgrade SQLite schema to version %d: %w", from+i+1, err))
			}
		}
		if err := tx.Commit(); err != nil {
			return fail(err)
		}
	}

	if version == 0 {
		if err := checkIntegrity(ctx, db); err != nil {
			return fail(err)
		}
	}
	if err = os.Chmod(path, 0600); err != nil {
		return fail(err)
	}
	return &Store{db: db}, nil
}
func checkIntegrity(ctx context.Context, db *sql.DB) error {
	var check string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&check); err != nil {
		return err
	}
	if check != "ok" {
		return fmt.Errorf("SQLite integrity check failed")
	}
	foreignRows, foreignErr := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if foreignErr != nil {
		return foreignErr
	}
	invalidForeignKeys := foreignRows.Next()
	foreignErr = foreignRows.Err()
	foreignRows.Close()
	if foreignErr != nil {
		return foreignErr
	}
	if invalidForeignKeys {
		return fmt.Errorf("SQLite foreign-key integrity check failed")
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().UnixMilli()
}
func fromMillis(n sql.NullInt64) time.Time {
	if !n.Valid {
		return time.Time{}
	}
	return time.UnixMilli(n.Int64).UTC()
}

func (s *Store) LoadState(ctx context.Context) (State, error) {
	state := State{Cursor: -1}
	var current, last sql.NullInt64
	if err := s.db.QueryRowContext(ctx, "SELECT paused,history_id,last_action_ms FROM playback_state WHERE singleton=1").Scan(&state.Paused, &current, &last); err != nil {
		return state, fmt.Errorf("load playback state: %w", err)
	}
	state.LastActionAt = fromMillis(last)
	rows, err := s.db.QueryContext(ctx, "SELECT id,public_ref,image_id,provider_id,input_id,settings_revision,path,accepted_at_ms FROM playback_history ORDER BY id")
	if err != nil {
		return state, err
	}
	ids := map[int64]int{}
	seenReferences := map[string]bool{}
	for rows.Next() {
		if len(state.History) >= 100 {
			rows.Close()
			return state, fmt.Errorf("playback history exceeds 100 entries")
		}
		var r Record
		var id, accepted int64
		if err = rows.Scan(&id, &r.Reference, &r.Identity.ImageID, &r.Identity.ProviderID, &r.Identity.InputID, &r.Identity.Revision, &r.Path, &accepted); err != nil {
			rows.Close()
			return state, err
		}
		if !ValidHistoryReference(r.Reference) || seenReferences[r.Reference] {
			rows.Close()
			return state, fmt.Errorf("invalid history reference")
		}
		seenReferences[r.Reference] = true
		r.AcceptedAt = time.UnixMilli(accepted).UTC()
		ids[id] = len(state.History)
		if current.Valid && id == current.Int64 {
			state.Cursor = len(state.History)
		}
		state.History = append(state.History, r)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return state, err
	}
	rows.Close()
	var in Intent
	var started int64
	var history sql.NullInt64
	err = s.db.QueryRowContext(ctx, "SELECT image_id,provider_id,input_id,settings_revision,path,history_id,started_at_ms FROM pending_apply WHERE singleton=1").Scan(&in.Identity.ImageID, &in.Identity.ProviderID, &in.Identity.InputID, &in.Identity.Revision, &in.Path, &history, &started)
	if err == nil {
		in.StartedAt = time.UnixMilli(started).UTC()
		in.HistoryIndex = -1
		if history.Valid {
			in.HistoryIndex = ids[history.Int64]
		}
		state.Pending = &in
	} else if !errors.Is(err, sql.ErrNoRows) {
		return state, err
	}
	return state, nil
}

// SaveState publishes history, navigation, pause and intent as one transaction.
// Image identities and memberships must have committed before this call.
func (s *Store) SaveState(ctx context.Context, state State) error {
	if len(state.History) > 100 || state.Cursor < -1 || state.Cursor >= len(state.History) || len(state.History) > 0 && state.Cursor < 0 {
		return fmt.Errorf("invalid playback history or cursor")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM pending_apply"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE playback_state SET history_id=NULL WHERE singleton=1"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM playback_history"); err != nil {
		return err
	}
	seen := map[string]bool{}
	for index, r := range state.History {
		if !ValidHistoryReference(r.Reference) || seen[r.Reference] || !filepath.IsAbs(r.Path) || r.AcceptedAt.IsZero() || r.Identity.ImageID <= 0 {
			return fmt.Errorf("invalid playback entry")
		}
		seen[r.Reference] = true
		_, err = tx.ExecContext(ctx, "INSERT INTO playback_history(id,public_ref,image_id,provider_id,input_id,settings_revision,path,accepted_at_ms) VALUES(?,?,?,?,?,?,?,?)", index+1, r.Reference, r.Identity.ImageID, r.Identity.ProviderID, r.Identity.InputID, r.Identity.Revision, r.Path, r.AcceptedAt.UnixMilli())
		if err != nil {
			return err
		}
	}
	var current any
	if state.Cursor >= 0 {
		current = state.Cursor + 1
	}
	if _, err = tx.ExecContext(ctx, "UPDATE playback_state SET paused=?,history_id=?,last_action_ms=? WHERE singleton=1", state.Paused, current, nullableTime(state.LastActionAt)); err != nil {
		return err
	}
	if in := state.Pending; in != nil {
		if !filepath.IsAbs(in.Path) || in.StartedAt.IsZero() || in.Identity.ImageID <= 0 {
			return fmt.Errorf("invalid pending apply")
		}
		var history any
		if in.HistoryIndex >= 0 && in.HistoryIndex < len(state.History) {
			history = in.HistoryIndex + 1
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO pending_apply(singleton,image_id,provider_id,input_id,settings_revision,path,history_id,started_at_ms) VALUES(1,?,?,?,?,?,?,?)", in.Identity.ImageID, in.Identity.ProviderID, in.Identity.InputID, in.Identity.Revision, in.Path, history, in.StartedAt.UnixMilli())
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func validJSON(v json.RawMessage) bool { return len(v) > 0 && json.Valid(v) }

// ValidHistoryReference accepts opaque URL-safe handles, never array positions.
func ValidHistoryReference(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
