package catalog

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/vadim82/panora/internal/source"
)

// CachedFile describes validated, published image bytes. Origin and attribution
// remain in the image catalog even when these disposable files are evicted.
type CachedFile struct {
	Path, Hash    string
	Width, Height int
	Bytes         int64
}

func (s *Store) LookupCandidate(ctx context.Context, id Identity) (source.Candidate, error) {
	var encoded string
	err := s.db.QueryRowContext(ctx, `SELECT candidate_json FROM input_images WHERE image_id=? AND provider_id=? AND input_id=? AND settings_revision=?`, id.ImageID, id.ProviderID, id.InputID, id.Revision).Scan(&encoded)
	if err != nil {
		return source.Candidate{}, fmt.Errorf("lookup image origin: %w", err)
	}
	var candidate source.Candidate
	if err = json.Unmarshal([]byte(encoded), &candidate); err != nil {
		return candidate, err
	}
	if err = candidate.Validate(); err != nil {
		return candidate, err
	}
	return candidate, nil
}

// CachedImage tolerates files disappearing between requests. Reconciliation only
// reports a miss while retaining the image-to-content association for repeat
// avoidance. Provenance and history survive cache eviction.
func (s *Store) CachedImage(ctx context.Context, imageID int64) (CachedFile, bool, error) {
	file, present, err := s.KnownCachedImage(ctx, imageID)
	if err != nil || !present {
		return file, present, err
	}
	info, err := os.Lstat(file.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return CachedFile{}, false, fmt.Errorf("inspect cached image: %w", err)
	}
	if errors.Is(err, os.ErrNotExist) || !info.Mode().IsRegular() || info.Size() != file.Bytes {
		return CachedFile{}, false, nil
	}
	return file, true, nil
}

// KnownCachedImage returns retained validated metadata even after file eviction.
func (s *Store) KnownCachedImage(ctx context.Context, imageID int64) (CachedFile, bool, error) {
	var file CachedFile
	err := s.db.QueryRowContext(ctx, `SELECT c.hash,c.path,c.bytes,c.width,c.height FROM image_content m JOIN cached_content c ON c.hash=m.hash WHERE m.image_id=?`, imageID).Scan(&file.Hash, &file.Path, &file.Bytes, &file.Width, &file.Height)
	if errors.Is(err, sql.ErrNoRows) {
		return file, false, nil
	}
	if err != nil {
		return file, false, err
	}
	return file, true, nil
}

// SaveCachedImage follows file publication. If its transaction fails, the
// unreferenced published file remains safe for the file cache to reconcile.
func (s *Store) SaveCachedImage(ctx context.Context, imageID int64, file CachedFile) error {
	hash, err := hex.DecodeString(file.Hash)
	if err != nil || len(hash) != 32 || !filepath.IsAbs(file.Path) || file.Bytes <= 0 || file.Width <= 0 || file.Height <= 0 {
		return fmt.Errorf("invalid cached image metadata")
	}
	info, err := os.Lstat(file.Path)
	if err != nil {
		return fmt.Errorf("inspect published cache image: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() != file.Bytes {
		return fmt.Errorf("published cache image does not match recorded bytes")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO cached_content(hash,path,bytes,width,height,last_used_ms) VALUES(?,?,?,?,?,?) ON CONFLICT(hash) DO UPDATE SET path=excluded.path,bytes=excluded.bytes,width=excluded.width,height=excluded.height,last_used_ms=excluded.last_used_ms`, file.Hash, file.Path, file.Bytes, file.Width, file.Height, time.Now().UnixMilli())
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO image_content(image_id,hash) VALUES(?,?) ON CONFLICT(image_id) DO UPDATE SET hash=excluded.hash`, imageID, file.Hash)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ContentHash returns validated content identity independently of file presence.
func (s *Store) ContentHash(ctx context.Context, imageID int64) (string, error) {
	var hash string
	err := s.db.QueryRowContext(ctx, "SELECT hash FROM image_content WHERE image_id=?", imageID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return hash, err
}
