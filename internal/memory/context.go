package memory

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/vaxjaz/memory-mcp/internal/textguard"
)

type ContextInput struct {
	Action        string   `json:"action" jsonschema:"prepare using conversation/project context, complete after reading sources, skip for a genuinely memory-independent task, or status"`
	Scope         string   `json:"scope"`
	SourceID      string   `json:"source_id" jsonschema:"Current actual user source ID; never an assistant answer"`
	Task          string   `json:"task,omitempty" jsonschema:"Concrete current task reconstructed from project and visible conversation, not just the last user sentence; no private chain of thought"`
	Questions     string   `json:"questions,omitempty" jsonschema:"Decision, conditions, unresolved references and counterevidence to look for"`
	Keywords      []string `json:"keywords,omitempty" jsonschema:"Grounded literal retrieval terms chosen from project/conversation and relevant memories; duplicates/whitespace normalized, no eight-term cap. Overlong queries return a directory with explicit keyword-search status"`
	Cursor        string   `json:"cursor,omitempty"`
	SelectedIDs   []string `json:"selected_ids,omitempty" jsonschema:"Current applicable entries ACTUALLY read with context_source_id; empty if none applicable"`
	Decision      string   `json:"decision,omitempty" jsonschema:"Concrete constraints carried into the intended action, or why memory is not applicable; not proof an error was prevented"`
	SaveCandidate bool     `json:"save_candidate,omitempty" jsonschema:"Only true if this turn contains a material decision, correction or reusable finding passing the forward/reverse admission challenge"`
}
type ContextResult struct {
	Questions       string             `json:"questions"`
	SourceID        string             `json:"source_id"`
	Status          string             `json:"status"`
	Task            string             `json:"task"`
	Directory       []InventoryItem    `json:"directory,omitempty"`
	NextCursor      string             `json:"next_cursor,omitempty"`
	Matches         []Hit              `json:"matches,omitempty"`
	MatchNextCursor string             `json:"match_next_cursor,omitempty"`
	SelectedIDs     []string           `json:"selected_ids"`
	Decision        string             `json:"decision"`
	SaveCandidate   bool               `json:"save_candidate"`
	Notice          string             `json:"notice"`
	KeywordSearch   *KeywordSearchInfo `json:"keyword_search,omitempty"`
}

