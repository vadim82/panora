package catalog

import "context"

// ImageExists accepts only the stable catalog ID; clients cannot turn an image
// preview request into a path lookup or a provider download.
func (s *Store) ImageExists(ctx context.Context, imageID int64) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM images WHERE id=?)", imageID).Scan(&exists)
	return exists, err
}
