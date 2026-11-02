# 文本完整性与乱码诊断（0.1.3）

## 已复现的漏洞与修复

此前只在解码后的字段检查 UTF-8。实际 MCP 字节测试复现：不完整的中文 UTF-8 字节进入当前 MCP SDK 后，会被替换为 U+FFFD（�）；替换后的字符串已经是合法 UTF-8，随后被当作正常原文写入。已有 � 的参数也会通过。回执 note 原先还缺少相同的字段检查。

新版本在 SDK 解码工具参数前检查原始字节与 Unicode 转义，拒绝坏 UTF-8、孤立的 UTF-16 代理项；正文、标题等写入字段和 review note 默认拒绝可疑 U+FFFD。失败明确返回，不写记录、不消耗本次 request_id，也不把 review 标为完成。不会静默删字符、猜测原文或反复写出“修复”版本。

正常中文、emoji、标点、空格和合法代理对均原样保留。独立字节测试覆盖了在多字节字符中间拆分 stdio 输入，正常分片本身不会导致丢字。该复现确定了本程序接收侧的漏洞；没有用户客户端当时的原始入站参数，不能据此断定用户报告中的每个坏字节具体由上游哪一层产生。

## 写入及回读回执

- 业务正文仍返回 body_bytes、body_sha256，与写入前原文字节比较。
- review 返回 review_note 原文，以及 review_note_bytes、review_note_sha256；摘要对应提交的 note 本身，不是内部包装 JSON。
- memory_read 对可疑旧内容返回 text_issues，包括字段、问题类型和首个 UTF-8 字节位置；不会修改正文。
- 错误消息不打印私密参数。原始字节校验失败会给出参数摘要，用于区分到达服务时的内容，不输出完整参数日志。

如果原文就是讨论 � 字符本身，可在 memory_record / memory_revise 的 draft 或 memory_review_done 显式传 allow_replacement_characters=true，逐字保留并带文本提示。**不能为绕过错误自动开启它。** 应先从原会话/原材料重新取得完整文字。此选项不能放行非法 UTF-8 字节或孤立代理项。

hook 原始事件保留宿主实际交付的可见文字，包括用户有意讨论的 �，并通过 text_issues 标注；整理成业务案例时仍执行默认保护。有效原文与已损坏参数需要宿主核对，程序不冒充知道用户原意。

## 查找旧记录（不改数据）

升级前先关闭所有使用该库的旧实例，备份整个数据目录及配置。然后：

```text
/absolute/memory-mcp doctor --data-dir /actual/memory-directory --check-text
```

text_integrity 列出可疑条目的 ID、scope、字段和首个位置，覆盖历史版本与回执 note，不输出聊天正文，也不执行修复。原文有意包含 � 的记录同样可能列出，需结合来源判断。

在原客户端用 memory_read 读取条目，与原聊天或原材料核对。已变成 � 的字节无法从数据库可靠逆推出原字句；只在拿到确切原文后，用 memory_revise 追加一个准确版本，保留旧版，不全库猜词替换。修订冲突按当前版本处理。

历史 review 回执保持原样作为审计记录；扫描会单独报告其中的乱码。不能用事后编写的新 note 冒充当时收到的原始参数。

## 验证入口

```text
python3 scripts/check_text_integrity.py /absolute/memory-mcp
```

开发测试使用独立临时库，检查真实 stdio 中的中文/emoji 分片传输、双重输出及重启后的逐字一致；坏字节、非法代理项及可疑正文/note 拒绝落盘；完整 note 的字节数和摘要；以及只读诊断。脚本需要 Python，最终用户程序不需要。
