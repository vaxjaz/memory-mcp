# ZCode 适配器

这是 [通用 hook 接入流程](../HOOK_SETUP.md) 下的一个宿主适配器。安装宿主 Agent 应先识别实际客户端，再选择对应适配方式，不能把本文件作为所有客户端的配置模板。

**v0.2.0 安装/升级先读 [安装和绑定验收](../AGENT_INSTALL.md)。注册服务不等于绑定项目；每个历史项目分别核对并 setup。**

目标：用户明确表达业务决定、约束或纠偏后，保存带原话和语境的案例；之后在相关任务开始及变更范围扩大时自动检索。不是每轮生成一条偏好。

## 已实现的执行链

1. SessionStart：按实际 cwd 在用户记忆库的项目绑定表中解析 scope；不猜 global，不向项目写文件。
2. UserPromptSubmit：保存原文和上下文关联，交付本轮 source_id 与轻量判断提示。宿主结合项目、可见前文及当前输入调用 memory_context，语义选择、读全文、补查并完成判断。普通无关回合可 skip；不再仅按最新一句泛词自动注入三个案例。
3. PreToolUse：忽略 memory 工具，普通操作快速放行。对能识别的写入、登录、SQL 写入和部分启动/部署动作，检查本轮上下文查阅是否完成及所选版本是否有效，缺少时返回 block。复杂脚本和未知工具不保证覆盖。
4. Stop：保存业务答复。只有宿主声明 save_candidate=true 才最多请求一次额外整理；无候选不继续模型、不生成案例。未交筛选结果记为 context_not_completed，不能宣称完全覆盖。重复决定用 reused_case_ids，不产生同文修订。

语义整理借用当前宿主模型，**有候选时才增加一次模型继续执行**，命令 hook 不会私自调用模型 API。完整准入、检索与清洗规则见 [质量和清洗](../QUALITY_AND_CLEANUP.md)。旧 hooks 未重新 setup 时仍可走旧触发流程，升级后必须更新配置并真实验收。

在启用 `--review-on-stop` 的接入中，整理回执完成后，宿主会收到提示，将已保存的业务答复全文作为续跑的最终输出。仍只请求一次续跑。ZCode 的原消息可能仍会折叠，最终位置显示的是宿主重新输出的正文。

`doctor` 最近事件中，`answer_replay_verified` 表示宿主在续跑 Stop 事件交付的答复与原文逐字相同，`answer_replay_mismatch` 表示有差异，`answer_replay_unverified` 表示没有足够数据比对。此检查不读取界面，也不会因比对失败再请求一次续跑。真实验收须同时核对正常界面中可见的业务答复和案例回执。

原始事件默认为普通搜索隐藏，只有整理后的案例、材料及假设用于日常查找。原话可以用 memory_read 回查，include_events 可显式搜索。纠偏继续使用不可变版本及来源关联。

## 安装前

- 确认程序 `version` 与安装包版本一致，读 help/guide。无需 Node、Python 或模型 Key。
- 升级前停止所有使用该库的 MCP 进程，备份整个原数据目录及用户配置；尤其从 v0.1.1 升级时必须在新程序第一次打开前完成备份。
- 程序放用户目录固定位置，使用绝对路径。不要覆盖正在运行的 Windows exe。
- 明确采集范围：只在绑定项目内保存 hook 实际提供的用户原文和最终业务答复。正文未加密；返回的材料可能进入宿主模型请求。不会复制附件、全量工具输出或历史 transcript。
- 旧客户端与新客户端共享同一数据库时一起更新，避免旧搜索把新采集的原始事件当作业务案例。核心数据库仍为 v2，hook 状态使用附加表；现有一致性备份包含附加状态。

## 接入：由安装宿主 Agent 执行

先在当前业务项目确定 scope；已有 project:vax-plan 就复用。安装目录不等于业务项目。

```text
/absolute/memory-mcp setup --host zcode --project /absolute/business-project --scope project:vax-plan --capture-user-text --review-on-stop --dry-run
/absolute/memory-mcp setup --host zcode --project /absolute/business-project --scope project:vax-plan --capture-user-text --review-on-stop
```

Windows 使用 memory-mcp.exe 和其真实绝对路径，按 PowerShell/客户端实际格式调用。setup 原生合并 `~/.zcode/cli/config.json` 的 `mcp.servers` 与 `hooks.events`，不依赖 Node 和固定盘符。保留其他 MCP、hook、env 和用户设置，重复安装不重复注册自己的事件。会替换先前提供的 zcode-hook-probe.mjs 条目。

setup 复用已有 memory-mcp 服务名和显式数据路径；--data-dir 与已有路径不一致时拒绝修改。如果原来通过 ~/.agents/mcp.json 加载 MCP 而没有原生配置，会保留这些有效服务器到原生配置，避免添加本服务导致原 fallback 被屏蔽。

配置备份在原配置旁；数据库一致性备份在数据目录 backups/。setup 输出实际位置和 restart_required，但不声称事件已经收到。钩子全局禁用且存在其他配置时拒绝偷偷启用，用户按 ZCode 原有设置处理后再安装。

