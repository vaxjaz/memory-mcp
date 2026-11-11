package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vaxjaz/memory-mcp/internal/textguard"
)

type CleanupOperation struct {
	AffectedForReview []string `json:"affected_for_review,omitempty" jsonschema:"Computed by the program for supersession; dependent interpretations to recheck, never silently confirmed by undo"`
	EntryID           string   `json:"entry_id"`
	Action            string   `json:"action" jsonschema:"archive, supersede, distill or recheck; never permanently deletes evidence"`
	ReplacementID     string   `json:"replacement_id,omitempty" jsonschema:"Current retained entry for supersede; full replacement only, not partial corrections"`
	Summary           string   `json:"summary,omitempty" jsonschema:"distill only: concise current conclusion WITH applicability, reasons, exceptions and unknowns"`
	Reason            string   `json:"reason"`
	EvidenceIDs       []string `json:"evidence_ids" jsonschema:"Actually read evidence supporting this exact operation; references are preserved"`
}
type CleanupPreview struct {
	Scope      string             `json:"scope"`
	Operations []CleanupOperation `json:"operations"`
}
type cleanupSnapshot struct {
	ID          string `json:"id"`
	Scope       string `json:"scope"`
	Version     int    `json:"management_version"`
	NeedsReview bool   `json:"needs_review"`
}
type CleanupPlan struct {
	DecidedAt        string                     `json:"decided_at,omitempty"`
	ID               string                     `json:"id"`
	Scope            string                     `json:"scope"`
	Operations       []CleanupOperation         `json:"operations"`
	Snapshots        map[string]cleanupSnapshot `json:"snapshots"`
	Before           map[string]ManagedState    `json:"before"`
	Status           string                     `json:"status"`
	CreatedAt        string                     `json:"created_at"`
	ApprovalSourceID string                     `json:"approval_source_id,omitempty"`
	ApprovalQuote    string                     `json:"approval_quote,omitempty"`
	BackupPath       string                     `json:"backup_path,omitempty"`
}
type CleanupDecision struct {
	PlanID           string `json:"plan_id"`
	ApprovalSourceID string `json:"approval_source_id" jsonschema:"New actual user message approving the displayed exact plan; upgrade/install permission is not cleanup permission"`
	ApprovalQuote    string `json:"approval_quote" jsonschema:"Exact nonblank approval excerpt from approval_source_id; host must establish its meaning and ask before applying"`
}

