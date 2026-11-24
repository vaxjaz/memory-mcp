# memory-mcp 接口参考

适用：0.6.4，集成协议 13。本文件与程序逐项核对，由验收脚本保证：`scripts/check_scenarios.py` 读出程序的接口定义和命令行帮助，检查这里列出的每个参数、每个必填标记、每个命令行参数都与程序一致，多一个少一个都算失败。

## 约定

- 签名里带 `*` 的是必填参数，其余可省略。嵌套在对象或数组里的必填参数，在类型栏写明「必填」。
- 嵌套参数用点连接，例如 `experience.fact`；数组元素写成 `queries[]`，元素里的字段写成 `queries[].terms`。
- 所有调用和返回都是一段 JSON 文本。
- `scope` 一律写成 `project:<标识>`。经验只属于一个项目，别的项目的规则不会唤醒它。

## 一览

| 函数 | 入口 | 作用 |
| --- | --- | --- |
| W 写入判断 | `memory_experience_record` | 写入或修订一条经验 |
| W 写入判断 | `memory_reference_record` | 写入或修订一条约定 |
| R 召回 | `memory_experience_lookup` | 带着疑问检索，按相关度排序 |
| R 召回 | `memory_search` | 字面关键词检索，只列出不排序 |
| R 召回 | `memory_read` | 按 ID 读一条记录或它的某个历史版本 |
| R 召回 | hook 事件 | 工具调用时自动触发；查询结果回执 |
| 维护 | `memory_cleanup` | 可撤销的清理与提醒周期 |
| 自描述 | `memory_bootstrap` | 返回工作流说明和内嵌指南 |

## W(x) 写入判断

### `memory_experience_record(scope*, project*, title*, experience*, admission*, request_id?, previous_id?, resolution?, cursor?)`

写入一条经验，或修订一条已有经验。

| 参数 | 类型 | 说明 |
| --- | --- | --- |
| `scope` | 字符串 | 这条经验所属的项目 |
| `project` | 字符串 | 已绑定的项目目录，原样路径。程序不从模型名或别的机器推断它 |
| `title` | 字符串 | 简短标题，显示在检索结果和候选列表里 |
| `experience` | 对象 | 正文、证据和触发规则 |
| `experience.fact` | 字符串，1–240 字 | 正文形态一：一条有来源的业务事实，把对象、环境、时间限定写进句子 |
| `experience.pitfall` | 字符串，1–240 字 | 正文形态二：当时失效的推断 |
| `experience.counterexample` | 字符串，1–240 字 | 正文形态二：推翻该推断的观察，带实际观察到的对象和环境 |
| `experience.impact`、`experience.applicability`、`experience.boundary` | 字符串 | 旧版限定词。`fact` 形态同时填它们会被拒绝；要表达限定，写进正文 |
| `experience.evidence` | 数组，必填，1–3 条 | 证据定位 |
| `experience.evidence[].kind` | 字符串，必填 | `code`、`runtime` 或 `business`，真实性由宿主核实 |
| `experience.evidence[].reference` | 字符串，必填，1–240 字 | 真实的提交号、追踪号或约定定位。`admission.evidence_ref` 和 cue 的 `evidence_refs` 必须逐字引用它 |
| `experience.evidence[].observation` | 字符串，必填，1–180 字 | 核实到的观察，不抄原文，不写推测的原因 |
| `experience.evidence[].path` | 字符串 | `kind` 为 `code` 时必填：绑定工作区里存在的文件 |
| `experience.evidence[].symbol` | 字符串 | `code` 证据的符号位置，只是定位，不证明语义 |
| `experience.evidence[].sha256` | 字符串 | 给出后期望文件内容一致，防止源码已变 |
| `experience.cues` | 数组，必填 | 触发规则，随写入一起提交。写法见内嵌指南的 cue 手册 |
| `experience.cues[].cue_id` | 字符串，必填 | 小写字母开头，最长 48 个 `[a-z0-9_]`，同一条记录内唯一 |
| `experience.cues[].label` | 字符串，必填，≤24 字 | 简短标签 |
| `experience.cues[].text` | 字符串 | 经验不填，填了会被拒绝。提醒引用的是正文 |
| `experience.cues[].evidence_refs` | 字符串数组，必填，1–3 条 | 逐字引用本次 `experience.evidence[].reference` |
| `experience.cues[].matches` | 数组，必填 | 匹配表达式，任意一条成立即触发；全部表达式合计 1–12 个入口词 |
| `experience.object_id_prefixes` | 字符串数组 | 只用于同一输入的业务结果关联，不影响直接触发和检索 |
| `admission` | 对象 | 说明这条经验会改变以后的哪个判断 |
| `admission.future_decision` | 字符串，必填，1–240 字 | 同样情况再出现时，这条经验会改变的具体判断 |
| `admission.evidence_ref` | 字符串，必填 | 逐字等于本次某一条 `experience.evidence[].reference` |
| `request_id` | 字符串 | 同一份内容重试时沿用同一个值：返回原收据，不重复写入；同值换内容会报冲突 |
| `previous_id` | 字符串 | 修订时填被修订记录的当前完整 ID，并发送完整的新正文；省略表示新建或去重 |
| `resolution` | 对象 | 第二次调用才带：对 `needs_comparison` 给出的候选作出的判断 |
| `resolution.action` | 字符串，必填 | `reuse` 同一事实；`supplement` 同一事实加一条独立来源；`revise` 新证据推翻旧记录；`create` 独立的另一条事实；`defer` 证据不足，不写入 |
| `resolution.target_id` | 字符串 | 所针对候选的当前 ID。`reuse`、`supplement`、`revise` 必填 |
| `resolution.difference` | 字符串，必填 | 本次观察与候选的最小差异，引用旧原句和本次证据 |
| `resolution.evidence_ref` | 字符串 | 本次提交里支持该判断的证据引用 |
| `resolution.comparison_token` | 字符串 | `needs_comparison` 响应里给的凭证，不能编造 |
| `cursor` | 字符串 | 候选分多页时，取上一页的 `next_cursor` 看下一页，草案保持不变 |

