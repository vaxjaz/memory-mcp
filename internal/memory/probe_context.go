package memory

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type ProbeContext struct {
	Host, Scope, Session, Project, Turn string
}

type ProbeFact struct {
	Definition, Key string
	Value           int
	TTL             time.Duration
	Session         bool
}

const probeCallRetention = 10 * time.Minute

func (s *Store) probeContext(ctx context.Context, p ProbeContext) (consumer, generation, turn string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT generation,native_turn FROM experience_epochs WHERE host=? AND scope=? AND session=?`,
		p.Host, p.Scope, p.Session).Scan(&generation, &turn)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (turn == "" || p.Turn != "" && turn != p.Turn) {
		return "", "", "", nil
	}
	return deliveryConsumer(p.Host, p.Scope, p.Session, p.Project, generation), generation, turn, err
}

func (s *Store) BeginProbeCall(ctx context.Context, p ProbeContext, tool, call, definition string) (bool, error) {
	consumer, generation, turn, err := s.probeContext(ctx, p)
	if err != nil || consumer == "" {
		return false, err
	}
	now := time.Now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM probe_context_calls WHERE expires<=?`, now); err != nil {
		return false, err
	}
	// Expired facts still suppress older in-flight results. Keep their
	// sequence until every call that could predate it has expired as well.
	if _, err := tx.ExecContext(ctx, `DELETE FROM probe_context_facts WHERE expires<=?`, now-probeCallRetention.Milliseconds()); err != nil {
		return false, err
	}
	// Record the exact call and input generation. Repeated Pre events retain
	// the first stamp, so a late result cannot claim a later input's identity.
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO probe_context_calls(consumer,tool,call,turn,definition,expires)
SELECT ?,?,?,?,?,? WHERE EXISTS (SELECT 1 FROM experience_epochs WHERE host=? AND scope=? AND session=? AND generation=? AND native_turn=?)
AND (SELECT count(*) FROM probe_context_calls WHERE consumer=?) < 128`,
		consumer, tool, call, turn, definition, now+probeCallRetention.Milliseconds(),
		p.Host, p.Scope, p.Session, generation, turn, consumer); err != nil {
		return false, err
	}
	var pending bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM probe_context_calls WHERE consumer=? AND tool=? AND call=? AND turn=? AND definition=? AND used=0)`,
		consumer, tool, call, turn, definition).Scan(&pending)
	if err != nil {
		return false, err
	}
	return pending, tx.Commit()
}

func (s *Store) CompleteProbeCall(ctx context.Context, p ProbeContext, tool, call, definition string, facts []ProbeFact) (bool, error) {
	consumer, generation, turn, err := s.probeContext(ctx, p)
	if err != nil || consumer == "" {
		return false, err
	}
	now := time.Now().UnixMilli()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var seq int64
	err = tx.QueryRowContext(ctx, `UPDATE probe_context_calls SET used=1
WHERE consumer=? AND tool=? AND call=? AND turn=? AND definition=? AND used=0 AND expires>?
AND EXISTS (SELECT 1 FROM experience_epochs WHERE host=? AND scope=? AND session=? AND generation=? AND native_turn=?)
RETURNING seq`, consumer, tool, call, turn, definition, now, p.Host, p.Scope, p.Session, generation, turn).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, fact := range facts {
		factTurn := turn
		if fact.Session {
			factTurn = ""
		}
		key := digestText(fact.Key)
		if _, err := tx.ExecContext(ctx, `INSERT INTO probe_context_facts
SELECT ?,?,?,?,?,?,? WHERE EXISTS(SELECT 1 FROM probe_context_facts WHERE consumer=? AND definition=? AND key=?)
OR (SELECT count(*) FROM probe_context_facts WHERE consumer=?) < 128
ON CONFLICT(consumer,definition,key) DO UPDATE SET turn=excluded.turn,value=excluded.value,seq=excluded.seq,expires=excluded.expires
WHERE excluded.seq>probe_context_facts.seq`, consumer, fact.Definition, key, factTurn, fact.Value, seq, now+fact.TTL.Milliseconds(),
			consumer, fact.Definition, key, consumer); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

func (s *Store) ReadProbeFact(ctx context.Context, p ProbeContext, definition, key string) (int, error) {
	consumer, _, turn, err := s.probeContext(ctx, p)
	if err != nil || consumer == "" {
		return -1, err
	}
	var value int
	err = s.db.QueryRowContext(ctx, `SELECT value FROM probe_context_facts WHERE consumer=? AND definition=? AND key=?
AND (turn='' OR turn=?) AND expires>?`, consumer, definition, digestText(key), turn, time.Now().UnixMilli()).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return -1, nil
	}
	return value, err
}
