# Codex adapter

先按 [安装说明](../AGENT_INSTALL.md) 备份原库与配置，确认实际 Codex 用户目录和业务项目。`--host codex` 使用 `[mcp_servers.<服务名>]` 注册 stdio，在 config.toml 同目录 hooks.json 安装 SessionStart、UserPromptSubmit、PreToolUse、Stop。

```text
<程序> setup --host codex --config <实际CODEX_HOME/config.toml> --data-dir <原库> --project <实际工作目录> --scope project:<原scope> --capture-user-text --review-on-stop
<程序> doctor --host codex --config <同上> --data-dir <同上> --project <同上> --scope project:<同上>
```

程序不打开或修改原生 hook 信任状态。通过 Codex 的 Hooks 管理入口信任这次具体变更，再按客户端要求重启/新建会话。旧 SessionStart 软引导不等于这套完整适配；若此前手工安装过软引导，核对其来源后移除对应旧项，保留其他用户 hook，避免双重提示。

原生轮次事件必须包含实际 `turn_id`。UserPromptSubmit 用它做幂等键；PreToolUse/Stop 用它定位输入，不回退到另一个轮次。Stop block 的 reason 可能成为新一轮提示；adapter 只匹配当前已请求的完整整理提示，并把续跑轮次映射回原输入，避免生成虚假用户证据。

未知/旧版协议会在 stderr 报错，hook 仍遵循 fail-open 原则，不把记忆故障变成所有操作无法进行。原生工具前检查仅覆盖能识别的工具名和参数，不是完整 shell 解析器，也不证明语义理解正确。

依据：[Codex Hooks](https://learn.chatgpt.com/docs/hooks)。版本升级后须重新核对事件及信任机制。发布时验证了协议与实际生成命令；本机已有 MCP 注册可验证，完整原生事件须用户完成信任并在真实会话验收。
