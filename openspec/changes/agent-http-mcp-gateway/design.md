# 设计

## Context

Hub 当前以 helper 席位和本机 IPC 为主；`session_delivery` 提供持久投递，delivery_ack v2 维护阶段确认。外部 Agent 只需要与席位交换普通消息。新增 HTTP 面必须收敛为消息能力的适配层，不能暴露 Hub 内部命令、工具执行、宿主文件/进程、代理操作或升级通道。

规范依据 MCP `2026-07-28`：Streamable HTTP 使用单一 endpoint 和 JSON-RPC POST；响应可为按请求 JSON 或 SSE；服务端须校验所有连接的 Origin，非法 Origin 返回 403。MCP HTTP 授权遵循 OAuth 2.1，并要求 Protected Resource Metadata（RFC 9728）发现授权服务器。详见 `context.md`。本方案尚未实测，互操作性未核实。

## Goals

- 提供能力等价、权限等价的 MCP 与 REST 通讯面。
- 将外部身份、账号、编组、消息目标和 ACK 绑定，令牌可吊销且仅存哈希。
- 复用 Hub 路由、投递队列、过期/失败记录和 delivery_ack v2 状态语义。
- 对现有 helper、sessionHelper、send.py 和已注册席位保持兼容。

## Non-Goals

- 不允许远程执行、升级下发、宿主访问、任意工具调用、shell、文件传输或原始 Hub 管理 API。
- 不让客户端指定底层 key ID、数据库主键、任意账户或编组。
- 不在 MVP 开放匿名注册、跨编组、浏览器 Cookie 认证或任意 CORS。

## Decisions

### 身份与授权

新增 `http` 席位类型，与现有 `helper` 席位并列。管理员预建外部席位并绑定一个 DingWei 账号、一个编组及唯一席位全名。每席位独立 OAuth 客户端/opaque access token，scope 限定为 `roster:read message:send inbox:read inbox:ack`；token 含足够熵、短期有效、可轮换/吊销，持久化保存不可逆校验值（可用带服务端 secret pepper 的哈希），pepper 由密钥管理系统持有。明文只在签发时显示一次。认证后从服务端席位记录取得账号/编组；请求体不得覆盖。

按 MCP 2026-07-28 Authorization 要求，Hub MCP 作为 OAuth 受保护资源发布 RFC 9728 元数据并使用标准 `Authorization: Bearer`。管理员预置客户端方案、token TTL、刷新/再授权流程须由 PM 确认；实现不能以“私有静态 bearer 校验”冒充合规 OAuth 流程。REST 与 MCP 共享同一授权中间件。

### 接口与等价能力

MCP 单一 Streamable HTTP endpoint（例如 `/mcp`），JSON-RPC 工具恰为四个：

- `list_online`：返回授权编组内席位规范全名、类型与 online/offline；不返回原始 key 或账户敏感字段。
- `send_message`：参数 `to_full_name`、`text`、可选 `client_message_id`；服务端解析全名，校验范围后进入既有 Hub 消息路由。
- `wait_for_messages`：参数 `wait_ms`，服务端硬上限 25,000ms；最多返回有界数量（建议 50）的新投递，不允许客户端延长租约或遍历其他席位队列。
- `ack`：参数 `delivery_id`、`state`、符合 v2 的确认元数据；仅允许确认该 token 席位已领取的投递。

REST 提供等价操作：`GET /api/agent-gateway/v1/online`、`POST /messages`、`POST /inbox:wait`、`POST /inbox/{delivery_id}:ack`。`inbox:wait` 的 `wait_ms` 同样限制为 0–25,000ms，响应含投递标识；REST 不增加 MCP 工具之外的权限。错误至少区分 `unauthorized`、`forbidden`、`rate_limited`、`delivery_expired`、`invalid_ack`、`origin_rejected`；最终路径与错误码在实现规格中确认。

MCP 工具响应遵循 JSON-RPC。MCP HTTP 连接按规范校验 Origin；机器客户端不需要浏览器 CORS 许可，但非法 Origin 仍按 MCP 规则拒绝。REST 的 Bearer 认证不依赖浏览器 Origin；若以后允许浏览器访问，则须经独立 CORS 与来源策略审查。

### 队列、确认和在线状态

外部发往普通席位的消息仍走 Hub 原有路由与持久队列。发往 HTTP 席位的消息写入既有 `session_delivery`，以外部席位稳定内部身份绑定 `key_id/session_name`，禁止暴露原始 key。投递服务按到期时间领取，等待接口仅唤醒并返回已领取消息；重复领取使用现有 lease/重试规则。

ACK 通过 delivery_ack v2 的单调状态机提交：接收/排队、注入（HTTP adapter 对应 Agent 已领取并接纳）、处理完成、不可重试失败/未知。ACK 必须绑定 delivery、席位、协议版本、processing mode、generation；确认仅在事务提交后返回。v2 当前检查 `processing_mode=driver`，故实现需新增 `http` adapter mode 或抽取 transport-neutral v2 校验；不得放宽/改变既有 driver/helper 路径。新状态映射和错误码须由规格覆盖。

成功认证的收件/轮询请求刷新 `last_seen_at`，长轮询开始与结束均刷新。建议租约 90 秒，超过即 offline；未完成长轮询的席位每次响应前更新活动时间。MVP 只报告席位状态，不保证外部 Agent 正在推理。

离线目标消息继续持久排队；提案默认最长存活 24 小时，到期转 `dead` 并保留失败原因，发送方可查询投递结果。不能因离线立即丢弃，也不能无限保存。队列按席位限额，满额返回容量错误并审计。保留期、容量和租约均为待 PM 确认的默认值。

### 地址、编组与提及

