package hooks

import (
"github.com/vaxjaz/memory-mcp/internal/matching"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

func TestReviewBusinessRecordBoundariesAcrossMCPEnvelopes(t *testing.T) {
	rows := []any{map[string]any{"order_no": "WXN-001", "action": "取消"}, map[string]any{"order_no": "WXN-002", "action": "普通查询"}, map[string]any{"order_no": "WXN-003", "action": "取消"}}
	encoded, _ := json.Marshal(map[string]any{"rows": rows})
	for _, shape := range []string{"rows", "array", "mcp-json", "json-text", "structured", "structured-and-text"} {
		t.Run(shape, func(t *testing.T) {
			s, r, p := trialFixture(t)
			_, err := s.RecordExperience(testCtx, memory.ExperienceWrite{Scope: "project:demo", Project: p, Title: "Synthetic record boundary", Admission: &memory.ExperienceAdmission{FutureDecision: "处理批量结果时检查动作是否来自同一行。", EvidenceRef: "fixture:record-boundary"}, Experience: memory.Experience{ObjectIDPrefixes: []string{"WXN"}, Pitfall: "合成批量动作误认为属于每条记录", Counterexample: "合成取消记录与普通查询记录并列返回", Impact: "可能把取消条件绑定另一对象", Applicability: "隔离批量回归", Boundary: "不推断跨记录关联", Evidence: []memory.ExperienceEvidence{{Kind: "business", Reference: "fixture:record-boundary", Observation: "仅隔离测试"}}, Cues: []memory.ExperienceCue{{ID: "cancel", Label: "取消动作", Text: "合成批量返回中另一条记录仅执行普通查询。", EvidenceRefs: []string{"fixture:record-boundary"}, Matches: []memory.CueMatch{{Rule:matching.Rule{Op:"token", Normalize:"whitespace"},Entrances:[]string{"取消"}}}}}}})
			if err != nil {
				t.Fatal(err)
			}
			start := event("UserPromptSubmit", p, "record-boundary", "")
			start.TurnID = "turn"
			post := event("PostToolUse", p, start.Session, "")
			post.TurnID = "turn"
			post.Tool = "mcp__office__query_orders"
			switch shape {
			case "structured":
				post.Response = map[string]any{"structuredContent": map[string]any{"rows": rows}}
			case "structured-and-text":
				post.Response = map[string]any{"structuredContent": map[string]any{"rows": rows}, "content": []any{map[string]any{"type": "text", "text": "取消 WXN-002"}}}
			case "rows":
				post.Response = map[string]any{"rows": rows}
			case "array":
				post.Response = rows
			case "json-text":
				post.Response = string(encoded)
			case "mcp-json":
				post.Response = map[string]any{"content": []any{map[string]any{"type": "text", "text": string(encoded)}}}
			}
			for _, id := range []string{"WXN-002", "WXN-001", "WXN-003"} {
                // Each row must match on its own before any shared challenge exists.
                start.Session = "record-boundary-"+id
                post.Session = start.Session
                handle(t,r,start)
                if out:=handle(t,r,post);len(out)!=0 {t.Fatal("source injected context",out)}
				q := event("PreToolUse", p, start.Session, "")
				q.TurnID = "turn"
				q.Tool = "mcp__mysql__query"
				q.Input = map[string]any{"sql": "select status from t where order_no='" + id + "'"}
				out := handle(t, r, q)
				if id == "WXN-002" && len(out) != 0 {
					t.Fatal("cross-record action", shape, out)
				}
				if id != "WXN-002" && out["decision"] != "block" {
					t.Fatal("valid record discarded", shape, id, out)
				}
			}
		})
	}
}

func TestReviewDirectAndObservedCueMergeOnceAndE2StillRequiresBody(t *testing.T) {
	s, r, p := trialFixture(t)
	write := memory.ExperienceWrite{Scope: "project:demo", Project: p, Title: "Synthetic direct and observed cue", Admission: &memory.ExperienceAdmission{FutureDecision: "改备注后分别核对日志是否读取更新值。", EvidenceRef: "fixture:mixed-cues"}, Experience: memory.Experience{ObjectIDPrefixes: []string{"WXN"}, Pitfall: "合成任务误认为备注与日志始终同源", Counterexample: "合成反例中更新备注未改变日志参数", Impact: "合成日志保留旧参数", Applicability: "隔离回馈实验", Boundary: "不代表实际业务规范", Evidence: []memory.ExperienceEvidence{{Kind: "business", Reference: "fixture:mixed-cues", Observation: "仅隔离回归"}}, Cues: []memory.ExperienceCue{{ID: "remark", Label: "备注变化", Text: "合成反例中更新备注未改变日志参数。", EvidenceRefs: []string{"fixture:mixed-cues"}, Matches: []memory.CueMatch{{Rule:matching.Rule{Op:"token"},Entrances:[]string{"WorkOrder.remark"}}}}, {ID: "log", Label: "日志入口", Text: "合成反例中日志参数保留了旧值。", EvidenceRefs: []string{"fixture:mixed-cues"}, Matches: []memory.CueMatch{{Rule:matching.Rule{Op:"token"},Entrances:[]string{"FeedbackLogBuilder.build"}}, {Rule:matching.Rule{Op:"token", Normalize:"whitespace"},Entrances:[]string{"生成回馈日志"}}}}}}}
	if _, err := s.RecordExperience(testCtx, write); err != nil {
		t.Fatal(err)
	}
	start := event("UserPromptSubmit", p, "mixed-cue", "")
	start.TurnID = "t1"
	handle(t, r, start)
	query := event("PreToolUse", p, start.Session, "")
	query.TurnID = "t1"
	query.Tool = "Grep"
	query.Input = map[string]any{"pattern": "WorkOrder.remark"}
	deliver := func(out map[string]any, call string) {
		t.Helper()
		token := regexp.MustCompile(`challenge_id="([a-f0-9]{32})"`).FindStringSubmatch(out["reason"].(string))[1]
		page, err := s.LookupExperience(testCtx, memory.ExperienceLookup{Scope: write.Scope, Project: p, Task: "隔离组合任务", Questions: "当前匹配的完整反例是什么", Queries: []memory.RecallQuery{{Terms: []string{"不存在的查询词"}}}, ChallengeID: token})
		if err != nil {
			t.Fatal(err)
		}
		observeLookup(t, s, r, p, start.Session, call, token, page)
	}
	deliver(handle(t, r, query), "mixed-body")
	post := event("PostToolUse", p, start.Session, "")
	post.TurnID = "t1"
	post.Tool = "mcp__office__get_ticket"
	post.Response = map[string]any{"order_no": "WXN-001", "action": "生成回馈日志"}
	handle(t, r, post)
	query.Tool = "mcp__mysql__query"
	query.Input = map[string]any{"sql": "select status from t where order_no='WXN-001' /* FeedbackLogBuilder.build */"}
	out := handle(t, r, query)
	if out["decision"] == "block" || strings.Count(text(out), write.Experience.Cues[1].Text) != 1 {
		t.Fatal("direct/observed cue duplicate or missing", out)
	}
	for i := 0; i < 100; i++ {
		if again := handle(t, r, query); len(again) != 0 {
			t.Fatal("repeated mixed cue", i, again)
		}
	}
	write.Title = "Synthetic independent second experience"
	write.Experience.Counterexample = "另一合成事务提交失败，关联状态未更新"
	write.Experience.Cues = []memory.ExperienceCue{{ID: "cancel", Label: "取消状态", Text: "另一合成事务提交失败，关联状态未更新。", EvidenceRefs: []string{"fixture:mixed-cues"}, Matches: []memory.CueMatch{{Rule:matching.Rule{Op:"token", Normalize:"whitespace"},Entrances:[]string{"取消"}}}}}
	if _, err := s.RecordExperience(testCtx, write); err != nil {
		t.Fatal(err)
	}
	post.Response = map[string]any{"order_no": "WXN-002", "action": "取消"}
	handle(t, r, post)
	query.Input = map[string]any{"sql": "select status from t where order_no='WXN-002' /* FeedbackLogBuilder.build */"}
	out = handle(t, r, query)
	if out["decision"] != "block" {
		t.Fatal("delivered direct B masked E2", out)
	}
	deliver(out, "mixed-e2")
	if again := handle(t, r, query); len(again) != 0 {
		t.Fatal("E2 or known B repeated", again)
	}
}

func TestReviewOutputProbeBoundsAndPrivateFields(t *testing.T) {
	e := Event{Tool: "mcp__office__query_orders", Response: map[string]any{"order_no": "WXN-001", "action": "取消", "password": "DO_NOT_PROBE"}}
	if records := businessObservationRecords(e); len(records) != 1 || strings.Contains(strings.Join(records, " "), "DO_NOT_PROBE") {
		t.Fatal(records)
	}
	e.Response = map[string]any{"text": strings.Repeat("x", 32769)}
	if businessObservationRecords(e) != nil {
		t.Fatal("oversized result accepted")
	}
	e.Response = map[string]any{"isError": true, "text": "取消 WXN-001"}
	if businessObservationRecords(e) != nil {
		t.Fatal("failed result observed")
	}
	rows := make([]any, 129)
	for i := range rows {
		rows[i] = map[string]any{"order_no": "WXN-001", "action": "取消"}
	}
	e.Response = map[string]any{"rows": rows}
	if businessObservationRecords(e) != nil {
		t.Fatal("oversized record batch accepted")
	}
}
