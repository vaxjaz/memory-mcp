本机运行的通用决策记忆 MCP 首个原型版本。

- 使用宿主 AI 推理，不内置模型，不需要额外模型密钥。
- 五个 MCP 工具：bootstrap、record、search、read、revise。
- SQLite 本地存储，保留原始材料、案例、偏好假设及其来源。
- 支持修订历史、依赖复查标记、导出和显式删除。
- 包含 Linux x64/ARM64、macOS Intel/Apple Silicon、Windows x64 程序包。

下载对应平台程序包，解压后运行 `memory-mcp config` 生成配置，
按宿主的 MCP 设置添加本地 stdio 服务。连接后让 AI 调用 `memory_bootstrap`。

这是原型，尚未验证偏好学习的实际增益，也未逐一验收全部宿主客户端。
MCP 不自动监听完整聊天；宿主提供哪些材料，本地才能保存哪些材料。
