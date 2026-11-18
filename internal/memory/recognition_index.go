package memory

import (
	"context"
	"database/sql"
)

func matchedRecallTokens(term string, present map[string]float64) ([]string, bool) {
	for _, tokens := range recallTermOptions(term) {
		all := true
		for _, token := range tokens {
			if present[token] == 0 {
				all = false
				break
			}
		}
		if all {
			return tokens, true
		}
	}
	return nil, false
}

// Derived postings are rebuilt without rewriting source entries or history.
// A hook never pays for a full rebuild: setup/serve performs it before use.
func (s *Store) initRecognitionRecallIndex(allowRebuild bool) error {
	if !allowRebuild {
		return nil
	}
	if _, err := s.db.Exec("CREATE TABLE IF NOT EXISTS recall_index_version(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL)"); err != nil {
		return err
	}
	var version int
	err := s.db.QueryRow("SELECT version FROM recall_index_version WHERE id=1").Scan(&version)
	if err == nil && version >= 4 {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	err = tx.QueryRow("SELECT version FROM recall_index_version WHERE id=1").Scan(&version)
	if err == nil && version >= 4 {
		return tx.Commit()
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err = migrateRecall(tx); err != nil {
		return err
	}
	return tx.Commit()
}