func (s *Store) PreviewCleanup(ctx context.Context, in CleanupPreview) (CleanupPlan, error) {
	p := CleanupPlan{ID: newID(), Scope: in.Scope, Operations: in.Operations, Snapshots: map[string]cleanupSnapshot{}, Before: map[string]ManagedState{}, Status: "preview", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := validScope(in.Scope); err != nil {
		return p, err
	}
	if len(in.Operations) < 1 || len(in.Operations) > 50 {
		return p, errors.New("preview needs 1..50 operations")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	capture := func(id string) (Entry, error) {
		e, er := scanEntry(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM entries WHERE id=?", id))
		if er != nil {
			return e, er
		}
		if e.Scope != in.Scope || !e.Current || strings.HasPrefix(e.SourceRef, HookSourcePrefix) {
			return e, errors.New("cleanup targets must be current non-event entries in the same scope")
		}
		m, er := stateFor(ctx, tx, e.RootID)
		if er != nil {
			return e, er
		}
		p.Snapshots[e.RootID] = cleanupSnapshot{e.ID, e.Scope, m.Version, e.NeedsReview}
		return e, nil
	}
	seen := map[string]bool{}
	reviewed := map[string]bool{}
	for opIndex := range in.Operations {
		op := in.Operations[opIndex]
		op.AffectedForReview = nil
		if err = textguard.Check("cleanup.reason", op.Reason, false); err != nil {
			return p, err
		}
		if strings.TrimSpace(op.Reason) == "" || len(op.Reason) > 4096 || len(op.EvidenceIDs) == 0 || len(op.EvidenceIDs) > 32 {
			return p, errors.New("cleanup needs a concrete reason and 1..32 evidence IDs")
		}
		e, er := capture(op.EntryID)
		if er != nil {
			return p, er
		}
		if seen[e.RootID] {
			return p, errors.New("one operation per lineage per plan")
		}
		seen[e.RootID] = true
		m, er := stateFor(ctx, tx, e.RootID)
		if er != nil {
			return p, er
		}
		p.Before[e.RootID] = m
		switch op.Action {
		case "archive":
			if op.ReplacementID != "" || op.Summary != "" {
				return p, errors.New("archive cannot carry replacement or summary")
			}
		case "supersede":
			target, er := capture(op.ReplacementID)
			if er != nil {
				return p, er
			}
			if target.RootID == e.RootID {
				return p, errors.New("cannot replace an entry with its own revision")
			}
			if target.NeedsReview {
				return p, errors.New("replacement needs source review")
			}
			tm, er := stateFor(ctx, tx, target.RootID)
			if er != nil {
				return p, er
			}
			if tm.State != "active" {
				return p, errors.New("replacement must be active")
			}
			if op.Summary != "" {
				return p, errors.New("supersede cannot rewrite retained content; revise it first if needed")
			}
			rows, er := tx.QueryContext(ctx, `WITH RECURSIVE affected(id) AS (SELECT ? UNION SELECT l.entry_id FROM links l JOIN affected a ON l.target_id=a.id UNION SELECT r.entry_id FROM reviewed_sources r JOIN affected a ON r.source_id=a.id) SELECT e.id,e.root_id,e.needs_review FROM entries e WHERE e.id IN (SELECT id FROM affected) AND e.current=1 AND e.kind IN ('case','hypothesis') AND e.root_id<>? AND e.root_id<>?`, e.ID, e.RootID, target.RootID)
			if er != nil {
				return p, er
			}
			type dependent struct {
				id, root string
				needs    bool
			}
			deps := []dependent{}
			for rows.Next() {
				var d dependent
				if er = rows.Scan(&d.id, &d.root, &d.needs); er != nil {
					rows.Close()
					return p, er
				}
				deps = append(deps, d)
			}
			er = rows.Err()
			rows.Close()
			if er != nil {
				return p, er
			}
			for _, d := range deps {
				m, er := stateFor(ctx, tx, d.root)
				if er != nil {
					return p, er
				}
				p.Snapshots[d.root] = cleanupSnapshot{d.id, in.Scope, m.Version, d.needs}
				op.AffectedForReview = append(op.AffectedForReview, d.id)
			}
		case "distill":
			if op.ReplacementID != "" || strings.TrimSpace(op.Summary) == "" || len([]rune(op.Summary)) > 1600 {
				return p, errors.New("distill needs a summary of at most 1600 characters")
			}
			if err = textguard.Check("cleanup.summary", op.Summary, false); err != nil {
				return p, err
			}
		case "recheck":
			if op.ReplacementID != "" || op.Summary != "" {
				return p, errors.New("source recheck does not rewrite or replace content")
			}
			if _, er = reviewSources(ctx, tx, e, op.EvidenceIDs, reviewed); er != nil {
				return p, er
			}
			reviewed[e.ID] = true
		default:
			return p, errors.New("unknown cleanup action")
		}
		for _, id := range op.EvidenceIDs {
			evidence, er := scanEntry(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM entries WHERE id=?", id))
			if er != nil {
				return p, er
			}
			if evidence.Scope != in.Scope && evidence.Scope != "global" {
				return p, errors.New("cleanup evidence outside scope")
			}
			var current string
			var nr bool
			if er = tx.QueryRowContext(ctx, "SELECT id,needs_review FROM entries WHERE root_id=? AND current=1", evidence.RootID).Scan(&current, &nr); er != nil {
				return p, er
			}
			em, er := stateFor(ctx, tx, evidence.RootID)
			if er != nil {
				return p, er
			}
			p.Snapshots[evidence.RootID] = cleanupSnapshot{current, evidence.Scope, em.Version, nr}
		}
		p.Operations[opIndex] = op
	}
	// Forbid chains within a batch. Replacement targets remain active throughout.
	for _, op := range in.Operations {
		if op.Action == "supersede" {
			var root string
			if err = tx.QueryRowContext(ctx, "SELECT root_id FROM entries WHERE id=?", op.ReplacementID).Scan(&root); err != nil {
				return p, err
			}
			if seen[root] {
				return p, errors.New("replacement cannot also be changed in the same plan")
			}
		}
	}
	ops, _ := json.Marshal(p.Operations)
	snaps, _ := json.Marshal(p.Snapshots)
	before, _ := json.Marshal(p.Before)
	if _, err = tx.ExecContext(ctx, "UPDATE cleanup_plans SET status='replaced' WHERE scope=? AND status='preview'", p.Scope); err != nil {
		return p, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO cleanup_plans(id,scope,operations_json,snapshots_json,before_json,created_at)VALUES(?,?,?,?,?,?)", p.ID, p.Scope, string(ops), string(snaps), string(before), p.CreatedAt); err != nil {
		return p, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE maintenance_policy SET notified_at=changes,initial_due=0 WHERE scope=?", in.Scope); err != nil {
		return p, err
	}
	return p, tx.Commit()
}
func loadPlan(ctx context.Context, q queryer, id string) (CleanupPlan, error) {
	p := CleanupPlan{ID: id}
	var ops, snaps, before string
	err := q.QueryRowContext(ctx, "SELECT scope,operations_json,snapshots_json,before_json,status,created_at,approval_source_id,approval_quote,backup_path,decided_at FROM cleanup_plans WHERE id=?", id).Scan(&p.Scope, &ops, &snaps, &before, &p.Status, &p.CreatedAt, &p.ApprovalSourceID, &p.ApprovalQuote, &p.BackupPath, &p.DecidedAt)
	if err != nil {
		return p, err
	}
	for _, v := range []struct {
		s   string
		dst any
	}{{ops, &p.Operations}, {snaps, &p.Snapshots}, {before, &p.Before}} {
		if err = json.Unmarshal([]byte(v.s), v.dst); err != nil {
			return p, err
		}
	}
	return p, nil
}
func (s *Store) CleanupPlan(ctx context.Context, id string) (CleanupPlan, error) {
	return loadPlan(ctx, s.db, id)
}
func checkPlanSnapshot(ctx context.Context, tx *sql.Tx, p CleanupPlan, undo bool) error {
	for root, snap := range p.Snapshots {
		var id string
		var nr bool
		if err := tx.QueryRowContext(ctx, "SELECT id,needs_review FROM entries WHERE root_id=? AND current=1", root).Scan(&id, &nr); err != nil {
			return err
		}
		m, err := stateFor(ctx, tx, root)
		if err != nil {
			return err
		}
		want := snap.Version
		if undo {
			if _, changed := p.Before[root]; changed {
				want++
			}
		}
		expectedReview := snap.NeedsReview
		if undo {
			for _, op := range p.Operations {
				if op.EntryID == id && op.Action == "recheck" {
					expectedReview = false
				}
			}
		}
		if undo {
			for _, op := range p.Operations {
				for _, affected := range op.AffectedForReview {
					if affected == id {
						expectedReview = true
					}
				}
			}
		}
		if id != snap.ID || m.Version != want || nr != expectedReview {
			return errors.New("cleanup conflict: content, evidence or management changed; create a new preview")
		}
	}
	return nil
}
func (s *Store) DecideCleanup(ctx context.Context, in CleanupDecision, undo bool) (CleanupPlan, error) {
	p, err := s.CleanupPlan(ctx, in.PlanID)
	if err != nil {
		return p, err
	}
	desired := "applied"
	expected := "preview"
	if undo {
		desired = "undone"
		expected = "applied"
	}
	// A retry of the exact approved operation is idempotent.
	if p.Status == desired && p.ApprovalSourceID == in.ApprovalSourceID && p.ApprovalQuote == in.ApprovalQuote {
		return p, nil
	}
	if p.Status != expected {
		return p, errors.New("plan is not in the required state")
	}
	if strings.TrimSpace(in.ApprovalQuote) == "" || len(in.ApprovalQuote) > 8192 {
		return p, errors.New("exact user approval excerpt required")
	}
	source, err := userSource(ctx, s.db, in.ApprovalSourceID, p.Scope)
	if err != nil {
		return p, err
	}
	after := p.CreatedAt
	if undo {
		after = p.DecidedAt
	}
	if source.CreatedAt <= after || !strings.Contains(source.Body, in.ApprovalQuote) {
		return p, errors.New("approval must quote a new user message after the preview; ask the user to approve this plan")
	}
	// Provenance checks cannot establish the meaning of an approval. The host is
	// responsible for showing the plan and interpreting an explicit user response.
	if err = os.MkdirAll(filepath.Join(s.Dir, "backups"), 0700); err != nil {
		return p, err
	}
	backup := filepath.Join(s.Dir, "backups", "cleanup-"+p.ID+"-"+newID()+".db")
	if err = s.Backup(ctx, backup); err != nil {
		return p, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	current, err := loadPlan(ctx, tx, p.ID)
	if err != nil {
		return p, err
	}
	if current.Status != expected {
		return p, errors.New("cleanup plan changed concurrently")
	}
	if err = checkPlanSnapshot(ctx, tx, p, undo); err != nil {
		return p, err
	}
	if _, err = userSource(ctx, tx, in.ApprovalSourceID, p.Scope); err != nil {
		return p, err
	}
	for _, op := range p.Operations {
		var root string
		if err = tx.QueryRowContext(ctx, "SELECT root_id FROM entries WHERE id=?", op.EntryID).Scan(&root); err != nil {
			return p, err
		}
		m := p.Before[root]
		if !undo {
			switch op.Action {
			case "archive":
				m.State = "archived"
				m.ReplacementRootID = ""
			case "supersede":
				m.State = "superseded"
				if err = tx.QueryRowContext(ctx, "SELECT root_id FROM entries WHERE id=?", op.ReplacementID).Scan(&m.ReplacementRootID); err != nil {
					return p, err
				}
			case "distill":
				m.DisplaySummary = op.Summary
				m.SummaryRevisionID = op.EntryID
			case "recheck":
				if err = clearReviewedTx(ctx, tx, p.Scope, op.EntryID, op.EvidenceIDs, op.Reason); err != nil {
					return p, err
				}
			}
			m.Reason = op.Reason
		}
		// Version keeps increasing even on undo; stale previews can never become valid again.
		v := p.Snapshots[root].Version + 1
		if undo {
			v++
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO managed_entries(root_id,state,replacement_root_id,display_summary,summary_revision_id,reason,version)VALUES(?,?,?,?,?,?,?) ON CONFLICT(root_id)DO UPDATE SET state=excluded.state,replacement_root_id=excluded.replacement_root_id,display_summary=excluded.display_summary,summary_revision_id=excluded.summary_revision_id,reason=excluded.reason,version=excluded.version`, root, m.State, m.ReplacementRootID, m.DisplaySummary, m.SummaryRevisionID, m.Reason, v)
		if err != nil {
			return p, err
		}
		if !undo {
			for _, id := range op.AffectedForReview {
				if _, err = tx.ExecContext(ctx, "UPDATE entries SET needs_review=1 WHERE id=? AND current=1", id); err != nil {
					return p, err
				}
			}
		}
		if undo && op.Action == "recheck" {
			if _, err = tx.ExecContext(ctx, "UPDATE entries SET needs_review=? WHERE id=?", p.Snapshots[root].NeedsReview, op.EntryID); err != nil {
				return p, err
			}
		}
	}
	_, err = tx.ExecContext(ctx, "UPDATE cleanup_plans SET status=?,approval_source_id=?,approval_quote=?,backup_path=?,decided_at=? WHERE id=?", desired, in.ApprovalSourceID, in.ApprovalQuote, backup, time.Now().UTC().Format(time.RFC3339Nano), p.ID)
	if err != nil {
		return p, err
	}
	if !undo {
		_, err = tx.ExecContext(ctx, "UPDATE maintenance_policy SET next_at=changes+interval,initial_due=0,notified_at=-1,enabled=CASE WHEN repeating=0 THEN 0 ELSE enabled END WHERE scope=?", p.Scope)
		if err != nil {
			return p, err
		}
	}
	if err = tx.Commit(); err != nil {
		return p, err
	}
	return s.CleanupPlan(ctx, p.ID)
}

type CleanupInventory struct {
	PendingPlans     []CleanupPlan     `json:"pending_plans"`
	Maintenance      MaintenanceStatus `json:"maintenance"`
	Items            []InventoryItem   `json:"items"`
	NextCursor       string            `json:"next_cursor,omitempty"`
	Reviews          []Hit             `json:"needs_review"`
	ReviewNextCursor string            `json:"review_next_cursor,omitempty"`
	Counts           map[string]int    `json:"counts"`
	Notice           string            `json:"notice"`
}

func (s *Store) CleanupInventory(ctx context.Context, scope, cursor string) (CleanupInventory, error) {
	o := CleanupInventory{Notice: "Read-only candidates, not verdicts. Read full content and sources; show a concrete plan and obtain fresh user approval before applying. Age, length and low use alone do not justify removal."}
	var err error
	o.Maintenance, err = s.Maintenance(ctx, scope)
	if err != nil {
		return o, err
	}
	o.Items, o.NextCursor, err = s.Inventory(ctx, scope, cursor, 30)
	if err != nil {
		return o, err
	}
	yes := true
	p, err := s.SearchPage(ctx, SearchInput{Scope: scope, NeedsReview: &yes, Limit: 30})
	if err != nil {
		return o, err
	}
	o.Reviews = p.Results
	o.ReviewNextCursor = p.NextCursor
	o.Counts = map[string]int{}
	planRows, er := s.db.QueryContext(ctx, "SELECT id FROM cleanup_plans WHERE scope=? AND status='preview' ORDER BY created_at DESC LIMIT 10", scope)
	if er != nil {
		return o, er
	}
	ids := []string{}
	for planRows.Next() {
		var id string
		if er = planRows.Scan(&id); er != nil {
			planRows.Close()
			return o, er
		}
		ids = append(ids, id)
	}
	er = planRows.Err()
	planRows.Close()
	if er != nil {
		return o, er
	}
	o.PendingPlans = []CleanupPlan{}
	for _, id := range ids {
		p, er := s.CleanupPlan(ctx, id)
		if er != nil {
			return o, er
		}
		o.PendingPlans = append(o.PendingPlans, p)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT CASE WHEN source_ref LIKE 'hook-event:%' THEN 'raw_events' WHEN current=0 THEN 'historical_revisions' ELSE kind END,count(*) FROM entries WHERE scope=? GROUP BY 1`, scope)
	if err != nil {
		return o, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		if err = rows.Scan(&k, &n); err != nil {
			return o, err
		}
		o.Counts[k] = n
	}
	return o, rows.Err()
}

func (s *Store) ClearReviewed(ctx context.Context, scope, id string, sources []string, note string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = clearReviewedTx(ctx, tx, scope, id, sources, note); err != nil {
		return err
	}
	return tx.Commit()
}
func clearReviewedTx(ctx context.Context, tx *sql.Tx, scope, id string, sources []string, note string) error {
	if strings.TrimSpace(note) == "" {
		return errors.New("source recheck explanation required")
	}
	if err := textguard.Check("source recheck note", note, false); err != nil {
		return err
	}
	e, err := scanEntry(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM entries WHERE id=?", id))
	if err != nil {
		return err
	}
	if e.Scope != scope || !e.Current {
		return errors.New("review target must be current in this scope")
	}
	sources, err = reviewSources(ctx, tx, e, sources, nil)
	if err != nil {
		return err
	}
	// A management plan provides an audit trail without creating another case.
	payload, _ := json.Marshal(sources)
	_, err = tx.ExecContext(ctx, "INSERT INTO source_review_receipts(id,entry_id,sources_json,note,created_at)VALUES(?,?,?,?,?)", newID(), id, string(payload), note, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM reviewed_sources WHERE entry_id=?", id); err != nil {
		return err
	}
	for _, source := range sources {
		if _, err = tx.ExecContext(ctx, "INSERT INTO reviewed_sources(entry_id,source_id)VALUES(?,?)", id, source); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, "UPDATE entries SET needs_review=0 WHERE id=? AND current=1", id)
	if err != nil {
		return err
	}
	return nil
}
