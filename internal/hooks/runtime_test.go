package hooks

import (
	"context"
	"encoding/json"
	"github.com/vaxjaz/memory-mcp/internal/memory"
	"os"
	"path/filepath"
	"testing"
)

var testCtx = context.Background()

func fixture(t *testing.T) (*memory.Store, Runtime, string) {
	t.Helper()
	root := t.TempDir()
	p := filepath.Join(root, "project")
	os.Mkdir(p, 0700)
	s, err := memory.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err = s.HookBind(testCtx, p, "project:demo"); err != nil {
		t.Fatal(err)
	}
	return s, Runtime{Store: s}, p
}
func handle(t *testing.T, r Runtime, e Event) map[string]any {
	t.Helper()
	out, err := r.Handle(testCtx, e)
	if err != nil {
		t.Fatal(err)
	}
	if emission, ok := out["_cue_emission"].(string); ok {
		if err = r.Store.FinishCueEmission(testCtx, emission, true); err != nil {
			t.Fatal(err)
		}
		delete(out, "_cue_emission")
	}
	return out
}
func event(name, cwd, session, prompt string) Event {
	return Event{Name: name, CWD: cwd, Session: session, Prompt: prompt}
}
func text(out map[string]any) string { b, _ := json.Marshal(out); return string(b) }

func observeLookup(t *testing.T, s *memory.Store, r Runtime, p, session, call, challenge string, result memory.ExperienceLookupResult) {
	t.Helper()
	e := event("PreToolUse", p, session, "")
	e.Tool = "mcp__decision_memory__memory_experience_lookup"
	e.ToolUseID = call
	e.Input = map[string]any{"scope": "project:demo", "challenge_id": challenge}
	handle(t, r, e)
	e.Name = "PostToolUse"
	b, _ := json.Marshal(result)
	e.Response = map[string]any{"content": []any{map[string]any{"type": "text", "text": string(b)}}}
	if out := handle(t, r, e); len(out) != 0 {
		t.Fatal("receipt injected context", out)
	}
}
