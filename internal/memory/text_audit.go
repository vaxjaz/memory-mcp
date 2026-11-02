package memory

import (
	"context"
	"encoding/json"

	"github.com/vaxjaz/memory-mcp/internal/textguard"
)

type TextFinding struct {
	Type    string            `json:"type"`
	ID      string            `json:"id"`
	Scope   string            `json:"scope"`
	Current bool              `json:"current"`
	Fields  []textguard.Issue `json:"fields"`
}
type TextAudit struct {
	ScannedEntries  int           `json:"scanned_entries"`
	ScannedReceipts int           `json:"scanned_receipts"`
	Findings        []TextFinding `json:"findings"`
	Notice          string        `json:"notice"`
}

// AuditText never changes evidence and never returns private text. A U+FFFD
// finding may be intentional; only comparison with the original can decide.
func (s *Store) AuditText(ctx context.Context) (TextAudit, error) {
	out := TextAudit{Findings: []TextFinding{}, Notice: "Read-only text scan. U+FFFD is suspected loss or an intentional literal; compare original sources. Nothing is repaired or rewritten."}
	rows, err := s.db.QueryContext(ctx, `SELECT id,scope,current,title,body,source_ref FROM entries ORDER BY created_at,id`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var id, scope, title, body, source string
		var current bool
		if err = rows.Scan(&id, &scope, &current, &title, &body, &source); err != nil {
			rows.Close()
			return out, err
		}
		out.ScannedEntries++
		issues := []textguard.Issue{}
		for _, field := range []struct{ name, value string }{{"title", title}, {"body", body}, {"source_ref", source}} {
			if issue := textguard.Inspect(field.name, field.value); issue != nil {
				issues = append(issues, *issue)
			}
		}
		if len(issues) > 0 {
			out.Findings = append(out.Findings, TextFinding{"entry", id, scope, current, issues})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	var tables int
	if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='hook_turns'`).Scan(&tables); err != nil || tables == 0 {
		return out, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT source_id,scope,note FROM hook_turns WHERE note<>'' ORDER BY created_at,source_id`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, scope, note string
		if err = rows.Scan(&id, &scope, &note); err != nil {
			return out, err
		}
		out.ScannedReceipts++
		issues := []textguard.Issue{}
		if issue := textguard.Inspect("stored_note", note); issue != nil {
			issues = append(issues, *issue)
		}
		var receipt struct {
			Explanation string `json:"explanation"`
		}
		if textguard.JSON([]byte(note)) == nil && json.Unmarshal([]byte(note), &receipt) == nil {
			if issue := textguard.Inspect("review_note", receipt.Explanation); issue != nil {
				issues = append(issues, *issue)
			}
		}
		if len(issues) > 0 {
			out.Findings = append(out.Findings, TextFinding{"review_receipt", id, scope, true, issues})
		}
	}
	return out, rows.Err()
}
