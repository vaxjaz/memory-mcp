package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func usefulProfile() *Profile {
	return &Profile{Class: "long_term", FutureUse: "Next test-environment login: supply explicit arguments to avoid a configuration-dependent hang", CounterCheck: "Verified reusable failure, not a one-off status report; compare existing login case before saving", Applicability: "This project and the tested login tool/environment only", Boundary: "Do not generalize to other clients; recheck after configuration changes", SearchHints: []string{"token request hangs", "login without arguments"}}
}
func usefulCase(source Entry) Draft {
	return Draft{Kind: "case", Scope: source.Scope, Title: "Test environment login arguments", Body: "Use explicit arguments in this environment. A no-argument call waited on missing environment configuration; an explicit call succeeded. Other environments are unknown.", Origin: "model_inference", Links: []Link{{source.ID, "supports"}}, Profile: usefulProfile()}
}
func approval(t *testing.T, s *Store, scope, body string) Entry {
	t.Helper()
	d := recordDraft()
	d.Scope = scope
	d.Body = body
	return put(t, s, d, "")
}

func TestAdmissionRequiresFutureUseCounterChallengeAndRealSources(t *testing.T) {
	source := Entry{ID: "source", Scope: "project:demo"}
	d := usefulCase(source)
	if err := ValidateAdmission(d); err != nil {
		t.Fatal(err)
	}
	d.Profile = nil
	if err := ValidateAdmission(d); err == nil {
		t.Fatal("accepted missing admission")
	}
	d = usefulCase(source)
	d.Profile.CounterCheck = ""
	if err := ValidateAdmission(d); err == nil {
		t.Fatal("accepted no reverse challenge")
	}
	d = usefulCase(source)
	d.Links = nil
	if err := ValidateAdmission(d); err == nil {
		t.Fatal("accepted ungrounded case")
	}
	d = usefulCase(source)
	d.Profile.Boundary = "bad\ufffdtext"
	if err := ValidateAdmission(d); err == nil {
		t.Fatal("profile bypassed text guard")
	}
}