**返回。** 写入成功返回 `id`、`root_id`、`indexed`、`reused`（同一 `request_id` 重试得到原收据）、`body_sha256`、`trigger_preview`（这条记录靠哪些入口词触发、当前能否触发）和 `notice`。

**什么时候会要求先比对。** 草案和本项目已有记录之间，只要有下列一种关系，第一次调用就不写入，返回 `status: "needs_comparison"`、`candidates`（每条带 `id`、`root_id`、`experience` 和命中原因 `matched_on`）、`comparison_token` 和预填好的 `resolution_template`：

- 引用了同一条证据定位（`evidence_ref`）；
- 共用一个入口词（`entrance`）；
- 两段正文共有的词，占到两者全部不同词的一半以上（`body_overlap`）。

只是共用几个词不算。精确重复直接复用原记录；带 `previous_id` 的修订直接写入。

**什么时候会被拒绝。** 以下情况什么都不写入，报错指出位置：两种正文形态混用或都没给；`fact` 形态同时填了旧版限定词；cue 带了 `text`；`admission` 缺失或引用对不上；各项长度超限；项目未绑定或 `scope` 与绑定不符；cue 引用了没有任何生效规则能提供的工具、字段或结果（报错写明是哪个条件和该登记什么）；伪造或过期的比对凭证。

### `memory_reference_record(scope*, project*, title*, agreement*, request_id?, previous_id?)`

写入或修订一条已经确认的约定。返回与经验写入相同的收据。约定不做写入前比对，也不需要 `admission`。

