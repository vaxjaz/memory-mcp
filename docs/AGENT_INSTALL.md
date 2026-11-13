# 给安装 Agent：0.3.1-experience.1 安装与验收

用户已授权安装或升级时按本说明执行。默认辅助模式；紧凑经验试验按实际项目启用。不逐轮询问模式，不修改项目 AGENTS.md、CLAUDE.md，不把领域路由搬进插件。

## 先核实与备份

1. 识别真实宿主、版本、程序执行系统；运行 adapters，参考 adapters/README.md。模型名不是宿主名。Windows 程序用 Windows 绝对路径，WSL 程序用 Linux 路径。
2. 从实际用户配置确认服务名、程序路径、原 data_dir、业务项目目录和稳定 scope。有多个库且无法判定才询问；禁止因为默认目录不同新建空库，禁止把安装包目录当业务项目。
3. 运行新程序 version、build-info、help、guide --mode assist；核对版本与包内 SOURCE_COMMIT。这些命令不开库。
4. 停止所有使用原库的旧进程，复制完整数据目录（含 WAL）及宿主配置到独立备份，再运行任何开库命令。status、doctor、setup、serve 都会开库。schema_version 仍为 3，但首次打开会增加内部元数据；回退旧程序应同时恢复升级前备份。

## 对实际项目启用试验

    memory-mcp setup --host <实际宿主> --mode assist --config <实际配置> --data-dir <原库> --project <真实业务目录> --scope <原scope> --experience-trial

保留服务名与其他绑定；有非默认服务名时按 help 传入。setup 配置 SessionStart、UserPromptSubmit、PreToolUse，不配置本试验的 Stop，不采集聊天原文。不运行旧 capture、context-check、review 整理流程，不建立 prepare/complete 仪式。需要保留普通 assist 的项目不加试验参数；已启用的策略不会因普通重装被悄悄关闭。

这版写入同步进行。用户纠正或自主发现只是候选信号，主线先用代码、运行证据和真实业务约定核实；确有被推翻的易错推断及具体影响，才调用 memory_experience_record。记录特征、反例和边界，不保存聊天全文或让历史指定本次修改位置。完整接口及限制见 EXPERIENCE_TRIAL.md。

原自由文本可通过 search/read 继续查询，但不会自动参与暂停。若已知少量真实踩坑案例，可先 read 原文、在真实项目核实，再以 previous_id 转成紧凑经验；禁止批量猜测提炼或给正式库填测试案例。没有合格案例时没有自动命中，是冷启动而非故障。

## 分层验收

1. doctor 检查绑定、辅助模式、experience_trial=true、experience_hooks_installed；bootstrap 确认两个 experience 工具可用。
2. 在隔离库运行随包源码的 check_experience_trial.py；不得向用户库造案例。脚本重放只证明程序协议，不证明真实宿主已接受 hooks。
3. 真实宿主重启后确认原生事件回执和 hooks 信任状态。Codex 信任要求由用户操作，不能伪造。没有回执就报告尚未验证，不能说已生效。
4. 用真实代码任务观察：普通搜索静默；具体锚点与独立条件同时命中才暂停，宿主需用自己的当前任务/疑问调用 memory_experience_lookup，成功返回后重试原操作。bootstrap、口头声明和失败查询不能完成挑战。
5. 当前行号只是字面定位；核查调用者、条件及必要运行证据后，才判断历史是否适用。缺失锚点不自动修订、归档或宣称经验过期。

记录主线总时延、实际模型输入、误暂停与返工，参照 HOST_TRIAL.md；不把模拟通过当成 GLM 消费能力已获证明。清洗沿用项目 N 次实质写入的预览与新确认，不自动应用。

## 退出与回退

    memory-mcp setup --host <实际宿主> --mode assist --config <实际配置> --data-dir <原库> --project <真实业务目录> --scope <原scope> --disable-experience-trial

退出保留经验与其他绑定，最后一个试验项目退出时移除自身 PreToolUse，保留其他插件 handlers。整版回退时停止进程，恢复程序、配置和升级前完整库备份。不要为回退伪造完成历史 pending，也不要改用户规范。
