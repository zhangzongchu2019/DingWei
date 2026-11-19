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

## Connect失败说明（03:30Z追加）

readAttempt的浏览器cookie哈希比较和五分钟oauthTTL逐字保留。只有connect的readAttempt失败分支调用新的统一展示helper：Accept明确接受text/html时为静态中文HTML、400，解释必须同浏览器且五分钟内从AI工具Authenticate重新发起，不复制URL；不存在/过期/缺cookie/错cookie返回完全相同字节。无attempt/客户端/redirect/原错误反射，不带表单、脚本或自动重试链接，CSP default-src/base-uri/form-action/frame-ancestors均none，no-store/no-referrer/nosniff。非HTML沿用400与原JSON invalid_request；q=0不选择HTML。已鉴权mcp的Origin变化不扩展到connect，connect原Origin守卫不改。

常驻格为三类拒绝（含缺/错cookie）×HTML/JSON、同cookie在5分钟减1ns仍成功、恰好5分钟拒绝、HTML禁用质量保留JSON。删cookie比较必须红“无cookie打开”业务断言；把TTL扩到10分钟须五分钟边界格红。

## 目标输入文案（03:48Z追加）

internal/feishulogin/connect.go 仅去掉手填 target_agent 的具体 placeholder 与示例，保留空初始 value 和既有服务端校验，提示逐字为「填写你要协作的 Agent 全名（向你的组长或管理员索取），系统中必须已存在」。GROUPS 产品 aaed8aae 对同文件只修改 renderConnectPage 中 allowUnpaired／allowReauthentication 自动入口条件，本片改模板两行，未覆盖该函数；后续合并应同时保留可不建临时配对入口及本片文案，不使目标重新必选。回归检查渲染输入无 placeholder／初始值空、提示不含具体全名，恢复旧 placeholder 必须红。

## A-AUTH-02 保留私有提示与既有06选择优先级

自查反例在907源确认：显式私有2026头配标准initialize提议时，standardInitialize可盖过privateHint而200。这不是可交付行为，已在新锚修正：先精确保留原06选择两式（显式06头；无版本头且initialize提议06），其它请求若带2026头／Mcp-Method／私有_meta协议键则交原私有codec严格拒绝；无私有提示才让标准codec协商未知版本。新增两格保护三版本混合封套与无头06旧优先级，未认证始终在分流之前401。删此私有提示保护须PRIVATE_INITIALIZE_GUARD业务红。907原七刀虽绿但未覆盖本支，旧反例与停门日志保留，最终八刀及完整门以修后源为准。

A-AUTH-02提示判定补齐：保留原06选择优先后，已知Mcp-Method／Mcp-Name按字段存在而非非空值识别，因此空头和重复头仍走原私有严格路径；params._meta中任一私有metaPrefix键也识别为私有提示，普通progressToken及其他命名空间不误判。矩阵覆盖8类提示、03/11、鉴权有无，以及两种既有06选择。359反例原输出保留；新增各分支删保护与兼容选择刀，固定最终源重新全门，不累加中间刀数。
