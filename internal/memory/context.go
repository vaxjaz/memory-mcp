package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vaxjaz/memory-mcp/internal/textguard"
)

type ContextInput struct {
	CandidateSourceID string   `json:"candidate_source_id,omitempty" jsonschema:"candidate action only: historical candidate to update/withdraw. source_id still identifies the actual current user source in the same scope."`
	ReviewAtStop      bool     `json:"review_at_stop,omitempty" jsonschema:"Only for an explicit meaningful pending candidate: ask for one extra model review at Stop. Default false; saved or dismissed candidates never trigger it."`
	ResponseMode      string   `json:"response_mode,omitempty" jsonschema:"compact (MCP default): bounded previews or short completion receipt; full: legacy output. Full-read checks are unchanged."`
	Action            string   `json:"action" jsonschema:"prepare, complete, skip, read-only status, candidate to declare/withdraw a concrete pending finding without resetting reads, or pending to list outstanding candidates in this scope"`
	Scope             string   `json:"scope"`
	SourceID          string   `json:"source_id,omitempty" jsonschema:"Actual user source ID; required except pending. Status may inspect historical sources; changes require the current actual source."`
	Task              string   `json:"task,omitempty" jsonschema:"Concrete current task reconstructed from project and visible conversation, not just the last user sentence; no private chain of thought"`
	Questions         string   `json:"questions,omitempty" jsonschema:"Decision, conditions, unresolved references and counterevidence to look for"`
	Keywords          []string `json:"keywords,omitempty" jsonschema:"Grounded literal retrieval terms chosen from project/conversation and relevant memories; duplicates/whitespace normalized, no eight-term cap. Overlong queries return a directory with explicit keyword-search status"`
	Cursor            string   `json:"cursor,omitempty"`
	SelectedIDs       []string `json:"selected_ids,omitempty" jsonschema:"Current applicable entries ACTUALLY read with context_source_id; empty if none applicable"`
	Decision          string   `json:"decision,omitempty" jsonschema:"Concrete constraints carried into the intended action, or why memory is not applicable; not proof an error was prevented"`
	SaveCandidate     bool     `json:"save_candidate,omitempty" jsonschema:"Only true if this turn contains a material decision, correction or reusable finding passing the forward/reverse admission challenge"`
}
type ContextResult struct {
	MemoryReview      *MemoryCandidate   `json:"memory_review,omitempty"`
	PendingCandidates []MemoryCandidate  `json:"pending_candidates,omitempty"`
	ResponseMode      string             `json:"response_mode,omitempty"`
	NextAction        string             `json:"next_action,omitempty"`
	Questions         string             `json:"questions,omitempty"`
	SourceID          string             `json:"source_id"`
	Status            string             `json:"status"`
	Task              string             `json:"task,omitempty"`
	Matches           []Hit              `json:"matches,omitempty"`
	MatchNextCursor   string             `json:"match_next_cursor,omitempty"`
	Directory         []InventoryItem    `json:"directory,omitempty"`
	NextCursor        string             `json:"next_cursor,omitempty"`
	SelectedIDs       []string           `json:"selected_ids"`
	Decision          string             `json:"decision,omitempty"`
	SaveCandidate     bool               `json:"save_candidate"`
	Notice            string             `json:"notice"`
	KeywordSearch     *KeywordSearchInfo `json:"keyword_search,omitempty"`
}

