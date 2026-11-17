package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type receiptItem struct {
	ID     string `json:"id"`
	RootID string `json:"root_id"`
}

// Response construction is only response_generated. No consumer is credited
// until an adapter observes the successful native result for a bound call ID.
func (s *Store) generateExperienceReceipt(ctx context.Context, scope, project, challenge string, out ExperienceLookupResult) (string, error) {
	id := out.ReceiptID
	if id == "" {
		return "", errors.New("missing generated receipt identity")
	}
	items := []receiptItem{}
	for _, item := range out.Items {
		if item.Experience != nil || item.Agreement != nil {
			items = append(items, receiptItem{item.ID, item.RootID})
		}
	}
	encoded, _ := json.Marshal(items)
	body, _ := json.Marshal(out.Items)
	_, err := s.db.ExecContext(ctx, "INSERT INTO experience_response_receipts(id,scope,project,challenge_id,items_digest,items_json)VALUES(?,?,?,?,?,?)", id, scope, project, challenge, digestText(string(body)), string(encoded))
	return id, err
}

// The identity comes exclusively from a native event, never lookup arguments.
// No query/prompt/tool output is stored, and call IDs cannot be rebound after
// compact, even if a duplicate PreToolUse arrives later.
func (s *Store) BeginExperienceLookup(ctx context.Context, host, scope, session, project, callID, tool, challenge string) error {
	if callID == "" || len(callID) > 512 {
		return errors.New("lookup receipt observation requires native tool_use_id")
	}
	project, err := s.ExperienceProjectBinding(ctx, project, scope)
	if err != nil {
		return err
	}
	consumer, generation, err := s.deliveryContext(ctx, host, scope, session, project)
	if err != nil {
		return err
	}
	if challenge != "" {
		var actual string
		if err = s.db.QueryRowContext(ctx, "SELECT consumer FROM experience_challenges WHERE id=?", challenge).Scan(&actual); err != nil {
			return err
		}
		if actual != consumer {
			return errors.New("lookup challenge belongs to another consumer/context")
		}
	}
	_, err = s.db.ExecContext(ctx, "INSERT OR IGNORE INTO experience_lookup_calls(host,scope,session,call_id,tool,project,generation,consumer,challenge_id)VALUES(?,?,?,?,?,?,?,?,?)", host, scope, session, callID, tool, project, generation, consumer, challenge)
	return err
}

func (s *Store) FailExperienceLookup(ctx context.Context, host, scope, session, callID string) error {
	// A known failed delivery ends only this pending technical gate. It does
	// not make any body/cue visible and does not create compensation reminders.
	_, err := s.db.ExecContext(ctx, `UPDATE experience_challenges SET faulted=1 WHERE id IN(SELECT challenge_id FROM experience_lookup_calls WHERE host=? AND scope=? AND session=? AND call_id=?)`, host, scope, session, callID)
	return err
}

func (s *Store) ObserveExperienceLookup(ctx context.Context, host, scope, session, callID, tool string, out ExperienceLookupResult) error {
	if callID == "" || out.ReceiptID == "" || out.ContentRole != "evidence_candidates" {
		return errors.New("invalid successful lookup result")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE experience_delivery_protocol SET version=version WHERE id=1"); err != nil {
		return err
	}
	var project, consumer, challenge, boundTool, received string
	if err = tx.QueryRowContext(ctx, "SELECT project,consumer,challenge_id,tool,receipt_id FROM experience_lookup_calls WHERE host=? AND scope=? AND session=? AND call_id=?", host, scope, session, callID).Scan(&project, &consumer, &challenge, &boundTool, &received); err != nil {
		return err
	}
	if tool != boundTool {
		return errors.New("native lookup tool mismatch")
	}
	if received != "" && received != out.ReceiptID {
		return errors.New("native lookup result identity conflict")
	}
	var rs, rp, rc, digest, encoded, observed, observedCall string
	if err = tx.QueryRowContext(ctx, "SELECT scope,project,challenge_id,items_digest,items_json,observed_consumer,call_id FROM experience_response_receipts WHERE id=?", out.ReceiptID).Scan(&rs, &rp, &rc, &digest, &encoded, &observed, &observedCall); err != nil {
		return err
	}
	body, _ := json.Marshal(out.Items)
	if rs != scope || rp != project || rc != challenge || digest != digestText(string(body)) {
		return errors.New("lookup receipt does not match actual complete items/project/challenge")
	}
	if observed != "" && (observed != consumer || observedCall != callID) {
		return errors.New("lookup receipt replayed across native consumers/calls")
	}
	var items []receiptItem
	if err = json.Unmarshal([]byte(encoded), &items); err != nil {
		return err
	}
	var matches []ExperienceMatch
	if challenge != "" {
		var data, actual string
		if err = tx.QueryRowContext(ctx, "SELECT ids_json,consumer FROM experience_challenges WHERE id=?", challenge).Scan(&data, &actual); err != nil {
			return err
		}
		if actual != consumer {
			return errors.New("challenge consumer mismatch")
		}
		if err = json.Unmarshal([]byte(data), &matches); err != nil {
			return err
		}
	}
	for _, item := range items {
		// A retired/revised page is historical material, never a permit for the new
		// version. Old-context late results remain attached to their PreToolUse.
		var valid int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM entries WHERE id=? AND root_id=? AND scope=? AND current=1 AND needs_review=0 AND NOT EXISTS(SELECT 1 FROM managed_entries m WHERE m.root_id=entries.root_id AND m.state<>'active')`, item.ID, item.RootID, scope).Scan(&valid); err != nil {
			return err
		}
		if valid == 0 {
			continue
		}
		if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO experience_body_deliveries VALUES(?,?,?,?)", consumer, item.RootID, item.ID, out.ReceiptID); err != nil {
			return err
		}
		for _, m := range matches {
			if m.ID != item.ID || m.RootID != item.RootID {
				continue
			}
			if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO experience_cue_deliveries VALUES(?,?,?,?,?,?,?)", consumer, item.RootID, item.ID, m.CueID, "body_receipt", out.ReceiptID, ""); err != nil {
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE experience_response_receipts SET observed_consumer=?,call_id=? WHERE id=?", consumer, callID, out.ReceiptID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE experience_lookup_calls SET receipt_id=? WHERE host=? AND scope=? AND session=? AND call_id=?", out.ReceiptID, host, scope, session, callID); err != nil {
		return err
	}
	if challenge != "" {
		complete := true
		for _, m := range matches {
			var n int
			if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM experience_body_deliveries WHERE consumer=? AND entry_id=?", consumer, m.ID).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				complete = false
			}
		}
		if complete {
			if _, err = tx.ExecContext(ctx, "UPDATE experience_challenges SET completed=1 WHERE id=?", challenge); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
