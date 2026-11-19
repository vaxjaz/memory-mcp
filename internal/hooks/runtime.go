package hooks

import (
	"context"
	"encoding/json"
	"github.com/vaxjaz/memory-mcp/internal/memory"
)

type Event struct {
	ToolUseID      string         `json:"tool_use_id"`
	ToolUseIDAlias string         `json:"toolUseId"`
	ToolCallID     string         `json:"toolCallId"`
	IsError        bool           `json:"is_error"`
	Error          any            `json:"error"`
	Source         string         `json:"source"`
	AgentID        string         `json:"agent_id"`
	Name           string         `json:"hook_event_name"`
	NameAlias      string         `json:"hookEventName"`
	Session        string         `json:"session_id"`
	SessionAlias   string         `json:"sessionId"`
	CWD            string         `json:"cwd"`
	Prompt         string         `json:"prompt"`
	EventID        string         `json:"event_id"`
	TurnID         string         `json:"turn_id"`
	TurnIDAlias    string         `json:"turnId"`
	Tool           string         `json:"tool_name"`
	ToolAlias      string         `json:"toolName"`
	Input          map[string]any `json:"tool_input"`
	InputAlias     map[string]any `json:"toolInput"`
	Response       any            `json:"tool_response"`
	ResponseAlias  any            `json:"toolResponse"`
}

func (e *Event) normalize() {
	if e.ToolUseID == "" {
		e.ToolUseID = e.ToolUseIDAlias
	}
	if e.ToolUseID == "" {
		e.ToolUseID = e.ToolCallID
	}
	if e.TurnID == "" {
		e.TurnID = e.TurnIDAlias
	}
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
	if e.Response == nil {
		e.Response = e.ResponseAlias
	}
}

type Runtime struct {
	Host       string
	Store      *memory.Store
	ServerName string
}

func (e *Event) normalizeAgentSession() {
	e.normalize()
	if e.AgentID != "" {
		if e.Session != "" {
			identity, _ := json.Marshal([]string{e.Session, e.AgentID})
			e.Session = string(identity)
		}
	}
}

func (r Runtime) Handle(ctx context.Context, e Event) (map[string]any, error) {
	e.normalizeAgentSession()
	empty := map[string]any{}
	if e.Name != "SessionStart" && e.Name != "UserPromptSubmit" && e.Name != "PreToolUse" && e.Name != "PostToolUse" && !(r.host() == "zcode" && e.Name == "PostToolUseFailure") {
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