func TestProfilesAreSearchableAndRetryDoesNotInflateMaintenanceCount(t *testing.T) {
	s := testStore(t)
	raw := put(t, s, recordDraft(), "")
	d := usefulCase(raw)
	d.RequestID = "one-login-decision"
	e := put(t, s, d, "")
	again := put(t, s, d, "")
	if e.ID != again.ID {
		t.Fatal("retry created another case")
	}
	m, err := s.Maintenance(ctx, raw.Scope)
	if err != nil || m.Changes != 1 {
		t.Fatal(m, err)
	}
	hits, err := s.Search(ctx, SearchInput{Scope: raw.Scope, Query: "token request"})
	if err != nil || len(hits) != 1 || hits[0].ID != e.ID {
		t.Fatal(hits, err)
	}
	r, err := s.Read(ctx, e.ID)
	if err != nil || r.Entry.Profile.FutureUse != d.Profile.FutureUse {
		t.Fatal(r, err)
	}
	ex, err := s.Export(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range ex.Entries {
		if x.ID == e.ID && x.Profile == nil {
			t.Fatal("export lost profile")
		}
	}
}

func TestMaintenanceIsPerProjectPersistentAndNotRawEventBased(t *testing.T) {
	s := testStore(t)
	raw := approval(t, s, "project:demo", "After two case changes, show one cleanup preview")
	m, err := s.ConfigureMaintenance(ctx, MaintenanceConfig{Scope: raw.Scope, Interval: 2, Enabled: true, Repeating: false, SourceID: raw.ID})
	if err != nil || m.Due {
		t.Fatal(m, err)
	}
	for i := 0; i < 5; i++ {
		put(t, s, recordDraft(), "")
	}
	m, _ = s.Maintenance(ctx, raw.Scope)
	if m.Changes != 0 {
		t.Fatal("raw records counted")
	}
	for i := 0; i < 2; i++ {
		put(t, s, usefulCase(raw), "")
	}
	m, _ = s.Maintenance(ctx, raw.Scope)
	if !m.Due {
		t.Fatal("threshold not reached")
	}
	if err = s.MarkMaintenanceNotified(ctx, raw.Scope); err != nil {
		t.Fatal(err)
	}
	put(t, s, usefulCase(raw), "")
	m, _ = s.Maintenance(ctx, raw.Scope)
	if m.Due {
		t.Fatal("nagged again without user decision")
	}
	other, _ := s.Maintenance(ctx, "project:other")
	if other.Changes != 0 || other.Due {
		t.Fatal("cross-project counting")
	}
	if err = s.DeferMaintenance(ctx, raw.Scope, 50); err != nil {
		t.Fatal(err)
	}
	m, _ = s.Maintenance(ctx, raw.Scope)
	if m.NextAt != 53 {
		t.Fatal(m)
	}
	if err = s.AcknowledgeMaintenance(ctx, raw.Scope); err != nil {
		t.Fatal(err)
	}
	m, _ = s.Maintenance(ctx, raw.Scope)
	if m.Enabled {
		t.Fatal("one-shot did not stop")
	}
}

func TestCleanupRequiresFreshApprovalPreservesSourcesAndCanUndo(t *testing.T) {
	s := testStore(t)
	old := put(t, s, recordDraft(), "")
	current := put(t, s, usefulCase(old), "")
	before := approval(t, s, old.Scope, "Upgrade the program")
	p, err := s.PreviewCleanup(ctx, CleanupPreview{Scope: old.Scope, Operations: []CleanupOperation{{EntryID: old.ID, Action: "supersede", ReplacementID: current.ID, Reason: "The retained current decision explicitly replaces the old convention", EvidenceIDs: []string{old.ID, current.ID}}}})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := s.Read(ctx, old.ID)
	if r.Entry.State.State != "active" {
		t.Fatal("preview mutated memory")
	}
	if _, err = s.DecideCleanup(ctx, CleanupDecision{p.ID, before.ID, "Upgrade the program"}, false); err == nil {
		t.Fatal("used old install permission")
	}
	yes := approval(t, s, old.Scope, "Apply the displayed cleanup plan")
	applied, err := s.DecideCleanup(ctx, CleanupDecision{p.ID, yes.ID, yes.Body}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(applied.BackupPath); err != nil {
		t.Fatal("no recoverable backup", err)
	}
	r, err = s.Read(ctx, old.ID)
	if err != nil || r.Entry.Body != old.Body || r.Entry.State.State != "superseded" || r.EffectiveCurrentID != current.ID {
		t.Fatal(r, err)
	}
	hits, err := s.Search(ctx, SearchInput{Scope: old.Scope, Kind: "record"})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.ID == old.ID {
			t.Fatal("retired source in ordinary search")
		}
	}
	effective, err := s.ResolveCurrent(ctx, old.ID)
	if err != nil || effective.ID != current.ID {
		t.Fatal(effective, err)
	}
	restore := approval(t, s, old.Scope, "Undo this cleanup")
	if _, err = s.DecideCleanup(ctx, CleanupDecision{p.ID, restore.ID, restore.Body}, true); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Read(ctx, old.ID)
	if r.Entry.State.State != "active" || r.EffectiveCurrentID != old.ID {
		t.Fatal("undo lost original", r)
	}
}

func TestCleanupRejectsChangedContentAndBatchReplacementChains(t *testing.T) {
	s := testStore(t)
	raw := put(t, s, recordDraft(), "")
	a := put(t, s, usefulCase(raw), "")
	b := put(t, s, usefulCase(raw), "")
	_, err := s.PreviewCleanup(ctx, CleanupPreview{Scope: raw.Scope, Operations: []CleanupOperation{{EntryID: a.ID, Action: "supersede", ReplacementID: b.ID, Reason: "duplicate", EvidenceIDs: []string{raw.ID}}, {EntryID: b.ID, Action: "archive", Reason: "temporary", EvidenceIDs: []string{raw.ID}}}})
	if err == nil {
		t.Fatal("accepted replacement into archived target")
	}
	p, err := s.PreviewCleanup(ctx, CleanupPreview{Scope: raw.Scope, Operations: []CleanupOperation{{EntryID: a.ID, Action: "distill", Summary: "Short current decision, only this tested environment; other environments unknown", Reason: "Remove redundant narration, preserve boundaries", EvidenceIDs: []string{raw.ID}}}})
	if err != nil {
		t.Fatal(err)
	}
	d := usefulCase(raw)
	d.Body += " New explicit condition."
	put(t, s, d, a.ID)
	yes := approval(t, s, raw.Scope, "Apply this plan")
	if _, err = s.DecideCleanup(ctx, CleanupDecision{p.ID, yes.ID, yes.Body}, false); err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatal("applied stale preview", err)
	}
}

