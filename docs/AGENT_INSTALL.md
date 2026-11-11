# 给安装 Agent：安装、升级和项目绑定验收

本说明用于用户已授权的 memory-mcp 安装/升级。不得修改项目 AGENTS.md、CLAUDE.md 等规则文件。

## 最常见的失败：服务装好了，项目没有绑定

注册 stdio MCP、写入 hooks、项目目录绑定、宿主收到真实事件，是四个独立状态。
**不能仅凭 setup 退出码为零，就报告当前项目已经在使用记忆。**
一个项目的 setup 不会默认绑定全部历史项目。数据库里有该项目案例，也不代表活动工作目录已绑定。
安装包解压目录不是业务项目目录；当前终端目录也不一定是用户实际工作的目录。

## 安装顺序

1. 识别实际宿主、操作系统与程序执行位置。内置适配器目前只有 ZCode。
   Windows 程序使用 Windows 绝对路径，WSL 程序使用 Linux 路径。不要混用两套配置与数据目录。
2. 从现有用户级 MCP 配置读取该服务的 executable、args 和 data-dir；再用 doctor 核对。
   复用原库，不因默认路径不同而新建空库。多个安装/数据目录有歧义时，先说明候选并确认。
3. 运行新包 `version`、`build-info`、`help`、`guide`。这些命令不打开数据库。
4. 升级前关闭所有使用原库的旧 MCP 实例，复制整个数据目录（含 WAL）到新备份目录。
   v0.2.0 首次打开会将数据库升级为 v3；v0.1.x 不能打开 v3，回退必须恢复升级前完整备份。
   **status、doctor、setup、serve 都会打开并可能升级数据库，必须放在备份之后。**
5. 确定当前业务项目的实际路径与稳定 scope。先复用既有明确绑定，不按目录重名猜项目身份。
6. 对当前项目执行以下命令，所有占位符必须替换为已核实的绝对路径：

```text
<新程序> setup --host zcode --config <实际用户配置> --data-dir <原数据目录> --project <实际业务项目目录> --scope project:<稳定标识> --capture-user-text --review-on-stop
```

setup 会复用已有服务名；多服务时显式传 `--server-name <已核实服务名>`。
v0.2.0 setup 同时安装 `--context-check`，升级旧包后也必须重新 setup 更新 managed hooks。
它只调整本服务的配置，保留其他服务和用户 hooks。每个需启用的历史项目分别 setup；不得未经确认批量推定绑定。

7. 检查 setup 输出：`binding_verified=true`、正确的 project/scope/data_dir，以及 config/database backup。
   `restart_required=true` 表示需要重新启动宿主或新会话，`host_event_verified=false` 仍不等于失败，但不能声称真实事件已验证。
8. 运行：

```text
<新程序> doctor --host zcode --config <实际用户配置> --data-dir <原数据目录> --project <实际业务项目目录> --scope project:<稳定标识> --reviews
```

核对 `binding_verified`、`resolved_scope`、`memory_services[].matches_opened_data_dir` 和 `context_check_handlers`。
只有 bindings 中存在记录还不够：传入当前实际工作目录，验证最长路径匹配后的 scope。

## 重启后的真实验收

- 在真实项目新会话确认 MCP 工具已加载，调用 `memory_bootstrap` 核对版本、实际 data_dir 和 features。
- 查看 doctor 的真实 `bound` / `input_saved` 事件，确认 scope 和当前会话正确。
- 相关任务应出现 `memory_context`，读取全文时带本轮 `context_source_id`，然后 complete。
- 普通结束语可 skip，Stop 不应额外启动整理，也不应增加案例。
- 有通过正反筛选的候选才设置 `save_candidate=true`；Stop 最多继续整理一次。
- 清洗只生成预览，未经用户对该预览的新确认，不得 apply。

没有真实事件时，先检查是否重启、项目是否绑定、实际 cwd 是否属于绑定路径、宿主是否加载了这份用户配置、服务与 hook 是否使用同一 data_dir。不得用手工伪造 hook 的成功代替真实验收。
未绑定项目会给宿主上下文提示并写入诊断，不回退 global、不自动写到其他项目。

## 最终向用户报告

简短列出版本/源码提交、程序与配置路径、实际数据目录、项目与 scope、备份路径，分别报告：
配置完成、目录绑定验证、真实工具连接、真实事件、事前使用与回合末筛选。
尚需重启或实测的项目要明确标为待验证，不要笼统说“安装成功，全部生效”。
