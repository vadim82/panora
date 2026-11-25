package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/vadim82/panora/internal/source"
)

// Ten entries fit the socket byte limit even with maximally escaped provider metadata.
const MaxExclusionsPageSize = 10

var ErrExclusionNotFound = errors.New("exclusion not found")

// ExclusionSet is the service's read-only projection of persisted marks. It is
// replaced after a committed mutation; selection does not query SQL per candidate.
type ExclusionSet struct {
	Images map[int64]bool
	Hashes map[string]bool
	Count  int
}

func (set ExclusionSet) Contains(imageID int64, hash string) bool {
	return set.Images[imageID] || hash != "" && set.Hashes[hash]
}

type Exclusion struct {
	ID                                   int64
	ProviderID, ProviderImageID, InputID string
	Candidate                            source.Candidate
	ExcludedAt                           time.Time
}

const exclusionSetQuery = "SELECT image_id,content_hash FROM image_exclusions"

func readExclusionSet(rows *sql.Rows, err error) (ExclusionSet, error) {
	set := ExclusionSet{Images: map[int64]bool{}, Hashes: map[string]bool{}}
	if err != nil {
		return set, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var hash sql.NullString
		if err := rows.Scan(&id, &hash); err != nil {
			return set, err
		}
		set.Images[id] = true
		if hash.Valid {
			set.Hashes[hash.String] = true
		}
		set.Count++
	}
	return set, rows.Err()
}

func (s *Store) LoadExclusions(ctx context.Context) (ExclusionSet, error) {
	return readExclusionSet(s.db.QueryContext(ctx, exclusionSetQuery))
}

// The replacement projection is read inside the transaction and returned only
// after commit, so a failed write/read cannot leave service memory ahead of SQL.
func commitExclusions(ctx context.Context, tx *sql.Tx) (ExclusionSet, error) {
	set, err := readExclusionSet(tx.QueryContext(ctx, exclusionSetQuery))
	if err != nil {
		return ExclusionSet{}, err
	}
	if err := tx.Commit(); err != nil {
		return ExclusionSet{}, err
	}
	return set, nil
}

func (s *Store) ExcludeImage(ctx context.Context, imageID int64) (ExclusionSet, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ExclusionSet{}, err
	}
	defer tx.Rollback()
	var hash sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT c.hash FROM images i LEFT JOIN image_content c ON c.image_id=i.id WHERE i.id=?", imageID).Scan(&hash); err != nil {
		return ExclusionSet{}, fmt.Errorf("lookup exclusion target: %w", err)
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO image_exclusions(image_id,content_hash,excluded_at_ms) VALUES(?,?,?) ON CONFLICT(image_id) DO NOTHING", imageID, hash, time.Now().UTC().UnixMilli())
	if err != nil {
		return ExclusionSet{}, err
	}
	return commitExclusions(ctx, tx)
}

// ID zero restores the latest still-active mark (Undo). Positive IDs are stable
// list handles; AUTOINCREMENT prevents an old handle from targeting a later mark.
func (s *Store) RestoreExclusion(ctx context.Context, id int64) (ExclusionSet, error) {
	if id < 0 {
		return ExclusionSet{}, ErrExclusionNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ExclusionSet{}, err
	}
	defer tx.Rollback()
	query := "DELETE FROM image_exclusions WHERE id=?"
	if id == 0 {
		query = "DELETE FROM image_exclusions WHERE id=(SELECT max(id) FROM image_exclusions)"
	}
	var result sql.Result
	if id == 0 {
		result, err = tx.ExecContext(ctx, query)
	} else {
		result, err = tx.ExecContext(ctx, query, id)
	}
	if err != nil {
		return ExclusionSet{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return ExclusionSet{}, err
	}
	if count == 0 {
		return ExclusionSet{}, ErrExclusionNotFound
	}
	return commitExclusions(ctx, tx)
}

// Newest-first keyset pagination stays bounded when marks are added/restored.
func (s *Store) ListExclusions(ctx context.Context, before int64, limit int) ([]Exclusion, int64, error) {
	if before < 0 || limit < 1 || limit > MaxExclusionsPageSize {
		return nil, 0, fmt.Errorf("exclusions require before >= 0 and limit between 1 and 10")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.id,i.provider_id,i.provider_image_id,
        CASE WHEN i.provider_id='local' THEN
            (SELECT m.input_id FROM input_images m WHERE m.image_id=i.id LIMIT 1)
            ELSE '' END,
        i.metadata_json,e.excluded_at_ms
        FROM image_exclusions e JOIN images i ON i.id=e.image_id
        WHERE (?=0 OR e.id<?) ORDER BY e.id DESC LIMIT ?`, before, before, limit+1)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	entries := []Exclusion{}
	for rows.Next() {
		var entry Exclusion
		var encoded string
		var stamp int64
		if err := rows.Scan(&entry.ID, &entry.ProviderID, &entry.ProviderImageID, &entry.InputID, &encoded, &stamp); err != nil {
			return nil, 0, err
		}
		if err := json.Unmarshal([]byte(encoded), &entry.Candidate); err != nil {
			return nil, 0, err
		}
		entry.ExcludedAt = time.UnixMilli(stamp).UTC()
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var next int64
	if len(entries) > limit {
		entries = entries[:limit]
		next = entries[len(entries)-1].ID
	}
	return entries, next, nil
}
