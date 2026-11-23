# Cue 编写手册（Cue Matching Guide）

面向给 memory-mcp 编写经验触发规则（cue）的使用者与 agent。目标：一次写对，不靠试错，不写"永远打不响"的规则。语法细节以 `memory_bootstrap` 返回的内嵌 guide 和 `memory_experience_record` 工具 schema 为准，本手册是它们的使用法展开。

## 0. 三十秒版

- 一条 cue = 稳定 `cue_id` + `evidence_refs` + 若干条 match 表达式（**数组内 OR，任一命中即触发**）
- 一条表达式 = 可选 `when` 前置门槛 + 主匹配（`entrances` 词表，或 `field+op+value` 守卫）
- cue 里能引用的字段只有三类来源：`tool`（原生工具名）、探针 `fields` 映射出的别名、探针 `observe` 观察到的 `fact`
- **引用了探针没暴露的字段 = 永远静默不命中**（哑火第一因，见 §2）
- 写完必须 `doctor --probe` 正、负双样本验证（§6）
- 提交时 cue 不写正文文本；`admission` 必带；`evidence_refs` 抄本次 evidence 的 reference 原值

## 1. 解剖结构

```json
{
  "cue_id": "settlement_code",
  "label": "结算字段",
  "evidence_refs": ["verified-source"],
  "matches": [ ... ]
}
```

`matches` 里的一条表达式（官方完整示例）：

```json
{
  "when": { "all": [
    {"field": "tool",  "op": "eq",     "value": "Write"},
    {"field": "path",  "op": "suffix", "value": ".sql"},
    {"field": "text",  "op": "token",  "value": "UPDATE"}
  ]},
  "field": "text",
  "op": "contains",
  "entrances": ["fix_second", "fix_third"]
}
```

语义：**只有** Write 工具 + `.sql` 文件 + 内容含 UPDATE 词元时，才在内容里找 `fix_second`/`fix_third` —— 同一列名出现在 SELECT、`.txt` 文件或仅文件名里都不触发。

### 硬性限制（提交前自查，避免被写边界拒绝）

| 字段 | 限制 |
| --- | --- |
| `cue_id` | 小写字母开头，共 ≤48 个 `[a-z0-9_]`；同一关联的多个别名共用一个 cue_id |
| `label` | ≤24 个 Unicode 字符 |
| `evidence_refs` | 1–3 条，**必须逐字抄本次提交 `experience.evidence[].reference` 的原值**（cue 引用与证据断链会被逐项校验拒绝） |
| `entrances` | 每个匹配组直接证据入口词合计 1–12 条；保留原始拼写 |
| `matches` | 任意一条命中即触发；`when` 为 false 或 unknown 时该条不参与 |

## 2. 字段从哪来（与探针的耦合 —— 哑火第一因）

| cue 里的字段 | 来源 |
| --- | --- |
| `tool` | 原生工具名（精确；配 `mcp_tool` 用裸 MCP 名） |
| `text` / `path` 等别名 | 探针 `tool-probes.json` 里 `fields` 映射定义的值（如 Write：`text←content`、`path←file_path`） |
| `fact` | 探针 `observe` 观察到的上一工具结果（按 `fact.key` 关联，带 TTL） |

**内置预设**（无需注册即生效）：Grep/rg/Glob/Read 等搜索工具（pattern→`text`、路径→`path`）、**仅搜索类**的 Bash 命令（含 rg/grep/cat 等，主命令作为 `text`）、mysql_query 等业务查询 MCP 工具（SQL→`text`）。

> ⚠️ **`when` 或主匹配引用了探针没映射的字段：不是报错，是 unknown → 表达式永不匹配 → 静默哑火。**
> 排查法：看 `tool-probes.json` 是否注册了该工具并映射了该字段；未注册的工具用 `doctor --probe` 会返回 `tool_not_registered` 并给出 `observed_fields`，照着补映射。改完 selector 后必须重跑 `setup` 并重启客户端。

## 3. 操作符速查

| op | 行为 |
| --- | --- |
| `token` | ASCII 标识符边界匹配（含下划线）——**默认首选**；不会把 `fix_third` 误匹到 `fix_thirdly` |
| `contains` | 单字段内子串；只记得标识符前缀时用 |
| `eq` / `prefix` / `suffix` / `exists` | 整值 / 开头 / 结尾 / 存在性；`eq` 保留布尔与数字类型 |
| `all` / `any` / `not` | 组合器；**缺字段或事实不可用 = unknown，`not unknown` 仍是 unknown**（不会误当 false，但也不会放行） |
| `case_sensitive` / `normalize` | 默认大小写不敏感；可要求精确大小写、`path` 归一化斜杠、`whitespace` 忽略 Unicode 空白 |

## 4. 写规则的顺序（六步）

