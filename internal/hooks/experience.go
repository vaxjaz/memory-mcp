package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Filter before spawning a hook process. Native aliases are explicit; broad
// MCP matching would charge every memory/browser/edit call for a code probe.
// Regex name filters are documented by all four supported adapters.
const experienceToolMatcher = "^([Gg]rep|[Gg]lob|[Rr]ead|[Bb]ash|exec_command|run_command|run_in_terminal|terminal|read_file|grep_code|search_file|search_files|code_search|mcp__.+__(read_file|search_files|grep|glob|code_search|get_ticket|read_ticket|search_tickets|get_order|query_order|query_orders|search_orders|query|read_query|execute_query|execute_sql|run_sql|query_database))$"

// Only observable literal arguments are used. This is not a shell parser or a
// semantic task detector. Unknown/compound wrappers may have no usable clues.
func experienceClues(e Event) string {
	if isBusinessTool(e.Tool) {
		return businessClues(e, false)
	}
	name := strings.ToLower(e.Tool)
	if strings.Contains(name, "memory_") || strings.Contains(name, "skill") ||
		strings.Contains(name, "web") || strings.Contains(name, "browser") {
		return ""
	}
	known := false
	for _, kind := range []string{"grep", "glob", "read", "search_file", "code_search", "bash", "exec_command", "run_command", "terminal"} {
		if strings.Contains(name, kind) {
			known = true
			break
		}
	}
	if !known {
		return ""
	}
	var clues []string
	for _, key := range []string{"pattern", "query", "regex", "search_pattern", "path", "file_path", "filePath", "command", "cmd", "script"} {
		value, ok := e.Input[key].(string)
		if !ok {
			continue
		}
		if len(value) > 16384 {
			return ""
		}
		normal := strings.ToLower(strings.ReplaceAll(value, "\\", "/"))
		for _, rule := range []string{"agents.md", "skill.md", "/.agents/", "/.codex/"} {
			if strings.Contains(normal, rule) {
				return ""
			}
		}
		if key == "path" || key == "file_path" || key == "filePath" {
			if filepath.IsAbs(value) {
				rel, err := filepath.Rel(e.CWD, value)
				if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					return ""
				}
			}
		}
		clues = append(clues, value)
	}
	result := strings.Join(clues, " ")
	if strings.Contains(name, "bash") || strings.Contains(name, "exec_command") || strings.Contains(name, "run_command") || strings.Contains(name, "terminal") {
		lower := " " + strings.ToLower(result)
		search := false
		for _, marker := range []string{" rg ", " rg\n", " grep ", "select-string", "get-content", " cat ", "readfile", "read_file", " sed "} {
			if strings.Contains(lower, marker) {
				search = true
				break
			}
		}
		if !search {
			return ""
		}
	}
	return result
}

func (r Runtime) experience(ctx context.Context, e Event, scope string) (map[string]any, error) {
	empty := map[string]any{}
	switch e.Name {
	case "SessionStart":
		if e.Source == "compact" {
			if err := r.Store.ExperienceAfterCompact(ctx, r.host(), scope, e.Session); err != nil {
				return empty, err
			}
		}
		if err := r.Store.HookAdapterReceipt(ctx, r.host(), e.Name, scope, e.Session, e.TurnID); err != nil {
			return empty, err
		}
		return contextOutput(e.Name, fmt.Sprintf("记忆经验；scope=%s；服务=%s。真实纠偏或自主发现经证据核实后，用 memory_experience_record 同步保存紧凑风险特征；可自主 memory_experience_lookup。当前规范先行，历史经验只提出待验证疑点。代码工具明确命中索引时才提示一次主动查询；无匹配静默。无原文采集、每轮提醒、异步写入或 Stop 整理。", scope, r.serverName())), nil
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
		clues := experienceClues(e)
		if clues == "" {
			return empty, nil
		}
		start := time.Now()
		probe, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		matches, err := r.Store.MatchExperiences(probe, scope, clues)
		if err != nil {
			return empty, err
		} // Run() fails open; never records completion.
		if len(matches) == 0 && isBusinessTool(e.Tool) {
			matches, err = r.Store.ObservedBusinessMatches(probe, r.host(), scope, e.Session, e.TurnID, clues)
			if err != nil {
				return empty, err
			}
		}
		if len(matches) == 0 {
			return empty, nil
		}
		challenge, err := r.Store.ChallengeExperience(probe, r.host(), scope, e.Session, e.TurnID, e.CWD, matches)
		if err != nil {
			return empty, err
		}
		info, _ := json.Marshal(map[string]any{"candidate_ids": matches, "outcome": challenge.Outcome, "probe_ms": time.Since(start).Milliseconds()})
		_ = r.Store.HookLog(ctx, e.Name, scope, e.Session, "experience_probe", string(info))
		if challenge.ID == "" {
			return empty, nil
		}
		// No case body, counterexample, inferred remedy or route is injected.
		// A real lookup completes this exact challenge; denial alone never does.
		reason := fmt.Sprintf("当前工具操作命中记忆索引的具体锚点 %q 与条件 %q。先结合完整任务和实际疑问调用 %s.memory_experience_lookup(scope=%q, task=当前任务, questions=当前疑问, queries=[宿主构造的业务与机制检索式], challenge_id=%q)，再重试。检索式由你理解当前上下文后生成；命中不是结论、授权或已核实，不要求写回或改路由。", matches[0].Anchor, matches[0].Signal, r.serverName(), scope, challenge.ID)
		return map[string]any{"decision": "block", "reason": reason}, nil
	case "PostToolUse":
		if !isBusinessTool(e.Tool) {
			return empty, nil
		}
		enabled, err := r.Store.ExperienceTrialEnabled(ctx, scope)
		if err != nil || !enabled {
			return empty, err
		}
		clues := businessClues(e, true)
		if clues == "" {
			return empty, nil
		}
		probe, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		// The source read has already occurred. Observe only; never fabricate a
		// denial, consume a pause budget, or inject an unsolicited case body.
		return empty, r.Store.ObserveBusiness(probe, r.host(), scope, e.Session, e.TurnID, clues)
	}
	return empty, nil
}
