package hooks

import (
	"testing"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

func TestDefaultStopLeavesOnlyExplicitPendingAndNeverReplays(t *testing.T) {
	s, r, p := fixture(t)
	r.Review = false
	r.ContextCheck = true
	handle(t, r, event("UserPromptSubmit", p, "ordinary-fast", "Ordinary question; no declared memory candidate"))
	e := event("Stop", p, "ordinary-fast", "")
	e.Answer = "Original business answer"
	if len(handle(t, r, e)) != 0 {
		t.Fatal("ordinary turn continued")
	}
	ordinary := latest(t, s, "ordinary-fast")
	diag, _ := s.HookDiagnostics(testCtx)
	if ordinary.Status != "skipped" || diag["pending_reviews"] != 0 {
		t.Fatal(ordinary, diag)
	}
	handle(t, r, event("UserPromptSubmit", p, "pending-fast", "A meaningful finding not yet saved"))
	turn := latest(t, s, "pending-fast")
	_, err := s.Context(testCtx, memory.ContextInput{Action: "candidate", Scope: turn.Scope, SourceID: turn.SourceID, SaveCandidate: true, Decision: "A real new condition changes the future fixture action"})
	if err != nil {
		t.Fatal(err)
	}
	e = event("Stop", p, "pending-fast", "")
	e.Answer = "Deliver without waiting for a new model turn"
	if len(handle(t, r, e)) != 0 {
		t.Fatal("pending forced model turn")
	}
	turn = latest(t, s, "pending-fast")
	if turn.Status != "pending" || turn.Attempts != 0 || turn.AnswerID == "" {
		t.Fatal("pending was silently completed", turn)
	}
	// Historical status remains readable after another real input arrives.
	handle(t, r, event("UserPromptSubmit", p, "pending-fast", "Different next task"))
	status, err := s.Context(testCtx, memory.ContextInput{Action: "status", Scope: turn.Scope, SourceID: turn.SourceID})
	if err != nil || status.MemoryReview.State != "pending" {
		t.Fatal(status, err)
	}
	if _, err = s.Context(testCtx, memory.ContextInput{Action: "candidate", Scope: turn.Scope, SourceID: turn.SourceID, Decision: "Must not change old turn"}); err == nil {
		t.Fatal("historical status read relaxed write guard")
	}
}

func TestHostCanExplicitlyRequestOneStopReview(t *testing.T) {
	s, r, p := fixture(t)
	r.Review = false
	r.ContextCheck = true
	handle(t, r, event("UserPromptSubmit", p, "opt-in", "Meaningful pending candidate needs one review"))
	turn := latest(t, s, "opt-in")
	in := memory.ContextInput{Action: "prepare", Scope: turn.Scope, SourceID: turn.SourceID, Task: "Fixture opt-in"}
	if _, err := s.Context(testCtx, in); err != nil {
		t.Fatal(err)
	}
	in.Action = "complete"
	in.SaveCandidate = true
	in.ReviewAtStop = true
	in.Decision = "Concrete new condition and future use"
	if _, err := s.Context(testCtx, in); err != nil {
		t.Fatal(err)
	}
	e := event("Stop", p, "opt-in", "")
	e.Answer = "Business answer"
	if handle(t, r, e)["decision"] != "block" {
		t.Fatal("explicit meaningful request ignored")
	}
	if len(handle(t, r, e)) != 0 {
		t.Fatal("repeated opt-in continuation")
	}
}
