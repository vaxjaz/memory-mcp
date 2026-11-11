# 宿主 adapter：v0.2.2-adapters.1

adapter 只处理“宿主怎样交付事件、怎样安装配置、怎样阻止或续跑”。案例准入、检索、复用与确认清洗仍共用一套逻辑和原数据库，不为每个宿主复制知识库。

| `--host` | 用户配置 | 阻止方式 | 本版验证范围 |
| --- | --- | --- | --- |
| zcode | `.zcode/cli/config.json`，`mcp.servers` / `hooks.events` | JSON block | 既有 ZCode 实战基础 + 回归 |
| codex | `.codex/config.toml` + 同目录 `hooks.json` | JSON block | 官方协议、编译程序执行生成命令；原生信任/事件仍需现场验收 |
| qoder | `.qoder/settings.json`，`mcpServers` / `hooks` | exit 2 + stderr | 官方 CLI 协议、生成命令的隔离回放；未宣称原生客户端验收 |
| qoder-ide | 同上，实际版本须核对 | exit 2 + stderr | IDE/JB 官方协议、隔离回放；未宣称 IDE 已加载 |

先读 [安装与绑定验收](../AGENT_INSTALL.md)，再读 [ZCode](ZCODE.md)、[Codex](CODEX.md) 或 [Qoder](QODER.md)。Windows、WSL、远程执行器各有用户目录和路径；不从模型名称或当前目录猜宿主。默认位置只是便利入口，使用自定义目录时必须显式传 `--config`。

## 实际保护

- 同一 data-dir 可共享项目记忆，但原始记录保留真实 host；宿主、项目、会话和原生轮次隔离。旧 ZCode 事件幂等键不变。
- Codex 的实际 turn_id 用于定位原轮次；迟到事件不写到较新输入。缺少必要 ID 时报告协议不符，不猜轮次。
- Stop 最多请求一次整理。只识别待续跑的完整提示，并保存已确认的原生续跑轮次；宿主有事件/轮次 ID 时，原始事件重投递、完成后的迟到续跑都不新增用户原话。整理完成后，用户在新轮次引用相同提示文本仍作为真实输入，不按文字误删。无稳定 ID 的客户端不能承诺识别乱序和重复投递。
- 安装保留其他服务、hook、环境变量及用户设置。Codex TOML 仅编辑本服务表，保留其他部分的字节和注释；不支持的内联/点式布局拒绝写入。
- 配置与生成脚本在用户目录，写前备份并检查并发修改；后续写入失败时回退已改配置。数据库有独立备份，已完成的项目绑定不因文件失败自动删除，失败后用 doctor 核对。
- adapter 不修改原生权限、信任记录或项目规则。Windows launcher 使用系统 PowerShell，并仅为本次脚本进程设置执行策略；Codex 自身的 hook 信任仍须正常完成。
- doctor 显示配置、实际绑定、启动脚本是否完整及按 host 区分的事件回执。脚本缺失/被改动/data-dir 不符时，不把配置引用计为有效 handler。手工回放也能产生回执，不能单凭存在回执证明原生已触发，更不能证明避免了错误。

## 新增其他 Agent

扩展 `internal/hooks/adapter.go` 的协议档案、Decode/Encode 和 `adapter_config.go` 的用户配置转换；内存逻辑留在 Runtime/Store。新增档案必须明确字段、真实轮次、阻止返回、信任要求和官方依据。补上配置保留、重复投递、跨宿主隔离、普通回合不续跑、候选仅续跑一次与安装命令测试，再另做真实客户端验收。没有兼容证据不注册“通用”冒名 adapter。

`scripts/check_adapters.py` 在 Windows 和 Linux 执行生成的命令，覆盖中文、空格/引号路径、阻止/放行、续跑和诊断。它模拟宿主事件，不调用模型，也不证明真实宿主的语义效果。
