# 严格兼容模式：明确选用

strict 保留 0.2 的每轮原文采集、prepare/read/complete 检查与 PreToolUse 门禁，用于明确希望保留旧工作流的安装。它是整个服务的持久配置，所有绑定项目沿用；不按任务询问切换。

```text
<程序> setup --host <实际宿主> --mode strict --config <实际配置> --data-dir <原库> --project <真实项目> --scope project:<稳定标识> --capture-user-text
```

服务和 hook 必须同时配置 strict。首次 bootstrap integration_protocol=2、mode=strict；guide --mode strict 输出当前兼容说明。既有领域规范、AGENTS 和用户要求仍然优先；插件不判断宿主是否真正理解或执行。

原文检查 source_id 必须是真实当前来源。读取选定全文时带 context_source_id，complete 仅校验顺序及版本。案例写入显式传实际 context_source_id，避免从历史证据猜当前轮次。普通无关任务 skip；不能把回执当授权或预防成功。

默认 Stop 仍不追加模型整理。只有显式有价值 candidate.review_at_stop=true，或用户明确选择 legacy --review-on-stop 才可能续一轮。相同问题无新增用复用回执，不为追加 link 制造版本；recorded_source_id 是独立记账关联，不是证据支持。

迁回辅助模式重新 setup --mode assist，默认去掉采集、门禁及 Stop；原库、历史和待判断事项保持不变。迁移不替用户做清洗。备份与原生验收顺序遵循 AGENT_INSTALL.md。
