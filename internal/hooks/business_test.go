package hooks

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

func TestBusinessResultObservationWakesOnlySameObjectAndInput(t *testing.T) {
	s, r, p := trialFixture(t)
	saved, err := s.RecordAgreement(testCtx, memory.AgreementWrite{Scope: "project:demo", Project: p, Title: "isolated agreement reference", Agreement: memory.Agreement{
		Anchors: []string{"WXN", "工单"}, AnchorPrefixes: []string{"WXN"}, Signals: []string{"取消"},
		Requirement: "仅为隔离测试的核验约定", Applicability: "隔离工单处理", Boundary: "不覆盖审批撤回，不授予执行权限",
		Authority: "isolated fixture owner", ConfirmationReference: "fixture:confirmed-agreement",
		Evidence: []memory.ExperienceEvidence{{Kind: "business", Reference: "fixture:confirmed-agreement", Observation: "explicitly synthetic test agreement"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	input := event("UserPromptSubmit", p, "business-session", "处理当前工单，不复制正文 PRIVATE_SENTINEL")
	input.TurnID = "turn-a"
	handle(t, r, input)
	read := event("PostToolUse", p, "business-session", "")
	read.TurnID = "turn-a"
	read.Tool = "mcp__office__get_ticket"
	read.Response = map[string]any{"content": []any{map[string]any{"type": "text", "text": "隔离测试来源：取消 WXN-001 PRIVATE_RESULT_SENTINEL"}}}
	if out := handle(t, r, read); len(out) != 0 {
		t.Fatal("source read injected memory material", out)
	}
	query := event("PreToolUse", p, "business-session", "")
	query.TurnID = "turn-a"
	query.Tool = "mcp__mysql__query"
	query.Input = map[string]any{"sql": "select status from work_order where order_no='WXN-002'"}
	if out := handle(t, r, query); len(out) != 0 {
		t.Fatal("another object was blocked", out)
	}
	query.Input["sql"] = "select status from work_order where order_no='WXN-001'"
	out := handle(t, r, query)
	if out["decision"] != "block" {
		t.Fatal("same-object query did not wake", out)
	}
	reason := out["reason"].(string)
	if strings.Contains(reason, "核验约定") {
		t.Fatal("agreement content injected")
	}
	token := regexp.MustCompile(`challenge_id="([a-f0-9]{32})"`).FindStringSubmatch(reason)
	if len(token) != 2 {
		t.Fatal(reason)
	}
	lookup, err := s.LookupExperience(testCtx, memory.ExperienceLookup{Scope: "project:demo", Task: "核对当前取消要求", Questions: "当前业务约定是否适用", Queries: []memory.RecallQuery{{Terms: []string{"WXN", "取消"}}}, ChallengeID: token[1]})
	if err != nil || len(lookup.Items) != 1 || lookup.Items[0].ID != saved.ID || lookup.ChallengeOutcome != "query_completed_not_applicability_verified" {
		t.Fatal(lookup, err)
	}
	if out := handle(t, r, query); len(out) != 0 {
		t.Fatal("lookup was not recognized", out)
	}
	input.TurnID = "turn-b"
	handle(t, r, input)
	query.TurnID = "turn-b"
	if out := handle(t, r, query); len(out) != 0 {
		t.Fatal("old source observation leaked into new input", out)
	}
	// Observation bookkeeping contains only bounded IDs/hashes; entries contain
	// neither the user request nor the source tool output.
	page, err := s.SearchPage(testCtx, memory.SearchInput{Scope: "project:demo", Query: "PRIVATE_SENTINEL PRIVATE_RESULT_SENTINEL", IncludeEvents: true})
	if err != nil || len(page.Results) != 0 {
		t.Fatal("raw text captured", page, err)
	}
}

func TestBusinessCluesStayBoundedAndDoNotProbeEveryTool(t *testing.T) {
	for _, name := range []string{"mcp__decision_memory__memory_experience_lookup", "web_search", "mcp__github__fetch", "apply_patch", "Skill"} {
		if isBusinessTool(name) {
			t.Fatal("overbroad native selector", name)
		}
	}
	if !isBusinessTool("mcp__mysql__query") {
		t.Fatal("known database tool omitted")
	}
	e := Event{Tool: "mcp__mysql__query", Input: map[string]any{"sql": "select order_no from t", "password": "DO_NOT_PROBE_SECRET"}}
	if strings.Contains(businessClues(e, false), "DO_NOT_PROBE_SECRET") {
		t.Fatal("secret field probed")
	}
	e.Input["sql"] = strings.Repeat("x", 32769)
	if businessClues(e, false) != "" {
		t.Fatal("oversized probe partially matched")
	}
	b, _ := json.Marshal(Event{Name: "PostToolUse", CWD: "/workspace", Session: "session", Response: map[string]any{"text": "data"}})
	decoded, err := (Adapter{Name: "zcode"}).Decode(b)
	if err != nil || decoded.Response == nil {
		t.Fatal("post event response lost", decoded, err)
	}
}

func TestCompactionAndExplicitChildIdentityDoNotReuseParentRead(t *testing.T) {
	s, r, p := trialFixture(t)
	start := event("UserPromptSubmit", p, "identity-session", "current request")
	start.TurnID = "identity-turn"
	handle(t, r, start)
	query := event("PreToolUse", p, "identity-session", "")
	query.TurnID = "identity-turn"
	query.Tool = "Grep"
	query.Input = map[string]any{"pattern": "LYN 创建准入"}
	blocked := handle(t, r, query)
	token := regexp.MustCompile(`challenge_id="([a-f0-9]{32})"`).FindStringSubmatch(blocked["reason"].(string))[1]
	if _, err := s.LookupExperience(testCtx, memory.ExperienceLookup{Scope: "project:demo", Task: "same investigation", Questions: "check current mechanism", Keywords: []string{"LYN"}, ChallengeID: token}); err != nil {
		t.Fatal(err)
	}
	compact := event("SessionStart", p, "identity-session", "")
	compact.Source = "compact"
	handle(t, r, compact)
	again := handle(t, r, query)
	if again["decision"] != "block" {
		t.Fatal("compaction reused lookup flag", again)
	}
	newToken := regexp.MustCompile(`challenge_id="([a-f0-9]{32})"`).FindStringSubmatch(again["reason"].(string))[1]
	if newToken == token {
		t.Fatal("compaction retained old challenge")
	}
	child := query
	child.AgentID = "isolated-child"
	if out := handle(t, r, child); len(out) != 0 {
		t.Fatal("parent input/permit reused by child lacking own epoch", out)
	}
}