| 参数 | 类型 | 说明 |
| --- | --- | --- |
| `scope` | 字符串 | 约定所属的项目 |
| `project` | 字符串 | 已绑定的项目目录 |
| `title` | 字符串 | 简短标题 |
| `agreement` | 对象 | 约定内容、依据、范围、证据和触发规则 |
| `agreement.requirement` | 字符串，必填 | 已经确认的约定本身，不是手册也不是新规则 |
| `agreement.applicability` | 字符串，必填 | 实际适用范围、条件和环境 |
| `agreement.boundary` | 字符串，必填 | 例外和未知。检索到不等于获得许可 |
| `agreement.authority` | 字符串，必填 | 确立这条约定的负责人或文档 |
| `agreement.confirmation_reference` | 字符串，必填 | 真实的确认记录定位。安装许可不算 |
| `agreement.evidence` | 数组，必填，1–3 条 | 来源定位，字段含义与经验的证据相同 |
| `agreement.evidence[].kind` | 字符串，必填 | `code`、`runtime` 或 `business` |
| `agreement.evidence[].reference` | 字符串，必填，1–240 字 | 真实的来源定位，例如确认记录或文档的提交号。cue 的 `evidence_refs` 逐字引用它 |
| `agreement.evidence[].observation` | 字符串，必填，1–180 字 | 核实到的观察，不抄原文 |
| `agreement.evidence[].path` | 字符串 | `kind` 为 `code` 时必填 |
| `agreement.evidence[].symbol` | 字符串 | `code` 证据的符号位置 |
| `agreement.evidence[].sha256` | 字符串 | 给出后期望文件内容一致 |
| `agreement.cues` | 数组，必填 | 触发规则 |
| `agreement.cues[].cue_id` | 字符串，必填 | 同经验 |
| `agreement.cues[].label` | 字符串，必填，≤24 字 | 同经验 |
| `agreement.cues[].text` | 字符串，必填，≤160 字 | 这条 cue 引用的已确认原句 |
| `agreement.cues[].evidence_refs` | 字符串数组，必填，1–3 条 | 逐字引用本次 `agreement.evidence[].reference` |
| `agreement.cues[].matches` | 数组，必填 | 匹配表达式，同经验 |
| `agreement.object_id_prefixes` | 字符串数组 | 同一输入结果关联用的对象标识前缀 |
| `request_id` | 字符串 | 同一份内容重试时沿用 |
| `previous_id` | 字符串 | 修订时填被修订约定的当前完整 ID，并发送完整的新内容 |

## R(x) 召回

### `memory_experience_lookup(scope*, task*, questions*, queries?, keywords?, project?, limit?, cursor?, challenge_id?)`

带着一个具体疑问检索，按相关度排序。它只提供材料，是否适用由宿主判断；检索到不等于获得许可。

| 参数 | 类型 | 说明 |
| --- | --- | --- |
| `scope` | 字符串 | 检索的项目。带着触发凭证时，凭证属于哪个项目就只能查哪个 |
| `task` | 字符串，1–4000 字节 | 根据可见对话重建的当前任务，不是最后一句话 |
| `questions` | 字符串，1–4000 字节 | 当前要核实的推断或疑问，不写内部推理过程 |
| `queries` | 数组，1–4 组 | 检索式。给了它就不看 `keywords` |
| `queries[].terms` | 字符串数组，必填，1–16 个 | 一组检索词。每个词至少含两个连续的标识符字符，或两个连续汉字，最长 256 字节 |
| `queries[].match` | 字符串 | `all`（默认）要求每个词都出现；`any` 任一出现即可 |
| `keywords` | 字符串数组 | 兼容旧写法，任一命中 |
| `project` | 字符串 | 已绑定的项目目录。不带 `challenge_id` 时必须给 |
| `limit` | 整数，1–3 | 返回条数，默认 2 |
| `cursor` | 字符串 | 查询和结果都没变时，取上一页的 `next_cursor` 看下一页 |
| `challenge_id` | 字符串 | hook 实际下发过的触发凭证。自己主动检索时不要带 |

**返回。** `items`、`has_more`、`next_cursor`、`dropped_terms`、`query_count`、`receipt_id`、`candidate_states`（候选各自的送达状态）、`challenge_outcome`（这次凭证是否被应答）、`maintenance_due`（是否到了清理提醒）和 `notice`。每一项含 `id`、`root_id`、`title`、`source_kind`（`experience`、`agreement_reference` 或 `legacy_evidence`）、正文和匹配到的查询与词。整段响应不超过 8 KiB，装不下就分页，不截断正文。

**会被丢弃的词。** 中文按相邻两字索引，所以单个汉字、或拆开后只剩单字符的词（如 `0.6.4`）检索不了。这类词被丢弃并列在 `dropped_terms` 里，所以不要把单字当成必须命中的条件；一组查询的词全被丢弃时才报错。

**会报错的情况。** 范围与项目不匹配；凭证不存在；游标与当前查询或结果不符；`limit` 超出 1–3；`match` 不是 `all` 或 `any`；候选超过 2000 条，需要收窄。

### `memory_search(scope*, query?, kind?, limit?, cursor?, include_inactive?, include_global?, needs_review?, include_events?)`

字面关键词检索，列出匹配的记录，不排序。