消息寻址沿用用户可见全名规则；服务端将全名解析到目标席位，绝不接受客户端提供 key ID。MVP 单编组且只可寻址授权范围内席位。在线清单只返回相同编组可见席位。提及正文使用现有 AMR 候选解析规则，不把 mention 解析改写为执行指令；未经批准不增加跨编组提及。外部席位在已有名称解析器中的全名命名空间尚须实现设计确认。

### 安全威胁与缓解

- 令牌泄漏/重放：每席位独立高熵 token、TLS、短 TTL、哈希存储、吊销/轮换、scope 最小化；审计不记 token 或 Authorization header。
- 越权寻址/IDOR：从认证主体派生席位/账号/编组并校验目标范围；ACK 校验 delivery 所有权和 generation。
- 轮询耗尽/队列洪泛：按席位和来源限流、长轮询硬上限、并发上限、队列配额、发送频率/响应体积上限；审计限流事件。
- 伪造 Origin / DNS rebinding：MCP 按规范校验所有连接 Origin；使用 HTTPS 并明确主机名/IP 策略。来源校验不替代 OAuth 身份认证。
- 不可信消息正文与提及滥用：沿用现有提及解析、长度与编码限制；正文作为数据，不解释为命令；防止日志注入与 Markdown/HTML 主动内容。
- 账号停用、成员移组或席位吊销后旧 token 仍可用：每次请求检查席位、账号、编组和授权状态，吊销即时失效并终止长轮询。
- 跨源浏览器窃取：MVP 不使用 Cookie 授权，不开放任意 CORS；浏览器接入须单独审查。
- 审计泄露身份或正文：记录事件类型、脱敏席位 ID、消息 ID 与结果码，默认不记录正文和凭据。

### R52 受信来源清单

R52 trusted-origins 设计只供浏览器类甲入口（Origin、Referer、Sec-Fetch、WebSocket 握手）判断来源；PublicOrigin 仍是生成链接、跳转及 Cookie 属性的规范值。服务器到服务器的 MCP/REST 以 OAuth Bearer 身份认证，不依赖浏览器来源豁免，也不得因来源在清单中就授权。MCP Streamable HTTP 自身的 Origin 校验仍按规范执行；若端点位于代理域名/端口，需把确切 HTTPS origin 按 R52 规则加入配置并验证 Host/Origin 一致性。清单不允许通配、不信任 http；受信来源不等于可信用户或可信消息。

### 对现有体系的影响

- `sessionHelper`、`send.py`：不改通信协议和默认路径；外部 Agent 直接调用 Hub HTTP，不要求安装 helper。
- 在线清单：增加过滤后的 HTTP 席位视图；调用者只能看授权编组，helper 席位在线语义不变。
- AMR：共用现有消息提及候选解析；增加席位到目标的受控映射，不改变既有 mention 行为。
- Console：MVP 不提供外部 Agent 管理 UI；由受控管理面创建/吊销席位。后续可展示类型、在线租约和队列状态，但不得展示 token。

## Delivery Phases

### Phase 1 — 单编组 MVP

REST 与 MCP 四工具同时提供；预置席位/令牌、在线清单、发消息、有限长轮询、ACK v2、单编组隔离、限流审计、吊销和 24 小时过期策略。验收：标准客户端完成 OAuth 资源发现和四工具往返；REST 对同一场景得到等价消息与 ACK；跨编组寻址和伪造 ACK 均拒绝；helper 回归保持一致；吊销后下一请求拒绝；过期消息不再投递且可见失败状态。MCP 互操作未实测，需在实现阶段补跑并记录。

### Phase 2 — 管理与运营

管理员 Console、编组策略、队列/限流指标、令牌轮换流程和可配置过期策略。验收：角色授权、审计检索、吊销传播和高并发轮询压测通过；升级与回滚不改变 helper 行为。

### Phase 3 — 多编组与客户端生态

经 PM 决定后支持多编组授权、外部 Agent 命名空间、SDK/客户端示例及更细粒度 scope。验收：跨组隔离与授权矩阵全覆盖，多个标准 MCP 客户端互操作通过；不得默认扩大既有 token 权限。

## Test Strategy

MVP 测试覆盖 OAuth 发现与 Bearer、签发/哈希/吊销、账号及组约束、全名解析、投递去重/重试/过期、poll 超时/上限/并发、ACK v2 单调性、REST/MCP 等价、审计脱敏、限流及 helper 金丝回归。空闲与洪流下验证队列有界和在线租约准确。R52 来源校验测试无 Origin 的机器调用、合法精确 HTTPS Origin、错误端口、HTTP 伪冒与恶意 Host；来源通过不得绕过授权。

变异测试至少包括：①删除编组/目标授权判定，跨编组用例必须失败；②移除吊销检查，吊销用例必须失败；③ACK 不校验 delivery 所有权/generation，伪造 ACK 用例必须失败；④忽略长轮询硬上限或离线过期，边界用例必须失败；⑤跳过 Origin 校验，非法 Origin 用例必须失败；⑥ MCP 与 REST 其中一面增加权限，能力等价测试必须失败。每刀须在原客观判据上变红。

## Open Questions

- 管理员预置 OAuth client 与 access token 的操作、TTL、续期机制及 RFC 9728 metadata 部署方式。
- HTTP 席位名称如何融入全名唯一性和在线清单；是否显示为外部类型。
- 外部 Agent 是否允许被 mention，MVP 是否只支持显式全名。
- 24 小时 TTL、90 秒在线租约、队列容量、poll 并发/频率默认值。
- REST 错误格式与幂等键保留时限；MCP 取消和客户端断连如何映射到等待操作。
- Hub 部署的反向代理是否完整保留 Origin、Authorization 与长轮询连接；目标 MCP 客户端互操作清单。