func (s *Store) RequireContext(ctx context.Context, t HookTurn) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO context_checks(source_id,scope,task,questions,status,updated_at)VALUES(?,?,'','','pending',?)`, t.SourceID, t.Scope, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
func (s *Store) ContextStatus(ctx context.Context, source string) (ContextResult, error) {
	r := ContextResult{SourceID: source, SelectedIDs: []string{}, Notice: "Read/selection receipts verify sequence and versions, not semantic correctness or actual prevention. Relevant full content remains authoritative over navigation summaries."}
	candidate, candidateErr := s.CandidateStatus(ctx, source)
	if candidateErr != nil {
		return r, candidateErr
	}
	if candidate.State != "none" || candidate.WrittenCaseCount > 0 {
		r.MemoryReview = &candidate
		r.SaveCandidate = candidate.State == "pending"
	}
	var selected string
	err := s.db.QueryRowContext(ctx, "SELECT task,status,selected_json,decision,questions,save_candidate FROM context_checks WHERE source_id=?", source).Scan(&r.Task, &r.Status, &selected, &r.Decision, &r.Questions, &r.SaveCandidate)
	if errors.Is(err, sql.ErrNoRows) {
		r.Status = "not_prepared"
		r.Notice = "No context check has been prepared for this source. Call memory_context(action=prepare) with the actual current user source before full-read tracking or complete; status inspection does not create or approve a check."
		return r, nil
	}
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal([]byte(selected), &r.SelectedIDs); err != nil {
		return r, err
	}
	r.SaveCandidate = candidate.State == "pending"
	r.Notice = "Navigation and receipts are not semantic proof. Follow relevant evidence/exception/replacement links. Do not turn repeated memory citations into independent evidence."
	return r, err
}
func (s *Store) Context(ctx context.Context, in ContextInput) (result ContextResult, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("memory_context.%s (data_dir=%q): %w", in.Action, s.Dir, err)
		}
	}()
	if err = ValidateResponseMode(in.ResponseMode); err != nil {
		return result, err
	}
	defer func() {
		if err == nil && in.ResponseMode == "compact" {
			err = compactContext(&result, in)
		}
	}()
	if err := validScope(in.Scope); err != nil {
		return ContextResult{}, err
	}
	if in.Action == "pending" {
		limit := 20
		if in.ResponseMode == "compact" {
			limit = 8
		}
		items, next, er := s.PendingCandidates(ctx, in.Scope, in.Cursor, limit)
		return ContextResult{Status: "pending_candidates", PendingCandidates: items, NextCursor: next, SelectedIDs: []string{}}, er
	}
	if in.SourceID == "" {
		return ContextResult{}, errors.New("actual source_id required for this action")
	}
	source, err := userSource(ctx, s.db, in.SourceID, in.Scope)
	if err != nil {
		return ContextResult{}, err
	}
	if in.Action == "status" {
		return s.ContextStatus(ctx, in.SourceID)
	}
	if strings.HasPrefix(source.SourceRef, HookSourcePrefix) {
		t, er := s.HookTurn(ctx, in.SourceID)
		if errors.Is(er, sql.ErrNoRows) {
			return ContextResult{}, missingLookup("hook_turn_not_found", "source_id", in.SourceID, in.Scope, "The raw entry exists, but its hook-turn metadata is missing. Verify actual hook/MCP data directories and the running versions, then use an actually captured current turn. Do not fabricate a turn or silently approve context.")
		}
		if er != nil {
			return ContextResult{}, er
		}
		latest, er := s.HookLatestForHost(ctx, source.Host, in.Scope, t.Session)
		if errors.Is(er, sql.ErrNoRows) {
			return ContextResult{}, missingLookup("hook_session_not_found", "source_id", in.SourceID, in.Scope, "No matching host/scope/session turn is registered in this library. Verify hook routing and data_dir before proceeding.")
		}
		if er != nil {
			return ContextResult{}, er
		}
		if latest.SourceID != in.SourceID {
			return ContextResult{}, errors.New("context must use the latest user input in this session")
		}
	}
	for name, v := range map[string]string{"task": in.Task, "questions": in.Questions, "decision": in.Decision} {
		if len(v) > 8192 {
			return ContextResult{}, errors.New("context field too large")
		}
		if err = textguard.Check(name, v, false); err != nil {
			return ContextResult{}, err
		}
	}
	switch in.Action {
	case "candidate":
		if err = candidateReason(in); err != nil {
			return result, err
		}
		target := in.SourceID
		if in.CandidateSourceID != "" {
			if _, err = userSource(ctx, s.db, in.CandidateSourceID, in.Scope); err != nil {
				return result, err
			}
			target = in.CandidateSourceID
			if in.ReviewAtStop && target != in.SourceID {
				return result, errors.New("review_at_stop must refer to the current source; declare a current candidate for an active review")
			}
		}
		tx, er := s.db.BeginTx(ctx, nil)
		if er != nil {
			return result, er
		}
		defer tx.Rollback()
		if er = setCandidate(ctx, tx, target, in.Scope, in.Decision, in.SaveCandidate, in.ReviewAtStop); er != nil {
			return result, er
		}
		if er = tx.Commit(); er != nil {
			return result, er
		}
		return s.ContextStatus(ctx, target)
	case "prepare":
		if strings.TrimSpace(in.Task) == "" {
			return ContextResult{}, errors.New("reconstruct the current task from project and conversation first")
		}
		query, terms, queryErr := normalizeSearchQuery(strings.Join(in.Keywords, " "))
		// Re-preparing means the task changed: earlier full-read receipts cannot
		// silently authorize a different action. Paging the SAME task preserves them.
		old, er := s.ContextStatus(ctx, in.SourceID)
		if er != nil {
			return ContextResult{}, fmt.Errorf("prepare.load_context_state: %w", er)
		}
		same := er == nil && old.Task == in.Task && old.Questions == in.Questions
		tx, er := s.db.BeginTx(ctx, nil)
		if er != nil {
			return ContextResult{}, er
		}
		defer tx.Rollback()
		_, er = tx.ExecContext(ctx, `INSERT INTO context_checks(source_id,scope,task,questions,status,updated_at)VALUES(?,?,?,?,'prepared',?) ON CONFLICT(source_id)DO UPDATE SET task=excluded.task,questions=excluded.questions,status='prepared',selected_json='[]',decision='',save_candidate=0,updated_at=excluded.updated_at`, in.SourceID, in.Scope, in.Task, in.Questions, time.Now().UTC().Format(time.RFC3339Nano))
		if er != nil {
			return ContextResult{}, er
		}
		if !same {
			if _, er = tx.ExecContext(ctx, "DELETE FROM context_reads WHERE source_id=?", in.SourceID); er != nil {
				return ContextResult{}, er
			}
		}
		if er = tx.Commit(); er != nil {
			return ContextResult{}, er
		}
		r, er := s.ContextStatus(ctx, in.SourceID)
		if er != nil {
			return r, er
		}
		pageLimit := 30
		if in.ResponseMode == "compact" {
			pageLimit = 8
		}
		r.Directory, r.NextCursor, er = s.Inventory(ctx, in.Scope, in.Cursor, pageLimit)
		if er != nil {
			return r, er
		}
		r.KeywordSearch = &KeywordSearchInfo{Status: "not_requested", Notice: "No literal search terms supplied; use the paginated directory and context to choose material."}
		if queryErr != nil {
			r.KeywordSearch = &KeywordSearchInfo{Status: "directory_only", Notice: queryErr.Error() + ". Directory preparation succeeded; keyword search was NOT executed. This is not evidence that no relevant memory exists. Refine the terms or inspect directory pages."}
		} else if in.ResponseMode == "compact" && in.Cursor != "" {
			r.KeywordSearch = &KeywordSearchInfo{Status: "not_repeated", Notice: "Directory continuation only; the initial keyword search was NOT repeated. Continue keyword results with memory_search using match_next_cursor from the initial prepare, or start a new prepare without cursor when the task/query changes."}
		} else if len(terms) > 0 {
			matchLimit := 15
			if in.ResponseMode == "compact" {
				matchLimit = 8
			}
			p, er := s.SearchPage(ctx, SearchInput{Scope: in.Scope, Query: query, Limit: matchLimit})
			if er != nil {
				return r, er
			}
			r.Matches = p.Results
			r.MatchNextCursor = p.NextCursor
			r.KeywordSearch = &KeywordSearchInfo{Status: "searched", UsedTerms: terms, Query: query, Notice: "All distinct supplied terms were searched with literal OR semantics. No terms were silently truncated; matching is not applicability."}
		}
		return r, nil
	case "skip":
		if in.SaveCandidate || len(in.SelectedIDs) > 0 || strings.TrimSpace(in.Task) == "" || strings.TrimSpace(in.Decision) == "" {
			return ContextResult{}, errors.New("skip needs task and concrete non-applicability reason; no selected IDs or save candidate")
		}
		tx, er := s.db.BeginTx(ctx, nil)
		if er != nil {
			return result, er
		}
		defer tx.Rollback()
		_, err = tx.ExecContext(ctx, `INSERT INTO context_checks(source_id,scope,task,questions,status,decision,updated_at)VALUES(?,?,?,?,'complete',?,?) ON CONFLICT(source_id)DO UPDATE SET task=excluded.task,questions=excluded.questions,status='complete',selected_json='[]',decision=excluded.decision,save_candidate=0,updated_at=excluded.updated_at`, in.SourceID, in.Scope, in.Task, in.Questions, in.Decision, time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			return ContextResult{}, err
		}
		if er = setCandidate(ctx, tx, in.SourceID, in.Scope, in.Decision, false, false); er != nil {
			return result, er
		}
		if er = tx.Commit(); er != nil {
			return result, er
		}
		return s.ContextStatus(ctx, in.SourceID)
	case "complete":
		if in.ReviewAtStop && !in.SaveCandidate {
			return result, errors.New("review_at_stop requires save_candidate=true")
		}
		if len(in.SelectedIDs) > 12 || strings.TrimSpace(in.Decision) == "" {
			return ContextResult{}, errors.New("complete needs a concrete action/non-applicability explanation and at most 12 entries")
		}
		tx, er := s.db.BeginTx(ctx, nil)
		if er != nil {
			return ContextResult{}, er
		}
		defer tx.Rollback()
		var status string
		if er = tx.QueryRowContext(ctx, "SELECT status FROM context_checks WHERE source_id=?", in.SourceID).Scan(&status); er != nil {
			if errors.Is(er, sql.ErrNoRows) {
				return ContextResult{}, missingLookup("context_not_prepared", "source_id", in.SourceID, in.Scope, "Call prepare using this current user source, read the selected full memories with context_source_id, then complete. Missing state is not automatically treated as a completed check.")
			}
			return ContextResult{}, er
		}
		if status != "prepared" && status != "complete" {
			return ContextResult{}, errors.New("prepare using current conversation before completing")
		}
		seen := map[string]bool{}
		for index, id := range in.SelectedIDs {
			if seen[id] {
				return ContextResult{}, errors.New("duplicate selected entry")
			}
			seen[id] = true
			e, er := resolveCurrent(ctx, tx, id)
			if er != nil {
				return ContextResult{}, fmt.Errorf("complete.selected_ids[%d]=%q: %w", index, id, er)
			}
			if e.ID != id || e.State.State != "active" || e.NeedsReview || (e.Scope != in.Scope && e.Scope != "global") {
				return ContextResult{}, errors.New("selected memory is outdated, inactive, outside scope or needs review")
			}
			if e.Profile != nil && e.Profile.ExpiresAt != "" {
				expiry, _ := time.Parse(time.RFC3339, e.Profile.ExpiresAt)
				if time.Now().After(expiry) {
					return ContextResult{}, errors.New("selected memory expired; verify current conditions")
				}
			}
			var n int
			if er = tx.QueryRowContext(ctx, "SELECT count(*) FROM context_reads WHERE source_id=? AND entry_id=?", in.SourceID, id).Scan(&n); er != nil {
				return ContextResult{}, er
			}
			if n == 0 {
				return ContextResult{}, errors.New("read selected full memory using context_source_id before completing")
			}
		}
		selected, _ := json.Marshal(in.SelectedIDs)
		if in.SelectedIDs == nil {
			selected = []byte("[]")
		}
		_, er = tx.ExecContext(ctx, "UPDATE context_checks SET status='complete',selected_json=?,decision=?,save_candidate=?,updated_at=? WHERE source_id=?", string(selected), in.Decision, in.SaveCandidate, time.Now().UTC().Format(time.RFC3339Nano), in.SourceID)
		if er != nil {
			return ContextResult{}, er
		}
		if er = setCandidate(ctx, tx, in.SourceID, in.Scope, in.Decision, in.SaveCandidate, in.ReviewAtStop); er != nil {
			return result, er
		}
		if er = tx.Commit(); er != nil {
			return ContextResult{}, er
		}
		return s.ContextStatus(ctx, in.SourceID)
	default:
		return ContextResult{}, errors.New("context action must be prepare, complete, skip, status, candidate or pending")
	}
}
func (s *Store) TrackContextRead(ctx context.Context, source, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var scope, status string
	if err = tx.QueryRowContext(ctx, "SELECT scope,status FROM context_checks WHERE source_id=?", source).Scan(&scope, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return missingLookup("context_not_prepared", "context_source_id", source, "", "Verify this source in the same library and call memory_context(action=prepare) before tracking full reads. No read receipt was recorded.")
		}
		return err
	}
	if status != "prepared" && status != "complete" {
		return errors.New("prepare context before tracking full reads")
	}
	var entryScope string
	if err = tx.QueryRowContext(ctx, "SELECT scope FROM entries WHERE id=?", id).Scan(&entryScope); err != nil {
		return err
	}
	if entryScope != scope && entryScope != "global" {
		return errors.New("context read outside scope")
	}
	_, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO context_reads(source_id,entry_id)VALUES(?,?)", source, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) ContextReady(ctx context.Context, source string) error {
	r, err := s.ContextStatus(ctx, source)
	if err != nil {
		return err
	}
	if r.Status != "complete" {
		return fmt.Errorf("context_not_complete: source_id=%q, status=%q; prepare the current task, read applicable full entries, then complete the memory check", source, r.Status)
	}
	for _, id := range r.SelectedIDs {
		e, er := s.ResolveCurrent(ctx, id)
		if er != nil {
			return er
		}
		if e.ID != id || e.State.State != "active" || e.NeedsReview {
			return errors.New("selected memory changed; prepare and check again")
		}
		if e.Profile != nil && e.Profile.ExpiresAt != "" {
			t, _ := time.Parse(time.RFC3339, e.Profile.ExpiresAt)
			if time.Now().After(t) {
				return errors.New("selected memory expired")
			}
		}
	}
	return nil
}
func (s *Store) CheckWriteContext(ctx context.Context, d Draft) error {
	_, err := s.ValidateWriteContext(ctx, d)
	return err
}

func (s *Store) CurrentCasesForSource(ctx context.Context, source, scope string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, casesForSourceSQL+" ORDER BY e.id", scope, source, source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
