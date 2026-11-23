# Verified experience and incremental delivery

Follow current user instructions and project rules. The host reconstructs the full visible task, builds grounded queries and judges applicability. MCP performs lexical retrieval, not semantic interpretation, translation or another model call.

## Function model

Four functions name the runtime. W and R execute inside this service, deterministically and without a model call. F and P execute in the host; this service guarantees only their materials, boundaries and receipts.

- **W(x), write judgment.** A write or revision is compared against same-project history first. A hit returns `needs_comparison` with zero writes; the same draft and request_id resubmitted with `resolution.action` commits the judged relation. See "Judge a new observation against similar history before it is stored".
- **R(x, M), recall.** Lexical retrieval over the library snapshot M: host-generated queries, identifier and Han-bigram tokens, BM25 ranking fused across queries, cursors bound to the result snapshot. Identical normalized input, snapshot and delivery state replay identically.
- **F(j, E, m), hot fusion.** The host weighs the evoked experience m against the judgment j being made and the current evidence E: keep, supplement, narrow or correct the judgment, or obtain one missing decisive fact. The standing prompt under "Fuse a recalled experience with the current step" carries the rules; applicability stays the host's judgment and continuing without refusal registration is allowed.
- **P, verification budget.** Produced inside F, never a stored score. When swapping two confusable cases would not change this step, continue; near delivery with unresolved evidence, verify the specific gap or narrow the claim.

## Installation and upgrade for agents

Follow the packaged installation guide when the user requests an install or upgrade. Identify the active client, verified package, actual library and scope. First installation defaults to finding verified pitfalls in project source, tests, fix commits and business history. Replay the relevant code path or test, identify the future decision it changes, and record only evidenced cases.

For an upgrade, stop writers and back up the full library and configuration. Preview the schema and automatically apply a supported migration with a new backup when required. Reuse the existing library, scope and wakeup policy. Enumerate existing records through all pages and have the host trace every existing record to the real project code, including records with existing cues. Verify the behavior with tests, isolated replay or the relevant historical code revision. Use business evidence to verify scope. Semantic distillation of stored text is prohibited. Treat old records as locators and rebuild cues from verified project facts. Preserve unverifiable material and report what blocks its activation. For duplicate consolidation or archival, show the exact cleanup preview and obtain fresh user approval before confirm/apply.


After a correction, fix the current task first. Skip experience writing when rereading the original wording resolves the error, such as a missed qualifier, mistaken referent or misunderstood scope. To record an evidenced counterexample or a newly verified boundary, identify which specific future judgment it changes when a similar situation recurs and cite the evidence for that change.

For every memory_experience_record call, put `admission` beside `experience`. Send `{"admission":{"future_decision":"Check each independent entry point before treating the local validation as complete.","evidence_ref":"trace:second-entry-point"}}` as part of the submission. Copy the exact `evidence_ref` from the actual `experience.evidence[].reference` in that same submission. New records and previous_id revisions use the same check. Missing or invalid admission rejects the write and leaves the library unchanged. Admission text is used for validation and stays outside recall and the stored experience body. A submitted reference is evidence to verify, and passing the check does not establish the host's interpretation.

## Judge a new observation against similar history before it is stored

The write path itself searches current records in this project by shared evidence locators, distinctive entrances and fact terms. You do not need to suspect a duplicate beforehand. When a candidate is worth checking, the first call writes NOTHING: it returns `status=needs_comparison`, the current facts of the candidates with their reasons (`evidence_ref`, `entrance`, `fact_terms`) and an opaque `comparison_token`. Exact structural duplicates still reuse silently, and an explicit `previous_id` revision commits directly without this step.

Candidate responses show at most three records per page. When `has_more` is true, resend the SAME draft with its `cursor` set to the returned `next_cursor` to read the next page; a hidden true duplicate must stay reachable this way before you decide. A `reuse`, `supplement` or `revise` target must be one of the candidates this comparison actually showed and still current; a mis-sent foreign ID is rejected with zero writes.

