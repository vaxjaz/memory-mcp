package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// A citation describes what was discussed, not whether its contents are trusted.
// Only supports is a positive dependency; valid context can establish provenance
// for existing clients, but stale/background targets alone never ground a claim.
func validGround(ctx context.Context, q queryer, e Entry) (bool, error) {
	m, err := stateFor(ctx, q, e.RootID)
	return e.Current && !e.NeedsReview && m.State == "active" && e.Kind != "hypothesis", err
}
func requireSupport(ctx context.Context, q queryer, e Entry) error {
	m, err := stateFor(ctx, q, e.RootID)
	if err != nil {
		return err
	}
	if !e.Current || e.NeedsReview || m.State != "active" {
		var current string
		if err = q.QueryRowContext(ctx, "SELECT id FROM entries WHERE root_id=? AND current=1", e.RootID).Scan(&current); err != nil {
			return err
		}
		return fmt.Errorf("supports source %s is unavailable (current=%t, needs review=%t, state=%s; current revision=%s); revise/recheck this upstream source first or use fresh evidence. For a correction ABOUT this item use contradicts/context, which does not validate it", e.ID, e.Current, e.NeedsReview, m.State, current)
	}
	return nil
}

// reviewed is a read-only preview of earlier rechecks in this SAME ordered plan.
// Apply validates again against transactional state; no preview clears a flag.
func reviewSources(ctx context.Context, tx *sql.Tx, e Entry, sources []string, reviewed map[string]bool) ([]string, error) {
	if err := loadGovernance(ctx, tx, &e); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT target_id,relation FROM links WHERE entry_id=? ORDER BY target_id,relation", e.ID)
	if err != nil {
		return nil, err
	}
	links := []Link{}
	for rows.Next() {
		var l Link
		if err = rows.Scan(&l.TargetID, &l.Relation); err != nil {
			rows.Close()
			return nil, err
		}
		links = append(links, l)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	expected := map[string]bool{}
	grounded := false
	for _, l := range links {
		source, er := resolveCurrent(ctx, tx, l.TargetID)
		if er != nil {
			return nil, fmt.Errorf("recheck target %s cannot resolve linked source %s: %w", e.ID, l.TargetID, er)
		}
		if reviewed[source.ID] {
			source.NeedsReview = false
		}
		if l.Relation == "supports" {
			if source.RootID == e.RootID {
				return nil, fmt.Errorf("recheck target %s resolves its supporting source %s back to itself; revise with independent evidence", e.ID, l.TargetID)
			}
			if er = requireSupport(ctx, tx, source); er != nil {
				return nil, fmt.Errorf("recheck target %s blocked by upstream source: %w", e.ID, er)
			}
		}
		if l.Relation != "contradicts" && source.RootID != e.RootID {
			valid, er := validGround(ctx, tx, source)
			if er != nil {
				return nil, er
			}
			grounded = grounded || valid
		}
		expected[source.ID] = true
	}
	if (e.Kind == "hypothesis" || e.Kind == "case") && !grounded {
		return nil, fmt.Errorf("recheck target %s has no current active reviewed record/case grounding; revise it with fresh evidence instead of clearing its warning", e.ID)
	}
	supplied := map[string]bool{}
	for _, id := range sources {
		supplied[id] = true
	}
	missing, extra := []string{}, []string{}
	for id := range expected {
		if !supplied[id] {
			missing = append(missing, id)
		}
	}
	for id := range supplied {
		if !expected[id] {
			extra = append(extra, id)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		return nil, fmt.Errorf("recheck target %s must acknowledge every linked source at its effective current version; missing=[%s], unexpected=[%s]. Do not submit the review target itself as evidence unless it is actually linked", e.ID, strings.Join(missing, ","), strings.Join(extra, ","))
	}
	if len(expected) == 0 {
		return nil, errors.New("source recheck needs linked evidence; revise with fresh evidence instead")
	}
	out := []string{}
	for id := range expected {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}
