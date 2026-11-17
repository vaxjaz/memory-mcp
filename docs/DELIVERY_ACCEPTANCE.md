# 首次正文与关联点增量交付：实施与验收

版本 0.6.0-delivery.1，integration protocol 6，schema 5。基线 387d44ddf81d75e4367f8513e9ad3543597a7834。保留已有识别归一化草稿，移除未提交的冷却草稿及旧整轮豁免；原草稿在本机 /tmp/memory-delivery-before.tgz 与 patch 留存。目标是嵌套 memory-mcp 的本地宿主组件，不涉及父项目 VPS。

## 已实现

稳定 cue 与入口别名索引、独立模型可见正文 DTO、按接收方/上下文/经验版本的正文与 cue 账本、原生 Pre 绑定调用和 Post 成功收据、完整分页逐页确认、原子正文/cue/索引更新、修订与旧数据兼容。主动 lookup/read 不受去重约束。Agent 可判断不适用、已修复或没有问题并继续，不要求登记、调查或改代码。

SessionStart 只给简短职责说明，不注入全库。正常输入不重置正文/cue；明确 compact/clear 后延迟到相关命中才查询。无时间/次数提醒、全轮通行证、原文采集、额外模型、异步经验写入或 Stop gate。新增风险进入新内容版本及首次正文，不能藏在短引用里。

## 真实原生传递与统计

证据：[合成原生统计](validation/native-delivery.json)，脚本 scripts/check_native_delivery.py。真实 Windows Codex CLI/app-server 0.169.0-alpha.11.1，桌面包 26.1111.2228.0。临时 CODEX_HOME、临时经验库、只读 fixture MCP 仅能读两份合成文件；模型端是本地确定性 Responses HTTP 服务，没有调用第二个模型。仅检查合成请求并保存聚合数据；未读取用户 transcript。

成功路径 A→lookup→重复 A→B→重复 B：实际 block 1、lookup 1、成功正文收据 1、触发 cue 1、增量 cue 发出 1、文件成功结果 3。首次 block 工具输出封装 479 字节；lookup 工具输出封装 1765 字节；重复 A 新增记忆 0；B 具体反例 developer context 封装 298 字节；重复 B 新增记忆 0。单次全过程 10.656 秒（含进程启动与本地模型往返）。累计记忆材料封装字节 0→479→2244→2244→2542→2542；这是 UTF-8 字节，不是 token。B 在实际下一次模型请求中可见，满足 A40；不是只在通知或日志中出现。

真实 app-server 继续输入及压缩实验：继续不新增 block；完成真实 contextCompaction 后 context generation 改变，未立即注入正文；下一次 A 命中重新查询，最后成功正文收据累计 2。全过程 13.375 秒。压缩模型仅返回合成摘要，不代替真实业务推理。测试只对 harness 自己创建的临时 hook 授信；真实用户 hook 信任配置未由测试改写。

错误实验：先生成真实 receipt，再由隔离代理改为 MCP isError。正文投递 0、cue 0，没有将生成当交付。此 Codex 版本不发送可用失败 Post 事件，因此不能即时故障放行；立即重试仍 block，最终以两分钟技术截止放行。数据库/适配器错误或有失败 Post 的路径会直接 fail-open。技术截止不记成功、不会安排补偿提醒。这是明确的宿主限制；不能声称所有失败即时放行。

Go 隔离链路连续 100 次 A 混用符号/文件/业务入口，新增记忆字节为 0；B 多入口合并为一个 cue，之后 100 次同样为 0。六个独立进程并发仅一个发送者；另一个真实子进程保留后退出，后续进程成功恢复。stdout 与 SQLite 无原子提交：写出后、提交前崩溃仍可能保守重放，不承诺端到端 exactly-once。

四适配器子进程重放（Linux）：无命中 hook 中位 11.1–13.4 ms、最大 14.6 ms。它不包含 Windows PowerShell 和真实模型耗时。此前同机 PowerShell 启动约 0.7–0.8 秒；窄 matcher 仍有必要。工具 schema、正文与追加上下文也有 token 成本；未测实际模型 token 增量或自主决策改善。

## 输出样例（仅合成 fixture）

首次 lookup 的 experience 正文形状如下；外层另有实际 root_id/id、receipt_id、分页和定位信息，均由系统生成：

```json
{"pitfall":"合成实验误以为备注变化自动影响日志","counterexample":"合成实验中业务备注已更新，日志仍读取独立旧参数。","impact":"合成日志可能保留旧文本","applicability":"同一合成回馈业务","boundary":"条件不同或已修复时可以直接继续，无需修改或登记拒绝","related_facts":["合成实验中备注更新后日志仍保留旧文本。","合成实验中日志生成读取独立旧参数，备注更新未传入。"],"evidence":[{"kind":"business","reference":"fixture:local-only","observation":"合成反例，不是真实事故"}]}
```

B 的单次短引用样例（身份前缀以占位符表示）：`E1@R1 · 取值入口：合成实验中日志生成读取独立旧参数，备注更新未传入。`。这不是修改指令，A/B 业务事实均不得写入真实经验库。

## A01–A43 对照

Linux 全量 Go 测试、go vet、14 项 Python 包装/发布单测、Windows TestDelivery 全部通过。

M = internal/memory/experience_delivery_test.go；H = internal/hooks/lookup_receipt_test.go；已有 experience/business/host_recall 测试一并回归。所有人为案例仅存在临时库。