Judge with this standing prompt:

> 比较本次观察与候选旧经验。假设旧经验原句在它注明的对象、时间和环境内完全成立，本次证据还增加了哪一项可核查的事实？请引用能区分两者的旧原句和本次证据。若只是转述，复用；若多了独立来源，补证；若证据推翻旧原句，指出被推翻的部分并修订；若两条可同时成立且各自影响判断，分别保留。若区分所需事实缺失，指出缺哪一项并暂缓。只返回动作、候选 ID、最小差异和证据定位。

Resubmit the SAME draft and request_id with `resolution` — start from the prefilled `resolution_template` in the response (token, evidence and a default target are already filled; only `action` and `difference` are yours): `{"action":"reuse|supplement|revise|create|defer","target_id":"candidate current ID","difference":"…","evidence_ref":"…","comparison_token":"…"}`. `reuse` returns the current ID (no new version); `supplement` appends a genuinely independent source to the same root as an evidence revision with the claim unchanged (restatements of one snapshot are not independent); `revise` stores your complete draft as the new version of that root; `create` stores a new root even when a cue or snapshot is shared (`target_id` never becomes `previous_id`); `defer` writes nothing and ends the exchange. A changed candidate set invalidates the token and returns the fresh list; nothing partial is stored. Keep admission reasons, comparison answers or disposal instructions out of the stored body.

Submit exactly one body form per experience, synchronously with compact evidence. `fact` is one sourced business fact that changes a specific future judgment, keeping the object, environment or time qualifiers it was actually observed under; a configuration owned by an offline per-environment system can be recorded from its confirmed source even when no differing value was observed. `counterexample` records the fallible inference (`pitfall`) and the observation that overturned it (`counterexample`), pointing at the object actually read. `impact`, `applicability` and `boundary` are optional legacy qualifiers; write the qualifiers that matter into the fact or counterexample text itself instead of separate columns — a `fact`-form submission that still fills them is rejected with the field position. A cue supplies a stable id, label, evidence_refs and directly supported entrances; cue text is no longer authored on experiences (submissions carrying it are rejected with the field position) and reminders quote the body fact, qualifiers included. Several file/symbol/config/business aliases for the same association share one cue_id. Pure business evidence and entrances are valid. Skip ordinary recoverable facts, progress and finding no problem. Do not expand dependency graphs or invent incidents.

A new risk, changed condition or boundary is a material previous_id revision (or an independent new experience), not a short-only alias. Alias/line/hash relocation preserves the existing version and delivery records. Body, cues and index commit atomically. request_id retries the same record payload; indexed is only a storage receipt.

For a revision, use the full current ID and send the complete revised body. Copy each cue reference exactly from the submitted evidence[].reference values. If evidence is removed, review the cues that cited it in the same submission. Resolve the listed field errors together before resubmitting.

Use memory_reference_record for compact pointers to existing explicitly confirmed agreements. Include authority, confirmation locator, scope, exceptions and evidence-backed cues. Indexing creates no authority. Changes of meaning/scope require the relevant authorization.

After a successful new write or revision, report one concrete trigger in normal use for each new or changed cue. State the tool, field, entrance and prerequisites when verified. Use trigger_preview to locate the saved cue and doctor --probe to check the actual event and one near miss. Report an unverified or disabled trigger only when it affects use. Keep the report short and describe an exact retry once.

To verify a direct trigger before calling it ready, use doctor --probe EVENT.json --expect RECORD_ID or an actual hook record that lists that cue. A result of a lookup alone verifies retrieval, separate from a direct trigger. The probe check is read-only and reports where to correct a miss. See **Check a trigger before using it** below.

Use literal entrances in cue match expressions for distinctive trigger phrases and identifiers. Optional object_id_prefixes belongs to the same-input business-result association only. Run memory-mcp migrate to inspect an older library. When the report lists a record without cues, verify its evidence and submit a revised cue with previous_id. The same report tracks current records whose necessary qualifiers live only in the legacy narrative fields and records whose cue text carries facts beyond the body; those records stay readable with their full stored shape. Verify them against the original evidence before slimming, and carry the qualifiers into any revision instead of dropping them.

