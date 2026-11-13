# 宿主适配器

先读 [安装步骤](../AGENT_INSTALL.md)，再识别真实宿主和执行系统。模型名称不是宿主，匹配事件名也不等于协议兼容。默认辅助模式只安装 SessionStart + UserPromptSubmit；模式持久作用于该服务全部绑定项目，scope 仍隔离，不逐轮询问切换。

| --host | 配置形态 | 验证边界 |
|---|---|---|
| zcode | .zcode/cli/config.json，mcp.servers / hooks.events | 既有实战基础与隔离协议回归；本版原生效果仍需实测 |
| codex | 实际 CODEX_HOME/config.toml + hooks.json | 配置保留和生成命令回归；实际 hook 信任/事件现场验证 |
| qoder | .qoder/settings.json，mcpServers / hooks | CLI 生成命令回归；真实版本/事件现场验证 |
| qoder-ide | 同上 | 独立 IDE/JB 档案，不能用 CLI 测试代替原生 IDE 验收 |

详细限制：[ZCode](ZCODE.md)、[Codex](CODEX.md)、[Qoder](QODER.md)。仅 MCP stdio 不自动捕获消息或提供 hook。默认提醒不保存正文、不检索案例、不写检查状态、不启动模型整理。

适配器仅管理协议/配置，不定义业务领域路由、不修改 AGENTS、原生信任或权限。保存其他服务/用户 hooks；写前备份并检查并发修改，失败回退已写配置。数据有独立备份，绑定不因文件失败自动删除，失败用 doctor 核对。

doctor 区分配置、服务/launcher 模式与实际事件元数据，默认 native_event_verified=false。手工事件回放也会产生回执，不证明原生执行、语义理解或错误预防。Windows launcher 由系统 PowerShell 执行，仅为本次脚本进程设置执行策略，不改客户端信任记录。

显式 [strict](../STRICT_MODE.md) 保留旧门禁/续轮协议：ZCode/Codex 使用 JSON block，Qoder 使用 exit 2 + stderr。辅助模式不返回 block。原始数据与原生轮次隔离机制在可选采集时继续适用。

新增适配器应扩展协议档案/Decode/Encode/配置转换，补配置保留、轮次隔离、提醒不写入、strict 兼容及生成命令测试；独立做真实客户端验收。不得注册无证据的通用冒名档案。