| 参数 | 类型 | 说明 |
| --- | --- | --- |
| `scope` | 字符串 | `project:<标识>` 或 `global` |
| `query` | 字符串 | 空格分隔的关键词，任一个作为子串出现（不分大小写）在标题、正文或结构化内容里就列出；留空列出最近的记录 |
| `kind` | 字符串 | 可选过滤：`case` 是经验和反例，`record` 是约定和早期笔记，`hypothesis` 只存在于历史里 |
| `limit` | 整数，1–30 | 默认 10 |
| `cursor` | 字符串 | 上一页的 `next_cursor`。范围、查询词和各过滤项必须保持不变，只能改每页条数 |
| `include_inactive` | 布尔 | 同时列出被清理归档的记录。被新版本替换的旧版本要用 `memory_read` 沿 `entry.previous_id` 读，不在这里 |
| `include_global` | 布尔 | 检索某个项目时同时带上全局范围的记录 |
| `needs_review` | 布尔 | 只看源码已变化待复核的记录；`false` 反选 |
| `include_events` | 布尔 | 同时列出原始 hook 事件。默认隐藏，避免把采集到的对话当成业务决定 |

**返回。** `results`（每条含 `id`、`kind`、`scope`、`title`、`excerpt`、`origin`、`needs_review`、`created_at`）、`has_more`、`next_cursor`。

### `memory_read(id*, reference_cursor?)`

按 ID 读一条记录或它的某个版本，不做任何登记。

| 参数 | 类型 | 说明 |
| --- | --- | --- |
| `id` | 字符串 | 记录的完整 ID，或它某个历史版本的 ID |
| `reference_cursor` | 字符串 | 上一页的 `references_next_cursor`，用来看下一页引用它的记录 |

**返回。** `entry`、`current_id`、`effective_current_id`、`referenced_by`、`references_has_more`、`references_next_cursor`。`entry.previous_id` 指向被它替换的上一版，沿它可以读回整条修订链；`entry.current` 说明这一版是否仍是当前版本。

### `memory_bootstrap()`

没有参数。返回 `workflow`、`version`、`integration_protocol`、`data_dir`、`storage`、`schema_version`、`features` 和完整的内嵌指南 `guide`。连接后先读它。

### hook 事件

由客户端在工具调用前后发给本程序，宿主 agent 不直接调用。

| 事件 | 带什么 | 返回什么 |
| --- | --- | --- |
| `PreToolUse(cwd*, session_id*, tool_name*, tool_input*, tool_use_id?)` | 当前目录、会话、工具名和入参 | 通常不返回。命中一条正文还没交付过的规则时返回 `decision: "block"` 和带 `challenge_id` 的说明，agent 用它调用 `memory_experience_lookup` 后重试 |
| `PostToolUse(…, tool_response)` | 同上，外加工具结果 | 不返回。查询工具的成功结果在这里被记为送达 |
| `SessionStart`、`UserPromptSubmit` | 会话开始或用户输入 | 可选的附加上下文，例如融合提示或清理提醒 |

一次调用会不会被扫描，由 `tool-probes.json` 里该项目的规则决定，详见内嵌指南的 Register 一节。到了 hook 但没被扫描的调用，在 `doctor --session` 里留一行 `probe_skipped` 及原因，不记入参内容。

## 维护

### `memory_cleanup(action*, scope*, cursor?, operations?, plan_id?, purpose?, approval_quote?, request_id?, approval_source_id?, defer_changes?, entry_id?, source_ids?, note?, policy?)`

按 `action` 返回 `inventory`、`plan`、`maintenance` 或 `approval_source_id`，以及 `done` 和 `notice`。