Lookup task/questions come from the whole current task and concrete uncertainty. Supply 1-4 independent grounded queries, terms with match=all (default) or any. Every term needs a searchable form: at least two consecutive identifier characters (letters, digits, `_`, `$`) or at least two Han characters. Chinese is indexed as adjacent pairs, so a single Han character or a term that splits into single characters (such as `0.6.4`) cannot be searched: such terms are dropped and listed in `dropped_terms` (a match=all query without them is correspondingly broader), and a query whose every term is dropped fails by name. keywords remains compatible. Similar words and source locations do not establish applicability. Check current evidence; no hit does not prove no risk. Up to three complete items (default two) fit one JSON text body <=8 KiB. Follow next_cursor or refine queries; changed queries/corpus require restarting. Legacy excerpts are navigation; read freely for full historical source. To enumerate historical records in a scope, call memory_search with an empty query and follow next_cursor until it ends. Keep the same filters on each page. Use inventory.next_cursor with memory_cleanup inspect and inventory.review_next_cursor with memory_search and needs_review=true.

## Fuse a recalled experience with the current step

The same experience legitimately produces different outcomes in different tasks: when asked for a test value, an existing test reading answers directly; when writing that value into a production plan, the same reading only supports the test environment. The host owns that judgment. When a piece of recalled history enters your context, use this standing prompt:

> 读到一段历史经验时，联系用户原始目标、你眼下要回答的问题或要作用的对象。
> 这段经验使哪个原先可能忽略的区别值得考虑？这个区别会改变本步的答案吗？
> 若会，看当前观察实际针对哪个对象、环境或版本，它支持、反驳，还是尚不足以判断？
> 已有证据足够就完成任务。证据不足且影响交付时，核查具体缺口，或明确当前能断言的范围。

If swapping two easily confused situations from the history would not change this step's answer or target — or current evidence already separates them — continue directly. When the conclusion would change and evidence has not separated the cases yet, verify the specific gap near delivery or narrow the claim; when evidence already contradicts the draft, correct it. No risk self-rating is required to receive or act on an experience.

Only specific observable index hits can intervene. If the body is undelivered, use the actual issued challenge_id to look up candidates; the original operation is not automatically rerun. For a challenge ID, the hook requests lookup at most once. Repeated or parallel hits already covered by that challenge proceed while the receipt is pending. Continue a partial result using the same ID and next_cursor. Use `memory-mcp doctor --challenge ID --data-dir LIBRARY` to distinguish query_requested, lookup_in_progress, response_pending_observation, partial_delivery, delivered, stale_context and technical failure or expiry. Status inspection is read-only. Only a verified native receipt credits delivery. Native Pre/Post tool_use_id associates the actual result with its original consumer/context, including late results. Successful results credit only actual complete returned items and that challenge's trigger cues. Errors fail open without credit. No acknowledgement/skip/complete tool exists.

After body delivery, another entrance for the same cue is silent. A new association of that body permits the operation with one fixed, evidenced short historical fact. Do not interpret this as an instruction to investigate or modify a named class. Ordinary turns preserve delivery; an explicit context reset starts lazy first delivery when a related entrance is seen, without reinjecting the library. Separate native agent identities are independent. Missing call IDs/hooks and unreported pruning are coverage gaps. Output is appended context, not a replaceable memory slot; accumulated context still has costs.

You may judge an experience inapplicable, already repaired or consistent with correct current code, and continue without any refusal registration, forced investigation or write-back. Voluntary lookup/read is always available. A receipt never proves model understanding, current applicability, completed investigation or permission.

Selected business results preserve record boundaries (including MCP JSON text and structuredContent) before matching. Direct and observed candidates are combined; ambiguous multi-object prose creates no inferred association. Only identifier hashes and matched candidate IDs are retained. No raw prompt, command or result is persisted. No per-turn experience reminder, time/count reminder, extra model, async write or Stop continuation. Existing explicit maintenance stays separate: preview a reversible plan and obtain actual fresh approval for that shown plan before confirm/apply; undo needs fresh approval. Preserve history and backups.

