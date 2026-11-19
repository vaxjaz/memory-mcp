package memory

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const ChallengeQueryRequired = "query_required"

type ChallengeCandidate struct {
	ID    string `json:"id"`
	CueID string `json:"cue_id"`
}

type ChallengeStatus struct {
	ID         string               `json:"challenge_id"`
	Host       string               `json:"host"`
	Scope      string               `json:"scope"`
	Session    string               `json:"session"`
	Stage      string               `json:"stage"`
	Candidates []ChallengeCandidate `json:"candidates"`
	ReadOnly   bool                 `json:"read_only"`
}

func (s *Store) ExperienceChallengeStatus(ctx context.Context, id string) (ChallengeStatus, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if raw, err := hex.DecodeString(id); err != nil || len(raw) != 16 {
		return ChallengeStatus{}, errors.New("--challenge requires a full 32-digit hexadecimal ID")
	}
	status, err := readChallengeStatus(ctx, s.db, id)
	if errors.Is(err, sql.ErrNoRows) {
		return status, errors.New("challenge not found; verify the full ID and --data-dir")
	}
	return status, err
}

func readChallengeStatus(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (ChallengeStatus, error) {
	status := ChallengeStatus{ID: id, ReadOnly: true}
	var created, encoded string
	var stale, completed, faulted, bound, generated, observed, partial bool
	err := q.QueryRowContext(ctx, `
SELECT c.host,c.scope,c.session,c.created_at,c.ids_json,
       NOT EXISTS(SELECT 1 FROM experience_epochs e WHERE e.host=c.host AND e.scope=c.scope AND e.session=c.session AND e.generation=c.generation),
       c.completed OR NOT EXISTS (
         SELECT 1 FROM json_each(c.ids_json) candidate
         WHERE NOT EXISTS (SELECT 1 FROM experience_body_deliveries d
            WHERE d.consumer=c.consumer AND d.entry_id=json_extract(candidate.value,'$.id'))
       ),c.faulted,
       EXISTS(SELECT 1 FROM experience_lookup_calls l WHERE l.challenge_id=c.id),
       EXISTS(SELECT 1 FROM experience_response_receipts r WHERE r.challenge_id=c.id),
       EXISTS(SELECT 1 FROM experience_response_receipts r WHERE r.challenge_id=c.id AND r.observed_consumer=c.consumer),
       EXISTS(SELECT 1 FROM json_each(c.ids_json) candidate JOIN experience_body_deliveries d
         ON d.consumer=c.consumer AND d.entry_id=json_extract(candidate.value,'$.id'))
FROM experience_challenges c WHERE c.id=?`, id).Scan(&status.Host, &status.Scope, &status.Session, &created, &encoded,
		&stale, &completed, &faulted, &bound, &generated, &observed, &partial)
	if err != nil {
		return status, err
	}
	at, parseErr := time.Parse(time.RFC3339Nano, created)
	switch {
	case stale:
		status.Stage = "stale_context"
	case completed:
		status.Stage = "delivered"
	case faulted:
		status.Stage = "failed_not_delivered"
	case parseErr != nil || time.Since(at) > ExperienceChallengeTTL:
		status.Stage = "expired_not_delivered"
	case partial:
		status.Stage = "partial_delivery"
	case observed:
		status.Stage = "observed_without_delivery"
	case generated:
		status.Stage = "response_pending_observation"
	case bound:
		status.Stage = "lookup_in_progress"
	default:
		status.Stage = "query_requested"
	}
	return status, json.Unmarshal([]byte(encoded), &status.Candidates)
}
