## 基线与修复

基main206d41b2。Register的/mcp先进入mcp鉴权入口，原guard用于其已鉴权后续处理；其它路由不改。mcp在method、Origin、媒体头、body与codec选择前调用Authenticate一次，401保留既有静态PublicOrigin资源元数据和scope挑战。未认证响应id为null，因不读取请求来取得id。已鉴权Seat只经当前调用的闭包传给mcpAuthorized和codec，不放到全局或握手会话；下一HTTP请求重新鉴权。valid token但权限/租户拒绝仍保留原403，不假装token无效。

已鉴权Origin非法仍403；未鉴权Origin非法先401，这是派单“任何未带有效bearer的/mcp请求先挑战”的明确优先级。不会放行Origin非法请求进入业务，也不修改其它OAuth/REST路径的Origin优先级。既有安全媒体限制、双Accept、请求体上限、严格已知字段、scope/audience/租户/AG与业务鉴权不变。

## A-AUTH-01 最小版本协商

服务端完整标准codec仍只声明2025-06-18。initialize客户端提议03-26、06-18、11-25或未知非空版本均返回06-18，客户端须使用返回版本继续，若客户端不支持应断开；不以“HTTP初始化成功”保证客户端支持返回版本。不会伪造03或11支持，亦不拓展业务工具。

03-26额外要求评估：支持请求batch、旧工具结果只有content/isError而没有06新增structuredContent/outputSchema。11-25额外要求评估：tasks、URL elicitation与图标等可选新能力，工具结果与06单请求工具子集可复用，但未知能力不能自动启用。本片只接收initialize.clientInfo的description/websiteUrl/icons作为未使用信息；不广告tasks、resources、sampling或elicitation，不取件URL，不实现batch。

| 方法 | 03-26 | 06-18 | 11-25 | 本片 |
|---|---|---|---|---|
| initialize | protocolVersion+capabilities+clientInfo | 同；info可含title | info可含description/icons/websiteUrl | 提议均返回06-18，接收已知可选info，不反射客户端能力 |
| initialized通知 | 无id，成功202空体 | 同 | 同 | 按协商06处理 |
| ping | result空对象 | 同 | 同 | 按协商06处理 |
| tools/list | 无structured输出schema能力 | 可有outputSchema | 可增加可选tasks相关声明 | 保留既有九工具及06结构，不新增能力 |
| tools/call | content/isError，03支持batch传输 | 可加structuredContent，不再batch | 可选任务增强，非强制工具能力 | 保留06 content+structuredContent+isError，不实现tasks |

无MCP-Protocol-Version且没有其他可信版本识别依据，按03-26。因本无状态网关不支持该操作codec，已鉴权非initialize返回400/-32022；这满足默认版本规则并明确不冒称旧版工具兼容。initialize用params提议协商，不受无头默认挡住。显式2026头或私有_meta/Mcp-Method沿用旧私有封套，全部匹配校验保留，不让标准codec旁路私有格式的错头。

## 依据与验证

2026-11-19只读核官方页面：
- [Lifecycle版本协商](https://modelcontextprotocol.io/specification/2025-11-25/basic/lifecycle)：不支持客户端提议时返回另一受支持版本；客户端不支持返回版本则断开。
- [HTTP无头默认与不支持头](https://modelcontextprotocol.io/specification/2025-11-25/basic/transports)：无协议头且无其它识别依据默认03-26；不支持版本头的操作返回400。
- [03传输](https://modelcontextprotocol.io/specification/2025-03-26/basic/transports)：旧版有batch形状；本片不能只改日期自称全支持。
- [06工具结果](https://modelcontextprotocol.io/specification/2025-06-18/server/tools)：structuredContent及outputSchema是06的新增可选结构。

三版本×未鉴权三方法（有/无版本头）、所有HTTP方法/无效媒体与Origin/不读body、错误token过期吊销、已鉴权协商与完整后续链、缺头默认与私有旧网关负格。删前置鉴权前增加版本检查须未鉴权401断言红；恢复initialize固定版本拒绝须协商格红。普通推送交件，R6到锚即中断。