func TestContextNeedsFullReadAndDetectsChangedSelectedMemory(t *testing.T) {
	s := testStore(t)
	raw := put(t, s, recordDraft(), "")
	e := put(t, s, usefulCase(raw), "")
	turn := approval(t, s, raw.Scope, "Here too")
	in := ContextInput{Action: "prepare", Scope: raw.Scope, SourceID: turn.ID, Task: "Apply the earlier export exception to this exact module", Questions: "Check scope and current exceptions"}
	result, err := s.Context(ctx, in)
	if err != nil || len(result.Directory) == 0 {
		t.Fatal(result, err)
	}
	complete := ContextInput{Action: "complete", Scope: raw.Scope, SourceID: turn.ID, SelectedIDs: []string{e.ID}, Decision: "Use explicit arguments only in the tested environment"}
	if _, err = s.Context(ctx, complete); err == nil {
		t.Fatal("claimed read without reading")
	}
	if _, err = s.Read(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.TrackContextRead(ctx, turn.ID, e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Context(ctx, complete); err != nil {
		t.Fatal(err)
	}
	if err = s.ContextReady(ctx, turn.ID); err != nil {
		t.Fatal(err)
	}
	d := usefulCase(raw)
	d.Body += " Updated condition."
	put(t, s, d, e.ID)
	if err = s.ContextReady(ctx, turn.ID); err == nil {
		t.Fatal("accepted outdated check")
	}
}

func TestReuseDoesNotReviseOrInvalidateDependentMemory(t *testing.T) {
	s := testStore(t)
	raw := put(t, s, recordDraft(), "")
	e := put(t, s, usefulCase(raw), "")
	dependent := put(t, s, hypothesis(e), "")
	turn, err := s.HookStart(ctx, raw.Scope, "reuse-session", t.TempDir(), "Same decision again", "1")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = s.HookReviewDone(ctx, ReviewInput{Scope: raw.Scope, SourceID: turn.SourceID, ReusedCaseIDs: []string{e.ID}, Note: "Already fully covered; no new condition"}); err != nil {
			t.Fatal(err)
		}
	}
	r, _ := s.Read(ctx, e.ID)
	if r.CurrentID != e.ID {
		t.Fatal("reuse created revision")
	}
	r, _ = s.Read(ctx, dependent.ID)
	if r.Entry.NeedsReview {
		t.Fatal("reuse invalidated dependent")
	}
	m, _ := s.Maintenance(ctx, raw.Scope)
	if m.Changes != 2 {
		t.Fatal("reuse inflated changes", m)
	}
}

func TestSourceRecheckDoesNotLoseFutureInvalidation(t *testing.T) {
	s := testStore(t)
	raw := put(t, s, recordDraft(), "")
	e := put(t, s, usefulCase(raw), "")
	d := recordDraft()
	d.Body += " Corrected source."
	newSource := put(t, s, d, raw.ID)
	yes := true
	p, err := s.SearchPage(ctx, SearchInput{Scope: raw.Scope, NeedsReview: &yes})
	if err != nil || len(p.Results) != 1 {
		t.Fatal(p, err)
	}
	if err = s.ClearReviewed(ctx, raw.Scope, e.ID, []string{raw.ID}, "Old conclusion still stands"); err == nil {
		t.Fatal("accepted outdated source version")
	}
	if err = s.ClearReviewed(ctx, raw.Scope, e.ID, []string{newSource.ID}, "Read corrected source; applicability and decision unchanged"); err != nil {
		t.Fatal(err)
	}
	d.Body += " Another correction."
	put(t, s, d, newSource.ID)
	r, _ := s.Read(ctx, e.ID)
	if !r.Entry.NeedsReview {
		t.Fatal("recheck severed future invalidation")
	}
}

func TestSourceRecheckPlanWaitsForApprovalAndUndoRestoresWarning(t *testing.T) {
	s := testStore(t)
	raw := put(t, s, recordDraft(), "")
	e := put(t, s, usefulCase(raw), "")
	d := recordDraft()
	d.Body += " Corrected source wording."
	current := put(t, s, d, raw.ID)
	p, err := s.PreviewCleanup(ctx, CleanupPreview{Scope: raw.Scope, Operations: []CleanupOperation{{EntryID: e.ID, Action: "recheck", Reason: "Read corrected source; original conclusion and boundary still hold", EvidenceIDs: []string{current.ID}}}})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := s.Read(ctx, e.ID)
	if !r.Entry.NeedsReview {
		t.Fatal("preview silently cleared warning")
	}
	yes := approval(t, s, raw.Scope, "Confirm this source-recheck plan")
	if _, err = s.DecideCleanup(ctx, CleanupDecision{p.ID, yes.ID, yes.Body}, false); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Read(ctx, e.ID)
	if r.Entry.NeedsReview || r.CurrentID != e.ID {
		t.Fatal("recheck changed content or kept stale flag")
	}
	no := approval(t, s, raw.Scope, "Undo that source recheck")
	if _, err = s.DecideCleanup(ctx, CleanupDecision{p.ID, no.ID, no.Body}, true); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Read(ctx, e.ID)
	if !r.Entry.NeedsReview {
		t.Fatal("undo failed to restore warning")
	}
}

func TestV2UpgradeKeepsHistoricalContentAndRequestsInitialInspection(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw := put(t, s, recordDraft(), "")
	d := usefulCase(raw)
	d.Profile = nil
	e := put(t, s, d, "")
	for _, table := range []string{"entry_profiles", "managed_entries", "maintenance_commands", "maintenance_policy", "cleanup_plans", "context_reads", "context_checks", "turn_case_reuse", "reviewed_sources", "source_review_receipts"} {
		if _, err = s.db.Exec("DROP TABLE " + table); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.db.Exec("PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	upgraded, err := openStore(dir, 10000, filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	r, err := upgraded.Read(ctx, e.ID)
	if err != nil || r.Entry.Body != e.Body || r.Entry.Profile != nil {
		t.Fatal("upgrade changed or invented historical content", r, err)
	}
	m, err := upgraded.Maintenance(ctx, e.Scope)
	if err != nil || !m.InitialDue || !m.Due || m.Changes != 0 {
		t.Fatal(m, err)
	}
}