## Register the tool and name its fields

Put `tool-probes.json` in the running service's `data_dir`. Use the bound scope from `doctor`. First check a representative event with `doctor --probe EVENT.json`. `observed_fields` lists paths, types and string lengths from that sample. For Write and Edit, `registration_template` shows the suggested mapping. Compare the template to the fields your client actually sent.

If you do not have an event sample, register only the selector, for example `{"tool":"Write"}` or `{"mcp_tool":"warehouse_query"}`. Run setup, restart, then use the tool once in that project and run `doctor --session SESSION_ID`. The `tool_field_shape` rows show field paths and types for observed PreToolUse and successful PostToolUse events. A selector with no fields or observations is for inspection and does not challenge the operation. Copy a confirmed path into `fields`. Save the match in your cue.

```json
{
  "projects": {
    "project:example/team-repo": [
      {"tool": "Write", "fields": {"path": "file_path", "text": "content"}},
      {"mcp_tool": "warehouse_query", "fields": {"text": "arguments.statement"}}
    ]
  }
}
```

In `fields` the left side names a value for cues. The right side is a path inside `tool_input`, with dots for nested objects. The example makes `tool_input.content` available to a cue as `text`. Use `tool` for an exact native tool name, including a full MCP name when targeting one server. Use `mcp_tool` for a bare name across MCP servers. A project entry takes precedence over a preset. An exact tool name takes precedence over a bare MCP name within the project. Set `enabled: false` to skip that tool in the project.

Bash search commands (including rg), Grep, Read and recognized MCP query tools such as mysql_query already have presets. Their primary command, pattern or SQL is available as `text`. Read paths are available as `path`. Use `tool_input.fields` to confirm what the tool actually exported. The Bash preset covers search operations. A project can explicitly map Bash's command field if other shell operations should participate. An invalid tool configuration yields `probe_config_invalid`; tool probes pause until the file is corrected.

Run setup and restart the client after changing tool selectors. `doctor` reports `probe_config_valid`, `custom_probe_rules` and `probe_matcher_mismatches`. Adjusting a cue's match or changing a field mapping for an already routed tool applies to subsequent calls. A valid configuration confirms the on-disk settings. Native events confirm what the client sends.

## Define when a cue matches

The full worked manual — anatomy, hard limits, complete examples and a silent-miss checklist — is the final section of this guide, visible at runtime through `memory_bootstrap` and `memory-mcp guide`. The following cue matches when Write targets a .sql file and its proposed content has both the UPDATE token and a column containing fix_third. The same column in a SELECT, a .txt file, or the filename alone does not trigger this cue. Replace the evidence reference with an exact reference in your submission.

```json
{
  "cue_id": "settlement_code",
  "label": "Settlement field",
  "evidence_refs": ["verified-source"],
  "matches": [{
    "when": {"all": [
      {"field": "tool", "op": "eq", "value": "Write"},
      {"field": "path", "op": "suffix", "value": ".sql"},
      {"field": "text", "op": "token", "value": "UPDATE"}
    ]},
    "field": "text",
    "op": "contains",
    "entrances": ["fix_second", "fix_third"]
  }]
}
```

