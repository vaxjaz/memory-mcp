package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/vaxjaz/memory-mcp/internal/matching"
	"github.com/vaxjaz/memory-mcp/internal/memory"
)

func (r Runtime) configuredInput(ctx context.Context, e Event, scope string) (*matching.Input, probeRule, error) {
	config, err := loadProbeConfig(r.Store.Dir)
	if err != nil {
		_ = r.Store.HookLog(ctx, e.Name, scope, e.Session, "probe_config_invalid", probeConfigName)
		return &matching.Input{}, probeRule{}, nil
	}
	rule, ok := config.ruleFor(scope, e.Tool)
	if !ok || !rule.active() {
		return &matching.Input{}, probeRule{}, nil
	}
	if rule.inspectionOnly() {
		err = r.logToolShape(ctx, e, scope, e.Input)
		return &matching.Input{}, rule, err
	}
	if len(rule.Observe) > 0 {
		if err = r.startProbeFacts(ctx, e, scope, rule.Observe); err != nil {
			return nil, rule, err
		}
	}
	input, report := rule.extract(e)
	if report.Outcome != "scanned" && len(rule.Fields) > 0 {
		raw, _ := json.Marshal(report)
		_ = r.Store.HookLog(ctx, e.Name, scope, e.Session, "custom_probe_input", string(raw))
	}
	r.attachFacts(ctx, e, scope, config, input)
	return input, rule, nil
}

func (r Runtime) probeMatches(ctx context.Context, e Event, scope string, input *matching.Input, rule probeRule) ([]memory.ExperienceMatch, []memory.ExperienceMatch, error) {
	direct, err := r.Store.MatchToolInput(ctx, scope, input)
	if err != nil || rule.preset != "business" || e.Session == "" {
		return direct, direct, err
	}
	matches := direct
	for _, field := range input.Texts {
		observed, err := r.Store.ObservedBusinessMatches(ctx, r.host(), scope, e.Session, e.TurnID, field.Value)
		if err != nil {
			return nil, nil, err
		}
		matches = mergeExperienceMatches(matches, observed)
	}
	return direct, matches, nil
}

func (r Runtime) effectiveResultRule(ctx context.Context, e Event, scope string) (probeRule, bool) {
	config, err := loadProbeConfig(r.Store.Dir)
	if err != nil {
		_ = r.Store.HookLog(ctx, e.Name, scope, e.Session, "probe_config_invalid", probeConfigName)
		return probeRule{}, false
	}
	rule, ok := config.ruleFor(scope, e.Tool)
	return rule, ok && rule.active()
}

