package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

type probeFactRule struct {
	Name       string          `json:"name"`
	Tool       string          `json:"tool,omitempty"`
	MCPTool    string          `json:"mcp_tool,omitempty"`
	Key        []string        `json:"key"`
	When       *probeCondition `json:"when"`
	Lifetime   string          `json:"lifetime,omitempty"`
	TTLSeconds int             `json:"ttl_seconds,omitempty"`
}

func (c probeConfig) validateFacts() error {
	total := 0
	for scope, rules := range c.Facts {
		if !strings.HasPrefix(scope, "project:") || strings.TrimSpace(scope) != scope || len(scope) <= len("project:") {
			return errors.New("context_facts keys must be project:<identifier>")
		}
		total += len(rules)
		if total > 128 {
			return errors.New("context_facts supports at most 128 rules")
		}
		seen := map[string]bool{}
		for _, rule := range rules {
			if !probeRuleID.MatchString(rule.Name) || seen[rule.Name] {
				return fmt.Errorf("context_facts %s requires unique fact names", scope)
			}
			seen[rule.Name] = true
			if err := validateProbeSelector(rule.Tool, rule.MCPTool); err != nil {
				return fmt.Errorf("fact %q: %w", rule.Name, err)
			}
			if !validProbeKey(rule.Key, true) || rule.When == nil {
				return fmt.Errorf("fact %q requires key and when", rule.Name)
			}
			if err := rule.When.validate(true, nil, 0); err != nil {
				return fmt.Errorf("fact %q: %w", rule.Name, err)
			}
			if rule.Lifetime != "" && rule.Lifetime != "input" && rule.Lifetime != "session" {
				return fmt.Errorf("fact %q lifetime must be input or session", rule.Name)
			}
			if rule.TTLSeconds < 0 || rule.TTLSeconds > 600 {
				return fmt.Errorf("fact %q ttl_seconds must be 0-600 (0 uses 120 seconds)", rule.Name)
			}
		}
	}
	return nil
}

func (c probeConfig) producers(scope, tool string) []probeFactRule {
	var matched []probeFactRule
	for _, rule := range c.Facts[scope] {
		pattern := probeRule{Tool: rule.Tool, MCPTool: rule.MCPTool}.pattern()
		if regexp.MustCompile(pattern).MatchString(tool) {
			matched = append(matched, rule)
		}
	}
	return matched
}

func probeDefinition(v any) string {
	data, _ := json.Marshal(v)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func probeResult(e Event) map[string]any {
	if e.Name == "PostToolUseFailure" || e.IsError || e.Error != nil {
		return nil
	}
	result, _ := e.Response.(map[string]any)
	if result == nil || result["isError"] == true {
		return nil
	}
	if structured, found := result["structuredContent"]; found {
		object, _ := structured.(map[string]any)
		return object
	}
	if content, ok := result["content"].([]any); ok {
		if len(content) != 1 {
			return nil
		}
		block, _ := content[0].(map[string]any)
		text, _ := block["text"].(string)
		if block["type"] != "text" || len(text) > 32768 {
			return nil
		}
		var object map[string]any
		if err := json.Unmarshal([]byte(text), &object); err != nil {
			return nil
		}
		return object
	}
	return result
}

func (r Runtime) probeContext(ctx context.Context, e Event, scope string) (memory.ProbeContext, error) {
	project, err := r.Store.ExperienceProjectBinding(ctx, e.CWD, scope)
	return memory.ProbeContext{Host: r.host(), Scope: scope, Session: e.Session, Project: project, Turn: e.TurnID}, err
}

func (r Runtime) startProbeFacts(ctx context.Context, e Event, scope string, producers []probeFactRule) error {
	if e.ToolUseID == "" || len(e.ToolUseID) > 512 {
		return r.Store.HookLog(ctx, e.Name, scope, e.Session, "probe_context_call_unknown", e.Tool)
	}
	context, err := r.probeContext(ctx, e, scope)
	if err != nil {
		return err
	}
	pending, err := r.Store.BeginProbeCall(ctx, context, e.Tool, e.ToolUseID, probeDefinition(producers))
	if err == nil && !pending {
		_ = r.Store.HookLog(ctx, e.Name, scope, e.Session, "probe_context_call_unknown", e.Tool)
	}
	return err
}

func (r Runtime) finishProbeFacts(ctx context.Context, e Event, scope string) error {
	config, err := loadProbeConfig(r.Store.Dir)
	if err != nil {
		_ = r.Store.HookLog(ctx, e.Name, scope, e.Session, "probe_config_invalid", probeConfigName)
		return nil
	}
	producers := config.producers(scope, e.Tool)
	if len(producers) == 0 {
		return nil
	}
	if e.ToolUseID == "" || len(e.ToolUseID) > 512 {
		return r.Store.HookLog(ctx, e.Name, scope, e.Session, "probe_context_result_unbound", e.Tool)
	}
	context, err := r.probeContext(ctx, e, scope)
	if err != nil {
		return err
	}
	values := probeValues{input: e.Input, result: probeResult(e)}
	var facts []memory.ProbeFact
	for _, rule := range producers {
		key := values.key(rule.Key)
		if key == "" {
			continue
		}
		state := probeUnknown
		if values.result != nil {
			state, err = rule.When.evaluate(values)
			if err != nil {
				return err
			}
		}
		ttl := rule.TTLSeconds
		if ttl == 0 {
			ttl = 120
		}
		facts = append(facts, memory.ProbeFact{Definition: probeDefinition(rule), Key: key, Value: int(state),
			TTL: time.Duration(ttl) * time.Second, Session: rule.Lifetime == "session"})
	}
	saved, err := r.Store.CompleteProbeCall(ctx, context, e.Tool, e.ToolUseID, probeDefinition(producers), facts)
	if err != nil {
		return err
	}
	if !saved {
		return r.Store.HookLog(ctx, e.Name, scope, e.Session, "probe_context_result_unbound", e.Tool)
	}
	if len(facts) == 0 {
		return r.Store.HookLog(ctx, e.Name, scope, e.Session, "probe_context_key_unknown", e.Tool)
	}
	return r.Store.HookLog(ctx, e.Name, scope, e.Session, "probe_context_result_observed", e.Tool)
}
