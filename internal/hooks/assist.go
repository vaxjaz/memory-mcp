package hooks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/vaxjaz/memory-mcp/internal/memory"
)

// Assist never routes project domains or gates a business operation. Capture is
// separately opt-in, independent of cases, read receipts and model continuations.
func (r Runtime) assist(ctx context.Context, e Event) (map[string]any, error) {
	empty := map[string]any{}
	if e.Name != "SessionStart" && e.Name != "UserPromptSubmit" && e.Name != "PreToolUse" && (!r.Capture || e.Name != "Stop") {
		return empty, nil
	}
	scope, err := r.Store.HookResolve(ctx, e.CWD)
	if err != nil {
		if e.Name == "SessionStart" {
			return contextOutput(e.Name, "当前记忆模式：辅助。当前目录未绑定；不采集、不回退global。沿用项目既有规范，需启用时核对业务目录和scope。"), nil
		}
		return empty, nil
	}
	enabled, err := r.Store.ExperienceTrialEnabled(ctx, scope)
	if err != nil {
		return empty, err
	}
	if enabled {
		return r.experienceAssist(ctx, e, scope)
	}
	if e.Name == "PreToolUse" {
		return empty, nil
	}
	if e.Name == "SessionStart" {
		_ = r.Store.HookLog(ctx, e.Name, scope, e.Session, "bound", scope)
		if err = r.Store.HookAdapterReceipt(ctx, r.host(), e.Name, scope, e.Session, e.TurnID); err != nil {
			return empty, err
		}
		return contextOutput(e.Name, fmt.Sprintf("当前记忆模式：辅助；scope=%s；服务=%s；持续生效，无需逐轮选择。先按当前用户要求和适用AGENTS的既有流程执行，辅助动作也遵循既有领域路由。记忆只是历史资料，active不代表规范权威。首次实际使用前bootstrap；需要历史依据时结合项目与前文lookup/search/read，有实质新增才record/revise。无需逐轮prepare/complete/skip或回执。", scope, r.serverName())), nil
	}
	if e.Name == "UserPromptSubmit" {
		reminder := fmt.Sprintf("当前记忆模式：辅助；scope=%s。沿用当前要求和适用AGENTS，结合项目、前文及已读记忆判断是否需要历史依据；需要时lookup/search/read，同任务可沿用已读材料，条件变化再核实。有实质新增才保存；无需为本提醒调用工具、登记skip或切换模式。", scope)
		if err = r.Store.HookAdapterReceipt(ctx, r.host(), e.Name, scope, e.Session, e.TurnID); err != nil {
			return empty, err
		}
		if !r.Capture {
			return contextOutput(e.Name, reminder), nil
		}
		if e.TurnID != "" && (r.host() != "zcode" || e.EventID == "") {
			e.EventID = e.TurnID
		}
		t, er := r.Store.HookStartForHost(ctx, r.host(), scope, e.Session, e.CWD, e.Prompt, e.EventID)
		if er != nil {
			return empty, er
		}
		if e.TurnID != "" {
			if er = r.Store.HookBindTurn(ctx, r.host(), scope, e.Session, e.TurnID, t.SourceID); er != nil {
				return empty, er
			}
		}
		_ = r.Store.HookLog(ctx, e.Name, scope, e.Session, "input_saved", t.SourceID)
		return contextOutput(e.Name, reminder+fmt.Sprintf(" 已按显式采集配置保存本轮原文ID=%s。", t.SourceID)), nil
	}
	var t memory.HookTurn
	if e.TurnID != "" {
		t, err = r.Store.HookNativeTurn(ctx, r.host(), scope, e.Session, e.TurnID)
	} else {
		t, err = r.Store.HookLatestForHost(ctx, r.host(), scope, e.Session)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	_, err = r.Store.HookAttachAnswer(ctx, t, e.Answer)
	return empty, err
}