func (r Runtime) experience(ctx context.Context, e Event, scope string) (map[string]any, error) {
	empty := map[string]any{}
	switch e.Name {
	case "SessionStart":
		if err := r.Store.ExperienceEnsureContext(ctx, r.host(), scope, e.Session); err != nil {
			return empty, err
		}
		if e.Source == "compact" || e.Source == "clear" {
			if err := r.Store.ExperienceContextReset(ctx, r.host(), scope, e.Session, e.EventID); err != nil {
				return empty, err
			}
		}
		if err := r.Store.HookAdapterReceipt(ctx, r.host(), e.Name, scope, e.Session, e.TurnID); err != nil {
			return empty, err
		}
		return contextOutput(e.Name, fmt.Sprintf("记忆经验；scope=%s；服务=%s。真实纠偏或自主发现经证据核实后，用 memory_experience_record 同步保存紧凑风险特征；可自主 memory_experience_lookup。当前规范先行，历史经验只提出待验证疑点。工具明确命中索引且正文未交付时主动查询；正文交付后仅新关联给短反例，同关联静默。无原文采集、每轮提醒、异步写入或 Stop 整理。", scope, r.serverName())), nil
	case "UserPromptSubmit":
		if err := r.Store.ExperienceStartTurn(ctx, r.host(), scope, e.Session, e.TurnID, e.EventID); err != nil {
			return empty, err
		}
		if err := r.Store.HookAdapterReceipt(ctx, r.host(), e.Name, scope, e.Session, e.TurnID); err != nil {
			return empty, err
		}
		if m, err := r.Store.Maintenance(ctx, scope); err == nil && m.Due {
			if err = r.Store.MarkMaintenanceNotified(ctx, scope); err != nil {
				return empty, err
			}
			return contextOutput(e.Name, "本项目已到记忆清洗检查点。完成当前任务后可 inspect/preview，展示具体变化，经用户确认再 apply；本提醒不启动额外整理，不要求当前任务等待。"), nil
		}
		return empty, nil
	case "PreToolUse":
		enabled, err := r.Store.ExperienceTrialEnabled(ctx, scope)
		if err != nil {
			return empty, err
		}
		if !enabled {
			return empty, nil
		}
		if err := r.Store.ExperienceEnsureContext(ctx, r.host(), scope, e.Session); err != nil {
			return empty, err
		}
		if r.isLookup(e.Tool) {
			challenge, _ := e.Input["challenge_id"].(string)
			if provided, _ := e.Input["scope"].(string); provided != scope {
				return empty, nil
			}
			err := r.Store.BeginExperienceLookup(ctx, r.host(), scope, e.Session, e.CWD, e.ToolUseID, e.Tool, challenge)
			if err == nil {
				_ = r.Store.HookLog(ctx, e.Name, scope, e.Session, "lookup_call_bound", e.Tool)
			}
			return empty, err // fail-open; no context and never an experience gate
		}
		input, rule, err := r.configuredInput(ctx, e, scope)
		business := rule.preset == "business"
		configured := rule.preset == "" && len(rule.Fields) > 0
		if err != nil {
			return empty, err
		}
		if len(input.Texts) == 0 {
			if configured {
				_ = r.Store.HookLog(ctx, e.Name, scope, e.Session, "custom_probe_no_clues", e.Tool)
			} else if business {
				_ = r.Store.HookLog(ctx, e.Name, scope, e.Session, "business_probe_no_clues", e.Tool)
			}
			return empty, nil
		}
		start := time.Now()
		probe, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		_, matches, err := r.probeMatches(probe, e, scope, input, rule)
		if err != nil {
			return empty, err
		} // Run() fails open; never records completion.
		if len(matches) == 0 {
			if configured {
				_ = r.Store.HookLog(ctx, e.Name, scope, e.Session, "custom_probe_no_match", e.Tool)
			} else if business {
				_ = r.Store.HookLog(ctx, e.Name, scope, e.Session, "business_probe_no_match", e.Tool)
			}
			return empty, nil
		}
		challenge, err := r.Store.ChallengeExperience(probe, r.host(), scope, e.Session, e.TurnID, e.CWD, matches)
		if err != nil {
			return empty, err
		}
		info, _ := json.Marshal(map[string]any{"tool": e.Tool, "candidate_ids": matches, "outcome": challenge.Outcome, "probe_ms": time.Since(start).Milliseconds()})
		_ = r.Store.HookLog(ctx, e.Name, scope, e.Session, "experience_probe", string(info))
		if challenge.Context != "" {
			out := contextOutput(e.Name, challenge.Context)
			out["_cue_emission"] = challenge.EmissionID
			return out, nil
		}
		if challenge.ID == "" {
			return empty, nil
		}
		reason := fmt.Sprintf("当前操作命中经验入口。请结合完整任务和当前疑问调用 %s.memory_experience_lookup(scope=%q, task=当前任务, questions=当前疑问, queries=自主检索式, challenge_id=%q)，再重试。", r.serverName(), scope, challenge.ID)
		return map[string]any{"decision": "block", "reason": reason}, nil

	case "PostToolUseFailure":
		if !r.isLookup(e.Tool) {
			enabled, err := r.Store.ExperienceTrialEnabled(ctx, scope)
			if err != nil || !enabled {
				return empty, err
			}
			rule, ok := r.effectiveResultRule(ctx, e, scope)
			if !ok {
				return empty, nil
			}
			return empty, r.finishProbeFacts(ctx, e, scope, rule.Observe)
		}
		_ = r.Store.HookLog(ctx, e.Name, scope, e.Session, "lookup_failed_native", e.Tool)
		return empty, r.Store.FailExperienceLookup(ctx, r.host(), scope, e.Session, e.ToolUseID)
	case "PostToolUse":
		if r.isLookup(e.Tool) {
			if e.Response == nil && !e.IsError && e.Error == nil {
				_ = r.Store.HookLog(ctx, e.Name, scope, e.Session, "lookup_result_missing", e.Tool)
				return empty, r.Store.FailExperienceLookup(ctx, r.host(), scope, e.Session, e.ToolUseID)
			}
			result, err := lookupResult(e)
			if err != nil {
				_ = r.Store.FailExperienceLookup(ctx, r.host(), scope, e.Session, e.ToolUseID)
				return empty, err
			}
			err = r.Store.ObserveExperienceLookup(ctx, r.host(), scope, e.Session, e.ToolUseID, e.Tool, result)
			if err != nil {
				_ = r.Store.FailExperienceLookup(ctx, r.host(), scope, e.Session, e.ToolUseID)
			} else {
				_ = r.Store.HookLog(ctx, e.Name, scope, e.Session, "lookup_result_observed", e.Tool)
			}
			return empty, err
		}
		enabled, err := r.Store.ExperienceTrialEnabled(ctx, scope)
		if err != nil || !enabled {
			return empty, err
		}
		rule, ok := r.effectiveResultRule(ctx, e, scope)
		if !ok {
			return empty, nil
		}
		if rule.inspectionOnly() {
			if result := probeResult(e); result != nil {
				return empty, r.logToolShape(ctx, e, scope, result)
			}
			return empty, nil
		}
		if err = r.finishProbeFacts(ctx, e, scope, rule.Observe); err != nil || rule.preset != "business" {
			return empty, err
		}
		records := businessObservationRecords(e)
		if len(records) == 0 {
			return empty, nil
		}
		probe, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		// The source read has already occurred. Observe only; never fabricate a
		// denial, credit delivery, or inject an unsolicited case body.
		return empty, r.Store.ObserveBusinessRecords(probe, r.host(), scope, e.Session, e.TurnID, records)
	}
	return empty, nil
}

