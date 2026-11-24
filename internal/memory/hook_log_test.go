package memory

import (
	"fmt"
	"testing"
)

// Every tool call of a bound project can leave a row; the log must not grow
// without end, and what is kept must be the newest.
func TestHookLogKeepsTheNewestRowsOnly(t *testing.T) {
	s := testStore(t)
	defer s.Close()
	if err := s.InitHooks(ctx); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	statement, err := tx.PrepareContext(ctx, `INSERT INTO hook_runs(event,scope,session,result,detail,created_at)VALUES('PreToolUse','project:demo','session','probe_skipped',?,'t')`)
	if err != nil {
		t.Fatal(err)
	}
	total := hookLogLimit + 25
	for i := 0; i < total; i++ {
		if _, err = statement.ExecContext(ctx, fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = s.trimHookLog(ctx); err != nil {
		t.Fatal(err)
	}
	var count, oldest, newest int
	if err = s.db.QueryRowContext(ctx, `SELECT count(*), min(cast(detail AS INTEGER)), max(cast(detail AS INTEGER)) FROM hook_runs`).Scan(&count, &oldest, &newest); err != nil {
		t.Fatal(err)
	}
	if count != hookLogLimit || oldest != total-hookLogLimit || newest != total-1 {
		t.Fatalf("kept %d rows spanning %d..%d, want %d spanning %d..%d", count, oldest, newest, hookLogLimit, total-hookLogLimit, total-1)
	}
}
