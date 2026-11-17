package hooks

import (
	"context"
	"github.com/vaxjaz/memory-mcp/internal/memory"
)

type Event struct {
	Name         string         `json:"hook_event_name"`
	NameAlias    string         `json:"hookEventName"`
	Session      string         `json:"session_id"`
	SessionAlias string         `json:"sessionId"`
	CWD          string         `json:"cwd"`
	Prompt       string         `json:"prompt"`
	EventID      string         `json:"event_id"`
	TurnID       string         `json:"turn_id"`
	Tool         string         `json:"tool_name"`
	ToolAlias    string         `json:"toolName"`
	Input        map[string]any `json:"tool_input"`
	InputAlias   map[string]any `json:"toolInput"`
}

func (e *Event) normalize() {
	if e.Name == "" {
		e.Name = e.NameAlias
	}
	if e.Session == "" {
		e.Session = e.SessionAlias
	}
	if e.Tool == "" {
		e.Tool = e.ToolAlias
	}
	if e.Input == nil {
		e.Input = e.InputAlias
	}
}

type Runtime struct {
	Host       string
	Store      *memory.Store
	ServerName string
}

func (r Runtime) Handle(ctx context.Context, e Event) (map[string]any, error) {
	e.normalize()
	empty := map[string]any{}
	if e.Name != "SessionStart" && e.Name != "UserPromptSubmit" && e.Name != "PreToolUse" {
		return empty, nil
	}
	scope, err := r.Store.HookResolve(ctx, e.CWD)
	if err != nil {
		return empty, nil
	}
	return r.experience(ctx, e, scope)
}
func (r Runtime) host() string {
	if r.Host == "" {
		return "zcode"
	}
	return r.Host
}
func (r Runtime) serverName() string {
	if r.ServerName == "" {
		return "decision-memory"
	}
	return r.ServerName
}
func contextOutput(event, body string) map[string]any {
	return map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": event, "additionalContext": body}}
}