func (r Runtime) logToolShape(ctx context.Context, e Event, scope string, fields map[string]any) error {
	detail, _ := json.Marshal(map[string]any{"tool": e.Tool, "observed_fields": describeToolFields(fields)})
	return r.Store.HookLog(ctx, e.Name, scope, e.Session, "tool_field_shape", string(detail))
}

func mergeExperienceMatches(direct, observed []memory.ExperienceMatch) []memory.ExperienceMatch {
	out := make([]memory.ExperienceMatch, 0, len(direct)+len(observed))
	seen := map[[2]string]bool{}
	for _, group := range [][]memory.ExperienceMatch{direct, observed} {
		for _, match := range group {
			key := [2]string{match.ID, match.CueID}
			if !seen[key] {
				seen[key] = true
				out = append(out, match)
			}
		}
	}
	return out
}

// Only this configured service's lookup gets receipt hooks. Host canonical
// names may normalize '-' to '_'; neither pattern matches arbitrary MCP calls.
func lookupToolMatcher(name string) string {
	escaped := regexp.QuoteMeta(name)
	normalized := regexp.QuoteMeta(strings.ReplaceAll(name, "-", "_"))
	return "^mcp__(" + escaped + "|" + normalized + ")__memory_experience_lookup$"
}
func (r Runtime) isLookup(tool string) bool {
	return regexp.MustCompile(lookupToolMatcher(r.serverName())).MatchString(tool)
}
