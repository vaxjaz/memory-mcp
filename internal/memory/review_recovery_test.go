package memory

import (
	"strings"
	"testing"
)

func staleCase(t *testing.T, s *Store) (Entry, Entry) {
	t.Helper()
	original := put(t, s, recordDraft(), "")
	c := put(t, s, usefulCase(original), "")
	updated := recordDraft()
	updated.Body = "Corrected original evidence"
	source := put(t, s, updated, original.ID)
	return c, source
}
func reviewOp(e Entry, ids ...string) CleanupOperation {
	return CleanupOperation{EntryID: e.ID, Action: "recheck", Reason: "Read current evidence, compare conclusion and exceptions", EvidenceIDs: ids}
}
func applyReview(t *testing.T, s *Store, p CleanupPlan) {
	t.Helper()
	yes := approval(t, s, p.Scope, "Apply this exact displayed source-review plan")
	if _, err := s.DecideCleanup(ctx, CleanupDecision{p.ID, yes.ID, yes.Body}, false); err != nil {
		t.Fatal(err)
	}
}

func TestCorrectionReferencesDoNotAdoptStaleEvidence(t *testing.T) {
	s := testStore(t)
	target, current := staleCase(t, s)
	for _, relation := range []string{"context", "contradicts"} {
		d := recordDraft()
		d.Body = "Actual correction about the flagged item"
		d.Links = []Link{{target.ID, relation}}
		correction := put(t, s, d, "")
		if correction.NeedsReview {
			t.Fatal("fresh actual correction inherited target warning")
		}
		old, _ := s.Read(ctx, target.ID)
		if !old.Entry.NeedsReview {
			t.Fatal("citation cleared target warning")
		}
	}
	d := recordDraft()
	d.Links = []Link{{target.ID, "supports"}}
	if _, err := s.Put(ctx, d, ""); err == nil || !strings.Contains(err.Error(), target.ID) {
		t.Fatal("stale support accepted or blocking ID missing", err)
	}
	// A correction may name the exact historical wording, without replacing it.
	oldSource, _ := s.Read(ctx, target.ID)
	historical := oldSource.Entry.Links[0].TargetID
	for _, relation := range []string{"context", "contradicts"} {
		d.Links = []Link{{historical, relation}}
		put(t, s, d, "")
	}
	d.Links = []Link{{historical, "supports"}}
	if _, err := s.Put(ctx, d, ""); err == nil || !strings.Contains(err.Error(), current.ID) {
		t.Fatal("historical support accepted or current version omitted", err)
	}
}

func TestStaleReferencesCannotLaunderAConclusion(t *testing.T) {
	s := testStore(t)
	target, _ := staleCase(t, s)
	for _, kind := range []string{"case", "hypothesis"} {
		for _, relation := range []string{"context", "contradicts", "supports"} {
			d := usefulCase(target)
			d.Kind = kind
			d.Links = []Link{{target.ID, relation}}
			if _, err := s.Put(ctx, d, ""); err == nil {
				t.Fatalf("%s with only stale %s became valid", kind, relation)
			}
		}
	}
	fresh := put(t, s, recordDraft(), "")
	d := usefulCase(fresh)
	d.Links = append(d.Links, Link{target.ID, "contradicts"})
	correction := put(t, s, d, "")
	if correction.NeedsReview {
		t.Fatal("fresh evidence could not ground correction")
	}
	// The correction itself still needs independent provenance, even if the item
	// being contradicted is clean. A contradiction target is not corroboration.
	d.Links = []Link{{fresh.ID, "contradicts"}}
	if _, err := s.Put(ctx, d, ""); err == nil {
		t.Fatal("opposing target treated as corroboration")
	}
}

func TestFlaggedCaseCanBeRevisedWithoutClearingFirst(t *testing.T) {
	s := testStore(t)
	target, current := staleCase(t, s)
	d := usefulCase(current)
	d.Body = "Corrected conclusion and scope"
	d.Links = append(d.Links, Link{target.ID, "context"})
	next := put(t, s, d, target.ID)
	if next.NeedsReview || next.PreviousID != target.ID {
		t.Fatal(next)
	}
	old, _ := s.Read(ctx, target.ID)
	if old.Entry.Current || old.Entry.Body != target.Body || !old.Entry.NeedsReview {
		t.Fatal("history or warning rewritten")
	}
}

func TestReviewPreviewNamesUpstreamBlockerBeforeApproval(t *testing.T) {
	s := testStore(t)
	upstream, current := staleCase(t, s)
	// Build a dependent while clean, then invalidate the upstream again.
	fixed := put(t, s, usefulCase(current), upstream.ID)
	dependent := put(t, s, usefulCase(fixed), "")
	changed := recordDraft()
	changed.Body = "Another evidence correction"
	put(t, s, changed, current.ID)
	_, err := s.PreviewCleanup(ctx, CleanupPreview{Scope: fixed.Scope, Operations: []CleanupOperation{reviewOp(dependent, fixed.ID)}})
	if err == nil || !strings.Contains(err.Error(), fixed.ID) || !strings.Contains(err.Error(), dependent.ID) {
		t.Fatal("preview failed to identify target/upstream", err)
	}
	var plans int
	if er := s.db.QueryRow("SELECT count(*) FROM cleanup_plans").Scan(&plans); er != nil || plans != 0 {
		t.Fatal("impossible plan persisted", plans, er)
	}
}

