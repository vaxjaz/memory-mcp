# memory-mcp · 本地决策记忆

一个通用的 stdio MCP。保存真实协作材料、决策案例和可修正的偏好假设。
不内置模型、不提供云端服务、不进行网络请求，由宿主 AI 完成理解和学习。

**状态：0.1.0 原型。存储与协议可以测试；偏好学习是否有效仍需实际对照实验。**

## 安装与运行

从 [GitHub Releases](https://github.com/vaxjaz/memory-mcp/releases) 下载对应平台的压缩包。
私有仓库需要先登录有访问权限的 GitHub 账户。`Source code` 是源码；
安装请选 `memory-mcp-版本-系统-架构` 的程序包，不需要自行编译。
校验文件为同一发布下的 `SHA256SUMS`。

解压对应系统与架构的分发包，无需 Go、Python、Node.js 或模型 API Key。

```sh
./memory-mcp version
./memory-mcp config
```

Windows 使用 `memory-mcp.exe`。把 `config` 输出添加到支持本地 stdio MCP 的宿主，
保留宿主已有配置。或者手工配置：

```json
{
  "mcpServers": {
    "decision-memory": {
      "command": "/absolute/path/to/memory-mcp",
      "args": ["serve"]
    }
  }
}
```

上面是通用示例，不是所有客户端的原生配置格式。连接后对 AI 说：

> 请调用 memory_bootstrap，按你的能力接入这个本地记忆系统。
> 区分我的原话与你的推断，保留来源、语境和反例。

完整说明见 [通用接入指南](docs/HOST_SETUP.md)。
无需自己保持终端运行 `serve`，宿主会启动程序；stdout 仅用于 MCP 协议。

## 五个工具

| 工具 | 用途 |
| --- | --- |
| memory_bootstrap | 自描述接入协议、存储位置、能力与边界 |
| memory_record | 保存材料或案例，不强迫产生偏好结论 |
| memory_search | 显式范围内的关键词检索，返回有限摘录及复查标记 |
| memory_read | 完整正文、来源、当前版本及最多 100 条入站引用 |
| memory_revise | 创建有来源的模型假设，或追加原记录的修订 |

## 信息如何保存

所有正文是自由文本；结构字段负责来源、范围、版本和依赖关系，不能证明语义正确。

- `record`：实际提供的材料，不允许标成模型推断。
- `case`：围绕具体决策的案例，可以没有结论；解释必须标明为推断。
- `hypothesis`：始终是模型推断，必须链接已有材料或案例，可保留冲突。

来源 `host_quote` 是宿主转交的引文，MCP 无法验证是否忠实于聊天原文。
`user_supplied` 表示直接提供用于保存的材料，`imported` 表示实际导入来源，
`model_inference` 表示模型解释。这些标签由调用方提供，不构成独立认证。

默认使用 `project:<稳定标识>`。跨项目通用项显式使用 `global`，检索也需显式启用。
项目可以引用同项目或 global 来源；global 不得依赖项目资料。
这是一致性边界，不是操作系统权限隔离：连接此 MCP 的客户端可以请求其他 scope。

每次修订新增不可变正文版本，旧版本可读。依赖旧材料的解释会被标记为
`needs_review`，包括间接依赖。宿主需要重新阅读依据再修订，不能自动“确认正确”。
并发修订只接受当前版本，拒绝静默覆盖。新推断不能只引用其他假设形成确认套娃。
这些检查不验证内容真假，也无法阻止模型将自己的猜测冒充引文。

检索使用字面关键词 OR 匹配，按时间倒序，支持中文子串；无 embedding、语义排序
或全量矛盾发现。摘要只用于导航，关键判断应读完整案例和相关来源。
写入没有自动重试去重：失败后不确定是否提交时，先检索核对，避免重复案例。

## 本地数据与维护

默认目录：

- Linux：`$XDG_DATA_HOME/memory-mcp` 或 `~/.local/share/memory-mcp`。
- macOS：`~/Library/Application Support/memory-mcp`。
- Windows：用户 `AppData/Roaming/memory-mcp`。

可通过 `MEMORY_MCP_DATA_DIR` 或每条命令的 `--data-dir` 指定位置。
目录中保存 `memory.db` 及 SQLite WAL 文件。本版只保存文本和材料引用，
**不自动复制截图或附件**；引用文件移动后可能失效，重要文本可直接保存在正文。

```sh
./memory-mcp status
./memory-mcp export --out memory-export.json
./memory-mcp forget --id ENTRY_ID --yes
```

导出包含原始正文和全部历史，不覆盖已有文件。当前导出用于检查与迁移准备，
尚无 JSON 恢复命令。完整数据库备份：关闭所有宿主中的本 MCP，复制整个数据目录。
恢复时同样先关闭全部实例，再恢复目录，不直接复制正在写入的 DB 文件。

`forget` 删除指定项的整个版本历史及依赖它的解释，不只删除单个版本。
它不会删除其他导出、宿主聊天、备份、文件系统快照中的副本。
删除通过 CLI 显式执行，不提供模型可调用的删除工具。

数据未加密。Unix 下新建专用目录为 0700、数据库和导出为 0600；Windows 使用
账户目录 ACL。请使用专用数据目录；已有目录或文件的权限不会被自动收紧。
程序不上传信息，但返回给宿主的内容可能进入宿主云端模型请求。
离线分发和本地存储不等于宿主模型离线运行。

## 宿主能力限制

MCP 不是聊天监听器。它只收到宿主传来的内容，不自动获取整段会话。
完整采集需要可选的日志导入或宿主 hook，本版尚未提供。
宿主关闭后没有后台学习；后续是否持续使用工具取决于宿主规则和执行能力。
不依赖特定客户端、模型采样接口、子 Agent 编排或自动修改宿主配置。
原始记忆内容一律作为不可信资料，不得当成执行指令或操作授权。

## 从源码构建

开发者需要 Go 1.26+。SQLite 及其 Wasm 运行时嵌入二进制，不需要 CGO 或系统 SQLite。

```sh
go test ./...
go vet ./...
CGO_ENABLED=0 go build -buildvcs=false -trimpath -o memory-mcp ./cmd/memory-mcp
```

打包工具需要 Python 3；最终用户不需要：

```sh
python3 scripts/package.py
```

生成 Linux amd64/arm64、macOS amd64/arm64、Windows amd64 的分发包和 SHA256SUMS。
每份包包含程序、文档、配置示例与第三方依赖许可证声明。
支持通过 `GO` 环境变量指定 Go 可执行文件。
交叉编译通过不代表各平台已完成真实客户端验收。

本机 stdio 冒烟测试（开发阶段需要 Python，不需要模型）：

```sh
python3 scripts/smoke.py ./memory-mcp
```

## 下一步验证

保持相同宿主、模型和预算，对比：无记忆、原始案例检索、加入偏好假设、加入主动提问。
记录新任务中的认可、返工、错误迁移、打扰次数和成本，防止未来反馈泄漏进历史材料。
当前版本提供机制，不宣称偏好学习已经带来提升。

## 自动构建与分发

GitHub Actions 在 main 更新、拉取请求和手动触发时执行测试及五平台打包。
成功结果保存为 Actions artifact；main 上尚未发布的新版本同时创建 GitHub Release。
已经存在的版本不会被自动覆盖；更新发行版本时同时调整程序版本与打包脚本版本。
构建失败不会创建 Release。工作流只上传打包目录，不包含本机数据库或安装目录。