| 项目 | 证据与结果 |
| --- | --- |
| A01–A08 | M 首次/重试/别名/新 cue/合并；真实 native success 验证 block、正文、短引用和静默 |
| A09–A10 | M E2 不继承 E1；首次仅消费触发 cue，不提前消费正文里的其他关联 |
| A11 | M 12 并发调用、6 独立进程，正常路径只有一个发送者 |
| A12–A13 | 生成后不登记；H isError/解析/传输错误；真实 isError 不误记；原生即时失败释放受限如上 |
| A14–A15 | M 篡改、跨身份、旧上下文不释放；相同成功回执幂等 |
| A16 | M ActualPageOnly：页1仅记一个；未返回版本仍 challenge；页2累计确认 |
| A17 | M challenge 候选强制纳入，查询词不匹配也不会悄悄消失 |
| A18 | M turn 变化不重置；真实 app-server 的 Continue 已验证 |
| A19 | M/H 显式 child identity 独立；真实 fork/subagent 生命周期未实测 |
| A20 | M compact 重放幂等；真实 contextCompaction 后延迟重查已验证；真实 clear 未实测 |
| A21 | M 别名、代码文件行移动/引用变更保持版本与历史证据；派生索引重建不清账本 |
| A22–A24 | M 实质修订旧收据不覆盖；主动 lookup；纯业务反例可写可查可触发 |
| A25 | 既有 business_test + check_host_recall.py：读后同对象观察，不能串单；真实 OA 客户端未实测 |
| A26 | M 同 cue 混用入口各 100 次，新增提示 0 字节 |
| A27–A28 | M 整句预算、剩余标记、未发不消费、超长/证据缺失拒绝；语义质量仍由宿主核实 |
| A29 | M stdout 失败释放、死 owner、真实进程退出恢复；保守重放窗口明确保留 |
| A30–A31 | H 精确服务名/原生别名、重复 setup、无 Stop/递归；真实 lookup Pre/Post 静默 |
| A32 | M 迁移/备份；真实原库只读副本 schema3→5 与旧版恢复，见下文 |
| A33 | 故障与技术超时的 fail-open 测试；原生无失败事件时立即重试仍可能阻挡，明确限制 |
| A34 | 单份 JSON text、无 structuredContent 双份；DTO 隐藏 cues/entrances；Post 不复制正文 |
| A35–A36 | M Pre 绑定旧上下文的晚到结果及并发普通 lookup 独立收据 |
| A37–A38 | 允许不适用/已修复判断的接口契约与不强迫后续行为已验证；自主模型判断质量未实测 |
| A39 | M 新 cue 事实进入实质新版本与首次正文，不能只发短线索 |
| A40 | 真实原生请求捕获：B 的具体反例进入模型继续请求 |
| A41–A42 | 无新匹配时静默；不采纳/没有新增事实不产生长期记录；不把任务拆解作为触发器 |
| A43 | M 边界修订保留旧版本；正文/cue/索引事务提交，故障回滚不退休旧正文 |

## 迁移与安装核对

原实际连接为 0.4.0-experience.1/schema3，Codex 配置同版；另有未启用的 0.5 包；ZCode 的 memory-review-trial 仍配置 0.3 并共享同一个 D: 库。安装必须一起更新共享入口；不以 0.5 源码覆盖原库。

[真实只读快照结果](validation/migration.json)：23 entries、9 profiles、19 links、2 bindings、1 experience_policy 及全部其他源表逻辑哈希完全一致；6 个 structured experience profiles 保留。重建17个可检索文档，旧 generated/cooldown 未提升为正文收据，正文投递0。Windows 副本迁移360 ms，integrity_check=ok。保留独立未升级副本并用原 0.4 二进制实际打开、核对源表哈希完成回滚验收。原库测试中只读，无合成业务写入。

生产安装的独立完整备份、配置与实际运行源码 SHA 以安装生成的 DELIVERY_INSTALLATION.json 为准；SOURCE_COMMIT/build-info 必须与本地和 GitHub 精确提交一致。安装需要重连宿主并审核新 hook，不能通过伪造真实信任哈希掩盖未启用。此报告不把独立 stdio bootstrap 冒充当前聊天的 MCP 热重载。

## 防退化复核及未验证范围

已对方案禁止项逐项复核：入口不是经验；cue 不扩张类依赖；无正文截断/标题代替事实；新风险不降级为短引用；无整轮豁免/冷却提醒/每任务提示；无 Agent 已读/拒绝/完成 API；无强迫采纳、改代码、补偿调查；无额外模型、原文日志、Stop 续轮。唯一流程是按经验版本、cue 与真实投递记录判断。收据不证明理解、适用性、调查完成或授权。追加上下文会累计，不能被当成覆盖槽。

未实测：ZCode/Qoder/Qoder IDE 的真实原生回执与模型可见注入、真实子 Agent/fork/clear、未报告的上下文裁剪、macOS/Linux 的真实 GUI 宿主。平台包交叉构建不是这些宿主验收。缺少 tool_use_id 的适配器只会 fail-open，不伪造成功。没有自然任务的自主模型使用样本，不能报告召回价值、误触发率、决策改善或模型 token 收益。

官方协议参考：[Codex hooks](https://learn.chatgpt.com/docs/hooks?site_variant=chatgpt&translationFallback=zh-Hans)。本机可执行文件的原生实测优先于网页对其他版本的描述。
