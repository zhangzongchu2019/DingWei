# Agent HTTP/MCP Gateway Specification

## Purpose

本变更把未注册外部 Agent 从无法参与 DingWei 会话通讯改为可经受控 HTTP 席位交换消息，目的是降低跨厂商协作接入门槛并维持最小权限。涉及 Hub、身份授权、消息投递、在线清单与审计，不影响现有 helper 席位、sessionHelper IPC 或 send.py 行为。

## ADDED Requirements

### Requirement: GW-001 通讯能力边界
Hub MUST 仅向外部 HTTP 席位开放在线清单、普通消息发送、收件和确认能力，不得通过该身份开放执行、升级下发、宿主访问、文件访问或任意工具调用。

#### Scenario: 外部席位调用允许能力
- **WHEN** 有效外部席位请求在线清单、发送普通消息、等待收件或确认其投递
- **THEN** Hub 按其账号和编组授权处理并记录结果

#### Scenario: 外部席位请求非通讯能力
- **WHEN** 外部席位调用执行、升级、宿主或未列入白名单的工具/API
- **THEN** Hub 拒绝请求且不产生执行或宿主副作用

### Requirement: GW-002 令牌身份与撤销
Hub MUST 为每个外部席位使用独立 Bearer token，将 token 绑定账号、编组、席位及最小 scope；持久化只保存不可逆哈希，吊销后请求 MUST 被拒绝。

#### Scenario: 有效令牌访问
- **WHEN** 有效 token 请求其授权编组内资源
- **THEN** 服务端从席位记录确定账号和编组，不接受请求体覆盖

#### Scenario: 吊销或越权
- **WHEN** token 已吊销/过期，或目标不在授权范围
- **THEN** Hub 拒绝访问且不返回受限资源或投递数据

### Requirement: GW-003 MCP Streamable HTTP 面
Hub MUST 提供符合 MCP `2026-07-28` Streamable HTTP 与 HTTP Authorization 要求的 endpoint，且只注册 `list_online`、`send_message`、`wait_for_messages`、`ack` 四个通讯工具。

#### Scenario: MCP 四工具调用
- **WHEN** 经标准授权的 MCP 客户端发现 endpoint 并调用四个工具之一
- **THEN** Hub 返回符合 MCP/JSON-RPC 的结果且权限与 REST 等价

#### Scenario: Origin 与授权失败
- **WHEN** MCP 请求含非法 Origin，或缺少/携带无效 Bearer token
- **THEN** Hub 按来源策略拒绝（非法 Origin 返回 403）或按授权错误拒绝，且不执行工具副作用

### Requirement: GW-004 REST 能力等价
Hub MUST 为不支持 MCP 的客户端提供在线清单、发消息、有限等待收件及 ACK REST 操作；REST 不得获得 MCP 四工具之外的权限。

#### Scenario: REST 与 MCP 等价
- **WHEN** 同一席位通过 REST 或 MCP 执行同一授权操作
- **THEN** 两种传输使用相同身份、编组校验、消息路由和 ACK 状态机

#### Scenario: REST 请求未授权能力
- **WHEN** 客户端调用未列出的管理或执行接口
- **THEN** 请求被拒绝且无状态副作用

### Requirement: GW-005 有界轮询和在线租约
Hub MUST 对收件长轮询施加服务端等待上限、返回数量上限及席位并发/速率限制，并以成功认证的收件活动刷新 HTTP 席位在线租约。

#### Scenario: 长轮询有界返回
- **WHEN** 等待参数超过配置上限或队列为空直到等待超时
- **THEN** 服务端拒绝/截断超限参数，至迟在硬超时返回有界响应并更新租约

#### Scenario: 租约过期
- **WHEN** HTTP 席位超过在线租约阈值没有成功活动
- **THEN** 在线清单报告离线，且不影响 helper 席位在线判断

### Requirement: GW-006 持久投递与过期
Hub MUST 将发往 HTTP 席位的普通消息持久排队，并复用 `session_delivery` 的领取、重试、失败和过期记录；队列满或消息到期须返回可审计结果。

#### Scenario: 离线期间收到消息
- **WHEN** 目标席位离线但未超配额
- **THEN** 消息排队至上线领取或 TTL 到期，过期后标记失败且不再投递

#### Scenario: 队列容量超限
- **WHEN** 新消息会使席位超过队列容量
- **THEN** Hub 返回容量错误并记录审计，不静默丢弃旧消息

### Requirement: GW-007 delivery_ack v2 绑定
Hub MUST 使用 delivery_ack v2 的事务确认、席位绑定、generation 与单调状态语义；HTTP adapter 扩展不得改变现有 driver/helper 路径。

#### Scenario: 合法 ACK
- **WHEN** HTTP 席位为自身领取的 delivery 提交匹配协议、处理模式及 generation 的确认
- **THEN** Hub 在事务提交单调状态后才返回确认

#### Scenario: 伪造或重放 ACK
- **WHEN** ACK 指向其他席位投递、generation 不符或状态回退
- **THEN** Hub 不改变投递状态并拒绝或忽略 ACK

### Requirement: GW-008 全名、编组与提及
Hub MUST 按规范全名解析目标并校验账号/编组权限；客户端不得使用内部 key ID 绕过名称解析。消息正文的提及处理 MUST 沿用现有 AMR 规则，不得将提及解释为执行指令。

#### Scenario: 授权全名寻址
- **WHEN** 外部席位发送给授权编组内有效全名
- **THEN** Hub 将普通消息路由到解析目标并沿用既有提及规则

#### Scenario: 跨组或原始 ID 寻址
- **WHEN** 客户端寻址未授权编组名称或传入内部 key ID
- **THEN** Hub 拒绝请求且不泄露目标是否存在

### Requirement: GW-009 来源校验、审计与限流
Hub MUST 按席位和来源实施限流并审计授权、投递、收件、ACK、吊销及拒绝事件；不得记录明文 token。MCP Origin 校验 MUST 遵循 MCP 规范及 R52 受信来源策略，且来源许可不得替代身份授权。

#### Scenario: 限流与安全审计
- **WHEN** 席位超过请求/消息/并发轮询限额或发生拒绝/吊销
- **THEN** 服务端按策略限流并生成不含凭据明文的审计记录

#### Scenario: 来源通过但身份无效
- **WHEN** 请求来源通过 R52 校验但 Bearer 身份缺失、无效或无权
- **THEN** Hub 仍拒绝请求

### Requirement: GW-010 现有席位兼容
新增 HTTP 席位 MUST 与 helper 席位并列隔离；不得改变 sessionHelper、send.py、helper 投递协议和既有在线状态语义。MVP 清单只显示调用者有权查看的编组席位。

#### Scenario: 现有 helper 回归
- **WHEN** 已注册 helper 按原协议收发和确认消息
- **THEN** 行为与变更前一致，且不需 HTTP token

#### Scenario: 编组内在线清单
- **WHEN** 外部席位查询在线清单
- **THEN** Hub 仅返回其授权可见编组内的规范全名、类型和在线状态
