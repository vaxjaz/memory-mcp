package memory

import (
	"context"
	"encoding/json"
	"time"
)

func hostRequestID(host, session, event string) string {
	if host == "zcode" {
		return hookRequestID(session, event)
	}
	raw, _ := json.Marshal([]string{session, event})
	return host + "-user:" + digestText(string(raw))
}

func (s *Store) initAdapterReceipts(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS hook_adapter_receipts(id INTEGER PRIMARY KEY AUTOINCREMENT,host TEXT NOT NULL,event TEXT NOT NULL,scope TEXT NOT NULL,session TEXT NOT NULL,turn_id TEXT NOT NULL,created_at TEXT NOT NULL)`)
	return err
}

func (s *Store) HookAdapterReceipt(ctx context.Context, host, event, scope, session, turn string) error {
	if err := s.initAdapterReceipts(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, "INSERT INTO hook_adapter_receipts(host,event,scope,session,turn_id,created_at)VALUES(?,?,?,?,?,?)", host, event, scope, session, turn, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
func (s *Store) HookAdapterDiagnostics(ctx context.Context, host string) ([]map[string]string, error) {
	if err := s.initAdapterReceipts(ctx); err != nil {
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
	return scanTurn(s.db.QueryRowContext(ctx, "SELECT "+hookColumns+" FROM hook_turns WHERE scope=? AND session=? AND source_id IN (SELECT id FROM entries WHERE host=?) ORDER BY rowid DESC LIMIT 1", scope, session, host))
}
