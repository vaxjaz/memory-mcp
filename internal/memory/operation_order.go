package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Compatible v3 metadata. Physical timestamps cannot establish operation order:
// Windows can assign the same instant to separate writes, and clocks can roll back.
// A single durable counter survives concurrent processes, VACUUM and backup/restore.
func (s *Store) initOperationOrder() error {
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, spec := range []struct {
		table   string
		columns []string
	}{{"entries", []string{"write_order"}}, {"cleanup_plans", []string{"created_order", "decided_order"}}} {
		rows, er := tx.Query("PRAGMA table_info(" + spec.table + ")")
		if er != nil {
			return er
		}
		present := map[string]bool{}
		for rows.Next() {
			var cid, notnull, pk int
			var name, kind string
			var def any
			if er = rows.Scan(&cid, &name, &kind, &notnull, &def, &pk); er != nil {
				rows.Close()
				return er
			}
			present[name] = true
		}
		er = rows.Err()
		rows.Close()
		if er != nil {
			return er
		}
		for _, column := range spec.columns {
			if !present[column] {
				if _, er = tx.Exec("ALTER TABLE " + spec.table + " ADD COLUMN " + column + " INTEGER NOT NULL DEFAULT 0"); er != nil {
					return er
				}
			}
		}
	}
	if _, err = tx.Exec("CREATE TABLE IF NOT EXISTS operation_clock(id INTEGER PRIMARY KEY CHECK(id=1),value INTEGER NOT NULL); INSERT OR IGNORE INTO operation_clock(id,value)VALUES(1,0)"); err != nil {
		return err
	}
	return tx.Commit()
}
func nextOperationOrder(ctx context.Context, tx *sql.Tx) (int64, error) {
	var value int64
	err := tx.QueryRowContext(ctx, "UPDATE operation_clock SET value=value+1 WHERE id=1 RETURNING value").Scan(&value)
	return value, err
}
func validateCleanupApproval(ctx context.Context, q queryer, p CleanupPlan, in CleanupDecision, undo bool) error {
	source, err := userSource(ctx, q, in.ApprovalSourceID, p.Scope)
	if err != nil {
		return err
	}
	if !strings.Contains(source.Body, in.ApprovalQuote) {
		return errors.New("approval must quote a new user message after the preview; ask the user to approve this plan")
	}
	var created, decided, sourceOrder int64
	if err = q.QueryRowContext(ctx, "SELECT created_order,decided_order FROM cleanup_plans WHERE id=?", p.ID).Scan(&created, &decided); err != nil {
		return err
	}
	if err = q.QueryRowContext(ctx, "SELECT write_order FROM entries WHERE id=?", source.ID).Scan(&sourceOrder); err != nil {
		return err
	}
	boundary := created
	if undo {
		boundary = decided
	}
	if boundary > 0 {
		if sourceOrder <= boundary {
			return fmt.Errorf("approval source %s was not recorded after this plan boundary; use a new actual user confirmation from the upgraded service", source.ID)
		}
		return nil
	}
	// Old pending plans have no ordering metadata. Keep the strict legacy check;
	// recreate their preview if equal/coarse timestamps cannot establish freshness.
	after := p.CreatedAt
	if undo {
		after = p.DecidedAt
	}
	approvalTime, er := time.Parse(time.RFC3339Nano, source.CreatedAt)
	if er != nil {
		return er
	}
	boundaryTime, er := time.Parse(time.RFC3339Nano, after)
	if er != nil {
		return er
	}
	if !approvalTime.After(boundaryTime) {
		return errors.New("legacy plan has no operation-order boundary and approval time is not strictly later; create a fresh preview, show it and request a new confirmation")
	}
	return nil
}
