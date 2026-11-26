package catalog

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/vadim82/panora/internal/source"
)

type Scan struct {
	ProviderID, InputID, Revision, ScanID string
	Cursor                                json.RawMessage
	Complete                              bool
	epoch                                 int64
}
type Candidate struct {
	Identity          Identity
	Candidate         source.Candidate
	Hash              string
	DecodedDimensions source.Dimensions
}

func (s *Store) StartScan(ctx context.Context, provider, input, revision string, fresh bool) (Scan, error) {
	out := Scan{ProviderID: provider, InputID: input, Revision: revision, Cursor: json.RawMessage("null")}
	if provider == "" || input == "" || revision == "" {
		return out, fmt.Errorf("discovery identity is required")
	}
	var existing Scan
	existing.ProviderID = provider
	existing.InputID = input
	var cursor sql.NullString
	err := s.db.QueryRowContext(ctx, "SELECT settings_revision,scan_id,cursor_json,complete,scan_epoch FROM discovery_progress WHERE provider_id=? AND input_id=?", provider, input).Scan(&existing.Revision, &existing.ScanID, &cursor, &existing.Complete, &existing.epoch)
	if err == nil && existing.Revision == revision && !fresh && !existing.Complete {
		existing.Cursor = json.RawMessage("null")
		if cursor.Valid {
			existing.Cursor = json.RawMessage(cursor.String)
		}
		return existing, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		return out, err
	}
	out.ScanID = hex.EncodeToString(token[:])
	err = s.db.QueryRowContext(ctx, `INSERT INTO discovery_progress(provider_id,input_id,settings_revision,scan_id,cursor_json,complete,updated_at_ms,scan_epoch)
 VALUES(?,?,?,?,NULL,0,?,1) ON CONFLICT(provider_id,input_id) DO UPDATE SET
 complete_epoch=CASE WHEN settings_revision=excluded.settings_revision THEN complete_epoch ELSE 0 END,
 settings_revision=excluded.settings_revision,scan_id=excluded.scan_id,cursor_json=NULL,complete=0,
 updated_at_ms=excluded.updated_at_ms,scan_epoch=scan_epoch+1 RETURNING scan_epoch`,
		provider, input, revision, out.ScanID, time.Now().UnixMilli()).Scan(&out.epoch)
	return out, err
}

func cursorValue(cursor json.RawMessage) any {
	if len(cursor) == 0 || source.IsNull(cursor) {
		return nil
	}
	return string(cursor)
}

