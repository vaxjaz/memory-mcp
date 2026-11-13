package hooks

import (
	"context"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

// Stop never decides semantic value. It only records the delivered answer and
// closes bookkeeping when no explicit outstanding candidate remains.
func (r Runtime) finishWithoutModel(ctx context.Context, e Event, t memory.HookTurn, candidate memory.MemoryCandidate) (map[string]any, error) {
	empty := map[string]any{}
	var err error
	t, err = r.Store.HookAttachAnswer(ctx, t, e.Answer)
	if err != nil {
		return empty, err
	}
	if candidate.State == "pending" {
		_ = r.Store.HookLog(ctx, e.Name, t.Scope, t.Session, "candidate_pending", t.SourceID)
		return empty, nil
	}
	if t.Status == "reviewed" || t.Status == "skipped" {
		return empty, nil
	}
	ids, err := r.Store.CurrentCasesForSource(ctx, t.SourceID, t.Scope)
	if err != nil {
		return empty, err
	}
	note := "No outstanding memory candidate was declared. Ending without an extra model review; this is not proof of a full semantic audit."
	if len(ids) > 0 {
		note = "Source-bound cases were already saved. Closing bookkeeping only, without rereading, adding evidence links or rewriting the business answer."
	}
	_, err = r.Store.HookReviewDone(ctx, memory.ReviewInput{Scope: t.Scope, SourceID: t.SourceID, CaseIDs: ids, Note: note})
	if err == nil {
		_ = r.Store.HookLog(ctx, e.Name, t.Scope, t.Session, "finished_without_model", t.SourceID)
	}
	return empty, err
}
