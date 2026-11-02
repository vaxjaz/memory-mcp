# ZCode 适配器（0.1.3 候选版）

这是 [通用 hook 接入流程](../HOOK_SETUP.md) 下的一个宿主适配器。安装宿主 Agent 应先识别实际客户端，再选择对应适配方式，不能把本文件作为所有客户端的配置模板。

目标：用户明确表达业务决定、约束或纠偏后，保存带原话和语境的案例；之后在相关任务开始及变更范围扩大时自动检索。不是每轮生成一条偏好。

## 已实现的执行链

1. SessionStart：按实际 cwd 在用户记忆库的项目绑定表中解析 scope；不猜 global，不向项目写文件。
2. UserPromptSubmit：保存收到的用户原文，与前一轮宿主交付的答复建立 context 关联；按任务主题自动检索已有项目材料，并向本轮交付有限原文。没有业务案例时不拿近期聊天填满上下文。
3. PreToolUse：在 Write/Edit/apply_patch 提供文件路径时按主题与资源两路补查，解析文件名、符号及来源定位，交付真实命中原因。相同案例版本与命中依据不重复注入；新增资源依据仍可交付。忽略 memory 工具，避免自触发。
4. Stop：保存宿主交付的本轮业务答复，最多请求一次主模型继续运行，读取原话、查询已有决定，按实际内容保存案例或修订旧案例，然后调用 memory_review_done 留下回执。普通聊天可以跳过，不生成假偏好。

语义整理借用当前宿主模型，**会增加一次模型继续执行**，命令 hook 不会私自调用模型 API。Stop 中包含的是本轮具体处理任务与原文 ID，不依赖每次开工记住用户级规则。

原始事件默认为普通搜索隐藏，只有整理后的案例、材料及假设用于日常查找。原话可以用 memory_read 回查，include_events 可显式搜索。纠偏继续使用不可变版本及来源关联。

## 安装前

- 确认程序 `version` 为安装包声明的 0.1.3 候选版本，读 help/guide。无需 Node、Python 或模型 Key。
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

- 只有 ZCode 协议适配器。Codex 本地 hooks 尚未适配；托管云编排不会因为容器中存在配置就执行用户 hook。
- 检索是本地词面与中文双字片段匹配，扫描项目最新 1000 条有效材料；优先保留至多 2 条资源候选，再保留 1 条主题候选，重复项去重，不足时互补，总计至多 3 条。单条超过 800 字标注截断并给全文入口。命中依据是定位线索，不确认适用性；无 embedding 或独立模型，不能保证语义完备。
- 用户输入是主要读取节点；工具前补查只覆盖实际提供路径的已配置写工具，无法观察模型内部思考。
- 宿主没有事件 ID 时不按相同正文去重，避免丢掉真实重复发言；提供稳定事件 ID 时重投可复用同条材料。事件到达不由本程序保证。
- 普通 shell/MCP 命令错误、数据库不可用或 hook 超时不会阻断任务；需通过诊断观察失败，不是强制安全闸门。
- 本候选的 Go 自动化测试模拟宿主模型工具操作。用户此前的 Windows 实测证明了旧探针的 UserPromptSubmit 通路，不等同于本候选 Stop 整理流程已在真实 ZCode 验收。

## 停用与回退

从用户配置仅移除 args 中 `--managed-id` 值为 `decision-memory-hooks-v1|对应服务名` 的处理器，保留其他设置及 MCP 服务，然后新建会话。可停止自动采集并继续手动使用 MCP。数据库默认保留。

不要用旧数据库备份覆盖后来写入的业务资料。原配置旁的备份可供人工对照恢复，但恢复前核对后续其他配置变化。删除用户原文可用既有 forget（会一并删除依赖案例），不自动删除备份及宿主聊天副本。

官方配置依据：https://zcode.z.ai/en/docs/hooks 、https://zcode.z.ai/en/docs/mcp-services 。

乱码接收保护、note 回执和旧内容只读检查见 [文本完整性](../TEXT_INTEGRITY.md)。
