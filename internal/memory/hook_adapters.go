package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

func hostRequestID(host, session, event string) string {
	if host == "zcode" {
		return hookRequestID(session, event)
	}
	raw, _ := json.Marshal([]string{session, event})
	return host + "-user:" + digestText(string(raw))
}

func (s *Store) initAdapterTurns(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS hook_native_turns(host TEXT NOT NULL,scope TEXT NOT NULL,session TEXT NOT NULL,turn_id TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,PRIMARY KEY(host,scope,session,turn_id));
	CREATE TABLE IF NOT EXISTS hook_adapter_receipts(id INTEGER PRIMARY KEY AUTOINCREMENT,host TEXT NOT NULL,event TEXT NOT NULL,scope TEXT NOT NULL,session TEXT NOT NULL,turn_id TEXT NOT NULL,created_at TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS hook_continuations(host TEXT NOT NULL,scope TEXT NOT NULL,session TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,reason_digest TEXT NOT NULL,PRIMARY KEY(host,scope,session));`)
	return err
}

func (s *Store) HookAdapterReceipt(ctx context.Context, host, event, scope, session, turn string) error {
	if err := s.initAdapterTurns(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, "INSERT INTO hook_adapter_receipts(host,event,scope,session,turn_id,created_at)VALUES(?,?,?,?,?,?)", host, event, scope, session, turn, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
func (s *Store) HookAdapterDiagnostics(ctx context.Context, host string) ([]map[string]string, error) {
	if err := s.initAdapterTurns(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT host,event,scope,session,turn_id,created_at FROM hook_adapter_receipts WHERE host=? ORDER BY id DESC LIMIT 20", host)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]string{}
	for rows.Next() {
		var h, e, sc, se, turn, at string
		if err = rows.Scan(&h, &e, &sc, &se, &turn, &at); err != nil {
			return nil, err
		}
		out = append(out, map[string]string{"host": h, "event": e, "scope": sc, "session": se, "turn_id": turn, "created_at": at})
	}
	return out, rows.Err()
}
func (s *Store) HookLatestForHost(ctx context.Context, host, scope, session string) (HookTurn, error) {
	return scanTurn(s.db.QueryRowContext(ctx, "SELECT "+hookColumns+" FROM hook_turns WHERE scope=? AND session=? AND source_id IN (SELECT id FROM entries WHERE host=?) ORDER BY created_at DESC,source_id DESC LIMIT 1", scope, session, host))
}
func (s *Store) HookBindTurn(ctx context.Context, host, scope, session, turn, id string) error {
	if err := s.initAdapterTurns(ctx); err != nil {
		return err
	}
	t, err := s.HookTurn(ctx, id)
	if err != nil {
		return err
	}
	var actual string
	if err = s.db.QueryRowContext(ctx, "SELECT host FROM entries WHERE id=?", id).Scan(&actual); err != nil {
		return err
	}
	if t.Scope != scope || t.Session != session || actual != host || turn == "" {
		return errors.New("native turn binding mismatch")
	}
	_, err = s.db.ExecContext(ctx, "INSERT OR IGNORE INTO hook_native_turns(host,scope,session,turn_id,source_id)VALUES(?,?,?,?,?)", host, scope, session, turn, id)
	if err != nil {
		return err
	}
	mapped, err := s.HookNativeTurn(ctx, host, scope, session, turn)
	if err == nil && mapped.SourceID != id {
		return errors.New("native turn ID already belongs to another input")
	}
	return err
}
func (s *Store) HookNativeTurn(ctx context.Context, host, scope, session, turn string) (HookTurn, error) {
	if err := s.initAdapterTurns(ctx); err != nil {
		return HookTurn{}, err
	}
	return scanTurn(s.db.QueryRowContext(ctx, "SELECT "+hookColumns+" FROM hook_turns WHERE source_id=(SELECT source_id FROM hook_native_turns WHERE host=? AND scope=? AND session=? AND turn_id=?)", host, scope, session, turn))
}
func (s *Store) HookRememberContinuation(ctx context.Context, host string, t HookTurn, reason string) error {
	if err := s.initAdapterTurns(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO hook_continuations(host,scope,session,source_id,reason_digest)VALUES(?,?,?,?,?) ON CONFLICT(host,scope,session) DO UPDATE SET source_id=excluded.source_id,reason_digest=excluded.reason_digest`, host, t.Scope, t.Session, t.SourceID, digestText(reason))
	return err
}

// Only the exact outstanding Stop reason is synthetic. A prefix or a user saying
// "continue" is never sufficient. Native turn mapping survives duplicate delivery.
func (s *Store) HookContinuation(ctx context.Context, host, scope, session, prompt, turn string) (HookTurn, bool, error) {
	if err := s.initAdapterTurns(ctx); err != nil {
		return HookTurn{}, false, err
	}
	var id, hash string
	err := s.db.QueryRowContext(ctx, "SELECT source_id,reason_digest FROM hook_continuations WHERE host=? AND scope=? AND session=?", host, scope, session).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return HookTurn{}, false, nil
	}
	if err != nil {
		return HookTurn{}, false, err
	}
	latest, err := s.HookLatestForHost(ctx, host, scope, session)
	if err != nil {
		return HookTurn{}, false, err
	}
	if hash != digestText(prompt) || latest.SourceID != id {
		_, err = s.db.ExecContext(ctx, "DELETE FROM hook_continuations WHERE host=? AND scope=? AND session=?", host, scope, session)
		return HookTurn{}, false, err
	}
	if turn != "" {
		if err = s.HookBindTurn(ctx, host, scope, session, turn, id); err != nil {
			return HookTurn{}, false, err
		}
	}
	t, err := s.HookTurn(ctx, id)
	return t, err == nil, err
}
