# 请宿主 Agent 执行：memory-mcp adapter 包升级

这是 `0.2.2-adapters.2` 试用升级包，未发布 GitHub Release。包含 ZCode、Codex、Qoder CLI/IDE adapter，以及之前的复查与检索修复。根目录 `MANIFEST.json` 记录源码提交与系统包，`SHA256SUMS` 记录压缩包校验值。先运行程序的 `adapters`，再按实际宿主选协议；不能按模型名称猜测。

用户把本包交给你并要求升级后，请自主完成以下工作；不要只把一串命令交还用户。保留宿主原生权限与信任要求，不修改项目规则，也不要未经核对新建空库。

## 选择程序

1. 先识别**当前服务实际运行的机器、系统与架构**，而非按模型名字推断。Windows 宿主程序与 WSL/Linux 程序分别核对。包内含 Windows amd64、Linux amd64/arm64、macOS amd64/arm64，位于 `packages/`；不需要本机安装 Go、Python、Node 或编译源码。
2. 读取 MANIFEST.json，校验所选压缩包的 SHA-256，解压到稳定用户目录。不要用临时目录作为长期 executable 路径。
3. 用新程序 `version`、`build-info`、`help`、`guide` 核对版本和源码提交。此时不要打开原数据库。

遇到缺 ID、complete 后仍拦写或双注入时，完成下述停用和备份后，先按根目录 DIAGNOSE_FIRST.md 用只读命令保存原始诊断，再升级。不要先清洗、补造来源或猜测修复 links。

## 复用原安装

4. 从**实际生效**的用户级 MCP 配置定位已有服务名、executable、args、data-dir 和 hook 命令。保留原数据目录、现有服务名与所有项目绑定；多个候选存在歧义时只询问缺失信息，不猜默认目录。
5. 识别使用该库的旧进程。按宿主支持方式停用旧实例，备份完整原数据目录（包括仍存在的 WAL）与配置；不能只拷贝正在使用的主 db 文件。先备份，再运行任何 status/doctor/setup/serve。数据库仍为 v3，会补齐兼容的操作顺序元数据，不主动修改旧条目的正文或 needs_review。不要混用旧进程记下的确认与新流程。
6. 仅更新本 MCP 的 executable 及必要的配套命令路径，原 args/data-dir、其他 MCP、用户 hook、信任配置保持原意。不要留下旧、新两套采集 hook 并行工作。
7. **ZCode**：使用包内 `docs/AGENT_INSTALL.md` 和 `docs/adapters/ZCODE.md`，对当前实际业务项目重新 setup（显式 `--host zcode`），复用原 data-dir、服务名和稳定 scope，更新本程序管理的 hooks。不要把安装包目录当业务项目，也不要默认绑定全部历史项目。
8. **Codex**：按 `docs/adapters/CODEX.md` 用 `setup --host codex`，实际 config.toml 由 `--config` 指定，hooks.json 位于同目录。保留其他设置。之前若只有手工 SessionStart 软引导，先读取脚本和配置确认其来源，再移除对应旧项，避免双重提示；不要删除其他用户 hook。新 hook 必须通过原生 Hooks 管理入口信任，再重启/新建会话，本程序不会绕过信任。
9. **Qoder CLI / IDE**：按 `docs/adapters/QODER.md` 分别使用 `--host qoder` / `--host qoder-ide`，核对真实用户 settings.json。两者可能共用同一文件，不可重复安装两套采集。缺少实际客户端或版本不符时标为待验证，不把命令回放当原生验收。
10. **未列出的宿主**：继续复用已验证的 stdio 与原有触发方式，不冒用其他 host 值。仅升级程序不得宣称新增了自动 hook。

## 验证与交付

11. 按宿主要求重启服务或新建会话。实际调用 `memory_bootstrap`，核对版本、原 data_dir、features 中的 `review_recovery`、`bounded_literal_search`、`host_adapters` 、`operation_ordered_approvals`、`lookup_diagnostics`、`explicit_write_context` 和 `readonly_diagnose`。命令行自测只能证明程序可运行，不能代替宿主已经调用新程序。
12. 使用只读搜索和完整读取核对原项目资料、历史和绑定仍在。doctor 必须带真实 `--host`、实际 `--project/--scope` 验证路由，不能只看 setup 退出码。核对 `context_check_handlers`、服务与 hook 的 data-dir、原生信任和实际新事件。手工回放也会生成 adapter_receipts，需与真实宿主会话交叉核对；native_event_verified 不会因配置存在自动变成 true。
13. 实际问题按 `docs/REVIEW_RECOVERY.md` 恢复：更正结论直接 revise 并引用新依据；原结论不变则先 recheck 预览，展示后等用户对该方案的新确认，再 apply。升级授权不是批量清标记或清洗授权。不在正式库写虚构验证案例。
14. 简短报告：升级前后版本、源码提交、程序路径、原数据目录、备份位置、服务名和当前 scope；分别说明配置更新、宿主实际工具版本、项目绑定及尚未实测的部分。失败时保留原库和旧程序，按备份回退配置，不直接修改数据库解除拦截。

程序不访问网络，不附带数据库或用户记忆。包中没有“自动合并、自动清洗、全部恢复有效”的操作。超过 8 个检索词可直接 prepare；如果响应 keyword_search.status 为 directory_only，表示只返回目录，不能解释为无相关记忆。