| Match property | Behavior |
| --- | --- |
| `matches` | Any matching expression is enough for this cue. A simple expression such as `{"op":"token","entrances":["findCustomer"]}` works with preset and registered tools. |
| `when` | Optional prerequisite on one match expression. Check the native tool name in `tool`, mapped inputs such as `path`, or an earlier result with `fact`. Use `all`, `any` or `not` for combinations. If it is false or unknown, this expression does not match. |
| `entrances` | Literal trigger phrases or identifiers. Any string in a leaf can match. A leaf with no `field` searches the exposed strings individually. Each successful alternative needs an entrance. |
| `value` | A comparison guard on a named field. Use it for an operation, path, switch, or result flag. |
| `all`, `any`, `not` | Require every condition, at least one condition, or a false child. Missing fields or unavailable facts are unknown, and negating unknown leaves it unknown. |
| `token`, `contains` | `token` uses ASCII identifier boundaries, including underscores. `contains` finds a substring within one field. Use it for a partial identifier such as `fix_third`. |
| `eq`, `prefix`, `suffix`, `exists` | Compare the whole value, its beginning, its end, or its presence. `eq` also accepts boolean and number values and preserves their types. `exists` takes no value. |
| `case_sensitive`, `normalize` | Entrances retain original spelling. String comparisons default to case-insensitive. Set `case_sensitive: true` on a comparison to require exact casing. `normalize: "path"` unifies slash styles, and `"whitespace"` ignores Unicode whitespace. |

For SQL queries, `text` can already be mapped by the mysql_query preset or by the warehouse_query example. To make the cue work for both file writing and queries, add a second expression to `matches` with an exact tool guard for the query and the desired SQL predicates. Use `value` inside `when` and put `entrances` beside it. Leave `when` out when an entrance alone should match. A path comparison interprets the event's filename as text, including a target in a separate documentation repository.

### Previous tool results

A tool can expose result fields through an observation. In this example, login exposes two fields for comparison with a cue. The ordered producer key uses `tool_input.connection_id`. The consumer names that same input value `connection`.

```json
{
  "projects": {
    "project:example/team-repo": [
      {"mcp_tool": "login", "observe": [{
        "name": "authenticated",
        "key": ["input.connection_id"],
        "fields": {"ok": "ok", "state": "status"},
        "ttl_seconds": 120
      }]},
      {"mcp_tool": "warehouse_query", "fields": {
        "text": "arguments.statement", "connection": "connection_id"
      }}
    ]
  }
}
```

In the query cue's `matches`, place the following expression alongside any other trigger expressions. The authentication comparison belongs to this cue.

```json
{
  "when": {"all": [
    {"field": "text", "op": "token", "value": "UPDATE"},
    {"fact": {"name": "authenticated", "key": ["connection"], "match": {
      "all": [
        {"field": "ok", "op": "eq", "value": true},
        {"field": "state", "op": "eq", "value": "READY"}
      ]
    }}}
  ]},
  "field": "text",
  "op": "contains",
  "entrances": ["fix_third"]
}
```

Observation field paths are relative to the result object, which can be a direct object, MCP structuredContent or a single JSON-object text block. A producer key has 1–4 ordered scalar paths prefixed with `input.` or `result.`. Use the same value types and order on the consumer. The hook needs the same native call ID on PreToolUse and PostToolUse, plus the session and input boundary. Default lifetime is the current input. Set `lifetime: "session"` to retain a fact across inputs within its TTL. TTL defaults to 120 seconds and is capped at 600. Only the comparison outcome and hashes are stored. Rerun the producer after changing the result predicate.

### Check a trigger before using it

Save a sample with the client's `tool_name` and `tool_input`. Use `--project` if the sample omits `cwd`. For example, save the following as EVENT.json.

```json
{"tool_name":"Write","tool_input":{"file_path":"example.sql","content":"update demo SET work_order_feedback_fix_third = 1;"}}
```

```text
memory-mcp doctor --host zcode --config CONFIG --data-dir LIBRARY --project PROJECT --probe EVENT.json --expect RECORD_ID --expect-cue settlement_code
```

Read `outcome` and `next`. For `tool_not_registered`, use `observed_fields` to map the actual paths. For `tool_not_routed`, rerun setup and refresh the client. For `no_scanned_fields` or missing named inputs, check `tool_input.fields` for missing, empty, unsupported or oversized values. For `cue_missed`, check `cue_checks`. A check with `stage: "when"` reports whether the prerequisite passed, failed, or lacked an input or fact. Its `conditions` give the tested fields. After it passes, the next check reports whether the entrance matched. Try the expected matching sample and a similar sample that should pass without a challenge. Include `--session SESSION_ID` or the native event identity if testing a saved result fact. The probe check executes no tool and writes no hook event, fact, challenge or receipt. It does not verify native dispatch.

