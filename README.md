# memory-mcp

本地决策记忆 MCP，由宿主 Agent 判断和使用。保存可复用的历史依据，支持检索、完整查读、关联来源、不可变修订和用户确认后的可逆清洗。没有内置模型、后台学习或网络请求。

当前试用版：**0.3.0-rc.1**。源码与离线包供实战验证，未发布正式 Release。

## 默认：辅助模式

模式是服务的持久配置，所有已绑定项目统一沿用，项目数据仍按 scope 隔离。显示当前模式，不逐轮询问是否切换。不修改项目 AGENTS.md、CLAUDE.md 或领域规范，也不负责替项目定义领域路由。

- SessionStart：告知模式、服务和项目绑定。
- UserPromptSubmit：短提醒，宿主结合当前项目、前文及已读记忆判断是否需要历史依据。默认不保存原文、不检索案例、不调用额外模型。
- 需要依据：可直接 search/read，或 context lookup 整理后的任务；不要求登记 source_id、prepare/complete、skip 或回执。
- 同一任务沿用已读材料，条件或版本可能变化时再核实。有实质新增才 record/revise，不把进度、普通问答或重复执行记成案例。
- 默认不安装 PreToolUse 门禁或 Stop hook，不在任务结束后启动整理回合。

当前用户要求与适用项目规范决定如何行动。记忆是历史资料，active 只是存储状态，不代表规范权威。辅助动作同样走项目既有规范路由。软提醒可稳定交付到已验证的宿主事件，但不能证明模型正确理解、检索或遵守规范。

## 安装或升级

给宿主 Agent 阅读 [AGENT_INSTALL.md](docs/AGENT_INSTALL.md)。必须识别实际宿主/执行系统、复用原 data_dir、先停进程完整备份，再打开数据库。

```text
<绝对路径程序> setup --host <实际宿主> --mode assist --config <实际用户配置> --data-dir <原数据目录> --project <实际业务项目> --scope project:<稳定标识>
```

升级需重新 setup，使服务和 owned hooks 同时切到辅助模式。只替换程序不算完成。所有既有绑定继续保留；一个 setup 不会猜测绑定其他历史项目。未知目录不回退 global。其他插件配置保持原样。支持协议与原生验证限制见 [adapters](docs/adapters/README.md)。

需要保留旧强预检时，明确选用 [strict 兼容模式](docs/STRICT_MODE.md)。模式不会自动升级为 strict；日常任务不询问切换。

## 工具

| 工具 | 用途 |
|---|---|
| memory_bootstrap | 查看真实版本、mode、data_dir、特性和当前单一说明 |
| memory_search | 指定 scope 的字面 OR 检索；结果用于定位，非语义排名 |
| memory_read | 完整正文、来源与当前版本、反向引用分页 |
| memory_context | lookup：结合任务与上下文选关键词，返回有界候选，不写检查状态；pending/candidate：显式待判断事项；旧 checkpoint 动作留作兼容 |
| memory_record | 必要原文或有价值案例；默认简短回执 |
| memory_revise | 完整替换正文与来源的新修订，保留历史 |
| memory_review_done | 旧 hook 整理回执，辅助模式无需调用 |
| memory_cleanup | 独立维护账本，预览后新确认才执行，保留备份和撤销 |

lookup 例子：宿主从当前项目及对话识别任务为“按已确定范围提交两个仓库”，以 scope/task/questions/keywords 查询，无需先制造一条用户 record。关键词来自上下文及已有材料；字面匹配不等于适用性。候选 JSON 默认最多 8192 字节，命中优先，两路分页；只有实际需要的材料才读全文。

## 数据与边界

案例/假设仍需 future_use、counter_check、applicability、boundary 和实际可用来源。重复问题无新增直接复用；相同问题有实质变化才修订。坏 ID、缺失来源、跨项目来源和文本损坏继续拒绝并给定位信息。模式迁移不改正文、证据链接、版本历史、待判断事项或清洗状态。

[质量与清洗](docs/QUALITY_AND_CLEANUP.md) · [存储可靠性](docs/STORAGE_RELIABILITY.md) · [文本完整性](docs/TEXT_INTEGRITY.md) · [故障诊断](docs/DIAGNOSE_FIRST.md) · [宿主试用验收](docs/HOST_TRIAL.md)

CLI：version、build-info、help、guide --mode assist 不开库；status、doctor、setup、serve 会开库。backup 生成一致 SQLite 快照；export 是明文，forget 是有明确确认的永久删除命令，不用于日常清洗。数据在本机未加密，工具返回仍可能进入宿主模型上下文。

## 开发验证

```text
go test -count=1 ./...
go vet ./...
python -m unittest discover -s scripts -p test_*.py
python scripts/check_assist_mode.py <程序>
```

CI 同时执行明确 strict 的旧流程回归与默认 assist 的迁移/接口回归。模拟事件仅验证程序协议，不冒充真实宿主执行、AGENTS 遵循或错误预防成功。0.2 历史流程保存在 docs/history/0.2，不作为当前安装指令。
