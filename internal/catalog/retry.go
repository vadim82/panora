package catalog

import (
	"context"
	"fmt"
	"time"
)

// Provider-wide keys use empty input/revision and image zero. Image-specific
// materialization keys never interfere with discovery or other candidate bytes.
type RetryKey struct {
	ProviderID, InputID, Revision, Scope string
	ImageID                              int64
}

type RetryState struct {
	RetryKey
	Attempts    int
	NextAt      time.Time
	Stage, Code string
}

func (s *Store) LoadRetries(ctx context.Context) ([]RetryState, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT provider_id,input_id,settings_revision,scope,image_id,attempts,next_at_ms,failure_stage,failure_code FROM provider_retries`)
	if err != nil {
		return nil, fmt.Errorf("load provider retries: %w", err)
	}
	defer rows.Close()
	result := []RetryState{}
	for rows.Next() {
		var state RetryState
		var stamp int64
		if err := rows.Scan(&state.ProviderID, &state.InputID, &state.Revision, &state.Scope, &state.ImageID, &state.Attempts, &stamp, &state.Stage, &state.Code); err != nil {
			return nil, err
		}
		state.NextAt = time.UnixMilli(stamp).UTC()
		result = append(result, state)
	}
	return result, rows.Err()
}

func (s *Store) SaveRetry(ctx context.Context, state RetryState) error {
	return s.SaveRetries(ctx, []RetryState{state})
}

// Related input and provider-wide limits commit together, so restart cannot
// observe an input limit without its shared quota gate.
func (s *Store) SaveRetries(ctx context.Context, states []RetryState) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, state := range states {
		_, err = tx.ExecContext(ctx, `INSERT INTO provider_retries(provider_id,input_id,settings_revision,scope,image_id,attempts,next_at_ms,failure_stage,failure_code) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(provider_id,input_id,settings_revision,scope,image_id) DO UPDATE SET attempts=excluded.attempts,next_at_ms=excluded.next_at_ms,failure_stage=excluded.failure_stage,failure_code=excluded.failure_code`, state.ProviderID, state.InputID, state.Revision, state.Scope, state.ImageID, state.Attempts, state.NextAt.UTC().UnixMilli(), state.Stage, state.Code)
		if err != nil {
			return fmt.Errorf("save provider retry: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteRetry(ctx context.Context, key RetryKey) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM provider_retries WHERE provider_id=? AND input_id=? AND settings_revision=? AND scope=? AND image_id=?`, key.ProviderID, key.InputID, key.Revision, key.Scope, key.ImageID)
	return err
}
