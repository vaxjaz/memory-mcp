# 先取证，再升级或修复数据

本包为 `0.2.3-compact.1`，不发布 GitHub Release。`diagnose` 是只读命令：必须指定原 data-dir，不建库、不迁移、不补状态、不清洗、不猜测或替换 ID。输出只包含元数据、原样 links、正文 SHA-256 和来源存在性，不包含正文；source_ref 可能包含内部地址，请按需脱敏后分享报告文件。

先从实际生效的 MCP 配置和 hook 命令核对 data-dir，按宿主方式停用旧实例并备份完整数据目录与配置，然后才运行诊断。若有双注入，同时保存两份原始工具返回、hook 命令与各自路径；不能仅凭模型转述的 ID 认定丢行或库损坏。不要让模型手工重抄长 ID 或重写诊断 JSON。

## 这次 GLM 库的取证命令

从 Windows 包解出新程序后，可直接运行以下命令；无需先 setup/serve。路径与远程机器实际配置核对后再用。输出文件必须尚不存在。

```powershell
& '<新程序完整路径>\memory-mcp.exe' diagnose --data-dir 'C:\Users\jie.li02\AppData\Roaming\memory-mcp' --scope 'project:after-sale-backend' --entry-id 'ad465eaff1d805e8cc886bdda75a3c3e' --probe-ids 'f58a7e035c0fe9e3cd38be78e98c8ea3,20524b7d19ebe22ced6b454779143750,95a1431360a80a44ed7d99207d73e420,a17d2a027513a5a51799e1812e65ee74,2f602089003451bc388591eb7a5f5475' --out "$env:TEMP\memory-diagnose-original.json"
```

再按需将 entry-id 换成成功修订 `89fe79e49686ed8d56c75270e54b3c46`，输出另一个新文件。保留清洗前备份与原始调用 JSON，对比 forward_links 中的原样字符串。

- `target.exists=false` 才表明这份库中的该目标确实不存在；提交 draft 里有错 ID 本身不证明库里也是错的。
- forward links 与 referenced_by 来自同一张 links 表，不是独立正反索引。正常写入本就逐一校验 target，不能从一段转述推断过去未校验。
- 缺 source 时比较 data_dir、host/session、hook_turn_exists、capture_logged_in_this_library。没有这条记录仍不能单独证明 hook 写入丢失；不同库、旧实例、恢复备份、转述误差都需原始证据排除。
- 绝不按“相近 ID”自动修复。诊断不会更改历史案例，不会补写业务结论。

## 本版确定修复的行为

1. 缺 entry、source、link target 或 prepare 状态时，返回具体操作、字段/ID、data_dir 与恢复提示，替代裸露的 `sql: no rows in result set`。不存在的检查返回 not_prepared，不伪造 complete。
2. EPM 闭环：本轮 complete 后引用旧用户来源，旧来源未完成不再误拦本轮。新调用请在 memory_record 传 context_source_id，在 memory_revise 的 draft 内传 context_source_id。links 继续保留真实历史依据。旧调用按相同 host/project/session 找最新输入；跨会话多来源时请显式指定本轮来源消除歧义。旧 complete 不能放行尚未检查的新输入。
3. 写入成功响应的 management.state 与读回一致；旧版空串是响应缺字段，不等于库中 active 状态丢失。
4. hook 提示附 host/data_dir/session 路由信息，便于查双注入；此处没有宣称已修复未复现的重复 hook 或丢行问题。

取证完成后按 UPGRADE_AGENT.md 升级。先停旧进程并备份完整库与配置，再运行任何会打开数据库的普通命令。升级不是清洗授权。离线回归均使用隔离假数据，不在正式库创建验证案例。
