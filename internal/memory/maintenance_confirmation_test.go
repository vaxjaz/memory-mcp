package memory

import "testing"

func TestMaintenanceConfirmationIsHiddenBoundToPlanAndReversible(t *testing.T) {
	s := testStore(t)
	raw := put(t, s, recordDraft(), "")
	draft := usefulCase(raw)
	current := put(t, s, draft, "")
	preview, err := s.PreviewCleanup(ctx, CleanupPreview{Scope: raw.Scope, Operations: []CleanupOperation{{EntryID: raw.ID, Action: "supersede", ReplacementID: current.ID, Reason: "isolated exact replacement", EvidenceIDs: []string{raw.ID, current.ID}}}})
	if err != nil {
		t.Fatal(err)
	}
	quote := "Approve this displayed replacement plan only"
	id, err := s.ConfirmMaintenance(ctx, raw.Scope, preview.ID, "apply", quote, "fixture-consent")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.ConfirmMaintenance(ctx, raw.Scope, preview.ID, "apply", quote, "fixture-consent")
	if err != nil || again != id {
		t.Fatal("confirmation retry duplicated audit", again, err)
	}
	hits, err := s.SearchPage(ctx, SearchInput{Scope: raw.Scope, Query: "displayed", IncludeEvents: true})
	if err != nil || len(hits.Results) != 0 {
		t.Fatal("maintenance excerpt leaked into memory", hits, err)
	}
	if _, err = s.ConfirmMaintenance(ctx, raw.Scope, preview.ID, "apply", "changed quote", "fixture-consent"); err == nil {
		t.Fatal("accepted changed retry")
	}
	if _, err = s.ConfigureMaintenance(ctx, MaintenanceConfig{Scope: raw.Scope, Enabled: true, Interval: 10, Repeating: true, SourceID: id}); err == nil {
		t.Fatal("cleanup approval reused for schedule")
	}
	applied, err := s.DecideCleanup(ctx, CleanupDecision{PlanID: preview.ID, ApprovalSourceID: id, ApprovalQuote: quote}, false)
	if err != nil || applied.Status != "applied" {
		t.Fatal(applied, err)
	}
	if _, err = s.DecideCleanup(ctx, CleanupDecision{PlanID: preview.ID, ApprovalSourceID: id, ApprovalQuote: quote}, true); err == nil {
		t.Fatal("apply approval reused for undo")
	}
	undoID, err := s.ConfirmMaintenance(ctx, raw.Scope, preview.ID, "undo", "Undo this specific displayed replacement", "fixture-undo")
	if err != nil {
		t.Fatal(err)
	}
	undone, err := s.DecideCleanup(ctx, CleanupDecision{PlanID: preview.ID, ApprovalSourceID: undoID, ApprovalQuote: "Undo this specific displayed replacement"}, true)
	if err != nil || undone.Status != "undone" {
		t.Fatal(undone, err)
	}
	after, _ := s.Read(ctx, raw.ID)
	if after.Entry.State.State != "active" {
		t.Fatal("undo lost original source", after.Entry.State)
	}
}
