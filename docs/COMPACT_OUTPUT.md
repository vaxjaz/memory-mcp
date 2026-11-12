# 0.2.3-compact.1：先减少无效回包

本版为离线试用包，不发布 Release，保留 0.2.2-adapters.2 的全部修复。原库不做清洗或正文改写，schema 仍为 3，工具仍为 8 个。

## 变化

- memory_context 默认 response_mode=compact。prepare 单份 JSON 预算为 8192 字节，关键词命中在目录前，每路最多 8 条；必要时缩小页，并从最后实际返回的条目继续分页，不能跳过因预算未显示的候选。
- 候选不返回完整 profile、管理摘要和管理理由，只给短导航摘录、适用条件/边界与状态。同一返回内，关键词命中不再在目录重复出现。预览截短带省略号，只作定位，不是完整依据。
- 两种分页分开：next_cursor 用于 memory_context.prepare 的目录；match_next_cursor 交给 memory_search，保持原 scope/query。翻目录页时不重复跑原来的关键词查询，响应明确标记 keyword_search.status=not_repeated；这不代表无命中。
- complete/skip 默认只回状态、选中 ID 与下一步。task/questions/decision 仍完整存入库，必要时 memory_context(action=status,response_mode=full) 查看。skip 仍必须传 task 和 decision；keywords 仍必须是字符串数组。
- memory_record(response_mode=compact)、memory_revise(draft.response_mode=compact) 只回 ID、状态、正文 bytes/SHA256、链接数量和实际检查的本轮来源。完整正文仍可 memory_read，未传模式的旧写入调用保持完整返回。request_id 的写入身份不包含 response_mode，改回包模式重试不会增加版本。
- memory_review_done(response_mode=compact) 返回状态、保存/复用的 ID、说明的 bytes/SHA256，不重复回送用户问题和整段说明。note 原文与回执仍完整保存。
- hook 说明区分三条路径：已保存并覆盖本轮直接回执；无新增使用 reused_case_ids；有实质新增首次修订就保存完整必要来源。不能为挂本轮关联常态化补链造版本。保存/复用结束后可提前 review_done，Stop 不再请求额外整理。

## 使用示例

```text
memory_context(action=prepare, scope=实际项目, source_id=本轮真实来源,
  task=结合项目和会话整理的任务, keywords=["真实线索"], response_mode=compact)
memory_read(id=选中完整ID, context_source_id=本轮真实来源)
memory_context(action=complete, scope=实际项目, source_id=本轮真实来源,
  selected_ids=[已读且适用的完整ID], decision=本轮实际约束, response_mode=compact)
memory_revise(previous_id=当前版本完整ID, draft={完整正文和真实依据,
  context_source_id:本轮真实来源, response_mode:compact})
```

检查来源 context_source_id 不会自动变成证据 links。已绑定 hook 提供本轮来源时，案例写入必须显式传它，修订放在 draft 内。不得编造来源；历史 links 仍保持真实含义。完整输出可显式请求 response_mode=full，不能把轻量回执当作空正文。

## 本版边界

同会话的跨轮全文读取仍按原检查执行；没有新增查询缓存或跨轮已读继承。SDK 的结构化数据与文本兼容副本仍保留，因此 8KB 是单份 JSON 上限，不是宿主模型实际 token 上限。是否重复注入须由宿主实测；本版不宣称直接省一半 token。

全文、来源链接、历史版本、清洗确认和操作前检查不因回包变小而放宽。旧客户端若依赖 context 的完整 profile/decision，显式传 response_mode=full。升级后必须重启加载新 schema，运行实际 memory_bootstrap 核对 compact_context/compact_receipts，并让宿主按新说明使用短回执。不要在正式库造验证案例。
