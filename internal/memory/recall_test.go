package memory

import (
	"fmt"
	"strings"
	"testing"
)

func TestResourceRecallSurvivesPromptNoiseAndExplainsRealMatches(t *testing.T) {
	s := testStore(t)
	scope := "project:resource-test"
	target := put(t, s, Draft{Kind: "case", Scope: scope, Title: "责任交接计时", Body: "TaskOrderCommonService 的计时应按实际责任交接核实。", Origin: "model_inference"}, "")
	query := "调整转派自动审核多场景流程"
	for i := 0; i < 4; i++ {
		put(t, s, Draft{Kind: "case", Scope: scope, Title: fmt.Sprintf("转派自动审核多场景流程 %d", i), Body: "转派自动审核多场景流程的其他模块记录。", Origin: "model_inference"}, "")
	}
	plain, err := s.HookRecall(ctx, scope, query)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range plain.Entries {
		if e.ID == target.ID {
			t.Fatal("fixture failed to reproduce dropped indirect case")
		}
	}
	recalled, err := s.HookRecallContext(ctx, scope, RecallContext{Query: query, Resources: []string{`D:\repo\src\TaskOrderCommonService.java`}})
	if err != nil {
		t.Fatal(err)
	}
	if len(recalled.Entries) == 0 || recalled.Entries[0].ID != target.ID {
		t.Fatal("resource case displaced by generic task hits", recalled)
	}
	reason := recalled.Matches[0].Reasons[0]
	if reason.Kind != "symbol" || reason.Value != "TaskOrderCommonService" || reason.Field != "body" || !strings.Contains(target.Body, reason.Value) {
		t.Fatal("reason does not describe actual match", reason)
	}
}

func TestResourceRecallMatchesSourceLocatorAndNormalizedIdentifiers(t *testing.T) {
	s := testStore(t)
	scope := "project:source-match"
	located := put(t, s, Draft{Kind: "case", Scope: scope, Title: "历史限制", Body: "具体行为需读当前实现。", SourceRef: `src\main\TaskAllocationService.java`, Origin: "model_inference"}, "")
	r, err := s.HookRecallContext(ctx, scope, RecallContext{Resources: []string{"src/main/TaskAllocationService.java"}})
	if err != nil || len(r.Entries) != 1 || r.Entries[0].ID != located.ID {
		t.Fatal(r, err)
	}
	if got := r.Matches[0].Reasons[0]; got.Kind != "resource_path" || got.Field != "source_ref" {
		t.Fatal(got)
	}
	normalized := put(t, s, Draft{Kind: "case", Scope: scope, Title: "处理职责", Body: "TaskOrderCommonService 包含多条处理路径。", Origin: "model_inference"}, "")
	r, err = s.HookRecallContext(ctx, scope, RecallContext{Resources: []string{"task_order_common_service.py"}})
	if err != nil || r.Entries[0].ID != normalized.ID {
		t.Fatal(r, err)
	}
	if r.Matches[0].Reasons[0].Kind != "symbol_normalized" {
		t.Fatal(r.Matches)
	}
}

func TestResourceAndTaskCandidatesBothHaveRoom(t *testing.T) {
	s := testStore(t)
	scope := "project:two-channels"
	for i := 0; i < 4; i++ {
		put(t, s, Draft{Kind: "case", Scope: scope, Title: fmt.Sprintf("TaskOrderCommonService 路径 %d", i), Body: "TaskOrderCommonService 的不同历史处理边界。", Origin: "model_inference"}, "")
	}
	topic := put(t, s, Draft{Kind: "case", Scope: scope, Title: "状态机化存量约束", Body: "状态机化应评估存量是否需要迁移。", Origin: "model_inference"}, "")
	out, err := s.HookRecallContext(ctx, scope, RecallContext{Query: "状态机化和存量迁移", Resources: []string{"TaskOrderCommonService.java"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Entries) != 3 {
		t.Fatal(out)
	}
	hasTopic := false
	for _, e := range out.Entries {
		if e.ID == topic.ID {
			hasTopic = true
		}
	}
	if !hasTopic {
		t.Fatal("resource matches crowded out all task constraints", out)
	}
}