After a real operation, use `doctor --session SESSION_ID` to inspect `recent_events`. An `experience_probe` with `candidate_ids` and a `silent` outcome means the cue matched and had no new delivery. A successful lookup can also retrieve a cue absent from the trigger candidates. Inspect the candidates before concluding that a cue triggered.

## Cue matching manual（cue 编写手册）

面向编写触发规则的使用者与 agent：一次写对，不靠试错，不写永远打不响的规则。

### 三十秒版

- 一条 cue = 稳定 `cue_id` + `evidence_refs` + 若干条 match 表达式（**数组内 OR，任一命中即触发**）
- 一条表达式 = 可选 `when` 前置门槛 + 主匹配（`entrances` 词表，或 `field+op+value` 守卫）
- cue 能引用的字段只有三类来源：`tool`（原生工具名）、探针 `fields` 映射出的别名、探针 `observe` 观察到的 `fact`
- **引用了探针没暴露的字段 = 永远静默不命中**（哑火第一因）
- 写完必须 `doctor --probe` 正、负双样本验证
- 提交时 cue 不写正文文本；`admission` 必带；`evidence_refs` 抄本次 evidence 的 reference 原值

### 解剖结构与硬性限制

```json
{
  "cue_id": "settlement_code",
  "label": "结算字段",
  "evidence_refs": ["verified-source"],
  "matches": [{ "when": { "all": [
    {"field": "tool", "op": "eq", "value": "Write"},
    {"field": "path", "op": "suffix", "value": ".sql"},
    {"field": "text", "op": "token", "value": "UPDATE"}
  ]}, "field": "text", "op": "contains", "entrances": ["fix_second", "fix_third"] }]
}
```

语义：**只有** Write + `.sql` 文件 + 内容含 UPDATE 词元时，才在内容里找 `fix_second`/`fix_third` —— 同一列名出现在 SELECT、`.txt` 或仅文件名里都不触发。

| 字段 | 限制 |
| --- | --- |
| `cue_id` | 小写字母开头，共 ≤48 个 `[a-z0-9_]`；同一关联的多个别名共用一个 cue_id |
| `label` | ≤24 个 Unicode 字符 |
| `evidence_refs` | 1–3 条，**逐字抄本次提交 `experience.evidence[].reference` 原值**（断链被逐项校验拒绝） |
| `entrances` | 每个匹配组直接证据入口词合计 1–12 条；保留原始拼写 |

### 字段从哪来（哑火第一因）

| cue 里的字段 | 来源 |
| --- | --- |
| `tool` | 原生工具名（精确；配 `mcp_tool` 用裸 MCP 名） |
| `text` / `path` 等别名 | `tool-probes.json` 里 `fields` 映射的值（如 Write：`text←content`、`path←file_path`） |
| `fact` | 探针 `observe` 观察到的上一工具结果（按 `fact.key` 关联，带 TTL） |

内置预设（无需注册）：Grep/rg/Glob/Read 等搜索工具（pattern→`text`、路径→`path`）、**仅搜索类**的 Bash 命令（主命令作为 `text`）、mysql_query 等业务查询 MCP 工具（SQL→`text`）。

> ⚠️ `when` 或主匹配引用了探针没映射的字段：不是报错，是 unknown → 表达式永不匹配 → 静默哑火。排查：`tool-probes.json` 是否注册并映射；未注册工具的 `doctor --probe` 返回 `tool_not_registered` 并给 `observed_fields`，照着补映射；改完 selector 重跑 `setup` 并重启客户端。

### 操作符速查

