package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Evidence and the current task checkpoint are different keys. In particular,
// old evidence cannot be forced to complete after a newer turn has started.
func (s *Store) writeContextSource(ctx context.Context, d Draft) (string, error) {
	if d.Kind == "record" {
		return "", nil
	}
	if d.ContextSourceID != "" {
		source, err := userSource(ctx, s.db, d.ContextSourceID, d.Scope)
		if err != nil {
			return "", fmt.Errorf("context_source_id=%q: %w", d.ContextSourceID, err)
		}
		id, err := s.currentCheckpoint(ctx, source, true)
		if err != nil {
			return "", err
		}
		return id, nil
	}
	// Legacy payloads have only links. Validate their existence, then resolve
	// hook-backed historical evidence to the CURRENT turn in that host/session.
	// Among linked sessions/manual sources prefer the newest persisted user input;
	// new clients should pass context_source_id to remove this ambiguity.
	candidates := map[string]bool{}
	for index, l := range d.Links {
		if l.Relation == "contradicts" {
			continue
		}
		e, err := scanEntry(s.db.QueryRowContext(ctx, "SELECT "+columns+" FROM entries WHERE id=?", l.TargetID))
		if errors.Is(err, sql.ErrNoRows) {
			return "", missingLookup("write_context_source_not_found", fmt.Sprintf("draft.links[%d].target_id", index), l.TargetID, d.Scope, "The linked source is absent from this opened library. Verify its full ID and MCP/hook data_dir; preserve the existing case rather than creating a duplicate.")
		}
		if err != nil {
			return "", err
		}
		if e.Kind != "record" || e.Scope != d.Scope {
			continue
		}
		id, err := s.currentCheckpoint(ctx, e, false)
		if err != nil {
			return "", err
		}
		if id != "" {
			candidates[id] = true
		}
	}
	var chosen string
	var bestOrder, bestRow int64
	for id := range candidates {
		var order, row int64
		if err := s.db.QueryRowContext(ctx, "SELECT write_order,rowid FROM entries WHERE id=?", id).Scan(&order, &row); err != nil {
			return "", err
		}
		if chosen == "" || order > bestOrder || order == bestOrder && row > bestRow {
			chosen = id
			bestOrder = order
			bestRow = row
		}
	}
	return chosen, nil
}
func (s *Store) currentCheckpoint(ctx context.Context, e Entry, explicit bool) (string, error) {
	id := e.ID
	if strings.HasPrefix(e.SourceRef, HookSourcePrefix) {
		t, err := s.HookTurn(ctx, e.ID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				if !explicit {
					return "", nil
				}
				return "", missingLookup("hook_turn_not_found", "context_source_id", e.ID, e.Scope, "Verify the real hook/MCP routing before writing; no current turn was fabricated.")
			}
			return "", err
		}
		latest, err := s.HookLatestForHost(ctx, e.Host, e.Scope, t.Session)
		if errors.Is(err, sql.ErrNoRows) {
			return "", missingLookup("hook_session_not_found", "context_source_id", e.ID, e.Scope, "No current turn exists for this host/project/session. Verify hook routing and data_dir; no completion was assumed.")
		}
		if err != nil {
			return "", err
		}
		if explicit && latest.SourceID != e.ID {
			return "", fmt.Errorf("stale_write_context: context_source_id=%q, current_source_id=%q; prepare/complete the actual current turn instead of the historical evidence turn", e.ID, latest.SourceID)
		}
		id = latest.SourceID
	}
	if !explicit {
		var n int
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM context_checks WHERE source_id=?", id).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return "", nil
		}
	}
	return id, nil
}

func (s *Store) ValidateWriteContext(ctx context.Context, d Draft) (string, error) {
	id, err := s.writeContextSource(ctx, d)
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", nil
	}
	if err = s.ContextReady(ctx, id); err != nil {
		return "", fmt.Errorf("write_context_not_complete: checked_context_source_id=%q: %w", id, err)
	}
	return id, nil
}
