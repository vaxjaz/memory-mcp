# 0.2.4-demand.1：按需收尾

离线试用包，不发 Release。包含 0.2.3-compact.1 及此前修复；schema 仍为 3，新增独立记账/候选元数据，工具仍为 8 个。升级前停旧进程、备份完整库和配置，不改写旧正文或清洗历史。

## 默认行为

重新 setup 时使用原业务目录、原 scope、原 data-dir，**不带 --review-on-stop**。核对返回 review_on_stop=false，以及实际托管 hook/launcher 中不再含旧参数。只替换 exe 而保留旧参数，仍可能允许旧式候选续轮。

- 普通任务：没有声明候选，Stop 保存实际答复并结束；不要求每轮额外报告“无价值”。
- 保存完成：案例写入和本轮记账关联同一事务提交，候选变为 saved。Stop 可直接完成记账，不新增版本、不重新检索、不拉模型回读并重放答复。记账完成不证明语义理解或全部知识覆盖。
- 明确候选尚未保存：保持 pending，Stop 直接放行，不能假记为已保存/已复核。保存失败或进程中断后重开仍可查询。
- 宿主明确认为需要一次收尾整理：在声明候选时传 review_at_stop=true；仅该候选未保存且本轮检查完成时，最多触发一次模型续轮。它仍会消耗宿主模型时间。保存/撤销后不再触发。

旧 --review-on-stop 仅作兼容选项；开启它会允许已声明的未保存候选续轮。不要为解决耗时把超时调大。原有操作前检查、质量准入、真实来源和清洗确认不放宽。

## 宿主如何使用

宿主自行决定跳过、复用、新建、修订，不由 MCP 自动判断价值。已有 prepare/complete 流程可继续使用 save_candidate；**有具体新增发现时才声明候选**，不把普通进度或提交成功算作长期知识。

任务中途新发现候选，不需要重新 prepare/read/complete，可单独声明：

```text
memory_context(action=candidate, scope=实际项目, source_id=本轮真实来源,
  save_candidate=true, decision=具体新增/变化及未来会改变的行动)
```

这个动作不完成操作前检查、不伪造全文已读记录。默认只保留待处理；确需一次 Stop 整理，再加 review_at_stop=true。

保存案例显式传 context_source_id；修订放 draft.context_source_id。首次写入就记录 recorded_source_id 的本轮记账关联，**不自动添加 supports/context 证据 links，不改业务证据图，也不能代替真实依据**。写回执、memory_read 返回 recorded_source_id。memory_review_done 的 case_ids 接受这种关联，不必为通过回执再 revise 补链。只使用旧案例仍走 reused_case_ids，既不改 links 也不造版本。

写入成功后，当前声明的候选记为 saved；它只表示这一候选已写入，不表示整轮知识已被全面审查。若又发现其他未保存事项，宿主可重新声明下一候选。保存/复用完成也可主动 memory_review_done；Stop 不会重复要求一次整理。

## 查询和处理待办

```text
memory_context(action=pending, scope=实际项目)                       # 仅列明确未处理候选，按 next_cursor 翻页
memory_context(action=status, scope=实际项目, source_id=旧来源ID)     # 历史状态只读可查，full 可查看完整原因
memory_context(action=candidate, scope=实际项目, source_id=本轮真实来源,
  candidate_source_id=待处理的旧来源ID, save_candidate=false,
  decision=无需再保存或已在其他事项处理的具体原因)
```

不能为处理旧候选伪造来源。更新仍验证实际当前来源和相同 scope；旧来源仅作为被处理对象。跨轮不能用旧来源绕过当前动作检查。pending 不是业务案例，不进入普通检索，也不自动重跑；无需每轮查询或提醒用户。doctor 的 pending_reviews 现在只计明确 pending 候选，而非所有未记账的原始输入。

已关闭回执若被重复提交其他内容，工具仍保留原回执并拒绝覆盖，但会明确要求停止记忆记账，不能再诱导“为改回执去 revise”。业务上真正的纠正仍按正常新依据修订。

升级时仅恢复旧库中明确 save_candidate=true 且未结束的候选；普通历史原文不会批量变成待办。候选保留原来源与原因，不据此新建案例或补业务 links。
