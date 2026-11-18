package hooks

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

func TestZCodeNamedMCPLookupRecordsSuccessfulPost(t *testing.T) {
	store, runtime, project := trialFixture(t)
	runtime.Host = "zcode"
	runtime.ServerName = "memory-review-trial"
	adapter, _ := AdapterFor("zcode")
	const session = "zcode-native-shape"
	const lookupName = "mcp__memory-review-trial__memory_experience_lookup"
	run := func(name, tool, call string, input map[string]any, response any) map[string]any {
		t.Helper()
		// ZCode ships camelCase fields and also emits snake_case tool aliases.
		payload := map[string]any{
			"cwd": project, "hookEventName": name, "sessionId": session, "turnId": "native-turn",
			"toolName": tool, "toolCallId": call, "toolInput": input, "toolResponse": response,
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		event, err := adapter.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		if event.TurnID != "native-turn" || event.ToolUseID != call {
			t.Fatalf("native identity lost: %+v", event)
		}
		return handle(t, runtime, event)
	}
	run("UserPromptSubmit", "", "", nil, nil)
	sql := map[string]any{"sql": "select status from fixture where note='LYN 创建准入'"}
	blocked := run("PreToolUse", "mcp__fixture__mysql_query", "db-call", sql, nil)
	if blocked["decision"] != "block" {
		t.Fatalf("namespaced mysql_query bypassed matching: %v", blocked)
	}
	challenge := regexp.MustCompile(`challenge_id="([a-f0-9]{32})"`).FindStringSubmatch(blocked["reason"].(string))[1]
	args := map[string]any{"scope": "project:demo", "challenge_id": challenge}
	run("PreToolUse", lookupName, "lookup-call", args, nil)
	page, err := store.LookupExperience(testCtx, memory.ExperienceLookup{
		Scope: "project:demo", Project: project, Task: "Synthetic ZCode dispatch",
		Questions: "Check actual candidate", Keywords: []string{"LYN"}, ChallengeID: challenge,
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	post := run("PostToolUse", lookupName, "lookup-call", args,
		map[string]any{"content": []any{map[string]any{"type": "text", "text": string(body)}}})
	if len(post) != 0 {
		t.Fatalf("receipt hook emitted context: %v", post)
	}
	if out := run("PreToolUse", "mysql_query", "db-retry", sql, nil); len(out) != 0 {
		t.Fatalf("lookup receipt did not release original work: %v", out)
	}
	report, err := store.HookDiagnostics(testCtx, "")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(report)
	s := string(encoded)
	if !strings.Contains(s, "lookup_call_bound") || !strings.Contains(s, "lookup_result_observed") || strings.Contains(s, "select status") {
		t.Fatalf("missing diagnostic stages or raw SQL logged: %s", s)
	}
}

func TestZCodeNativeFailureDoesNotCountAsDelivered(t *testing.T) {
	store, runtime, project := trialFixture(t)
	runtime.Host = "zcode"
	run := func(name, tool, call string, input map[string]any) map[string]any {
		t.Helper()
		return handle(t, runtime, Event{Name: name, CWD: project, Session: "zcode-failure", TurnID: "native-turn", Tool: tool, ToolUseID: call, Input: input})
	}
	run("UserPromptSubmit", "", "", nil)
	input := map[string]any{"sql": "select 'LYN 创建准入'"}
	out := run("PreToolUse", "mysql_query", "db-call", input)
	if out["decision"] != "block" {
		t.Fatal(out)
	}
	challenge := regexp.MustCompile(`challenge_id="([a-f0-9]{32})"`).FindStringSubmatch(out["reason"].(string))[1]
	lookup := map[string]any{"scope": "project:demo", "challenge_id": challenge}
	const tool = "mcp__decision-memory__memory_experience_lookup"
	run("PreToolUse", tool, "fail-call", lookup)
	run("PostToolUseFailure", tool, "fail-call", lookup)
	if out = run("PreToolUse", "mysql_query", "db-retry", input); len(out) != 0 {
		t.Fatalf("scope-limited native failure did not fail open: %v", out)
	}
	report, err := store.HookDiagnostics(testCtx, "")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(report)
	if !strings.Contains(string(data), "lookup_failed_native") || strings.Contains(string(data), "lookup_result_observed") {
		t.Fatal(string(data))
	}
}
