package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// These receipts prove response creation only, not transport delivery, current
// model-context inclusion or semantic consumption. They allow bounded pages to
// cover required candidates without a model-authored "complete" assertion.
func (s *Store) recallResponseReceipt(ctx context.Context, id, generation, scope string, required map[string]bool, items []ExperienceItem) (string, error) {
	stale := "stale_no_current_turn_release"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var actual, created string
	var completed bool
	err = tx.QueryRowContext(ctx, `SELECT c.generation,c.created_at,c.completed FROM experience_challenges c JOIN experience_epochs e ON e.host=c.host AND e.scope=c.scope AND e.session=c.session AND e.generation=c.generation WHERE c.id=? AND c.scope=?`, id, scope).Scan(&actual, &created, &completed)
	if err == sql.ErrNoRows {
		return stale, nil
	}
	if err != nil {
		return "", err
	}
	at, er := time.Parse(time.RFC3339Nano, created)
	if er != nil || actual != generation || time.Since(at) > ExperienceChallengeTTL {
		return stale, nil
	}
	if completed {
		return "query_completed_not_applicability_verified", nil
	}
	for _, item := range items {
		if !required[item.RootID] {
			continue
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO recall_responses(challenge_id,root_id,entry_id)
 SELECT ?,root_id,id FROM entries WHERE id=? AND scope=? AND current=1 AND needs_review=0
 AND NOT EXISTS(SELECT 1 FROM managed_entries m WHERE m.root_id=entries.root_id AND m.state<>'active')
 ON CONFLICT(challenge_id,root_id) DO UPDATE SET entry_id=excluded.entry_id`, id, item.ID, scope)
		if err != nil {
			return "", err
		}
	}
	roots := []string{}
	for root := range required {
		roots = append(roots, root)
	}
	b, _ := json.Marshal(roots)
	var count int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM recall_responses r JOIN entries e ON e.id=r.entry_id WHERE r.challenge_id=? AND e.current=1 AND e.needs_review=0 AND e.scope=?
 AND r.root_id IN(SELECT value FROM json_each(?))
 AND NOT EXISTS(SELECT 1 FROM managed_entries m WHERE m.root_id=e.root_id AND m.state<>'active')`, id, scope, string(b)).Scan(&count)
	if err != nil {
		return "", err
	}
	outcome := "partial_query_response_not_completed"
	if count == len(required) && count > 0 {
		if _, err = tx.ExecContext(ctx, "UPDATE experience_challenges SET completed=1 WHERE id=? AND generation=?", id, generation); err != nil {
			return "", err
		}
		outcome = "query_completed_not_applicability_verified"
	}
	return outcome, tx.Commit()
}
