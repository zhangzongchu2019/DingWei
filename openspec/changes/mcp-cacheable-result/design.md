# 设计

2026-07-28 `tools/list` 结果增加具名常量 `toolsListTTLMS=300000` 与 `toolsListCacheScope="private"`。鉴权上下文不同的响应不得由共享缓存复用。2025-06-18 codec、tools/call、server/discover 与未实现的 prompts/resources 均不改动。

按 2026-07-28 changelog 逐项核对：`server/discover` 已实现并声明协议版本、tools capability 与 identity；普通结果已有必需 `resultType=complete`；`tools/call` 的 content、structuredContent、isError 符合已支持的结果形态；tools/list 工具列表顺序稳定。尚缺的 MUST 只有 tools/list 的 ttlMs/cacheScope，本变更补齐。

SHOULD 项清单：客户端每请求发送 clientInfo；服务端每个结果在 `_meta.io.modelcontextprotocol/serverInfo` 提供身份（当前只有 server/discover 有）；tools/list 返回确定顺序（当前实现已满足）。按任务范围，仅记录前两项，不在本变更改动。