| 参数 | 类型 | 用于 | 说明 |
| --- | --- | --- | --- |
| `action` | 字符串 | 全部 | `inspect`、`preview`、`confirm`、`apply`、`undo`、`read_plan`、`defer`、`acknowledge`、`configure`、`recheck_sources` |
| `scope` | 字符串 | 全部 | 项目范围 |
| `cursor` | 字符串 | `inspect` | 上一页的 `inventory.next_cursor` |
| `operations` | 数组 | `preview` | 提议的操作 |
| `operations[].entry_id` | 字符串，必填 | `preview` | 操作的记录 |
| `operations[].action` | 字符串，必填 | `preview` | `archive`、`supersede`、`distill` 或 `recheck`；都不永久删除证据 |
| `operations[].reason` | 字符串，必填 | `preview` | 原因 |
| `operations[].evidence_ids` | 字符串数组，必填 | `preview` | 实际读过的证据 ID |
| `operations[].replacement_id` | 字符串 | `preview` | `supersede` 时填：保留下来的那条记录。整条替换，不做局部修正 |
| `operations[].summary` | 字符串 | `preview` | `distill` 时填：当前的结论，含适用范围、原因、例外和未知 |
| `operations[].affected_for_review` | 字符串数组 | 不用填 | `supersede` 的预览结果里由程序列出需要一并复核的记录；撤销也不会替你确认它们 |
| `entry_id` | 字符串 | `recheck_sources` | 复核了证据的那条记录 |
| `source_ids` | 字符串数组，1–32 个 | `recheck_sources` | 实际读过的证据 ID |
| `note` | 字符串 | `recheck_sources` | 具体原因 |
| `plan_id` | 字符串 | `confirm`、`apply`、`undo`、`read_plan` | `preview` 返回的 `plan.id`。`confirm` 且 `purpose` 为 `configure` 时不带 |
| `purpose` | 字符串 | `confirm` | `apply`、`undo` 或 `configure` |
| `approval_quote` | 字符串 | `confirm`、`apply`、`undo` | 用户确认这份预览时的原话，三次调用保持一致 |
| `request_id` | 字符串 | `confirm` | 这一次确认的稳定标识，重试不会重复登记 |
| `approval_source_id` | 字符串 | `apply`、`undo` | `confirm` 返回的 `approval_source_id` |
| `defer_changes` | 整数，1–1000000 | `defer` | 把下次提醒推迟多少条已存入的经验 |
| `policy` | 对象 | `configure` | 新的提醒策略 |
| `policy.scope` | 字符串，必填 | `configure` | 与 `scope` 相同 |
| `policy.interval` | 整数，必填 | `configure` | 每隔多少条已存入的经验提醒一次；复用和暂缓的不计 |
| `policy.enabled` | 布尔，必填 | `configure` | `false` 关闭提醒 |
| `policy.repeating` | 布尔，必填 | `configure` | `true` 每隔 N 条提醒；`false` 再过 N 条只提醒一次 |
| `policy.source_id` | 字符串，必填 | `configure` | `confirm` 且 `purpose` 为 `configure` 所返回的 `approval_source_id` |

**顺序。** `inspect` 查看 → `preview` 生成计划并得到 `plan.id`（此时什么都没改）→ 给用户看受影响的记录、保留和失去的内容、原因和未知 → `confirm` 登记用户对这份预览的批准，得到 `approval_source_id` → `apply` 执行。要撤销：再次 `confirm`（`purpose: "undo"`）后调用 `undo`。

**提醒周期。** 每存入一条经验计一次，默认每 100 条提醒一次，可重复。`defer` 推迟，`acknowledge` 重新计数。要改周期或关闭：先 `confirm`（`purpose: "configure"`，`approval_quote` 是用户的指令），再 `configure`（`policy.source_id` 填它返回的 ID）。

**步骤写错时会怎样。** 每种情况都被拒绝，并用一句话说明缺什么、下一步该做什么：没有 `plan_id`；`plan_id` 不存在；计划还没被批准就 `apply`；还没 `apply` 就 `undo`；`purpose` 与计划状态不符；改提醒周期时 `source_id` 不是一次确认。

## 命令行

下表的命令都写成 `memory-mcp <命令>`；`memory-mcp <命令> --help` 只列出该命令自己的参数，写了别的命令的参数会直接报错。

