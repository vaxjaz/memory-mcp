# Hook 行为与持久模式

当前唯一安装入口是 [AGENT_INSTALL.md](AGENT_INSTALL.md)。辅助模式默认安装 SessionStart 与 UserPromptSubmit，服务和 owned hooks 都带 --mode assist。所有已绑定项目沿用，scope 隔离不变，不逐轮询问模式切换。

SessionStart 告知当前模式/服务/绑定。新输入时短提醒宿主按当前用户要求和适用 AGENTS 执行，结合项目、前文和已读记忆判断是否需要历史依据。有需要才 lookup/search/read，有实质新增才写。辅助动作的规范路由由项目既有约定决定。

默认 hook 不保存输入正文，不检索材料，不创建 checkpoint/candidate/case，不装 PreToolUse/Stop，不发起额外模型调用。提醒本身不需要任何 MCP 工具响应。可以复用仍适用的已读材料，无需每轮重读或回执。软提醒不能保证判断正确，不能被当作行为证明。

若用户明确要求采集，可加 --capture-user-text：实际输入与可用答复作为原始事件保存；Stop 仅采集，无模型整理。原始事件不自动变案例。没有真实原生来源 ID 就不编造 source_id。失败开路，诊断到 stderr，不阻断业务。

strict 是明确选用的旧检查协议，见 [STRICT_MODE.md](STRICT_MODE.md)。切换需完整 setup 更新 service 与 hooks，不能只改其中一处。doctor mode_mismatches 列出已识别的不一致；配置符合不证明真实事件到达。

旧未带 mode 的 hook 命令若含旧 capture/context-check/review 标志，按 strict 兼容解释；新 serve 默认 assist。因此旧安装只换二进制会产生不一致，升级必须重新 setup。项目规则与其他插件配置不改。

安装支持及验证限制见 [适配器](adapters/README.md)。真实客户端验收见 [HOST_TRIAL.md](HOST_TRIAL.md)。
