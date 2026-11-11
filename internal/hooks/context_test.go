package hooks

import (
	"strings"
	"testing"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

func TestContextHookOrdinaryTurnDoesNotForceExtraReview(t *testing.T) {
	s, r, p := fixture(t)
	r.ContextCheck = true
	out := handle(t, r, event("UserPromptSubmit", p, "ordinary", "谢谢，今天先到这里"))
	if !strings.Contains(text(out), "memory_context") {
		t.Fatal(out)
	}
	turn := latest(t, s, "ordinary")
	if _, err := s.Context(testCtx, memory.ContextInput{Action: "skip", Scope: turn.Scope, SourceID: turn.SourceID, Task: "结束当前对话", Decision: "普通结束语，没有待决定事项或新增可复用知识"}); err != nil {
		t.Fatal(err)
	}
	e := event("Stop", p, "ordinary", "")
	e.Answer = "好的。"
	out = handle(t, r, e)
	if len(out) != 0 {
		t.Fatal("ordinary turn forced continuation", out)
	}
	turn = latest(t, s, "ordinary")
	if turn.Attempts != 0 || turn.Status != "skipped" {
		t.Fatal(turn)
	}
	cases, err := s.Search(testCtx, memory.SearchInput{Scope: turn.Scope, Kind: "case"})
	if err != nil || len(cases) != 0 {
		t.Fatal(cases, err)
	}
}

func TestContextGateBeforeLoginAndSQLIsPerTurn(t *testing.T) {
	s, r, p := fixture(t)
	r.ContextCheck = true
	handle(t, r, event("UserPromptSubmit", p, "login", "使用当前项目的测试环境登录"))
	turn := latest(t, s, "login")
	e := event("PreToolUse", p, "login", "")
	e.Tool = "mcp__development__login"
	e.Input = map[string]any{}
	if got := handle(t, r, e); got["decision"] != "block" {
		t.Fatal("login without context check not stopped", got)
	}
	e.Tool = "execute_query"
	e.Input = map[string]any{"sql": "UPDATE tickets SET flag=1 WHERE id=2"}
	if got := handle(t, r, e); got["decision"] != "block" {
		t.Fatal("SQL write not stopped", got)
	}
	in := memory.ContextInput{Action: "prepare", Scope: turn.Scope, SourceID: turn.SourceID, Task: "测试环境登录", Questions: "检查现行约定和环境边界"}
	if _, err := s.Context(testCtx, in); err != nil {
		t.Fatal(err)
	}
	in.Action = "complete"
	in.Decision = "当前库无可用登录约定，核对工具说明和环境后继续，不把旧信息当授权"
	if _, err := s.Context(testCtx, in); err != nil {
		t.Fatal(err)
	}
	if got := handle(t, r, e); len(got) != 0 {
		t.Fatal("completed check still blocked", got)
	}
	handle(t, r, event("UserPromptSubmit", p, "login", "现在改另一处数据"))
	if got := handle(t, r, e); got["decision"] != "block" {
		t.Fatal("previous turn check authorized new task", got)
	}
}

func TestOnlyAdmittedCandidateRequestsOneStopContinuation(t *testing.T) {
	s, r, p := fixture(t)
	r.ContextCheck = true
	handle(t, r, event("UserPromptSubmit", p, "decision", "以后在这个测试环境调用登录工具必须显式传参"))
	turn := latest(t, s, "decision")
	in := memory.ContextInput{Action: "prepare", Scope: turn.Scope, SourceID: turn.SourceID, Task: "确定本环境登录调用约定"}
	if _, err := s.Context(testCtx, in); err != nil {
		t.Fatal(err)
	}
	in.Action = "complete"
	in.SaveCandidate = true
	in.Decision = "用户明确提出可复用的环境约定，保留适用范围；不泛化到其他工具"
	if _, err := s.Context(testCtx, in); err != nil {
		t.Fatal(err)
	}
	e := event("Stop", p, "decision", "")
	e.Answer = "已按这个环境的约定处理。"
	out := handle(t, r, e)
	if out["decision"] != "block" || !strings.Contains(text(out), "future_use") {
		t.Fatal(out)
	}
	out = handle(t, r, e)
	if len(out) != 0 {
		t.Fatal("repeated model continuation", out)
	}
}

func TestUnboundProjectWarnsWithoutGlobalFallback(t *testing.T) {
	_, r, _ := fixture(t)
	r.ContextCheck = true
	out := handle(t, r, event("UserPromptSubmit", t.TempDir(), "unbound", "继续"))
	if !strings.Contains(text(out), "尚未绑定") {
		t.Fatal("silent unbound project", out)
	}
}
