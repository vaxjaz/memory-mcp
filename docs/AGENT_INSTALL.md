# 给安装 Agent：0.3 安装、升级与验收

用户已授权的安装/升级按本说明执行。默认辅助模式，持久作用于该服务的全部已绑定项目。不逐轮询问是否切换；只显示当前模式。不得修改项目 AGENTS.md、CLAUDE.md 或把项目路由搬进插件。

## 先核实与备份

1. 识别真实客户端、版本、程序执行系统；运行 adapters，参考 adapters/README.md。模型名不是宿主名。Windows 程序使用 Windows 绝对路径，WSL 程序使用 Linux 路径，不混用配置和数据目录。
2. 从实际用户级配置确认服务名、可执行文件及原 data_dir。多个库有歧义才请用户澄清，禁止因为默认目录不同创建空库。读取真实业务项目路径与既有稳定 scope，不能把安装包目录当业务项目。
3. 运行新程序 version、build-info、help、guide --mode assist，核对包内提交与版本；这些命令不开库。
4. 停止所有使用原库的旧进程，复制整个数据目录（含 WAL）到新的完整备份目录，然后才运行任何开库命令。status、doctor、setup、serve 均会开库。schema 仍是 3；从旧版首次打开可能补齐内部元数据，旧程序回退应恢复升级前备份。

## 一次设置，持续生效

```text
<新程序> setup --host <实际适配器> --mode assist --config <实际用户配置> --data-dir <原数据目录> --project <实际业务项目目录> --scope project:<既有稳定标识>
```

多服务时加 --server-name <已核实名称>。默认不加 capture-user-text、context-check、review-on-stop。程序更新后必须重新 setup：同时更新服务 --mode 与该服务拥有的 hooks；移除旧 PreToolUse/Stop 配置，保留其他插件。只替换 exe 不代表迁移完成。

检查旧安装遗留的用户级记忆专用引导：本插件明确拥有的说明片段应替换为本版 guide --mode assist，不在旧指引后追加一层“新版优先”。用户自写的其他规范、项目 AGENTS 和 CLAUDE 文件不改；若仍有要求每轮强制闭环的外部规则，报告具体来源及冲突，不能把配置迁移冒充流程已生效。普通文件记忆里的旧版本/操作描述属于历史资料，不应继续当安装指令。

模式存于用户级服务及其 managed hook 配置，适用于全部既有绑定；不是每条输入重新选择。一个项目 setup 不会猜测其他历史项目身份，新增需要绑定的实际项目分别 setup。已有数据/绑定/待判断事项保留，不清洗、不补链、不自动标完成。

默认安装 SessionStart + UserPromptSubmit 短提醒。提醒不写聊天、不查询案例、不生成回执、不阻断任务。显式需要原文采集时可在已授权范围内加 --capture-user-text；这会额外安装 Stop 采集实际答复，但仍不启动模型整理。严格兼容模式见 STRICT_MODE.md，不能因普通任务主动询问切换。

Codex 使用当前 CODEX_HOME，否则用户 .codex/config.toml；hooks.json 同目录。若实际配置不同需传 --config。按宿主原生管理入口完成 hook 信任并重启，程序不会替用户改信任/审批设置。Qoder CLI/IDE 共用配置时不能重复安装两套采集，先核对真正执行端。

## 配置和真实运行分开验收

- setup 应报告 mode=assist、raw_capture=false、context_check=false、binding_verified=true 和备份路径。
- 按输出 verification_argv 运行 doctor，不丢 config/server-name。核对 resolved_scope、memory_services[].matches_opened_data_dir、mode=assist、mode_configuration_consistent=true、context_check_handlers=0、assist_handlers=2。原生 launcher 缺失或被改动要先修复。
- 重启/新会话后实际调用 memory_bootstrap，核对 0.3.0-rc.2、mode=assist、原 data_dir、integration_protocol=3。若模式不一致，检查残留进程或不同配置。
- 在真实业务项目观察 SessionStart 与新输入短提醒；核对 doctor adapter_receipts 与当前真实会话。手工回放也能生成回执，不能当原生验收。无 input_saved 是默认不采集的预期，不能据此判失败。
- 普通任务不应因记忆出现 gate、prepare/complete/skip 义务、Stop 续轮或自动案例。历史 pending 仍可用 context pending 查询，不能悄悄清零。
- 按 HOST_TRIAL.md 记录实际效果。协议测试不证明模型理解或遵守 AGENTS；真实事件未验证要明确报告待验。

仅 MCP stdio 而无已验证原生 hook 时，核心工具仍可用，但每轮自动提醒不保证。不要伪称装了某客户端 adapter，就有另一客户端能力。不要为绕过安装限制写项目规则。

## 给用户的交付

简要报告版本/提交、程序/配置、原库、绑定与 scope、备份、mode 和需重启事项。分别说清配置、绑定、实际工具连接、真实事件和业务行为的验证状态，不笼统说全部生效。清洗需要另行展示具体预览并取得新确认；安装授权不等于清洗授权。
