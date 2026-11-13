# Codex 适配器

先读 [安装说明](../AGENT_INSTALL.md)。确认真实执行端和 CODEX_HOME；默认取当前进程 CODEX_HOME，否则用户 .codex。配置在 config.toml 的 mcp_servers，hooks.json 同目录。

```text
<程序> setup --host codex --mode assist --config <实际CODEX_HOME/config.toml> --data-dir <原库> --project <真实工作目录> --scope project:<原scope>
```

辅助模式仅注册 SessionStart/UserPromptSubmit。服务与 owned launcher 都显式 assist，持久适用于全部绑定，不询问逐轮切换。TOML 只改本服务表，保留其他字节/注释；不支持的内联/点式布局拒绝覆盖。

程序不更改原生 hook 信任状态。按实际客户端 Hooks 管理入口信任具体变更，再重启/新会话；旧 SessionStart 软引导不能当成本套适配已加载的证明，核对来源后协调旧项，保留其他用户 hooks，避免双提示。

当前适配档案要求原生轮次事件提供真实 turn_id。缺必要字段时 stderr 报协议不符，hook 失败开路，不猜另一个轮次。可选采集使用实际 turn_id 幂等与定位，默认不采集。strict 的 block/续跑轮次保护仍仅用于明确兼容模式，不能推断为辅助模式会拦操作。

doctor 核对 launcher 是否完整、data_dir/模式一致与绑定；本机生成命令测试和手工回放不能证明真实 Codex 已信任/触发。以真实 bootstrap 和新会话回执验证。仅注册 MCP、仅有开发者 SessionStart 软引导的环境，不宣称有完整原生 hook 能力。

该档案的协议参考为 https://learn.chatgpt.com/docs/hooks，客户端升级需现场重新核对支持与信任机制；这里描述本项目的实现和验证边界，不承诺任意客户端版本兼容。
