# v0.2.0：记忆质量、上下文检索与用户确认的清洗

这不是内置模型或向量检索。宿主负责语义理解，程序负责来源、版本、范围、流程和可恢复管理。

## 1. 准入

新 case/hypothesis 必须有真实来源和 profile：

```json
{
  "class": "long_term",
  "future_use": "下次在已验证的测试环境登录时，显式传参以避免缺配置造成挂起",
  "counter_check": "是有复用价值的环境限制，不是单次完成状态；先比较现有登录案例",
  "applicability": "当前项目和已验证工具/环境",
  "boundary": "其他环境未知；配置变化后重新核验，不推导成所有登录工具的通用规则",
  "search_hints": ["获取 token 没响应", "无参登录挂起"]
}
```

程序检查字段、文本完整性和来源存在性，不能证明用途具体、推断正确或引文忠实。
临时任务用 temporary，可附有明确依据的 expires_at；不编造期限。过期条目退出目录和适用选择，不销毁正文。
原始事件默认隐藏，不等于每轮生成案例。现有旧案例不补造历史准入理由。

## 2. 当前任务驱动的检索和去重

UserPromptSubmit 的轻量 hook 交付本轮 source_id。宿主结合项目、可见会话前文、已读记忆和当前输入组织任务，不只截取最新一句。

1. memory_context prepare：task、questions、最多八个有依据的关键词。
2. 返回同项目当前有效目录（30 条/页）和关键词候选。目录/摘要不是全文，也不是语义排名。
   按 next_cursor 继续目录，关键词结果的分页用 memory_search；不能把首屏当完整结果。
3. 宿主进行语义选择，memory_read 时传 context_source_id，读取来源、例外和替代关系。
   通常两轮补查，有明确新问题才继续；重复引用不是新的独立证据。
4. complete：selected_ids 是实际读过且当前有效的版本；decision 写会影响本次行动的要求或不适用原因。
   普通无关问题用 skip，写清任务与原因，不强迫检索。
5. 只有有实质新增且通过正反筛选，才 save_candidate=true 请求 Stop 额外整理。未提交筛选会留下 context_not_completed，程序不假装知道是否漏记。

比较同一事项的对象、条件与决定：无实质新增用 memory_review_done 的 reused_case_ids；新增关联写到复用账，不修订正文，不增加变更计数，不使下游失效。
确有条件/证据补充或纠正才 revise；不同事项分开保存。关键词和标题相似不能自动证明重复。
没有通用对象编号体系，也不宣称提供了语义相似度模型。

## 3. 事前使用检查

setup 的 PreToolUse matcher 覆盖所有工具，handler 忽略 memory 工具并快速放行普通操作。
对可识别的 Write/Edit/apply_patch、login/authenticate/deploy、SQL 写入及若干启动/部署命令，检查本轮是否完成 context，已选版本是否仍有效。失败返回 block，补查后重试。
这只是可观察工具名称/参数的覆盖，不是完整 shell 解析器；复杂脚本、隐藏调用、未识别工具不能声称已覆盖。
来源读取、选择和 gate 的记录只证明流程与版本，不证明理解正确、已经执行约定或“成功避免错误”。
已安装 ZCode 的 native 执行器支持根 decision=block/reason；其他宿主必须另行适配，不套用格式。

## 4. 清洗触发和确认

按 scope 单独计数：新 case/hypothesis 与正文修订计数；原始采集、重试、复用、提炼摘要与清洗账不计。
默认每 100 次变化；旧库首次升级有一次只读检查提示。无宿主运行不进行后台模型工作。

自然语言设置由宿主翻译成 memory_cleanup configure，并回告项目、口径、次数与是否重复：

- “每 200 次后清洗”：interval=200, repeating=true, enabled=true。
- “再过 50 次提醒”：interval=50, repeating=false, enabled=true；从设置时的当前计数起算。
- “关闭自动清洗”：enabled=false，保留原间隔。
- “现在清洗”：inspect 后按需 preview，不改长期配置。
- “稍后”：defer_changes 指定再累计多少次；未决定时不每轮重复催促。

configure 的 source_id 必须是实际用户指令。提示会被记账；没有发现候选时 acknowledge 安排下个间隔。

清洗过程：inspect 分页盘点（含计数和 needs_review 队列）→ read 全文/依据 → preview → 展示具体清洗单 → 用户选择确认/调整/稍后 → apply。
preview 仅新增管理账，不改业务记忆。计划支持 archive、supersede 和 distill。
同项目保留一份待确认方案，新 preview 替代旧 preview；inspect 可找回跨会话的待确认计划。
supersede 只用于整体替代，不能因“被引用”或部分纠正就作废整条；不同范围、未解决冲突不能强行合并。
distill 只更新简洁展示内容，保留原文；正文后续修订时旧摘要自动失效。

apply 必须引用预览之后新的用户确认原文和精确摘录；升级授权不是清洗授权。
程序核对确认来源，不能独立判断中文批准的含义，宿主仍须展示计划并取得明确答复。
执行前做一致性数据库备份，并在事务中核对目标、替代项、依据版本和管理状态；变化就拒绝旧预览。
管理修改可 undo，同样需要新的用户确认；发生后续修改则拒绝覆盖。
替代可能使其他解释需要复查，预览会列出 affected_for_review；撤销管理动作不自动确认这些解释正确。
历史编号仍可 read，并给出 effective_current_id；默认检索隐藏归档/被替代项，审计时 include_inactive=true。
没有自动永久删除。CLI forget 会删除依赖闭包，**不可用于事件清理或批量清洗**。

## 5. 复查与验收

doctor --scope project:ID --reviews 或 memory_search(needs_review=true) 提供分页队列。
来源改变但结论仍成立时，读当前依据后 recheck_sources，提交所有当前有效来源 ID 和说明，生成复查预览；用户确认后 apply 才清除标记，保存复查账，不制造同文版本。结论变化则 revise。
v0.2.1：预览即检查上游并列出阻塞 ID；可按上游→下游顺序放在同一计划复查。contradicts/context 可以关联待复查、历史或非活动条目，但不能代替有效依据，亦不会清除被引用条目的警告；详见 [恢复说明](REVIEW_RECOVERY.md)。
复查保留后续来源变化的失效传播，不能批量清空标志。

回归使用普通结束语、重复决定、旧约定替代、token 症状、范围不同和相反决定等样本。
代码测试/模拟宿主能验证机制，不能代替真实 GLM 在决策点正确理解并落实约定的实战验收。
