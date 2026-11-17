package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

func TestDeliveryA13A14A30A31NativeEnvelope(t *testing.T) {
	s, r, p := trialFixture(t)
	r.Host = "codex"
	e := event("UserPromptSubmit", p, "session", "")
	e.TurnID = "t1"
	handle(t, r, e)
	m, err := s.MatchExperiences(testCtx, "project:demo", "LYN 创建准入")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.ChallengeExperience(testCtx, "codex", "project:demo", "session", "t1", p, m)
	if err != nil {
		t.Fatal(err)
	}
	input := event("PreToolUse", p, "session", "")
	input.Tool = "mcp__decision_memory__memory_experience_lookup"
	input.ToolUseID = "native1"
	input.Input = map[string]any{"scope": "project:demo", "challenge_id": c.ID}
	if o := handle(t, r, input); len(o) != 0 {
		t.Fatal("A31 lookup observer injected context")
	}
	result, err := s.LookupExperience(testCtx, memory.ExperienceLookup{Scope: "project:demo", Task: "合成任务", Questions: "合成不确定点", Queries: []memory.RecallQuery{{Terms: []string{"LYN"}}}, ChallengeID: c.ID})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result)
	for _, test := range []struct {
		name      string
		response  any
		transport bool
	}{
		{"isError", map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": string(encoded)}}}, false},
		{"parse", map[string]any{"content": []any{map[string]any{"type": "text", "text": "invalid"}}}, false},
		{"transport", map[string]any{"content": []any{map[string]any{"type": "text", "text": string(encoded)}}}, true},
		{"business text", map[string]any{"text": string(encoded)}, false},
		{"duplicates", map[string]any{"content": []any{map[string]any{"type": "text", "text": string(encoded)}, map[string]any{"type": "text", "text": string(encoded)}}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := input
			e.Name = "PostToolUse"
			e.Response = test.response
			e.IsError = test.transport
			if _, err := r.Handle(testCtx, e); err == nil {
				t.Fatal("A13 accepted unsupported/failed result")
			}
		})
	}
	// The recorded technical fault fails open without pretending delivery. A
	// subsequent actual success still records the original body's actual arrival.
	observeLookup(t, s, r, p, "session", "native1", c.ID, result)
	again, err := s.ChallengeExperience(testCtx, "codex", "project:demo", "session", "t1", p, m)
	if err != nil || again.ID != "" {
		t.Fatal("successful native result unrecognized", again, err)
	}
	matcher := regexp.MustCompile(lookupToolMatcher("test-service.1"))
	for _, name := range []string{"mcp__test-service.1__memory_experience_lookup", "mcp__test_service.1__memory_experience_lookup"} {
		if !matcher.MatchString(name) {
			t.Fatal("A30 exact service excluded", name)
		}
	}
	for _, name := range []string{"mcp__test-serviceX1__memory_experience_lookup", "mcp__other__memory_experience_lookup", "mcp__test-service.1__memory_read"} {
		if matcher.MatchString(name) {
			t.Fatal("A30 overbroad", name)
		}
	}
}

func TestDeliveryA30ReinstallMatchers(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "project")
	os.Mkdir(p, 0700)
	for _, host := range []string{"codex", "zcode"} {
		t.Run(host, func(t *testing.T) {
			ext := "config.json"
			if host == "codex" {
				ext = "config.toml"
			}
			o := SetupOptions{Host: host, Config: filepath.Join(root, host, ext), DataDir: filepath.Join(root, "data-"+host), Project: p, Scope: "project:demo", Binary: filepath.Join(root, "memory-mcp"), ServerName: "delivery-fixture"}
			for i := 0; i < 2; i++ {
				if _, err := Setup(testCtx, o); err != nil {
					t.Fatal(err)
				}
			}
			adapter, _ := AdapterFor(host)
			c, _, _, err := readAdapterConfig(adapter, o.Config)
			if err != nil {
				t.Fatal(err)
			}
			events := c["hooks"].(map[string]any)["events"].(map[string]any)
			for _, name := range []string{"PreToolUse", "PostToolUse"} {
				groups := events[name].([]any)
				if len(groups) != 1 {
					t.Fatal("A30 duplicate handler", groups)
				}
				g := groups[0].(map[string]any)
				pattern := g["matcher"].(string)
				re, err := regexp.Compile(pattern)
				if err != nil {
					t.Fatal(err)
				}
				if !re.MatchString("mcp__delivery_fixture__memory_experience_lookup") || re.MatchString("mcp__other__memory_experience_lookup") {
					t.Fatal("A30 matcher", pattern)
				}
			}
			if _, exists := events["Stop"]; exists {
				t.Fatal("Stop gate installed")
			}
		})
	}
}

func TestDeliveryA33MissingIdentityFailsOpenWithoutOutput(t *testing.T) {
	_, r, p := trialFixture(t)
	handle(t, r, event("UserPromptSubmit", p, "session", ""))
	e := event("PreToolUse", p, "session", "")
	e.Tool = "mcp__decision_memory__memory_experience_lookup"
	e.Input = map[string]any{"scope": "project:demo"}
	out, err := r.Handle(testCtx, e)
	if err == nil || len(out) != 0 {
		t.Fatal("missing native identity invented success", out, err)
	}
	for _, tool := range []string{"memory_read", "mcp__decision_memory__memory_read", "mcp__decision_memory__memory_experience_record"} {
		e.Tool = tool
		if out := handle(t, r, e); len(out) != 0 {
			t.Fatal("A31 memory recursion", tool, out)
		}
	}
	if strings.Contains(text(map[string]any{}), "additionalContext") {
		t.Fatal("silent output gained context")
	}
}
