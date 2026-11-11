# Qoder CLI / IDE adapter

Qoder CLI 选 `--host qoder`；IDE/JB 选 `--host qoder-ide`。两者协议档案分开报告，用户配置通常共用 `.qoder/settings.json`，MCP 在 `mcpServers`，事件在 `hooks`，不能套 ZCode 的 `mcp.servers/hooks.events`。

```text
<程序> setup --host qoder --config <实际用户settings.json> --data-dir <原库> --project <真实项目目录> --scope project:<原scope> --capture-user-text --review-on-stop
<程序> doctor --host qoder --config <同上> --data-dir <同上> --project <同上> --scope project:<同上>
```

IDE 替换为 qoder-ide。复用原服务名；有多个实例时指定 `--server-name`。CLI/IDE 共用同一配置时不能重复安装两个采集档案；setup 检出另一档案会报错。先核对实际使用形态，协调旧项或使用客户端支持的独立用户配置，不默默重复采集。

PreToolUse 阻止与 Stop 续跑使用 exit 2，具体原因在 stderr，避免客户端把退出零的 JSON 当成放行。普通回合正常退出；只有声明有价值候选才一次整理。若客户端提供原生轮次 ID，按轮次定位；没有 ID 时只使用该宿主/项目/会话最新轮次，因此不能承诺识别这类客户端乱序到达的旧事件。

安装后重启并遵守实际客户端的目录信任和权限机制；程序不替用户放开这些配置。本机未安装 Qoder，发布验收只覆盖官方协议、配置保留及编译程序执行生成命令。必须在目标机以实际 CLI/IDE 版本验证事件、工具与业务效果；旧版配置不同则停止套用本档案。

依据：[CLI Hooks](https://docs.qoder.com/cli/hooks)、[CLI MCP](https://docs.qoder.com/cli/mcp-reference)、[IDE/JB Hooks](https://docs.qoder.com/extensions/hooks)、[Qoder Connectors](https://docs.qoder.com/qoder/connectors)。
