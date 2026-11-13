package memory

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTurnWriteAssociationClosesReceiptWithoutChangingEvidence(t *testing.T) {
	s := testStore(t)
	evidence := put(t, s, recordDraft(), "")
	p := t.TempDir()
	s.HookBind(ctx, p, evidence.Scope)
	turn, err := s.HookStartForHost(ctx, "zcode", evidence.Scope, "one", p, "Actual request for a new reusable finding", "input")
	if err != nil {
		t.Fatal(err)
	}
	s.RequireContext(ctx, turn)
	_, err = s.Context(ctx, ContextInput{Action: "prepare", Scope: turn.Scope, SourceID: turn.SourceID, Task: "Verify new finding"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Context(ctx, ContextInput{Action: "complete", Scope: turn.Scope, SourceID: turn.SourceID, Decision: "New test finding changes future action", SaveCandidate: true})
	if err != nil {
		t.Fatal(err)
	}
	d := usefulCase(evidence)
	d.ContextSourceID = turn.SourceID
	if _, err = s.ValidateWriteContext(ctx, d); err != nil {
		t.Fatal(err)
	}
	saved := put(t, s, d, "")
	read, err := s.Read(ctx, saved.ID)
	if err != nil || read.Entry.RecordedSourceID != turn.SourceID || len(read.Entry.Links) != 1 || read.Entry.Links[0].TargetID != evidence.ID {
		t.Fatal(read, err)
	}
	raw, err := s.Read(ctx, turn.SourceID)
	if err != nil || len(raw.ReferencedBy) != 0 {
		t.Fatal("bookkeeping manufactured an evidence link", raw, err)
	}
	before, _ := s.Stats(ctx)
	ack, err := s.HookReviewDone(ctx, ReviewInput{Scope: turn.Scope, SourceID: turn.SourceID, CaseIDs: []string{saved.ID}, Note: "Saved once with separate turn bookkeeping"})
	after, _ := s.Stats(ctx)
	if err != nil || ack.Status != "reviewed" || before.Entries != after.Entries {
		t.Fatal("receipt forced another revision", ack, err)
	}
	state, _ := s.CandidateStatus(ctx, turn.SourceID)
	if state.State != "saved" || state.WrittenCaseCount != 1 {
		t.Fatal(state)
	}
	_, err = s.HookReviewDone(ctx, ReviewInput{Scope: turn.Scope, SourceID: turn.SourceID, Note: "A different receipt after closure"})
	if err == nil || strings.Contains(err.Error(), "correct the case with memory_revise") {
		t.Fatal("closed receipt encouraged revision", err)
	}
}

func TestExplicitCandidateSurvivesFailedWriteAndReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	source := put(t, s, recordDraft(), "")
	_, err = s.Context(ctx, ContextInput{Action: "candidate", Scope: source.Scope, SourceID: source.ID, SaveCandidate: true, Decision: "Verified new condition not saved yet"})
	if err != nil {
		t.Fatal(err)
	}
	d := usefulCase(source)
	d.ContextSourceID = source.ID
	d.Links = []Link{{TargetID: strings.Repeat("f", 27), Relation: "supports"}}
	if _, err = s.Put(ctx, d, ""); err == nil {
		t.Fatal("bad write accepted")
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.Context(ctx, ContextInput{Action: "status", Scope: source.Scope, SourceID: source.ID})
	if err != nil || r.MemoryReview == nil || r.MemoryReview.State != "pending" || !r.SaveCandidate || r.Status != "not_prepared" {
		t.Fatal(r, err)
	}
	if _, err = s.ValidateWriteContext(ctx, usefulCaseWithContext(source)); err == nil {
		t.Fatal("candidate declaration bypassed preparation")
	}
	_, err = s.Context(ctx, ContextInput{Action: "candidate", Scope: source.Scope, SourceID: source.ID, Decision: "Comparison shows existing memory already covers it"})
	if err != nil {
		t.Fatal(err)
	}
	items, _, err := s.PendingCandidates(ctx, source.Scope, "", 8)
	if err != nil || len(items) != 0 {
		t.Fatal(items, err)
	}
}

func usefulCaseWithContext(e Entry) Draft { d := usefulCase(e); d.ContextSourceID = e.ID; return d }

func TestOrdinaryInputsDoNotBecomeCandidatesAndPendingPaginates(t *testing.T) {
	s := testStore(t)
	for i := 0; i < 21; i++ {
		e := put(t, s, recordDraft(), "")
		if i == 0 {
			continue
		}
		if _, err := s.Context(ctx, ContextInput{Action: "candidate", Scope: e.Scope, SourceID: e.ID, SaveCandidate: true, Decision: strings.Repeat("具体新增与未来用途", 200)}); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		r, err := s.Context(ctx, ContextInput{Action: "pending", Scope: "project:demo", Cursor: cursor, ResponseMode: "compact"})
		body, _ := json.Marshal(r)
		if err != nil || len(body) > CompactContextBudget {
			t.Fatal(len(body), err)
		}
		for _, c := range r.PendingCandidates {
			if seen[c.SourceID] {
				t.Fatal("duplicate page", c)
			}
			seen[c.SourceID] = true
		}
		cursor = r.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 20 {
		t.Fatal("ordinary input queued or pending entries lost", len(seen))
	}
}

func TestBookkeepingCannotGroundCaseOrInvalidateEvidenceDependents(t *testing.T) {
	s := testStore(t)
	evidence := put(t, s, recordDraft(), "")
	control := put(t, s, recordDraft(), "")
	d := usefulCase(evidence)
	d.ContextSourceID = control.ID
	d.Links = []Link{{TargetID: evidence.ID, Relation: "contradicts"}}
	if _, err := s.Put(ctx, d, ""); err == nil {
		t.Fatal("turn association laundered missing evidence")
	}
	d = usefulCase(evidence)
	d.ContextSourceID = control.ID
	e := put(t, s, d, "")
	change := recordDraft()
	change.Body = "Correct control wording, not the case's evidence"
	put(t, s, change, control.ID)
	r, err := s.Read(ctx, e.ID)
	if err != nil || r.Entry.NeedsReview {
		t.Fatal("bookkeeping used as evidence dependency", r, err)
	}
}

func TestUpgradeRecoversOnlyDeclaredLegacyCandidatesOnce(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := t.TempDir()
	s.HookBind(ctx, p, "project:demo")
	var pendingSource string
	for _, name := range []string{"ordinary", "pending", "saved"} {
		turn, er := s.HookStart(ctx, "project:demo", name, p, "Legacy fixture "+name, "input")
		if er != nil {
			t.Fatal(er)
		}
		s.RequireContext(ctx, turn)
		if name == "ordinary" {
			continue
		}
		if _, er = s.Context(ctx, ContextInput{Action: "prepare", Scope: turn.Scope, SourceID: turn.SourceID, Task: "Legacy fixture"}); er != nil {
			t.Fatal(er)
		}
		if _, er = s.Context(ctx, ContextInput{Action: "complete", Scope: turn.Scope, SourceID: turn.SourceID, Decision: "Explicit meaningful legacy candidate", SaveCandidate: true}); er != nil {
			t.Fatal(er)
		}
		if name == "pending" {
			pendingSource = turn.SourceID
		}
		if name == "saved" {
			raw, _ := s.Read(ctx, turn.SourceID)
			put(t, s, usefulCase(raw.Entry), "") // legacy link only, no new ledger
		}
	}
	before, _ := s.Stats(ctx)
	if _, err = s.db.Exec("DROP TABLE turn_case_writes; DROP TABLE memory_candidates; DROP TABLE memory_candidate_migrations;"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	items, _, err := s.PendingCandidates(ctx, "project:demo", "", 8)
	after, _ := s.Stats(ctx)
	if err != nil || len(items) != 1 || items[0].SourceID != pendingSource || before.Entries != after.Entries {
		t.Fatal(items, after, err)
	}
	if _, err = s.Context(ctx, ContextInput{Action: "candidate", Scope: "project:demo", SourceID: pendingSource, Decision: "Compared and withdrew this candidate"}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	items, _, err = s.PendingCandidates(ctx, "project:demo", "", 8)
	if err != nil || len(items) != 0 {
		t.Fatal("migration rescanned and requeued a withdrawn candidate", items, err)
	}
}

func TestSavedHypothesisDoesNotStayPendingOrMasqueradeAsCase(t *testing.T) {
	s := testStore(t)
	source := put(t, s, recordDraft(), "")
	if _, err := s.Context(ctx, ContextInput{Action: "candidate", Scope: source.Scope, SourceID: source.ID, SaveCandidate: true, Decision: "A bounded hypothesis changes a future fixture comparison"}); err != nil {
		t.Fatal(err)
	}
	d := usefulCase(source)
	d.Kind = "hypothesis"
	d.ContextSourceID = source.ID
	put(t, s, d, "")
	state, err := s.CandidateStatus(ctx, source.ID)
	if err != nil || state.State != "saved" || len(state.WrittenCaseIDs) != 0 {
		t.Fatal(state, err)
	}
}