1. **先定代价方向**：误报伤还是哑火伤？红线类约定（凭据、合并方向、删改操作）用强标识词 + 多重门槛，宁哑勿误；提示类可以宽。
2. **entrances 用有区分度的字面量**：具体符号名、编码、函数名、字段名 > 泛词（"登录"、"密码"谁都会撞）。
3. **`when` 收窄场景**：`tool eq` 锁工具、`path suffix` 锁文件类型、`text token` 锁操作类型（UPDATE/DELETE）。
4. **多场景 = `matches` 多条**：同一经验要在"写 .sql 文件"和"mysql_query 查询"都触发，就写两条表达式各带自己的 when。
5. **组装提交**：body 二选一形态（`fact` 单句带限定词，或 `pitfall`+`counterexample`）；cue 不写 text；`admission.future_decision` + `admission.evidence_ref` 必带。若返回 `needs_comparison`（同项目有可比旧经验，零写入），按响应内 `resolution_template` 填 `action` 与 `difference`，用同一草案和 request_id 复调。
6. **正、负双样本验证**（缺一不可）：正样本证明打得响，近失样本证明不过宽。

## 5. 完整示例

### 5.1 Write 写 .sql 的门槛（结算编码类）

先保证探针注册了 Write（放入库目录 `tool-probes.json`）：

```json
{
  "projects": {
    "project:your-org/your-repo": [
      {"tool": "Write", "fields": {"text": "content", "path": "file_path"}}
    ]
  }
}
```

cue（挂在证据 reference 为 `fix-commit-abc` 的经验上）：

```json
{
  "cue_id": "settlement_code_write_gate",
  "label": "结算编码写入",
  "evidence_refs": ["fix-commit-abc"],
  "matches": [{
    "when": { "all": [
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

正样本 `EVENT-pos.json` 与近失样本 `EVENT-neg.json`（词元边界，应不触发）：

```json
{"tool_name":"Write","tool_input":{"file_path":"sql/patch.sql","content":"update t set fix_third = 1"},"cwd":"<项目绝对路径>"}
{"tool_name":"Write","tool_input":{"file_path":"sql/patch.sql","content":"update t set fix_thirdly = 1"},"cwd":"<项目绝对路径>"}
```

```text
memory-mcp doctor --host zcode --config <config> --data-dir <library> \
  --probe EVENT-pos.json --expect <记录ID> --expect-cue settlement_code_write_gate
```

### 5.2 MCP login 的凭据自主获取约定

探针注册（裸名跨服务器匹配 login）：

```json
{"mcp_tool": "login", "fields": {"text": "username"}}
```

cue：入口词用**具体账号/调用形态**这类有区分度的字面量，而不是"登录"：

```json
{
  "cue_id": "credential_self_fetch",
  "label": "凭据自主获取",
  "evidence_refs": ["ops-runbook-credential"],
  "matches": [{
    "when": {"field": "tool", "op": "eq", "value": "login"},
    "field": "text",
    "op": "contains",
    "entrances": ["G003027", "svc_after_sale_ro", "login()"]
  }]
}
```

> 示例中的 evidence ref、账号名均为占位；实际提交必须引用本次提交的真实证据，入口词来自你在项目里核实过的真实调用。

## 6. 验证与诊断

`doctor --probe` 是只读检查，不执行工具、不写事件。关键输出：

| outcome | 含义与下一步 |
| --- | --- |
| `direct_match` | 命中，正样本通过 |
| `cue_missed` | 看 `cue_checks`：`stage:"when"` 报哪个条件 fail / 缺输入 / 缺 fact；entrance 阶段报词没命中 |
| `tool_not_registered` | 该工具没进预设也没注册；用 `observed_fields` 对照补 `tool-probes.json` |
| `tool_not_routed` | 注册了但客户端没派发；重跑 `setup` 并重启客户端 |
| `no_scanned_fields` | 映射了但输入缺失/为空/超限（`text` 预算 32KB） |

提交成功后用 `trigger_preview` 定位已存 cue，再各做一次正/负 probe。真实会话里可用 `doctor --session SESSION_ID` 看 `recent_events` 中的 `experience_probe` 是否带出 `candidate_ids`。

## 7. 常见坑（防"写了不生效"清单）

- [ ] `when`/主匹配引用的字段，探针真的暴露了吗？（未映射 = 静默哑火，§2）
- [ ] Bash 预设只覆盖搜索类命令；要让全部命令文本进匹配，需项目级注册 `{"tool":"Bash","fields":{"text":"command"}}`
- [ ] cue 里写了 `text` 正文字段 → 0.6.4 写边界直接拒（提醒自动引用 body）
- [ ] 新风险、条件变化、边界收紧 ≠ 加别名：要 `previous_id` 实质修订；纯别名/行号搬迁才保留原版本
- [ ] `evidence_refs` 是否逐字抄了本次 `evidence[].reference`？
- [ ] 改了 selector / fields 之后：重跑 `setup` + 重启客户端，再验证
- [ ] 大小写：默认不敏感；要精确大小写加 `case_sensitive: true`；路径比较考虑 `normalize: "path"`
- [ ] 双样本都验了吗？只验正样本 = 不知道规则有多宽
