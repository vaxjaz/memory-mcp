package hooks

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

func TestAssistReminderHasNoCaptureGateOrStopContinuation(t *testing.T) {
	s, r, p := fixture(t)
	r.Mode = memory.ModeAssist
	r.Capture = false
	before, err := s.Stats(testCtx)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"SessionStart", "UserPromptSubmit", "UserPromptSubmit", "PreToolUse", "Stop"} {
		out := handle(t, r, event(name, p, "assist", "Actual request, not implicitly archived"))
		if out["decision"] == "block" {
			t.Fatal("assist blocked work", out)
		}
		if name == "PreToolUse" || name == "Stop" {
			if len(out) != 0 {
				t.Fatal("assist continued work at action/Stop", out)
			}
		} else if len(out) == 0 {
			t.Fatal("soft reminder not delivered", name)
		}
	}
	after, err := s.Stats(testCtx)
	if err != nil || before != after {
		t.Fatal("reminder changed entries", before, after, err)
	}
	items, _, err := s.PendingCandidates(testCtx, "project:demo", "", 20)
	if err != nil || len(items) != 0 {
		t.Fatal("ordinary reminder created candidates", items, err)
	}
	if _, err := s.HookLatest(testCtx, "project:demo", "assist"); err == nil {
		t.Fatal("reminder captured a user source without consent")
	}
}

func TestSetupStrictToAssistPreservesRulesDataBindingsAndOtherHooks(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	os.Mkdir(project, 0700)
	rules := []byte("SQL -> existing develop rules; delivery follows existing workflow.\n")
	rulesPath := filepath.Join(project, "AGENTS.md")
	os.WriteFile(rulesPath, rules, 0600)
	config := filepath.Join(root, "user", "config.json")
	os.MkdirAll(filepath.Dir(config), 0700)
	original := `{"mcp":{"servers":{"other":{"command":"other-tool"}}},"hooks":{"events":{"Stop":[{"hooks":[{"type":"process","command":"other-hook","args":[]}]}]}}}`
	os.WriteFile(config, []byte(original), 0600)
	o := SetupOptions{Host: "zcode", Mode: memory.ModeStrict, Capture: true, Config: config, DataDir: filepath.Join(root, "data"), Project: project, Scope: "project:demo", Binary: filepath.Join(root, "memory-mcp")}
	if _, err := Setup(testCtx, o); err != nil {
		t.Fatal(err)
	}
	s, err := memory.Open(o.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.Put(testCtx, memory.Draft{Kind: "record", Scope: o.Scope, Title: "Evidence", Body: "Preserve exact evidence", Origin: "user_supplied"}, "")
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	o.Mode = memory.ModeAssist
	o.Capture = false
	report, err := Setup(testCtx, o)
	if err != nil {
		t.Fatal(err)
	}
	if report["mode"] != "assist" || report["raw_capture"] != false {
		t.Fatal(report)
	}
	raw, _ := os.ReadFile(config)
	var c map[string]any
	json.Unmarshal(raw, &c)
	events := c["hooks"].(map[string]any)["events"].(map[string]any)
	for _, name := range []string{"SessionStart", "UserPromptSubmit"} {
		if len(events[name].([]any)) != 1 {
			t.Fatal(name, events)
		}
	}
	if len(events["PreToolUse"].([]any)) != 0 || len(events["Stop"].([]any)) != 1 {
		t.Fatal("old gate/Stop not removed or unrelated hook lost", events)
	}
	if c["mcp"].(map[string]any)["servers"].(map[string]any)["other"] == nil {
		t.Fatal("unrelated server lost")
	}
	actual, _ := os.ReadFile(rulesPath)
	if !bytes.Equal(actual, rules) {
		t.Fatal("project rules modified")
	}
	s, err = memory.Open(o.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Read(testCtx, saved.ID)
	if err != nil || got.Entry.Body != saved.Body {
		t.Fatal("existing evidence lost", err)
	}
	bound, err := s.HookResolve(testCtx, project)
	if err != nil || bound != o.Scope {
		t.Fatal(bound, err)
	}
}
