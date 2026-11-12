package memory

import (
	"strings"
	"testing"
)

func TestHistoricalHookEvidenceDoesNotRequireCompletingItsOldTurn(t *testing.T) {
	s := testStore(t)
	p := t.TempDir()
	if err := s.HookBind(ctx, p, "project:demo"); err != nil {
		t.Fatal(err)
	}
	old, err := s.HookStartForHost(ctx, "zcode", "project:demo", "same-session", p, "Original contract request", "old")
	if err != nil {
		t.Fatal(err)
	}
	s.RequireContext(ctx, old)
	current, err := s.HookStartForHost(ctx, "zcode", "project:demo", "same-session", p, "Continue and record the verified contract", "current")
	if err != nil {
		t.Fatal(err)
	}
	s.RequireContext(ctx, current)
	_, err = s.Context(ctx, ContextInput{Action: "prepare", Scope: current.Scope, SourceID: current.SourceID, Task: "Record verified contract", Questions: "Verify actual current contract"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Context(ctx, ContextInput{Action: "complete", Scope: current.Scope, SourceID: current.SourceID, Decision: "Verified current contract; historical unrelated case is not a constraint", SaveCandidate: true})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := s.Read(ctx, old.SourceID)
	draft := usefulCase(raw.Entry)
	// Exact legacy EPM shape: supports OLD user source, current source omitted.
	checked, err := s.ValidateWriteContext(ctx, draft)
	if err != nil || checked != current.SourceID {
		t.Fatal("historical checkpoint blocked current complete", checked, err)
	}
	oldState, _ := s.ContextStatus(ctx, old.SourceID)
	if oldState.Status != "pending" {
		t.Fatal("old state was falsely completed", oldState)
	}
	if _, err = s.Context(ctx, ContextInput{Action: "complete", Scope: old.Scope, SourceID: old.SourceID, Decision: "Must not complete obsolete turn"}); err == nil {
		t.Fatal("historical complete guard was relaxed")
	}
	draft.ContextSourceID = current.SourceID
	checked, err = s.ValidateWriteContext(ctx, draft)
	if err != nil || checked != current.SourceID {
		t.Fatal("explicit current checkpoint unavailable", checked, err)
	}
	draft.ContextSourceID = old.SourceID
	if _, err = s.ValidateWriteContext(ctx, draft); err == nil || !strings.Contains(err.Error(), "stale_write_context") {
		t.Fatal("explicit stale checkpoint accepted", err)
	}
}

func TestCompletedHistoricalTurnCannotAuthorizePendingCurrentInput(t *testing.T) {
	s := testStore(t)
	p := t.TempDir()
	s.HookBind(ctx, p, "project:demo")
	old, _ := s.HookStartForHost(ctx, "zcode", "project:demo", "same-session", p, "Earlier completed task", "old")
	s.RequireContext(ctx, old)
	if _, err := s.Context(ctx, ContextInput{Action: "skip", Scope: old.Scope, SourceID: old.SourceID, Task: "Earlier ordinary task", Decision: "No memory applies"}); err != nil {
		t.Fatal(err)
	}
	current, _ := s.HookStartForHost(ctx, "zcode", "project:demo", "same-session", p, "New task not yet checked", "current")
	s.RequireContext(ctx, current)
	raw, _ := s.Read(ctx, old.SourceID)
	draft := usefulCase(raw.Entry)
	if _, err := s.ValidateWriteContext(ctx, draft); err == nil || !strings.Contains(err.Error(), current.SourceID) {
		t.Fatal("old complete bypassed current pending checkpoint", err)
	}
}

func TestExplicitWriteContextSeparatesManualEvidenceFromCurrentTask(t *testing.T) {
	s := testStore(t)
	evidence := put(t, s, recordDraft(), "")
	source := put(t, s, recordDraft(), "")
	_, err := s.Context(ctx, ContextInput{Action: "prepare", Scope: source.Scope, SourceID: source.ID, Task: "Actual current task"})
	if err != nil {
		t.Fatal(err)
	}
	draft := usefulCase(evidence)
	draft.ContextSourceID = source.ID
	if _, err = s.ValidateWriteContext(ctx, draft); err == nil {
		t.Fatal("explicit pending context was bypassed")
	}
	if _, err = s.Context(ctx, ContextInput{Action: "complete", Scope: source.Scope, SourceID: source.ID, Decision: "Full applicable material checked; no historic constraint"}); err != nil {
		t.Fatal(err)
	}
	checked, err := s.ValidateWriteContext(ctx, draft)
	if err != nil || checked != source.ID {
		t.Fatal(checked, err)
	}
}