| 命令 | 参数 | 作用 |
| --- | --- | --- |
| `serve` | `[--data-dir DIR]` | 以 MCP 方式运行。客户端配置里启动的就是它，不要在终端里直接运行 |
| `setup` | `--host HOST --project DIR --scope project:ID [--data-dir DIR] [--config FILE] [--server-name NAME] [--enable-wakeup \| --disable-wakeup] [--dry-run]` | 绑定项目，安装 hook，生成或补全 `tool-probes.json`，刷新客户端里的匹配规则。换过程序之后要重跑一次 |
| `hook` | `--host HOST [--data-dir DIR] [--server-name NAME] [--config FILE]` | 客户端每次事件调用的处理程序，从标准输入读一个事件。不是 MCP 服务，不用手动运行 |
| `doctor` | `[--host HOST] [--config FILE] [--data-dir DIR] [--project DIR] [--scope project:ID] [--server-name NAME] [--probe EVENT.json [--expect RECORD_ID [--expect-cue CUE_ID]]] [--session ID] [--challenge ID] [--reviews] [--check-text] [--out FILE]` | 检查安装。只读，不改任何东西 |
| `adapters` | 无 | 列出支持的客户端、配置位置和验证状态 |
| `status` | `[--data-dir DIR]` | 版本、库格式版本、数据目录和记录数 |
| `backup` | `--out FILE [--data-dir DIR]` | 生成一份一致的数据库快照，不覆盖已有文件 |
| `export` | `--out FILE [--data-dir DIR]` | 导出为明文 JSON，不覆盖已有文件 |
| `forget` | `--id ID --yes [--data-dir DIR]` | 永久删除一条记录及其所有版本。清不掉已有的导出、备份和客户端里的对话 |
| `migrate` | `[--data-dir DIR]`，或 `--apply --backup FILE [--data-dir DIR]` | 不带 `--apply`：只读报告，列出库的格式版本和需要人工处理的记录。带 `--apply`：先把原库另存为 `FILE`，再升级存储格式。只改存储格式，不改写任何记录内容 |
| `diagnose` | `--data-dir DIR [--entry-id ID] [--scope project:ID] [--probe-ids ID,ID] [--out FILE]` | 只读的元数据和链接诊断，不修复。`--data-dir` 必须显式给 |
| `guide` | 无 | 打印内嵌指南，与 `memory_bootstrap` 返回的是同一份 |
| `config` | 无 | 打印一段可直接使用的 MCP 配置 |
| `version`、`build-info` | 无 | 版本；以及版本、库格式版本和确切的源码提交号 |

`--data-dir` 省略时取环境变量 `MEMORY_MCP_DATA_DIR`，再省略取平台默认目录。`--host` 取 `zcode`、`codex`、`qoder` 或 `qoder-ide`，指实际使用的客户端，不是模型名。`--server-name` 默认 `decision-memory`。

**`setup` 的三个要点。**

- `--enable-wakeup` 和 `--disable-wakeup` 不能同时给；都不给时保持这个项目原来的唤醒设置。
- 第一次改动之前，库会被复制到 `<数据目录>/backups/`。
- 装完要重启客户端。

**`doctor` 的几种用法。**

| 用法 | 作用 |
| --- | --- |
| 基本检查 | 看 `configuration_consistent`、`split_executables`、`probe_config_valid`、`probe_surface`、`probe_matcher_mismatches`、`unreachable_cues`、`wakeup_enabled`、`bindings` |
| 加 `--scope project:ID` | 附带这个项目的记录数、生效的工具规则、永远触发不了的规则和提醒状态 |
| 加 `--probe EVENT.json` | 用一段事件样本模拟一次工具调用，看会不会触发、不触发是哪个字段、什么原因。`-` 表示从标准输入读 |
| 加 `--expect RECORD_ID --expect-cue CUE_ID` | 配合 `--probe`，指定要检查的记录和规则 |
| 加 `--session SESSION_ID` | 看某个会话最近的事件，包括没被扫描的调用（`probe_skipped`）和查询回执 |
| 加 `--challenge ID` | 看一次触发的送达状态。与 `--probe` 不能同时用 |
| 加 `--reviews` | 配合 `--scope`，附带待复核的记录 |
| 加 `--check-text` | 检查存储的文本有没有编码损坏 |
| 加 `--out FILE` | 把完整报告写进新文件，屏幕上只显示路径，避免输出被截断 |

`--host` 省略时 `doctor` 按 `zcode` 检查，用别的客户端必须写明。`--probe` 的样本里没有 `cwd` 时必须给 `--project`，程序不会用当前终端的目录去猜。

## `tool-probes.json`

放在数据目录里，决定哪些工具调用会被扫描。详见内嵌指南的 Register 一节。

| 项 | 含义 |
| --- | --- |
| `tools` | 对所有项目生效的规则。`setup` 首次安装时写入默认规则，之后补上缺的，不改已有的 |
| `projects` | 按项目写的规则，覆盖 `tools` 里的同名工具 |
| `tool` 或 `mcp_tool` | 二选一：原生工具的准确名字，或不带服务器名的 MCP 工具名 |
| `enabled` | `false` 停用这条规则 |
| `fields` | 左边是给 cue 用的名字，右边是入参里的路径，或一组候选路径（取第一个存在的） |
| `scan` | 只对命令类规则：列出命令名，命令里出现其中任一个完整的词才扫描 |
| `discover` | `"query"`：按已知的参数名找出数据库类工具的查询语句 |
| `observe` | 登记上一个工具的结果，供后续调用的条件使用。只能写在 `projects` 下 |

路径里用点进入嵌套对象，用数字选数组元素，用 `*` 取每一个元素。
