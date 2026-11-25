package catalog

import (
	"context"
	"fmt"
)

// InputRevisionUpgrade identifies an unchanged input in a provider-wide catalog.
type InputRevisionUpgrade struct {
	InputRevision
	LegacyRevision string
}

// UpgradeInputRevisions rekeys memberships, including their playback references,
// and scoped retry state for inputs that exactly match the saved legacy digest.
// Provider cursors use a configuration fingerprint containing that digest and
// must restart with the new input revision. A transaction makes retries safe.
func (s *Store) UpgradeInputRevisions(ctx context.Context, inputs []InputRevisionUpgrade) error {
	if len(inputs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, input := range inputs {
		for _, query := range []string{
			`INSERT INTO input_images(provider_id,input_id,settings_revision,image_id,candidate_json,first_seen_ms,last_seen_ms)
			  SELECT provider_id,input_id,?1,image_id,candidate_json,first_seen_ms,last_seen_ms FROM input_images
			  WHERE provider_id=?2 AND input_id=?3 AND settings_revision=?4 ON CONFLICT DO NOTHING`,
			`UPDATE playback_history SET settings_revision=?1 WHERE provider_id=?2 AND input_id=?3 AND settings_revision=?4`,
			`UPDATE pending_apply SET settings_revision=?1 WHERE provider_id=?2 AND input_id=?3 AND settings_revision=?4`,
			`UPDATE discovery_progress SET settings_revision=?1, cursor_json=NULL, complete=0,
			  scan_epoch=1,complete_epoch=0 WHERE provider_id=?2 AND input_id=?3 AND settings_revision=?4`,
			`INSERT INTO provider_retries(provider_id,input_id,settings_revision,scope,image_id,attempts,next_at_ms,failure_stage,failure_code)
			  SELECT provider_id,input_id,?1,scope,image_id,attempts,next_at_ms,failure_stage,failure_code FROM provider_retries
			  WHERE provider_id=?2 AND input_id=?3 AND settings_revision=?4
			  ON CONFLICT DO UPDATE SET attempts=max(provider_retries.attempts,excluded.attempts), next_at_ms=excluded.next_at_ms,
			  failure_stage=excluded.failure_stage, failure_code=excluded.failure_code
			  WHERE excluded.next_at_ms>provider_retries.next_at_ms`,
			`DELETE FROM provider_retries WHERE ?1<>settings_revision AND provider_id=?2 AND input_id=?3 AND settings_revision=?4`,
			`DELETE FROM input_images WHERE ?1<>settings_revision AND provider_id=?2 AND input_id=?3 AND settings_revision=?4`,
		} {
			if _, err := tx.ExecContext(ctx, query, input.Revision, input.ProviderID, input.InputID, input.LegacyRevision); err != nil {
				return fmt.Errorf("upgrade catalog input revisions: %w", err)
			}
		}
	}
	return tx.Commit()
}