| op | 行为 |
| --- | --- |
| `token` | ASCII 标识符边界（含下划线）——**默认首选**；不会把 `fix_third` 误匹到 `fix_thirdly` |
| `contains` | 单字段子串；只记得标识符前缀时用 |
| `eq` / `prefix` / `suffix` / `exists` | 整值 / 开头 / 结尾 / 存在性；`eq` 保留布尔与数字类型 |
| `all` / `any` / `not` | 组合器；**缺字段或事实不可用 = unknown，`not unknown` 仍是 unknown** |
| `case_sensitive` / `normalize` | 默认大小写不敏感；可要求精确大小写、`path` 归一化斜杠、`whitespace` 忽略 Unicode 空白 |

### 写规则的顺序

1. 先定代价方向：红线类（凭据、合并方向、删改操作）用强标识词 + 多重门槛，宁哑勿误；提示类可以宽。
2. `entrances` 用有区分度的字面量：具体符号名、编码、函数名 > 泛词（“登录”“密码”谁都会撞）。
3. `when` 收窄场景：`tool eq` 锁工具、`path suffix` 锁文件类型、`text token` 锁操作类型（UPDATE/DELETE）。
4. 多场景 = `matches` 多条：同一经验要在“写 .sql 文件”和“mysql_query 查询”都触发，就写两条表达式各带自己的 when。
5. 组装提交：body 二选一形态；cue 不写 text；`admission` 必带；`needs_comparison` 时按 `resolution_template` 填 `action`/`difference` 用同一草案和 request_id 复调。
6. 正、负双样本验证，缺一不可。

### 完整示例

Write 门槛类（探针注册 + cue 两半缺一不可）。探针：

```json
{"projects": {"project:your-org/your-repo": [
  {"tool": "Write", "fields": {"text": "content", "path": "file_path"}},
  {"mcp_tool": "login", "fields": {"text": "username"}}
]}}
```

cue 示例一（写 .sql 的结算编码门槛）与示例二（login 凭据自主获取约定，入口词用具体账号/调用形态而非“登录”）：

```json
{"cue_id": "settlement_code_write_gate", "label": "结算编码写入", "evidence_refs": ["fix-commit-abc"],
 "matches": [{ "when": { "all": [
   {"field": "tool", "op": "eq", "value": "Write"},
   {"field": "path", "op": "suffix", "value": ".sql"},
   {"field": "text", "op": "token", "value": "UPDATE"}]},
   "field": "text", "op": "contains", "entrances": ["fix_second", "fix_third"] }]}

{"cue_id": "credential_self_fetch", "label": "凭据自主获取", "evidence_refs": ["ops-runbook-credential"],
 "matches": [{ "when": {"field": "tool", "op": "eq", "value": "login"},
   "field": "text", "op": "contains", "entrances": ["G003027", "svc_after_sale_ro", "login()"] }]}
```

示例中的 evidence ref、账号名为占位；实际提交必须引用本次的真实证据，入口词来自项目中核实过的真实调用。验证样本：

```json
{"tool_name":"Write","tool_input":{"file_path":"sql/patch.sql","content":"update t set fix_third = 1"},"cwd":"<项目绝对路径>"}
```

```text
memory-mcp doctor --host zcode --config <config> --data-dir <library> --probe EVENT.json --expect <记录ID> --expect-cue settlement_code_write_gate
```

### 防“写了不生效”清单

- [ ] `when`/主匹配引用的字段，探针真的暴露了吗？（未映射 = 静默哑火）
- [ ] Bash 预设只覆盖搜索类命令；全部命令文本进匹配需项目级注册 `{"tool":"Bash","fields":{"text":"command"}}`
- [ ] cue 里写了正文文本字段 → 写边界直接拒（提醒自动引用 body）
- [ ] 新风险、条件变化、边界收紧 ≠ 加别名：要 `previous_id` 实质修订；纯别名/行号搬迁才保留原版本
- [ ] `evidence_refs` 逐字抄了本次 `evidence[].reference`？
- [ ] 改了 selector / fields：重跑 `setup` + 重启客户端再验证
- [ ] 大小写：默认不敏感；精确大小写加 `case_sensitive`；路径比较考虑 `normalize: "path"`
- [ ] 双样本都验了吗？只验正样本 = 不知道规则有多宽
