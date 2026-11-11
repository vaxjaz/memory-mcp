# 请宿主 Agent 执行：memory-mcp 修复包升级

这是 `0.2.1-reviewfix.1` 试用升级包，未发布 GitHub Release，不含暂停中的 Codex/Qoder adapter。只处理复查恢复和检索词数量限制。根目录 `MANIFEST.json` 记录源码提交与系统包，`SHA256SUMS` 记录压缩包校验值。

用户把本包交给你并要求升级后，请自主完成以下工作；不要只把一串命令交还用户。保留宿主原生权限与信任要求，不修改项目规则，也不要未经核对新建空库。

## 选择程序

1. 先识别**当前服务实际运行的机器、系统与架构**，而非按模型名字推断。Windows 宿主程序与 WSL/Linux 程序分别核对。包内含 Windows amd64、Linux amd64/arm64、macOS amd64/arm64，位于 `packages/`；不需要本机安装 Go、Python、Node 或编译源码。
2. 读取 MANIFEST.json，校验所选压缩包的 SHA-256，解压到稳定用户目录。不要用临时目录作为长期 executable 路径。
3. 用新程序 `version`、`build-info`、`help`、`guide` 核对版本和源码提交。此时不要打开原数据库。

## 复用原安装

4. 从**实际生效**的用户级 MCP 配置定位已有服务名、executable、args、data-dir 和 hook 命令。保留原数据目录、现有服务名与所有项目绑定；多个候选存在歧义时只询问缺失信息，不猜默认目录。
5. 识别使用该库的旧进程。按宿主支持方式停用旧实例，备份完整原数据目录（包括仍存在的 WAL）与配置；不能只拷贝正在使用的主 db 文件。先备份，再运行任何 status/doctor/setup/serve。数据库仍为 v3，不主动修改旧条目的正文或 needs_review。
6. 仅更新本 MCP 的 executable 及必要的配套命令路径，原 args/data-dir、其他 MCP、用户 hook、信任配置保持原意。不要留下旧、新两套采集 hook 并行工作。
7. **ZCode**：使用包内 `docs/AGENT_INSTALL.md` 和 `docs/adapters/ZCODE.md`，对当前实际业务项目重新 setup（显式 `--host zcode`），复用原 data-dir、服务名和稳定 scope，更新本程序管理的 hooks。不要把安装包目录当业务项目，也不要默认绑定全部历史项目。
8. **其他宿主**：复用原来已经验证的 stdio 注册和宿主适配方式，只升级程序及其已核对路径。本包没有新 Codex/Qoder hook adapter，不得冒用 `--host zcode`，也不得把“程序已升级”报告为“自动 hook 新增且已生效”。手工旧引导若引用旧 executable，核对后同步更新相关路径。

## 验证与交付

9. 按宿主要求重启服务或新建会话。实际调用 `memory_bootstrap`，核对版本、原 data_dir、features 中的 `review_recovery` 和 `bounded_literal_search`。命令行自测只能证明程序可运行，不能代替宿主已经调用新程序。
10. 使用只读搜索和完整读取核对原项目资料、历史和绑定仍在。ZCode 再用 doctor 加实际 `--project/--scope` 验证路由，不能只看 setup 退出码。当前会话无法重新加载时，说明具体需要的新会话/重启步骤，勿声称已生效。
11. 实际问题按 `docs/REVIEW_RECOVERY.md` 恢复：更正结论直接 revise 并引用新依据；原结论不变则先 recheck 预览，展示后等用户对该方案的新确认，再 apply。升级授权不是批量清标记或清洗授权。不在正式库写虚构验证案例。
12. 简短报告：升级前后版本、源码提交、程序路径、原数据目录、备份位置、服务名和当前 scope；分别说明配置更新、宿主实际工具版本、项目绑定及尚未实测的部分。失败时保留原库和旧程序，按备份回退配置，不直接修改数据库解除拦截。

程序不访问网络，不附带数据库或用户记忆。包中没有“自动合并、自动清洗、全部恢复有效”的操作。超过 8 个检索词可直接 prepare；如果响应 keyword_search.status 为 directory_only，表示只返回目录，不能解释为无相关记忆。
