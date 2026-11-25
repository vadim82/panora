package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const MaxRulePreviewImages = 200
const MaxRulePreviewInputs = 512

type InputRevision struct {
	ProviderID, InputID, Revision string
}

// RulePreviewCandidates returns one current enabled membership per unique image.
// Results are a bounded read snapshot; no rows stay open during file inspection.
func (s *Store) RulePreviewCandidates(ctx context.Context, inputs []InputRevision) ([]Candidate, bool, error) {
	if len(inputs) > MaxRulePreviewInputs {
		return nil, false, fmt.Errorf("rule preview accepts at most %d inputs", MaxRulePreviewInputs)
	}
	if len(inputs) == 0 {
		return []Candidate{}, false, ctx.Err()
	}
	values := make([]string, len(inputs))
	args := make([]any, 0, len(inputs)*3+1)
	for i, input := range inputs {
		if input.ProviderID == "" || input.InputID == "" || input.Revision == "" {
			return nil, false, fmt.Errorf("preview input identity is required")
		}
		values[i] = "(?,?,?)"
		args = append(args, input.ProviderID, input.InputID, input.Revision)
	}
	args = append(args, MaxRulePreviewImages+1)
	query := `WITH requested(provider_id,input_id,settings_revision) AS (VALUES ` + strings.Join(values, ",") + `),
 selected AS (SELECT MIN(m.rowid) AS membership FROM input_images m JOIN requested r
 ON r.provider_id=m.provider_id AND r.input_id=m.input_id AND r.settings_revision=m.settings_revision
 LEFT JOIN discovery_progress d ON d.provider_id=m.provider_id AND d.input_id=m.input_id AND d.settings_revision=m.settings_revision
 WHERE m.provider_id<>'local' OR m.seen_epoch>=COALESCE(d.complete_epoch,0)
 GROUP BY m.image_id ORDER BY m.image_id LIMIT ?)
 SELECT m.image_id,m.provider_id,m.input_id,m.settings_revision,m.candidate_json
 FROM selected x JOIN input_images m ON m.rowid=x.membership ORDER BY m.image_id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	result := []Candidate{}
	for rows.Next() {
		if len(result) == MaxRulePreviewImages {
			return result, true, nil
		}
		var row Candidate
		var data string
		if err := rows.Scan(&row.Identity.ImageID, &row.Identity.ProviderID, &row.Identity.InputID, &row.Identity.Revision, &data); err != nil {
			return nil, false, err
		}
		if err := json.Unmarshal([]byte(data), &row.Candidate); err != nil {
			return nil, false, err
		}
		if err := row.Candidate.Validate(); err != nil {
			return nil, false, err
		}
		result = append(result, row)
	}
	return result, false, rows.Err()
}

// RulePreviewDiscoveryComplete verifies coverage for the captured revisions.
// Process-local health can still describe the previous revision just after a
// settings save; that snapshot must not turn an empty new catalog into a complete
// preview. No discovery or progress writes are made by this query.
func (s *Store) RulePreviewDiscoveryComplete(ctx context.Context, inputs []InputRevision) (bool, error) {
	if len(inputs) > MaxRulePreviewInputs {
		return false, fmt.Errorf("rule preview accepts at most %d inputs", MaxRulePreviewInputs)
	}
	if len(inputs) == 0 {
		return true, ctx.Err()
	}
	values := make([]string, len(inputs))
	args := make([]any, 0, len(inputs)*3)
	for i, input := range inputs {
		if input.ProviderID == "" || input.InputID == "" || input.Revision == "" {
			return false, fmt.Errorf("preview input identity is required")
		}
		values[i] = "(?,?,?)"
		args = append(args, input.ProviderID, input.InputID, input.Revision)
	}
	query := `WITH requested(provider_id,input_id,settings_revision) AS (VALUES ` + strings.Join(values, ",") + `)
 SELECT COUNT(*) FROM requested r JOIN discovery_progress d ON d.provider_id=r.provider_id
 AND d.input_id=r.input_id AND d.settings_revision=r.settings_revision WHERE d.complete=1`
	var complete int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&complete); err != nil {
		return false, err
	}
	return complete == len(inputs), nil
}
