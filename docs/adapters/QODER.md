# Qoder CLI / IDE 适配器

按 [AGENT_INSTALL.md](../AGENT_INSTALL.md) 先核实、备份。CLI 用 --host qoder，IDE/JB 用 --host qoder-ide，不能靠模型名判断。配置通常共用 .qoder/settings.json，MCP 在 mcpServers，事件在 hooks，不套 ZCode 的嵌套格式。

```text
<程序> setup --host qoder --mode assist --config <实际用户settings.json> --data-dir <原库> --project <真实业务目录> --scope project:<原scope>
```

IDE 替换实际 host。服务与 managed launcher 同时更新，模式持久适用于全部绑定项目，不逐轮询问。辅助模式仅短提醒，不装 PreToolUse/Stop；显式采集时 Stop 只存答复。

CLI/IDE 共用同配置不能重复装两个档案，setup 检出另一档案会拒绝。先核对实际形态，协调旧项或使用客户端支持的独立用户配置，避免双提示/采集。多服务时明确 --server-name，不新建空库。

按实际客户端目录信任与权限机制启用并重启，本程序不替用户放开。有原生轮次 ID 时按轮次定位；无 ID 的可选采集只使用该宿主/项目/会话最新输入，不承诺识别乱序旧事件。

显式 strict 的 PreToolUse/Stop 阻止通过 exit 2 + stderr；辅助模式不使用这种阻断。生成命令已通过隔离程序回归，未宣称真实 Qoder CLI/IDE 验收。目标机需要用实际版本确认事件和业务效果，配置不同时不要强套。

既有档案参考：https://docs.qoder.com/cli/hooks 与 https://docs.qoder.com/extensions/hooks。当前协议回归不能代替客户端升级后的现场验证。
