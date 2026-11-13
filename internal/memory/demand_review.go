package memory

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// This is bookkeeping, deliberately independent of evidence links/versions.
func (s *Store) initDemandReview() error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS turn_case_writes(entry_id TEXT PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE,source_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE);
 CREATE INDEX IF NOT EXISTS turn_case_writes_source ON turn_case_writes(source_id);
 CREATE TABLE IF NOT EXISTS memory_candidates(source_id TEXT PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE,scope TEXT NOT NULL,state TEXT NOT NULL CHECK(state IN ('pending','saved','dismissed')),reason TEXT NOT NULL,review_at_stop INTEGER NOT NULL DEFAULT 0,updated_at TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS memory_candidate_migrations(version INTEGER PRIMARY KEY);`)
	if err != nil {
		return err
	}
	var done int
	if err = tx.QueryRow(`SELECT count(*) FROM memory_candidate_migrations WHERE version=1`).Scan(&done); err != nil {
		return err
	}
	if done == 0 {
		var hasHooks int
		if err = tx.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='hook_turns'`).Scan(&hasHooks); err != nil {
			return err
		}
		join, condition := "", " AND e.source_ref NOT LIKE 'hook-event:%'"
		if hasHooks > 0 {
			join = " LEFT JOIN hook_turns h ON h.source_id=c.source_id "
			condition = " AND (h.status IN ('pending','review_requested') OR (h.source_id IS NULL AND e.source_ref NOT LIKE 'hook-event:%'))"
		}
		// One-time recovery only. Do not rescan conversation history on every hook.
		_, err = tx.Exec(`INSERT OR IGNORE INTO memory_candidates(source_id,scope,state,reason,review_at_stop,updated_at)
 SELECT c.source_id,c.scope,CASE WHEN EXISTS(SELECT 1 FROM entries x JOIN links l ON l.entry_id=x.id WHERE l.target_id=c.source_id AND x.scope=c.scope AND x.current=1 AND x.kind='case') THEN 'saved' ELSE 'pending' END,c.decision,0,c.updated_at
 FROM context_checks c JOIN entries e ON e.id=c.source_id ` + join + `
 WHERE c.save_candidate=1 AND e.kind='record' AND e.origin<>'model_inference' AND e.scope=c.scope` + condition)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO memory_candidate_migrations(version)VALUES(1)`); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type MemoryCandidate struct {
	SourceID         string   `json:"source_id"`
	Scope            string   `json:"scope"`
	State            string   `json:"state"`
	Reason           string   `json:"reason,omitempty"`
	ReviewAtStop     bool     `json:"review_at_stop"`
	WrittenCaseIDs   []string `json:"written_case_ids,omitempty"`
	WrittenCaseCount int      `json:"written_case_count,omitempty"`
}

func setCandidate(ctx context.Context, tx *sql.Tx, source, scope, reason string, value, atStop bool) error {
	if value {
		_, err := tx.ExecContext(ctx, `INSERT INTO memory_candidates(source_id,scope,state,reason,review_at_stop,updated_at)VALUES(?,?,'pending',?,?,?) ON CONFLICT(source_id)DO UPDATE SET state='pending',reason=excluded.reason,review_at_stop=excluded.review_at_stop,updated_at=excluded.updated_at`, source, scope, reason, atStop, time.Now().UTC().Format(time.RFC3339Nano))
		return err
	}
	// Ordinary turns never create a queue item. A host may withdraw its own
	// candidate; a successful saved state is not rewritten as "nothing valuable".
	_, err := tx.ExecContext(ctx, `UPDATE memory_candidates SET state='dismissed',reason=?,review_at_stop=0,updated_at=? WHERE source_id=? AND state='pending'`, reason, time.Now().UTC().Format(time.RFC3339Nano), source)
	return err
}

func (s *Store) CandidateStatus(ctx context.Context, source string) (MemoryCandidate, error) {
	r := MemoryCandidate{SourceID: source, State: "none"}
	err := s.db.QueryRowContext(ctx, `SELECT scope,state,reason,review_at_stop FROM memory_candidates WHERE source_id=?`, source).Scan(&r.Scope, &r.State, &r.Reason, &r.ReviewAtStop)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return r, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.id FROM turn_case_writes w JOIN entries e ON e.id=w.entry_id WHERE w.source_id=? AND e.current=1 ORDER BY e.id`, source)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return r, err
		}
		r.WrittenCaseCount++
		if len(r.WrittenCaseIDs) < 4 {
			r.WrittenCaseIDs = append(r.WrittenCaseIDs, id)
		}
	}
	if r.State == "none" && r.WrittenCaseCount > 0 {
		r.State = "saved"
		r.Reason = "Case write committed; the turn association is separate from evidence links."
	}
	return r, rows.Err()
}

func (s *Store) PendingCandidates(ctx context.Context, scope, cursor string, limit int) ([]MemoryCandidate, string, error) {
	filter := digestText("pending-memory:" + scope)
	anchor := ""
	if cursor != "" {
		c, err := decodeCursor(cursor, filter)
		if err != nil {
			return nil, "", err
		}
		if c.Sort != "source_id" {
			return nil, "", errors.New("invalid pending cursor")
		}
		anchor = c.ID
	}
	if limit < 1 || limit > 30 {
		return nil, "", errors.New("pending limit must be 1..30")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT source_id,scope,state,reason,review_at_stop FROM memory_candidates WHERE state='pending' AND scope=? AND source_id>? ORDER BY source_id LIMIT ?`, scope, anchor, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := []MemoryCandidate{}
	for rows.Next() {
		var r MemoryCandidate
		if err = rows.Scan(&r.SourceID, &r.Scope, &r.State, &r.Reason, &r.ReviewAtStop); err != nil {
			return nil, "", err
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = encodeCursor(pageCursor{Version: 1, Filter: filter, ID: out[len(out)-1].SourceID, Sort: "source_id"})
	}
	return out, next, nil
}

func loadRecordedSource(ctx context.Context, q queryer, e *Entry) error {
	if e.Kind != "case" {
		return nil
	}
	// Read-only diagnostics must also work on older libraries without migration.
	var exists int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='turn_case_writes'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	err := q.QueryRowContext(ctx, `SELECT source_id FROM turn_case_writes WHERE entry_id=?`, e.ID).Scan(&e.RecordedSourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

const casesForSourceSQL = `SELECT e.id FROM entries e WHERE e.scope=? AND e.kind='case' AND e.current=1 AND
 (EXISTS(SELECT 1 FROM links l WHERE l.entry_id=e.id AND l.target_id=?) OR EXISTS(SELECT 1 FROM turn_case_writes w WHERE w.entry_id=e.id AND w.source_id=?))`

func compactCandidate(c *MemoryCandidate) {
	if c != nil {
		c.Reason = preview(c.Reason, 400)
	}
}

func candidateReason(in ContextInput) error {
	if strings.TrimSpace(in.Decision) == "" {
		return errors.New("candidate needs a concrete new/changed finding or withdrawal reason in decision")
	}
	if in.ReviewAtStop && !in.SaveCandidate {
		return errors.New("review_at_stop requires an actual save_candidate=true")
	}
	return nil
}
