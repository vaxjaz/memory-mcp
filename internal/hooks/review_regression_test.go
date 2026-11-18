package hooks

import (
	"regexp"
	"testing"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

func TestReviewDirectDeliveredMatchCannotHideObservedUndeliveredExperience(t *testing.T) {
	s, r, p := trialFixture(t)
	start := event("UserPromptSubmit", p, "review-mixed", "")
	start.TurnID = "t1"
	handle(t, r, start)
	query := event("PreToolUse", p, "review-mixed", "")
	query.TurnID = "t1"
	query.Tool = "Grep"
	query.Input = map[string]any{"pattern": "LYN 创建准入"}
	first := handle(t, r, query)
	token := regexp.MustCompile(`challenge_id="([a-f0-9]{32})"`).FindStringSubmatch(first["reason"].(string))[1]
	page, err := s.LookupExperience(testCtx, memory.ExperienceLookup{Scope: "project:demo", Project: p, Task: "合成审查任务", Questions: "首次交付直接命中的反例", Queries: []memory.RecallQuery{{Terms: []string{"不匹配查询"}}}, ChallengeID: token})
	if err != nil {
		t.Fatal(err)
	}
	observeLookup(t, s, r, p, "review-mixed", "lookup-e1", token, page)
	second, err := s.RecordExperience(testCtx, memory.ExperienceWrite{Scope: "project:demo", Project: p, Title: "Synthetic observed second experience", Experience: memory.Experience{
		ObjectIDPrefixes: []string{"WXN"},
		Pitfall:          "合成取消动作误以为无需处理关联记录", Counterexample: "合成测试中取消后关联记录仍保留旧状态", Impact: "合成关联状态不一致", Applicability: "合成取消动作", Boundary: "仅隔离审查测试",
		Evidence: []memory.ExperienceEvidence{{Kind: "business", Reference: "fixture:review-observed", Observation: "隔离测试，不是真实事故"}},
		Cues:     []memory.ExperienceCue{{ID: "cancel", Label: "取消动作", Text: "合成测试中取消后关联记录仍保留旧状态。", EvidenceRefs: []string{"fixture:review-observed"}, Entrances: []memory.CueEntrance{{Kind: "business_signal", Value: "取消"}}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	post := event("PostToolUse", p, "review-mixed", "")
	post.TurnID = "t1"
	post.Tool = "mcp__office__get_ticket"
	post.Response = map[string]any{"text": "取消 WXN-001"}
	handle(t, r, post)
	query.Tool = "mcp__mysql__query"
	query.Input = map[string]any{"sql": "select status from work_order where order_no='WXN-001' /* LYN 创建准入 */"}
	out := handle(t, r, query)
	if out["decision"] != "block" {
		t.Fatalf("delivered E1 hid observed undelivered E2 %s: %#v", second.ID, out)
	}
}
