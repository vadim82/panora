package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

const reclaimBatchSize = 256

// ReclaimCursor is a process-local position in the catalog's maintenance sweep.
// A completed pass starts from the beginning to catch rows changed meanwhile.
type ReclaimCursor struct{ memberships, progress, retries int64 }

const obsoleteCatalogRows = `WITH configured AS MATERIALIZED (
 SELECT json_extract(value,'$.ProviderID') AS provider, json_extract(value,'$.InputID') AS input,
        json_extract(value,'$.Revision') AS revision FROM json_each(?)
)
DELETE FROM %s WHERE rowid IN (
 SELECT m.rowid FROM %s AS m WHERE m.rowid>? AND m.rowid<=? AND %s<?
 AND (NOT EXISTS (SELECT 1 FROM configured c WHERE c.provider=m.provider_id AND c.input=m.input_id
   AND (c.revision='' OR c.revision=m.settings_revision)) %s)
 %s
)`

const staleLocalMemberships = `OR (m.provider_id='local' AND EXISTS (
 SELECT 1 FROM discovery_progress d WHERE d.provider_id=m.provider_id AND d.input_id=m.input_id
   AND d.settings_revision=m.settings_revision AND d.complete_epoch>m.seen_epoch))`

const retainedMemberships = `
 AND NOT EXISTS (SELECT 1 FROM playback_history h WHERE
   (h.provider_id,h.input_id,h.settings_revision,h.image_id) = (m.provider_id,m.input_id,m.settings_revision,m.image_id))
 AND NOT EXISTS (SELECT 1 FROM pending_apply p WHERE
   (p.provider_id,p.input_id,p.settings_revision,p.image_id) = (m.provider_id,m.input_id,m.settings_revision,m.image_id))
 AND (NOT EXISTS (SELECT 1 FROM image_exclusions e WHERE e.image_id=m.image_id)
   OR m.rowid > (SELECT MIN(p.rowid) FROM input_images p WHERE p.image_id=m.image_id))`

// ReclaimObsolete sweeps stale local and retired revision memberships,
// retired checkpoints and expired retry
// records, without recycling image IDs or touching user files or playback bytes.
// An empty revision protects all revisions of a configured input whose current
// generation is unavailable. Nonempty revisions protect only exact matches.
func (s *Store) ReclaimObsolete(ctx context.Context, inputs []InputRevision, before time.Time, cursor ReclaimCursor) (ReclaimCursor, []RetryKey, error) {
	for _, input := range inputs {
		if input.ProviderID == "" || input.InputID == "" {
			return cursor, nil, fmt.Errorf("catalog maintenance requires provider and input IDs")
		}
	}
	encoded, err := json.Marshal(inputs)
	if err != nil {
		return cursor, nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return cursor, nil, err
	}
	defer tx.Rollback()
	var next ReclaimCursor
	var retired []RetryKey
	for _, sweep := range []struct {
		table, ageColumn, stale, extra string
		position                       int64
		next                           *int64
	}{
		{"input_images", "last_seen_ms", staleLocalMemberships, retainedMemberships, cursor.memberships, &next.memberships},
		// Any surviving local membership can carry a scan epoch. Retain the
		// counter so a later scan cannot make higher old epochs look current.
		{"discovery_progress", "updated_at_ms", "", `AND (m.provider_id<>'local' OR NOT EXISTS (
 SELECT 1 FROM input_images i WHERE i.provider_id=m.provider_id AND i.input_id=m.input_id))`, cursor.progress, &next.progress},
		{"provider_retries", "next_at_ms", "", "AND m.scope IN ('discovery','materialization')", cursor.retries, &next.retries},
	} {
		end, count, err := reclaimBatchEnd(ctx, tx, sweep.table, sweep.position)
		if err != nil {
			return cursor, nil, err
		}
		if count == 0 {
			continue
		}
		if count == reclaimBatchSize {
			*sweep.next = end
		}
		query := fmt.Sprintf(obsoleteCatalogRows, sweep.table, sweep.table, sweep.ageColumn, sweep.stale, sweep.extra)
		args := []any{string(encoded), sweep.position, end, before.UnixMilli()}
		if sweep.table == "provider_retries" {
			retired, err = reclaimRetries(ctx, tx, query, args)
		} else {
			_, err = tx.ExecContext(ctx, query, args...)
		}
		if err != nil {
			return cursor, nil, fmt.Errorf("reclaim %s: %w", sweep.table, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return cursor, nil, err
	}
	return next, retired, nil
}

func reclaimBatchEnd(ctx context.Context, tx *sql.Tx, table string, start int64) (end int64, count int, err error) {
	query := `SELECT COALESCE(MAX(rowid),0),COUNT(*) FROM (SELECT rowid FROM ` + table + ` WHERE rowid>? ORDER BY rowid LIMIT ?)`
	err = tx.QueryRowContext(ctx, query, start, reclaimBatchSize).Scan(&end, &count)
	return end, count, err
}

func reclaimRetries(ctx context.Context, tx *sql.Tx, query string, args []any) ([]RetryKey, error) {
	rows, err := tx.QueryContext(ctx, query+` RETURNING provider_id,input_id,settings_revision,scope,image_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var retired []RetryKey
	for rows.Next() {
		var key RetryKey
		if err := rows.Scan(&key.ProviderID, &key.InputID, &key.Revision, &key.Scope, &key.ImageID); err != nil {
			return nil, err
		}
		retired = append(retired, key)
	}
	return retired, rows.Err()
}