打开 ZCode 设置核对 MCP 与 Hooks，保留客户端信任/启用流程，然后新建会话。安装器不会修改项目 AGENTS.md、CLAUDE.md 或任何项目配置。

多项目：在另一个业务项目重复上述 setup，使用该项目的 scope。项目绑定保存在用户库，hook 仍只注册一份。未绑定的 cwd 自动跳过并记录 unbound，不落入 global。路径匹配按目录边界和最长绑定处理；worktree/fork 不自动合并，须显式绑定。

## 首轮真实业务验收（不要只测固定答案）

在一个独立临时测试项目绑定独立测试 scope 和数据目录，避免测试材料进入正式库。可使用独立服务名，不覆盖已有正式 MCP：

```text
/absolute/memory-mcp setup --host zcode --server-name memory-business-acceptance --data-dir /absolute/NEW-test-data --project /absolute/NEW-test-project --scope project:memory-business-acceptance --capture-user-text --review-on-stop
```

测试项目必须与正式绑定项目分开。安装器仅调整同名服务拥有的 hook，保留正式连接、原数据及正式 hook；事件在未绑定的数据库中会自动跳过。hook 交付的材料和整理请求会标明应使用哪个 MCP 服务。验收后仅移除 memory-business-acceptance 服务及其 managed-id 对应的 hook，不动正式服务。

新会话自然提出业务约束，例如：“日期安排详情和编辑归在安排模块，不增加一级菜单。按这个原则给我一版方案。” 不输入 MCP 工具名。

检查 Stop 后是否出现业务案例，包含真实原话、背景、适用范围、来源链接，并有 reviewed 回执。模型若认为无须形成案例，应给出 skipped 及理由，不通过空回执伪造成功。

另一个会话提出相关任务：“给日期安排新增编辑页，你怎么安排入口？” 检查 UserPromptSubmit 自动检索是否命中刚才案例、模型是否正确应用。

再纠偏：“独立业务域可以新增一级入口，详情和编辑仍不单独增加。” 检查 memory_revise 是否保留旧版本，并在后续任务自动召回当前版本。不可用新增矛盾规则掩盖旧版本。

再测不相关任务、另一个项目、普通结束语，分别不误用材料、不串项目、不强行生成偏好。进入 migrations/*.sql 时检查工具前能补查已入库的数据库迁移约束。

## 诊断

```text
/absolute/memory-mcp doctor --host zcode --data-dir /actual/data
```

显示最近事件、项目、原始材料 ID、召回案例 ID、扫描上限和未处理 review 数，不输出聊天正文。read/record/revise 的 ID、原文长度及 SHA-256 用于检查内容往返，不证明语义正确。

状态区分：配置写入 → 收到实际事件 → 召回候选 → 请求宿主整理 → 案例/修订及回执 → 后续业务正确使用。

review_requested 不是已保存业务案例。模型不调用工具、忽略请求或本轮被中断时，原文保留，pending_reviews 暴露缺口；不会用“成功”掩盖。可用 memory_search(include_events=true) 及 memory_read 补查。仅一次继续执行，避免 Stop 套娃或阻断业务。

## 当前局限

- v0.2.2-adapters.1 同时提供其他宿主的独立 adapter，见 [清单](README.md)。本文件仅适用 ZCode；托管云编排不会因为容器中存在配置就执行用户 hook。
- v0.2.0 新上下文模式提供分页目录与关键词候选，由宿主结合会话进行语义选择，不按泛词补足三个注入位置。未重新 setup 的旧触发模式仍使用词面匹配（扫描上限10000），会解析明确替代项并排除归档/到期材料；命中不证明适用。无 embedding 或独立模型，不能保证语义完备。
- 用户输入是主要读取节点；工具前补查只覆盖实际提供路径的已配置写工具，无法观察模型内部思考。
- 宿主没有事件 ID 时不按相同正文去重，避免丢掉真实重复发言；提供稳定事件 ID 时重投可复用同条材料。事件到达不由本程序保证。
- 普通 shell/MCP 命令错误、数据库不可用或 hook 超时不会阻断任务；需通过诊断观察失败，不是强制安全闸门。
- 本版的 Go 自动化测试模拟宿主模型工具操作。用户此前的 Windows 实测证明了旧探针的 UserPromptSubmit 通路，不等同于本版 Stop 整理流程已在真实 ZCode 验收。

## 停用与回退

从用户配置仅移除 args 中 `--managed-id` 值为 `decision-memory-hooks-v1|对应服务名` 的处理器，保留其他设置及 MCP 服务，然后新建会话。可停止自动采集并继续手动使用 MCP。数据库默认保留。

不要用旧数据库备份覆盖后来写入的业务资料。原配置旁的备份可供人工对照恢复，但恢复前核对后续其他配置变化。删除用户原文可用既有 forget（会一并删除依赖案例），不自动删除备份及宿主聊天副本。

官方配置依据：https://zcode.z.ai/en/docs/hooks 、https://zcode.z.ai/en/docs/mcp-services 。

乱码接收保护、note 回执和旧内容只读检查见 [文本完整性](../TEXT_INTEGRITY.md)。