func TestReviewAcknowledgesStaleBackgroundWithoutTrustingIt(t *testing.T) {
	s := testStore(t)
	stale, _ := staleCase(t, s)
	original := put(t, s, recordDraft(), "")
	d := usefulCase(original)
	d.Links = append(d.Links, Link{stale.ID, "context"}, Link{stale.ID, "contradicts"})
	target := put(t, s, d, "")
	changed := recordDraft()
	changed.Body = "Source updated, applicability unchanged"
	current := put(t, s, changed, original.ID)
	p, err := s.PreviewCleanup(ctx, CleanupPreview{Scope: target.Scope, Operations: []CleanupOperation{reviewOp(target, current.ID, stale.ID)}})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.Read(ctx, target.ID)
	if !before.Entry.NeedsReview {
		t.Fatal("preview changed target")
	}
	applyReview(t, s, p)
	reviewed, _ := s.Read(ctx, target.ID)
	background, _ := s.Read(ctx, stale.ID)
	if reviewed.Entry.NeedsReview || !background.Entry.NeedsReview {
		t.Fatal("target failed to recover or background got validated")
	}
	// Positive reuse remains blocked; reading/acknowledging is not certification.
	if _, err = s.Put(ctx, usefulCase(stale), ""); err == nil {
		t.Fatal("acknowledgment validated stale support")
	}
	changed.Body = "Yet another source update"
	put(t, s, changed, current.ID)
	reviewed, _ = s.Read(ctx, target.ID)
	if !reviewed.Entry.NeedsReview {
		t.Fatal("future invalidation was lost")
	}
}

func TestOrderedReviewBatchRecoversChainAtomically(t *testing.T) {
	s := testStore(t)
	raw := put(t, s, recordDraft(), "")
	up := put(t, s, usefulCase(raw), "")
	down := put(t, s, usefulCase(up), "")
	change := recordDraft()
	change.Body = "Current evidence"
	current := put(t, s, change, raw.ID)
	reverse := CleanupPreview{Scope: raw.Scope, Operations: []CleanupOperation{reviewOp(down, up.ID), reviewOp(up, current.ID)}}
	if _, err := s.PreviewCleanup(ctx, reverse); err == nil || !strings.Contains(err.Error(), up.ID) {
		t.Fatal("reverse order accepted or unclear error", err)
	}
	p, err := s.PreviewCleanup(ctx, CleanupPreview{Scope: raw.Scope, Operations: []CleanupOperation{reviewOp(up, current.ID), reviewOp(down, up.ID)}})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{up.ID, down.ID} {
		r, _ := s.Read(ctx, id)
		if !r.Entry.NeedsReview {
			t.Fatal("preview applied a recheck")
		}
	}
	applyReview(t, s, p)
	for _, id := range []string{up.ID, down.ID} {
		r, _ := s.Read(ctx, id)
		if r.Entry.NeedsReview {
			t.Fatal("chain remained blocked", id)
		}
	}
}

func TestRecheckCannotBlessOnlyStaleBackground(t *testing.T) {
	s := testStore(t)
	target, current := staleCase(t, s)
	// Model an admitted legacy case whose only provenance is now pending review.
	d := usefulCase(current)
	d.Profile = nil
	d.Links = []Link{{target.ID, "context"}}
	legacy := put(t, s, d, "")
	profile := usefulProfile()
	legacyDraft := d
	legacyDraft.Profile = profile
	if _, err := s.Put(ctx, legacyDraft, legacy.ID); err == nil {
		t.Fatal("stale-only revision became valid")
	}
	// Legacy cases without profiles need fresh grounding for recheck too; an
	// older schema must not turn a stale background citation into valid evidence.
	if _, err := s.PreviewCleanup(ctx, CleanupPreview{Scope: legacy.Scope, Operations: []CleanupOperation{reviewOp(legacy, target.ID)}}); err == nil || !strings.Contains(err.Error(), "fresh evidence") {
		t.Fatal("stale-only review became valid", err)
	}
}

func TestReviewVersionMismatchListsRequiredCurrentIDs(t *testing.T) {
	s := testStore(t)
	target, current := staleCase(t, s)
	_, err := s.PreviewCleanup(ctx, CleanupPreview{Scope: target.Scope, Operations: []CleanupOperation{reviewOp(target, target.ID)}})
	if err == nil || !strings.Contains(err.Error(), "missing=["+current.ID) || !strings.Contains(err.Error(), "unexpected=["+target.ID) {
		t.Fatal("missing actionable version diff", err)
	}
}