func (s *Store) RequireContext(ctx context.Context, t HookTurn) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO context_checks(source_id,scope,task,questions,status,updated_at)VALUES(?,?,'','','pending',?)`, t.SourceID, t.Scope, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
func (s *Store) ContextStatus(ctx context.Context, source string) (ContextResult, error) {
	r := ContextResult{SourceID: source, SelectedIDs: []string{}, Notice: "Read/selection receipts verify sequence and versions, not semantic correctness or actual prevention. Relevant full content remains authoritative over navigation summaries."}
	var selected string
	err := s.db.QueryRowContext(ctx, "SELECT task,status,selected_json,decision,questions FROM context_checks WHERE source_id=?", source).Scan(&r.Task, &r.Status, &selected, &r.Decision, &r.Questions)
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal([]byte(selected), &r.SelectedIDs); err != nil {
		return r, err
	}
	// save candidate is explicit per-turn workflow state, not a permanent case.
	var candidate bool
	err = s.db.QueryRowContext(ctx, "SELECT save_candidate FROM context_checks WHERE source_id=?", source).Scan(&candidate)
	r.SaveCandidate = candidate
	r.Notice = "Navigation and receipts are not semantic proof. Follow relevant evidence/exception/replacement links. Do not turn repeated memory citations into independent evidence."
	return r, err
}
func (s *Store) Context(ctx context.Context, in ContextInput) (ContextResult, error) {
	if err := validScope(in.Scope); err != nil {
		return ContextResult{}, err
	}
	source, err := userSource(ctx, s.db, in.SourceID, in.Scope)
	if err != nil {
		return ContextResult{}, err
	}
	if strings.HasPrefix(source.SourceRef, HookSourcePrefix) {
		t, er := s.HookTurn(ctx, in.SourceID)
		if er != nil {
			return ContextResult{}, er
		}
		latest, er := s.HookLatest(ctx, in.Scope, t.Session)
		if er != nil {
			return ContextResult{}, er
		}
		if latest.SourceID != in.SourceID {
			return ContextResult{}, errors.New("context must use the latest user input in this session")
		}
	}
	if in.Action == "status" {
		return s.ContextStatus(ctx, in.SourceID)
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
	case "prepare":
		if strings.TrimSpace(in.Task) == "" {
			return ContextResult{}, errors.New("reconstruct the current task from project and conversation first")
		}
		query, terms, queryErr := normalizeSearchQuery(strings.Join(in.Keywords, " "))
		// Re-preparing means the task changed: earlier full-read receipts cannot
		// silently authorize a different action. Paging the SAME task preserves them.
		old, er := s.ContextStatus(ctx, in.SourceID)
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
		r.Directory, r.NextCursor, er = s.Inventory(ctx, in.Scope, in.Cursor, 30)
		if er != nil {
			return r, er
		}
		r.KeywordSearch = &KeywordSearchInfo{Status: "not_requested", Notice: "No literal search terms supplied; use the paginated directory and context to choose material."}
		if queryErr != nil {
			r.KeywordSearch = &KeywordSearchInfo{Status: "directory_only", Notice: queryErr.Error() + ". Directory preparation succeeded; keyword search was NOT executed. This is not evidence that no relevant memory exists. Refine the terms or inspect directory pages."}
		} else if len(terms) > 0 {
			p, er := s.SearchPage(ctx, SearchInput{Scope: in.Scope, Query: query, Limit: 15})
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
		_, err = s.db.ExecContext(ctx, `INSERT INTO context_checks(source_id,scope,task,questions,status,decision,updated_at)VALUES(?,?,?,?,'complete',?,?) ON CONFLICT(source_id)DO UPDATE SET task=excluded.task,questions=excluded.questions,status='complete',selected_json='[]',decision=excluded.decision,save_candidate=0,updated_at=excluded.updated_at`, in.SourceID, in.Scope, in.Task, in.Questions, in.Decision, time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			return ContextResult{}, err
		}
		return s.ContextStatus(ctx, in.SourceID)
	case "complete":
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
			return ContextResult{}, er
		}
		if status != "prepared" && status != "complete" {
			return ContextResult{}, errors.New("prepare using current conversation before completing")
		}
		seen := map[string]bool{}
		for _, id := range in.SelectedIDs {
			if seen[id] {
				return ContextResult{}, errors.New("duplicate selected entry")
			}
			seen[id] = true
			e, er := resolveCurrent(ctx, tx, id)
			if er != nil {
				return ContextResult{}, er
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
		if er = tx.Commit(); er != nil {
			return ContextResult{}, er
		}
		return s.ContextStatus(ctx, in.SourceID)
	default:
		return ContextResult{}, errors.New("context action must be prepare, complete, skip or status")
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
		return errors.New("current task memory check has not completed")
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
	if d.Kind == "record" {
		return nil
	}
	for _, l := range d.Links {
		if l.Relation == "contradicts" {
			continue
		}
		if l.Relation == "context" {
			source, err := scanEntry(s.db.QueryRowContext(ctx, "SELECT "+columns+" FROM entries WHERE id=?", l.TargetID))
			if err != nil {
				return err
			}
			valid, err := validGround(ctx, s.db, source)
			if err != nil {
				return err
			}
			if !valid {
				continue
			}
		}
		var n int
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM context_checks WHERE source_id=?", l.TargetID).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			if err := s.ContextReady(ctx, l.TargetID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) CurrentCasesForSource(ctx context.Context, source, scope string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT e.id FROM entries e JOIN links l ON l.entry_id=e.id WHERE l.target_id=? AND e.scope=? AND e.kind='case' AND e.current=1`, source, scope)
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
