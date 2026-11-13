# ZCode 适配器

执行 [AGENT_INSTALL.md](../AGENT_INSTALL.md) 的备份与核实步骤。默认辅助模式：

```text
<程序> setup --host zcode --mode assist --config <实际用户config.json> --data-dir <原库> --project <真实业务目录> --scope project:<既有稳定标识>
```

配置为 mcp.servers / hooks.events。setup 保留其他服务/handlers/env/用户设置，重复安装不重复注册自身事件；会识别本项目此前提供的探针。若有效 MCP 来自 .agents/mcp.json 而没有原生配置，先保留有效服务，避免新增原生配置屏蔽 fallback。

服务和 managed hooks 同时更新为 assist。SessionStart 告知模式与绑定；UserPromptSubmit 短提醒；默认不采集正文、不要求检查闭环、不安装 PreToolUse/Stop。不会更改项目 AGENTS.md。模式对该服务所有既有绑定持续生效；新项目需要核实身份后明确绑定，最长目录边界匹配，不回退 global。

显式 --capture-user-text 才采集实际输入/可用答复，Stop 仅采集不整理。事件有稳定 ID 时幂等；没有稳定 ID 不按相同正文吞掉真实重复输入，也不能承诺识别乱序重投。原始事件默认隐藏，include_events 才检索。

钩子全局禁用且存在其他 hooks 时安装拒绝偷偷启用。按 ZCode 原有信任/启用流程处理后重启，核对实际 bootstrap 与 doctor 绑定和真实 SessionStart/UserPromptSubmit 回执；默认无 input_saved 是不采集的正常状态。配置写入、事件收到、正确使用记忆分别验收，见 [HOST_TRIAL.md](../HOST_TRIAL.md)。

[严格兼容模式](../STRICT_MODE.md) 需要显式 --mode strict --capture-user-text，旧 review_on_stop/回读答复规则仅在明确启用旧整理路径时适用，不是辅助模式步骤。历史原文保存在 docs/history/0.2/adapters。

停用只移除本服务 managed-id 对应处理器，保留其他设置/数据。回退前核对升级后新增资料，不能用旧备份覆盖后续业务记录。forget 不用于维护清洗。
