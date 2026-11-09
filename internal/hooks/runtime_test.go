package hooks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

var testCtx = context.Background()

func fixture(t *testing.T) (*memory.Store, Runtime, string) {
	t.Helper()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := memory.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err = s.HookBind(testCtx, project, "project:demo"); err != nil {
		t.Fatal(err)
	}
	return s, Runtime{Store: s, Capture: true, Review: true}, project
}
func handle(t *testing.T, r Runtime, e Event) map[string]any {
	t.Helper()
	out, err := r.Handle(testCtx, e)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func event(name, cwd, session, prompt string) Event {
	return Event{Name: name, CWD: cwd, Session: session, Prompt: prompt}
}
func text(out map[string]any) string { b, _ := json.Marshal(out); return string(b) }
func latest(t *testing.T, s *memory.Store, session string) memory.HookTurn {
	t.Helper()
	r, e := s.HookLatest(testCtx, "project:demo", session)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func putCase(t *testing.T, s *memory.Store, scope, title, body, source, previous string) memory.Entry {
	t.Helper()
	d := memory.Draft{Kind: "case", Scope: scope, Title: title, Body: body, Origin: "model_inference"}
	if source != "" {
		d.Links = []memory.Link{{TargetID: source, Relation: "context"}}
	}
	e, err := s.Put(testCtx, d, previous)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestBusinessDecisionCorrectionAndLaterAutomaticRecall(t *testing.T) {
	s, r, p := fixture(t)
	user := "新增详情页不需要新增一级菜单。查看和编辑放在原模块。\n原话有两个空格  "
	handle(t, r, event("UserPromptSubmit", p, "first", user))
	turn := latest(t, s, "first")
	raw, err := s.Read(testCtx, turn.SourceID)
	if err != nil || raw.Entry.Body != user {
		t.Fatal("original quote changed", err)
	}
	normal, _ := s.Search(testCtx, memory.SearchInput{Scope: "project:demo"})
	if len(normal) != 0 {
		t.Fatal("raw chat leaked into business search")
	}
	all, _ := s.Search(testCtx, memory.SearchInput{Scope: "project:demo", IncludeEvents: true})
	if len(all) != 1 {
		t.Fatal("raw event not explicitly accessible")
	}
	stop := event("Stop", p, "first", "")
	stop.Answer = "详情和编辑沿用安排模块。"
	out := handle(t, r, stop)
	if out["decision"] != "block" || !strings.Contains(text(out), turn.SourceID) {
		t.Fatal("no host-model review requested", out)
	}
	if len(handle(t, r, stop)) != 0 {
		t.Fatal("review loop repeated")
	}
	// The host model is simulated explicitly; no test claims semantic judgment.
	c := putCase(t, s, "project:demo", "页面与菜单归属", "用户明确决定：详情页、编辑页属于原模块，不新增一级菜单。", turn.SourceID, "")
	receipt, err := s.HookReviewDone(testCtx, memory.ReviewInput{Scope: turn.Scope, SourceID: turn.SourceID, CaseIDs: []string{c.ID}, Note: "保存已明确的页面归属约束"})
	if err != nil || receipt.Status != "reviewed" {
		t.Fatal(receipt, err)
	}
	next := handle(t, r, event("UserPromptSubmit", p, "later", "新建详情页面和编辑页面，该放哪个菜单？"))
	if !strings.Contains(text(next), c.ID) {
		t.Fatal("later task did not automatically recall business case", next)
	}
	correction := "纠正：独立业务模块可以新增菜单，详情和编辑页仍不单独增加。"
	handle(t, r, event("UserPromptSubmit", p, "later", correction))
	correctedTurn := latest(t, s, "later")
	revised := putCase(t, s, "project:demo", "页面与菜单归属及独立业务例外", c.Body+"\n用户纠偏："+correction, correctedTurn.SourceID, c.ID)
	if _, err = s.HookReviewDone(testCtx, memory.ReviewInput{Scope: turn.Scope, SourceID: correctedTurn.SourceID, CaseIDs: []string{revised.ID}, Note: "保留原版本并追加独立模块例外"}); err != nil {
		t.Fatal(err)
	}
	old, _ := s.Read(testCtx, c.ID)
	if old.Entry.Body != c.Body || old.Entry.Current || old.CurrentID != revised.ID {
		t.Fatal("correction overwrote historical decision")
	}
	// An identical completed receipt stays retryable after that case is superseded.
	if _, err = s.HookReviewDone(testCtx, memory.ReviewInput{Scope: turn.Scope, SourceID: turn.SourceID, CaseIDs: []string{c.ID}, Note: "保存已明确的页面归属约束"}); err != nil {
		t.Fatal(err)
	}
	recalled := handle(t, r, event("UserPromptSubmit", p, "third", "新增详情页面和独立业务模块怎么安排菜单？"))
	if !strings.Contains(text(recalled), revised.ID) || strings.Contains(text(recalled), "ID="+c.ID+"；") {
		t.Fatal("superseded case recalled as current", recalled)
	}
}

func TestNewResourceRecallBeforeWriteAndNoRepeatedInjection(t *testing.T) {
	s, r, p := fixture(t)
	putCase(t, s, "project:demo", "页面归属", "详情页面属于原菜单模块，不增加菜单。", "", "")
	adr := putCase(t, s, "project:demo", "ADR-0003 数据库迁移约束", "数据库存量不强制迁移。现有数据保持兼容，新增字段先采用可选值。", "", "")
	handle(t, r, event("UserPromptSubmit", p, "s", "新增详情页面"))
	write := event("PreToolUse", p, "s", "")
	write.Tool = "Edit"
	write.Input = map[string]any{"file_path": filepath.Join(p, "migrations", "001_data.sql")}
	out := handle(t, r, write)
	if !strings.Contains(text(out), adr.ID) {
		t.Fatal("migration constraint missed at resource boundary", out)
	}
	if len(handle(t, r, write)) != 0 {
		t.Fatal("same material repeatedly injected")
	}
	write.Tool = "mcp__decision_memory__memory_record"
	if len(handle(t, r, write)) != 0 {
		t.Fatal("memory call self-triggered")
	}
}

func TestProjectIsolationContinueAndUnboundCWD(t *testing.T) {
	s, r, p := fixture(t)
	other := filepath.Join(filepath.Dir(p), "other")
	os.Mkdir(other, 0700)
	s.HookBind(testCtx, other, "project:other")
	private := putCase(t, s, "project:other", "页面菜单私密案例", "页面菜单秘密决定。", "", "")
	out := handle(t, r, event("UserPromptSubmit", p, "s", "设计页面菜单"))
	if strings.Contains(text(out), private.ID) {
		t.Fatal("cross-project leak")
	}
	first := latest(t, s, "s")
	handle(t, r, event("UserPromptSubmit", p, "s", "继续"))
	if latest(t, s, "s").Query != first.Query {
		t.Fatal("continue lost task topic")
	}
	unbound := filepath.Join(filepath.Dir(p), "project-sibling")
	os.Mkdir(unbound, 0700)
	if len(handle(t, r, event("UserPromptSubmit", unbound, "s", "页面菜单"))) != 0 {
		t.Fatal("prefix collision bound wrong project")
	}
}

func TestReviewSkipProvenanceAndStableEventRetry(t *testing.T) {
	s, r, p := fixture(t)
	e := event("UserPromptSubmit", p, "s", "感谢，今天先到这里。")
	e.EventID = "event-1"
	handle(t, r, e)
	turn := latest(t, s, "s")
	stop := event("Stop", p, "s", "")
	stop.Answer = "好的。"
	handle(t, r, stop)
	handle(t, r, e)
	if latest(t, s, "s").SourceID != turn.SourceID {
		t.Fatal("stable event replay duplicated raw input")
	}
	bad := e
	bad.Prompt = "different"
	if _, err := r.Handle(testCtx, bad); err == nil {
		t.Fatal("event ID reuse accepted changed text")
	}
	noSource := putCase(t, s, "project:demo", "无证据案例", "不能用无来源案例冒充已处理本条反馈。", "", "")
	if _, err := s.HookReviewDone(testCtx, memory.ReviewInput{Scope: turn.Scope, SourceID: turn.SourceID, CaseIDs: []string{noSource.ID}, Note: "done"}); err == nil {
		t.Fatal("unlinked case accepted")
	}
	in := memory.ReviewInput{Scope: turn.Scope, SourceID: turn.SourceID, Note: "普通结束语，没有业务决策或纠偏"}
	ack, err := s.HookReviewDone(testCtx, in)
	if err != nil || ack.Status != "skipped" {
		t.Fatal(ack, err)
	}
	if _, err = s.HookReviewDone(testCtx, in); err != nil {
		t.Fatal("skip receipt retry failed", err)
	}
	if len(handle(t, r, stop)) != 0 {
		t.Fatal("completed review requested again")
	}
	diag, err := s.HookDiagnostics(testCtx)
	if err != nil || diag["pending_reviews"].(int) != 0 {
		t.Fatal(diag, err)
	}
}

func TestSetupPreservesConfigAndReusesDataWithoutProjectWrites(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	os.MkdirAll(home, 0700)
	os.Mkdir(project, 0700)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	config := filepath.Join(home, ".zcode", "cli", "config.json")
	os.MkdirAll(filepath.Dir(config), 0700)
	data := filepath.Join(home, "memory")
	before := map[string]any{"unrelated": "keep", "mcp": map[string]any{"servers": map[string]any{"other": map[string]any{"command": "other-tool"}, "decision-memory": map[string]any{"command": "/old/memory-mcp", "args": []string{"serve", "--data-dir", data}, "env": map[string]string{"KEEP": "value"}}}}, "hooks": map[string]any{"enabled": true, "events": map[string]any{"UserPromptSubmit": []any{map[string]any{"hooks": []any{map[string]any{"type": "process", "command": "other-hook", "args": []any{}}}}}}}}
	raw, _ := json.Marshal(before)
	os.WriteFile(config, raw, 0600)
	o := SetupOptions{Config: config, DataDir: data, Project: project, Scope: "project:demo", Binary: filepath.Join(home, "memory-mcp"), Capture: true, Review: true}
	report, err := Setup(testCtx, o)
	if err != nil {
		t.Fatal(err)
	}
	if report["host_event_verified"] != false {
		t.Fatal("configuration falsely claims runtime verification")
	}
	b, _ := os.ReadFile(report["config_backup"].(string))
	if string(b) != string(raw) {
		t.Fatal("backup differs")
	}
	_, err = Setup(testCtx, o)
	if err != nil {
		t.Fatal(err)
	}
	c, _, _ := readConfig(config)
	if c["unrelated"] != "keep" {
		t.Fatal("other settings lost")
	}
	mcp := c["mcp"].(map[string]any)["servers"].(map[string]any)
	if mcp["other"] == nil {
		t.Fatal("other MCP removed")
	}
	entry := mcp["decision-memory"].(map[string]any)
	if entry["env"].(map[string]any)["KEEP"] != "value" {
		t.Fatal("MCP env lost")
	}
	events := c["hooks"].(map[string]any)["events"].(map[string]any)
	if len(events["UserPromptSubmit"].([]any)) != 2 {
		t.Fatal("repeated setup duplicated hooks or removed existing handler")
	}
	files, _ := os.ReadDir(project)
	if len(files) != 0 {
		t.Fatal("project modified")
	}
	o.DataDir = filepath.Join(home, "other-data")
	if _, err = Setup(testCtx, o); err == nil {
		t.Fatal("existing data silently redirected")
	}
}

func TestSetupRefusesUnsafeTargetsAndPreservesFallback(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	os.MkdirAll(home, 0700)
	os.Mkdir(project, 0700)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	fallback := filepath.Join(home, ".agents", "mcp.json")
	os.MkdirAll(filepath.Dir(fallback), 0700)
	os.WriteFile(fallback, []byte(`{"mcpServers":{"existing":{"command":"existing-tool"}}}`), 0600)
	o := SetupOptions{Config: filepath.Join(home, ".zcode", "cli", "config.json"), DataDir: filepath.Join(home, "data"), Project: project, Scope: "project:demo", Binary: filepath.Join(home, "memory-mcp"), Capture: true, Review: true}
	if _, err := Setup(testCtx, o); err != nil {
		t.Fatal(err)
	}
	c, _, _ := readConfig(o.Config)
	if c["mcp"].(map[string]any)["servers"].(map[string]any)["existing"] == nil {
		t.Fatal("adding native config hid fallback servers")
	}
	o.Config = filepath.Join(project, ".zcode", "config.json")
	if _, err := Setup(testCtx, o); err == nil {
		t.Fatal("project config accepted")
	}
	o.Config = filepath.Join(home, "disabled.json")
	os.WriteFile(o.Config, []byte(`{"hooks":{"enabled":false,"events":{"Stop":[]}}}`), 0600)
	if _, err := Setup(testCtx, o); err == nil {
		t.Fatal("silently enabled globally disabled hooks")
	}
}

func TestNamedAcceptanceServerDoesNotReplaceFormalMemory(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	p := filepath.Join(root, "project")
	testProject := filepath.Join(root, "acceptance")
	os.MkdirAll(home, 0700)
	os.Mkdir(p, 0700)
	os.Mkdir(testProject, 0700)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	config := filepath.Join(home, ".zcode", "cli", "config.json")
	formal := SetupOptions{Config: config, DataDir: filepath.Join(home, "formal-data"), Project: p, Scope: "project:formal", Binary: filepath.Join(home, "memory-mcp"), Capture: true, Review: true}
	if _, err := Setup(testCtx, formal); err != nil {
		t.Fatal(err)
	}
	probe := formal
	probe.ServerName = "memory-acceptance"
	probe.DataDir = filepath.Join(home, "test-data")
	probe.Project = testProject
	probe.Scope = "project:acceptance"
	if _, err := Setup(testCtx, probe); err != nil {
		t.Fatal(err)
	}
	c, _, _ := readConfig(config)
	servers := c["mcp"].(map[string]any)["servers"].(map[string]any)
	if configData(servers["decision-memory"].(map[string]any)) != formal.DataDir || configData(servers["memory-acceptance"].(map[string]any)) != probe.DataDir {
		t.Fatal("named test setup redirected formal data")
	}
	if len(c["hooks"].(map[string]any)["events"].(map[string]any)["Stop"].([]any)) != 2 {
		t.Fatal("named setup replaced formal hook")
	}
}

func TestCaptureAndRecallCanRunWithoutModelReview(t *testing.T) {
	s, r, p := fixture(t)
	r.Review = false
	handle(t, r, event("UserPromptSubmit", p, "s", "本轮仅保存原始反馈"))
	stop := event("Stop", p, "s", "")
	stop.Answer = "实际业务答复"
	if len(handle(t, r, stop)) != 0 {
		t.Fatal("review disabled still continued model")
	}
	turn := latest(t, s, "s")
	if turn.AnswerID == "" {
		t.Fatal("capture-only mode lost delivered answer")
	}
	answer, err := s.Read(testCtx, turn.AnswerID)
	if err != nil || answer.Entry.Body != stop.Answer {
		t.Fatal(answer, err)
	}
	if err = s.HookBind(testCtx, p, "project:other"); err == nil {
		t.Fatal("project binding silently replaced")
	}
}

func TestResourceExplanationIsDeliveredEvenForPreviouslySeenCase(t *testing.T) {
	s, r, p := fixture(t)
	target := putCase(t, s, "project:demo", "页面校验", "TaskOrderCommonService 处理页面保存边界。", "", "")
	first := handle(t, r, event("UserPromptSubmit", p, "s", "TaskOrderCommonService 的页面保存怎么处理？"))
	if !strings.Contains(text(first), target.ID) || !strings.Contains(text(first), "任务关键词") {
		t.Fatal(first)
	}
	e := event("PreToolUse", p, "s", "")
	e.Tool = "Edit"
	e.Input = map[string]any{"file_path": filepath.Join(p, "TaskOrderCommonService.java")}
	changed := handle(t, r, e)
	if !strings.Contains(text(changed), "当前类名/符号") || !strings.Contains(text(changed), "案例正文") {
		t.Fatal("new resource reason suppressed by ID-only dedup", changed)
	}
	if len(handle(t, r, e)) != 0 {
		t.Fatal("identical resource reason repeated")
	}
	diag, err := s.HookDiagnostics(testCtx)
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range diag["recent_events"].([]map[string]string) {
		if strings.Contains(run["detail"], "页面保存怎么处理") {
			t.Fatal("private query text copied to diagnostics")
		}
	}
}

func TestStopContinuationChecksReplayWithoutRepeatingReview(t *testing.T) {
	s, r, p := fixture(t)
	original := "## 业务报告\n\n详情和编辑沿用原入口。"
	for _, tc := range []struct {
		name   string
		answer string
		want   string
	}{
		{"identical", original, "answer_replay_verified"},
		{"changed", original + "\n已完成记忆整理。", "answer_replay_mismatch"},
		{"missing", "", "answer_replay_unverified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handle(t, r, event("UserPromptSubmit", p, tc.name, "测试业务答复"))
			stop := event("Stop", p, tc.name, "")
			stop.Answer = original
			out := handle(t, r, stop)
			turn := latest(t, s, tc.name)
			if out["decision"] != "block" || turn.AnswerID == "" ||
				!strings.Contains(out["reason"].(string), "memory_read(id="+turn.AnswerID+")") {
				t.Fatal("missing replay instruction", out)
			}
			if _, err := s.HookReviewDone(testCtx, memory.ReviewInput{
				Scope: turn.Scope, SourceID: turn.SourceID, Note: "测试空回执",
			}); err != nil {
				t.Fatal(err)
			}
			stop.StopActiveAlias = true
			stop.Answer = tc.answer
			if out := handle(t, r, stop); len(out) != 0 {
				t.Fatal("replay triggered another continuation", out)
			}
			updated := latest(t, s, tc.name)
			if updated.Status != "skipped" || updated.Attempts != 1 || updated.AnswerID != turn.AnswerID {
				t.Fatal("replay changed review state", updated)
			}
			diagnostics, err := s.HookDiagnostics(testCtx)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range diagnostics["recent_events"].([]map[string]string) {
				if row["session"] == tc.name && row["result"] == tc.want && row["detail"] == turn.SourceID {
					return
				}
			}
			t.Fatal("missing replay outcome", diagnostics)
		})
	}
}
