package hooks

import (
"github.com/vaxjaz/memory-mcp/internal/matching"
	"testing"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

func TestReviewBusinessBatchDoesNotTransferActionBetweenObjects(t *testing.T) {
	s, r, p := trialFixture(t)
	_, err := s.RecordExperience(testCtx, memory.ExperienceWrite{Scope: "project:demo", Project: p, Title: "Synthetic row-local cancellation risk", Admission: &memory.ExperienceAdmission{FutureDecision: "判断是否取消时检查目标记录自己的状态。", EvidenceRef: "fixture:review-batch"}, Experience: memory.Experience{
		ObjectIDPrefixes: []string{"WXN"},
		Pitfall:          "合成取消动作误以为关联状态已同步", Counterexample: "合成测试中取消后关联记录仍保留旧状态", Impact: "合成关联状态不一致", Applicability: "合成取消动作", Boundary: "仅隔离审查测试",
		Evidence: []memory.ExperienceEvidence{{Kind: "business", Reference: "fixture:review-batch", Observation: "隔离测试，不是真实事故"}},
		Cues:     []memory.ExperienceCue{{ID: "cancel", Label: "取消动作", EvidenceRefs: []string{"fixture:review-batch"}, Matches: []memory.CueMatch{{Rule:matching.Rule{Op:"token", Normalize:"whitespace"},Entrances:[]string{"取消"}}}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	start := event("UserPromptSubmit", p, "review-batch", "")
	start.TurnID = "t1"
	handle(t, r, start)
	post := event("PostToolUse", p, "review-batch", "")
	post.TurnID = "t1"
	post.Tool = "mcp__office__query_orders"
	post.Response = map[string]any{"rows": []any{
		map[string]any{"order_no": "WXN-001", "action": "取消"},
		map[string]any{"order_no": "WXN-002", "action": "普通查询"},
	}}
	handle(t, r, post)
	query := event("PreToolUse", p, "review-batch", "")
	query.TurnID = "t1"
	query.Tool = "mcp__mysql__query"
	query.Input = map[string]any{"sql": "select status from work_order where order_no='WXN-002'"}
	if out := handle(t, r, query); len(out) != 0 {
		t.Fatalf("action from WXN-001 was incorrectly attached to WXN-002: %#v", out)
	}
	query.Input["sql"] = "select status from work_order where order_no='WXN-001'"
	if out := handle(t, r, query); out["decision"] != "block" {
		t.Fatalf("correct row-local association must still be retained: %#v", out)
	}
}
