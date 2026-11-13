package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vaxjaz/memory-mcp/internal/textguard"
)

// Adapters own client configuration and wire conversion. Memory semantics remain
// in Runtime/Store; an unregistered host never inherits a guessed protocol.
type Adapter struct {
	Name           string `json:"name"`
	ConfigFormat   string `json:"config_format"`
	ConfigRelative string `json:"config_relative"`
	SeparateHooks  bool   `json:"separate_hooks"`
	RequiresTrust  bool   `json:"requires_hook_trust"`
	NativeTurnIDs  bool   `json:"native_turn_ids"`
	BlockViaExit   bool   `json:"block_via_exit_2"`
	Reference      string `json:"official_protocol_reference"`
	Validation     string `json:"validation"`
}

var adapters = []Adapter{
	{Name: "zcode", ConfigFormat: "zcode-json", ConfigRelative: filepath.Join(".zcode", "cli", "config.json"), Validation: "existing adapter; real prior ZCode acceptance plus protocol regressions"},
	{Name: "codex", ConfigFormat: "codex-toml", ConfigRelative: filepath.Join(".codex", "config.toml"), SeparateHooks: true, RequiresTrust: true, NativeTurnIDs: true, Reference: "https://learn.chatgpt.com/docs/hooks", Validation: "documented protocol and isolated executable regressions; native trust/event delivery must be verified locally"},
	{Name: "qoder", ConfigFormat: "qoder-json", ConfigRelative: filepath.Join(".qoder", "settings.json"), BlockViaExit: true, Reference: "https://docs.qoder.com/cli/hooks", Validation: "Qoder CLI protocol regressions; native client/version verification required"},
	{Name: "qoder-ide", ConfigFormat: "qoder-json", ConfigRelative: filepath.Join(".qoder", "settings.json"), BlockViaExit: true, Reference: "https://docs.qoder.com/extensions/hooks", Validation: "IDE/JB command-hook protocol regressions; do not infer native execution from CLI compatibility"},
}

func Adapters() []Adapter { return append([]Adapter(nil), adapters...) }
func AdapterFor(name string) (Adapter, error) {
	for _, a := range adapters {
		if a.Name == name {
			return a, nil
		}
	}
	return Adapter{}, fmt.Errorf("host %q is not implemented; run memory-mcp adapters; no guessed adapter is installed", name)
}
func (a Adapter) DefaultConfig(home string) string {
	if a.Name == "codex" {
		if dir := os.Getenv("CODEX_HOME"); dir != "" {
			return filepath.Join(dir, "config.toml")
		}
	}
	return filepath.Join(home, a.ConfigRelative)
}
func (a Adapter) DefaultHooks(config string) string {
	if a.SeparateHooks {
		return filepath.Join(filepath.Dir(config), "hooks.json")
	}
	return config
}

func (a Adapter) Decode(raw []byte) (Event, error) {
	var e Event
	if err := textguard.JSON(raw); err != nil {
		return e, err
	}
	if err := json.Unmarshal(raw, &e); err != nil {
		return e, err
	}
	e.normalize()
	if e.Name != "SessionStart" && e.Name != "UserPromptSubmit" && e.Name != "PreToolUse" && e.Name != "Stop" {
		return e, nil
	}
	if strings.TrimSpace(e.CWD) == "" || strings.TrimSpace(e.Session) == "" {
		return e, errors.New("hook requires actual cwd and session_id; neither project nor host is inferred")
	}
	if a.NativeTurnIDs && e.Name != "SessionStart" && strings.TrimSpace(e.TurnID) == "" {
		return e, errors.New("Codex turn-scoped event requires native turn_id; update/verify this client protocol rather than attaching it to another turn")
	}
	if a.NativeTurnIDs && e.Name == "UserPromptSubmit" {
		e.EventID = e.TurnID
	}
	return e, nil
}

type HookResponse struct {
	ExitCode int
	Stdout   []byte
	Stderr   string
}

func (a Adapter) Encode(event Event, out map[string]any) (HookResponse, error) {
	if a.BlockViaExit && out["decision"] == "block" {
		reason, _ := out["reason"].(string)
		if strings.TrimSpace(reason) == "" {
			return HookResponse{}, errors.New("blocking hook needs a reason")
		}
		// Exit 2 + stderr is documented for both Qoder CLI and IDE/JB. Never
		// let a JSON-only success accidentally allow a tool on another client.
		return HookResponse{ExitCode: 2, Stderr: reason}, nil
	}
	if event.Name == "PreToolUse" && out["decision"] == "block" {
		reason, _ := out["reason"].(string)
		out["hookSpecificOutput"] = map[string]any{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason}
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return HookResponse{}, err
	}
	return HookResponse{Stdout: append(raw, '\n')}, nil
}

type HookExitError struct{ Code int }

func (e *HookExitError) Error() string { return fmt.Sprintf("hook requested exit %d", e.Code) }