func (s *Store) CommitPage(ctx context.Context, scan Scan, scope string, page source.Page) ([]Identity, error) {
	if page.Candidates == nil || len(page.Candidates) > source.MaxPageSize || !validJSON(page.NextCursor) || len(page.NextCursor) > source.MaxCursorBytes {
		return nil, fmt.Errorf("invalid discovery page")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	complete := source.IsNull(page.NextCursor)
	result, err := tx.ExecContext(ctx, `UPDATE discovery_progress SET cursor_json=?,complete=?,updated_at_ms=?,
 complete_epoch=CASE WHEN ? THEN scan_epoch ELSE complete_epoch END
 WHERE provider_id=? AND input_id=? AND settings_revision=? AND scan_id=? AND cursor_json IS ? AND complete=0`,
		cursorValue(page.NextCursor), complete, time.Now().UnixMilli(), complete, scan.ProviderID, scan.InputID, scan.Revision, scan.ScanID, cursorValue(scan.Cursor))
	if err != nil {
		return nil, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, ErrStaleScan
	}
	ids := make([]Identity, 0, len(page.Candidates))
	seen := map[string]bool{}
	for _, c := range page.Candidates {
		if seen[c.ID] {
			return nil, fmt.Errorf("repeated candidate in page")
		}
		seen[c.ID] = true
		id, err := ensureImage(ctx, tx, scan.ProviderID, scan.InputID, scan.Revision, scope, c, scan.epoch)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *Store) EnsureImage(ctx context.Context, provider, input, revision, scope string, c source.Candidate) (Identity, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Identity{}, err
	}
	defer tx.Rollback()
	id, err := ensureImage(ctx, tx, provider, input, revision, scope, c, 0)
	if err != nil {
		return Identity{}, err
	}
	if err = tx.Commit(); err != nil {
		return Identity{}, err
	}
	return id, nil
}

func ensureImage(ctx context.Context, tx *sql.Tx, provider, input, revision, scope string, c source.Candidate, epoch int64) (Identity, error) {
	id := Identity{ProviderID: provider, InputID: input, Revision: revision}
	if provider == "" || input == "" || revision == "" {
		return id, fmt.Errorf("image provenance is required")
	}
	if err := c.Validate(); err != nil {
		return id, err
	}
	metadata, err := json.Marshal(struct {
		Dimensions  *source.Dimensions  `json:"dimensions,omitempty"`
		Attribution *source.Attribution `json:"attribution,omitempty"`
	}{c.Dimensions, c.Attribution})
	if err != nil {
		return id, err
	}
	// Retain optional attribution for this membership without merging the opaque
	// provider reference, whose object fields may have changed meaning.
	var priorJSON string
	priorErr := tx.QueryRowContext(ctx, `SELECT candidate_json FROM input_images m JOIN images i ON i.id=m.image_id WHERE m.provider_id=? AND m.input_id=? AND m.settings_revision=? AND i.identity_scope=? AND i.provider_image_id=?`, provider, input, revision, scope, c.ID).Scan(&priorJSON)
	if priorErr == nil {
		var prior source.Candidate
		if err := json.Unmarshal([]byte(priorJSON), &prior); err != nil {
			return id, err
		}
		if c.Dimensions == nil && provider != "local" {
			c.Dimensions = prior.Dimensions
		}
		if c.Attribution == nil {
			c.Attribution = prior.Attribution
		} else if prior.Attribution != nil {
			freshCopy := *c.Attribution
			c.Attribution = &freshCopy
			fresh, old := c.Attribution, prior.Attribution
			for _, pair := range []struct {
				dst *string
				old string
			}{{&fresh.SourceURL, old.SourceURL}, {&fresh.Title, old.Title}, {&fresh.PageURL, old.PageURL}, {&fresh.Author, old.Author}, {&fresh.AuthorURL, old.AuthorURL}, {&fresh.LicenseURL, old.LicenseURL}, {&fresh.LicenseName, old.LicenseName}} {
				if *pair.dst == "" {
					*pair.dst = pair.old
				}
			}
		}
	} else if !errors.Is(priorErr, sql.ErrNoRows) {
		return id, priorErr
	}
	candidate, err := json.Marshal(c)
	if err != nil {
		return id, err
	}
	now := time.Now().UnixMilli()
	// json_patch retains absent metadata fields and stable integer identities.
	err = tx.QueryRowContext(ctx, `INSERT INTO images(provider_id,identity_scope,provider_image_id,metadata_json,first_seen_ms,last_seen_ms) VALUES(?,?,?,?,?,?) ON CONFLICT(provider_id,identity_scope,provider_image_id) DO UPDATE SET metadata_json=json_patch(images.metadata_json,excluded.metadata_json),last_seen_ms=excluded.last_seen_ms RETURNING id`, provider, scope, c.ID, string(metadata), now, now).Scan(&id.ImageID)
	if err != nil {
		return id, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO input_images(provider_id,input_id,settings_revision,image_id,candidate_json,first_seen_ms,last_seen_ms,seen_epoch)
 VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(provider_id,input_id,settings_revision,image_id) DO UPDATE SET
 candidate_json=excluded.candidate_json,last_seen_ms=excluded.last_seen_ms,seen_epoch=max(seen_epoch,excluded.seen_epoch)`,
		provider, input, revision, id.ImageID, string(candidate), now, now, epoch)
	return id, err
}

var ErrCandidateLimit = errors.New("catalog candidate limit exceeded")

// Candidates rejects an oversized input instead of returning a biased prefix.
func (s *Store) Candidates(ctx context.Context, provider, input, revision string, limit int) ([]Candidate, error) {
	if limit < 0 {
		return nil, ErrCandidateLimit
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.image_id,m.candidate_json,COALESCE(x.hash,''),COALESCE(c.width,0),COALESCE(c.height,0) FROM input_images m
 LEFT JOIN image_content x ON x.image_id=m.image_id
 LEFT JOIN cached_content c ON c.hash=x.hash
 LEFT JOIN discovery_progress d ON d.provider_id=m.provider_id AND d.input_id=m.input_id AND d.settings_revision=m.settings_revision
 WHERE m.provider_id=? AND m.input_id=? AND m.settings_revision=?
 AND (m.provider_id<>'local' OR m.seen_epoch>=COALESCE(d.complete_epoch,0))
 ORDER BY m.image_id LIMIT ?`, provider, input, revision, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Candidate{}
	for rows.Next() {
		if len(out) == limit {
			return nil, ErrCandidateLimit
		}
		c := Candidate{Identity: Identity{ProviderID: provider, InputID: input, Revision: revision}}
		var encoded string
		if err = rows.Scan(&c.Identity.ImageID, &encoded, &c.Hash, &c.DecodedDimensions.Width, &c.DecodedDimensions.Height); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(encoded), &c.Candidate); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
