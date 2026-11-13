# 0.3.1-experience.1 离线实战升级包

本包给另一台电脑的宿主 Agent 安装，包含五平台程序包、源码、校验和与 SOURCE_COMMIT。不发布正式 Release，不自动合并主分支，不自动改其他电脑的用户库。

先解开适用平台包并读 docs/AGENT_INSTALL.md。核对 version、build-info 与 SOURCE_COMMIT；停止旧进程、完整备份原 data_dir 和配置，再使用原服务名、原库、真实业务目录及稳定 scope：

    memory-mcp setup --host <实际宿主> --mode assist --config <实际配置> --data-dir <原库> --project <真实业务目录> --scope <原scope> --experience-trial

本轮试验是同步经验写入、真实代码工具线索匹配、选择性要求自主 query、当前锚点定位。无每轮材料注入、聊天原文采集、Stop 整理或异步子 agent；保留实际项目规范和路由。

旧自由文本不自动获得暂停资格。用少量真实、有证据的旧案例按 EXPERIENCE_TRIAL.md 转换，或者等待实际纠偏后同步写入；不要在正式库制造验收案例。无合格案例不会自动命中。

程序协议回归与真实宿主验收分别报告：doctor 与脚本通过不等于客户端真正执行 hooks，更不等于主模型已正确消费经验。真实试验按 HOST_TRIAL.md 观察漏查、误暂停、主线时延、上下文和返工。

退出当前项目时把 --experience-trial 换成 --disable-experience-trial，经验数据保留。完整旧版本回退使用升级前程序、配置及完整库备份。清洗只预览，经新的用户确认才应用。
