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
Hub MUST 在网页飞书扫码并可信核验 tenant_key 等于本团队后，自动创建外部主体/席位与所选目标的 AG，签发绑定稳定飞书身份、席位、AG 和最小 scope 的 OAuth Bearer token；不要求管理员预建。内部键使用带租户/issuer 命名空间的 open_id／union_id，显示归属名优先英文名、缺则仅 [a-z] 拼音且不作唯一键。令牌 MUST 仅存不可逆校验值，每次成功鉴权交互持久续至本次＋14天；now>=expiry 拒绝并须重新扫码，吊销优先于续期且不得复活。

#### Scenario: 有效令牌访问
- **WHEN** 有效 token 请求其授权编组内资源
- **THEN** 服务端从席位记录确定账号和编组，不接受请求体覆盖

#### Scenario: 吊销或越权
- **WHEN** token 已吊销/过期，或目标不在授权范围
- **THEN** Hub 拒绝访问且不返回受限资源或投递数据

#### Scenario: GW-002-S03 团队扫码与租户拒绝
- **WHEN** 服务端验证网页飞书扫码，分别取得本团队、他租户或缺tenant_key的可信身份
- **THEN** 仅本团队可签发令牌并建AG；其他情形拒绝且不产生席位、令牌或AG

#### Scenario: GW-002-S04 显示名不合并身份
- **WHEN** 两个不同open_id主体有相同英文名或中文转写结果
- **THEN** 保持两个独立稳定主体，归属显示不造成授权或令牌合并

#### Scenario: GW-002-S05 名称来源
- **WHEN** 飞书身份含英文名或只有中文名
- **THEN** 前者用英文归属显示名，后者以确定拼音方案生成仅[a-z]显示名，均不作为内部唯一键

#### Scenario: GW-002-S06 十四天边界与四工具触达
- **WHEN** 令牌在第13天23小时通过发送、ACK、在线清单或空长轮询鉴权，或至第14天整未交互
- **THEN** 前者各自续到本次＋14天且重启不丢续期；后者拒绝并须重新扫码

#### Scenario: GW-002-S07 吊销与续期并发
- **WHEN** 吊销与触达并发，或时钟早于已持久最后活动
- **THEN** 吊销后请求不能成功/复活令牌，回拨不续期或扩大权限；等价聚合不得丢已成功交互

#### Scenario: GW-002-S08 续期持久失败
- **WHEN** 已鉴权交互的持久触达写入失败或进程在expiry投影更新前重启
- **THEN** 失败不声称已续期；已成功响应的触达可从持久记录恢复精确expiry

### Requirement: GW-003 MCP Streamable HTTP 面
Hub MUST 提供MCP `2026-07-28`无状态Streamable HTTP与标准OAuth endpoint，去协议会话/initialize，支持server/discover；每请求声明版本/能力并验证标准头，结果含resultType。仅注册 `list_online`、`send_message`、`wait_for_messages`、`ack` 四个通讯工具。可选收件资源/订阅仅通知本人变更；不以流保活续平台寿命，不把私有WSS称MCP标准。

#### Scenario: MCP 四工具调用
- **WHEN** 经标准授权的 MCP 客户端发现 endpoint 并调用四个工具之一
- **THEN** Hub 返回符合 MCP/JSON-RPC 的结果且权限与 REST 等价

#### Scenario: Origin 与授权失败
- **WHEN** MCP 请求含非法 Origin，或缺少/携带无效 Bearer token
- **THEN** Hub 按来源策略拒绝（非法 Origin 返回 403）或按授权错误拒绝，且不执行工具副作用

#### Scenario: GW-003-S03 无握手发现与请求校验
- **WHEN** 客户端无initialize/Mcp-Session-Id直接调用server/discover或工具，并分别提供匹配/不匹配版本能力元数据及规定请求头
- **THEN** 合法请求独立处理，结果有resultType；头不匹配或版本不支持按协议拒绝，不建立MCP会话

#### Scenario: GW-003-S04 可选收件通知
- **WHEN** 启用本人收件箱资源的resourceSubscriptions，通知流断开或重复通知
- **THEN** 仅通知本人变化且不算投递ACK；客户端重订阅并主动收件补漏，无订阅客户端仍可轮询，流保活不续平台租约

#### Scenario: GW-003-S05 新版客户端注册与兼容边界
- **WHEN** 新客户端按CIMD授权，或旧SDK只会initialize/GET流/旧resources订阅
- **THEN** 前者走标准OAuth与现代RPC；一期对后者明确不兼容，不静默改协议或开放私有WSS

### Requirement: GW-004 REST 能力等价
Hub MUST 为不支持 MCP 的客户端提供在线清单、发消息、有限等待收件及 ACK REST 操作；REST 不得获得 MCP 四工具之外的权限。

#### Scenario: REST 与 MCP 等价
- **WHEN** 同一席位通过 REST 或 MCP 执行同一授权操作
- **THEN** 两种传输使用相同身份、编组校验、消息路由和 ACK 状态机

#### Scenario: REST 请求未授权能力
- **WHEN** 客户端调用未列出的管理或执行接口
- **THEN** 请求被拒绝且无状态副作用

### Requirement: GW-005 有界轮询和在线租约
Hub MUST 对收件长轮询施加25000ms硬上限、返回数量上限及席位并发/速率限制；仅成功请求刷新HTTP席位在线租约，90秒无成功请求即离线。空轮询可续14天令牌，但不得把在线状态等同正在推理。

#### Scenario: 长轮询有界返回
- **WHEN** 等待参数超过配置上限或队列为空直到等待超时
- **THEN** 服务端拒绝/截断超限参数，至迟在硬超时返回有界响应并更新租约

#### Scenario: 租约过期
- **WHEN** HTTP 席位超过在线租约阈值没有成功活动
- **THEN** 在线清单报告离线，且不影响 helper 席位在线判断

### Requirement: GW-006 持久投递与过期
Hub MUST 将发往HTTP席位的普通消息持久排队，复用 `session_delivery` 的领取、重试与确认事实；每消息从入队起24小时到期，标dead并给可查询原因，续令牌/编组不延消息TTL。解散后本AG未领取消息须失败；已领取不重投或伪记完成；两侧各有幂等、持久生命周期通知。跨库结果未知MUST记unknown并对账，不宣称原子成功。

#### Scenario: 离线期间收到消息
- **WHEN** 目标席位离线但未超配额
- **THEN** 消息排队至上线领取或 TTL 到期，过期后标记失败且不再投递

#### Scenario: 队列容量超限
- **WHEN** 新消息会使席位超过队列容量
- **THEN** Hub 返回容量错误并记录审计，不静默丢弃旧消息

#### Scenario: GW-006-S03 解散与在途结算
- **WHEN** AG自动或主动解散，存在未领取、已领取消息及离线的两侧成员
- **THEN** 新普通投递/领取拒绝，未领取转失败，已领取不重投/伪完成，两侧通知经幂等outbox持久等待；合法结算ACK不续组

#### Scenario: GW-006-S04 断流未确认批次重取
- **WHEN** 收件响应流中断，该批仅部分delivery已持久ACK，客户端用新RPC请求ID重收并重交可能已提交的ACK
- **THEN** 未ACK在领取租约释放/到期后以同delivery_id/generation再给，已ACK不回退；客户端去重处理，重复ACK幂等，不以返回即标送达

#### Scenario: GW-006-S05 发送结果未知与稳定去重键
- **WHEN** send_message响应断流，客户端换RPC请求ID但以相同client_message_id和正文重试，或使用相同键提交不同正文
- **THEN** 前者返回既有受理/unknown事实不新建消息，后者拒绝；scope限定同发送席位与AG代际

### Requirement: GW-007 delivery_ack v2 绑定
Hub MUST 使用 delivery_ack v2 的事务确认、席位绑定、generation 与单调状态语义；HTTP adapter 扩展不得改变现有 driver/helper 路径。

#### Scenario: 合法 ACK
- **WHEN** HTTP 席位为自身领取的 delivery 提交匹配协议、处理模式及 generation 的确认
- **THEN** Hub 在事务提交单调状态后才返回确认

#### Scenario: 伪造或重放 ACK
- **WHEN** ACK 指向其他席位投递、generation 不符或状态回退
- **THEN** Hub 不改变投递状态并拒绝或忽略 ACK

### Requirement: GW-008 全名、编组与提及
Hub MUST 为每次合法接入自动分配持久且永不复用的全局AG序号，临时组恰含外部Agent与所选目标。短名MUST匹配 `[a-z0-9]{1,8}`，Agent名为短名＋下划线＋AG名；全局AG唯一保证组合名唯一，不另要求用户查重。MVP只允许显式全名且本AG内寻址/提及，不接受原始key或递归目标其他组；正文不解释为执行指令。组创建及有效消息/ACK后续到本次＋3天，到期/解散即拒绝普通通信；纯轮询不续组已定（㊹-补6）。

#### Scenario: 授权全名寻址
- **WHEN** 外部席位发送给授权编组内有效全名
- **THEN** Hub 将普通消息路由到解析目标并沿用既有提及规则

#### Scenario: 跨组或原始 ID 寻址
- **WHEN** 客户端寻址未授权编组名称或传入内部 key ID
- **THEN** Hub 拒绝请求且不泄露目标是否存在

#### Scenario: GW-008-S03 并发创建与重启序号
- **WHEN** 同一或不同使用方并发创建AG、重试同一创建意图并重启服务
- **THEN** 同幂等意图只一个结果，新AG序号全局唯一且不复用，每AG恰两成员，回滚允许空号

#### Scenario: GW-008-S04 组合名与多目标
- **WHEN** 使用方以合法同短名为两个目标各建AG，或提交大写、第9字符、空串、Unicode短名
- **THEN** 前者得到不同AG与唯一组合名且无需人工查重；后者拒绝且不建组

#### Scenario: GW-008-S05 三天边界与重启清理
- **WHEN** 未到3天的AG接收有效消息或首次有效ACK，或AG在3天整仍无使用且Hub重启
- **THEN** 前者持久续至本次＋3天，后者立即禁止通信并由可恢复清理任务解散，不因晚扫描延长权限

#### Scenario: GW-008-S06 仅轮询不续组
- **WHEN** 有效令牌只持续空轮询/查清单而无消息或有效ACK
- **THEN** 按已定口径（㊹-补6）续令牌但不续AG，AG在3天到期

#### Scenario: GW-008-S07 解散与迟到续期
- **WHEN** 旧代际发送/ACK与解散竞争或解散后重放
- **THEN** 栅栏阻止复活AG，重复事件不延寿、不重复通知，不影响目标其他组

### Requirement: GW-009 来源校验、审计与限流
Hub MUST 按席位和来源实施限流并审计授权、投递、收件、ACK、吊销及拒绝事件；不得记录明文 token。MCP Origin 校验 MUST 遵循 MCP 规范及 R52 受信来源策略，且来源许可不得替代身份授权。

#### Scenario: 限流与安全审计
- **WHEN** 席位超过请求/消息/并发轮询限额或发生拒绝/吊销
- **THEN** 服务端按策略限流并生成不含凭据明文的审计记录

#### Scenario: 来源通过但身份无效
- **WHEN** 请求来源通过 R52 校验但 Bearer 身份缺失、无效或无权
- **THEN** Hub 仍拒绝请求

### Requirement: GW-010 现有席位兼容
新增HTTP席位MUST与helper隔离，不改sessionHelper、send.py或旧在线/投递语义。Console MUST 在Agents下单列临时编组，仅列当前登录者名下任一Agent所在AG；属性页显示成员、创建者归属名、创建/最后使用/预计解散时刻。解散MUST由相关成员主人或平台管理员二次确认并经服务端现核、写审计；到期清理与主动解散共用代际栅栏、两侧通知及在途处理，不波及普通组。

#### Scenario: 现有 helper 回归
- **WHEN** 已注册 helper 按原协议收发和确认消息
- **THEN** 行为与变更前一致，且不需 HTTP token

#### Scenario: 编组内在线清单
- **WHEN** 外部席位查询在线清单
- **THEN** Hub 仅返回其授权可见编组内的规范全名、类型和在线状态

#### Scenario: GW-010-S03 只列相关AG
- **WHEN** 登录者名下Agent在AG-A，另有无关AG-B，且登录者曾创建AG-C但已无名下成员
- **THEN** Agents下独立栏只列AG-A，不返回或渲染AG-B/AG-C；删服务端相关过滤的变异必须红

#### Scenario: GW-010-S04 属性字段与合法解散
- **WHEN** 相关Agent主人或平台管理员查看AG属性并二次确认解散
- **THEN** 展示两成员、创建者归属名、创建/最后使用/预计解散时刻，服务端现核后审计解散，通知两侧并按在途规则结算

#### Scenario: GW-010-S05 无关人和取消确认
- **WHEN** 无关非管理员直接调用解散，或合法用户取消二次确认
- **THEN** 前者服务端拒绝，后者不发送变更请求，两者均不修改AG或成员

#### Scenario: GW-010-S06 普通组隔离与到期竞态
- **WHEN** 清理worker扫描到期AG或属性页操作时该AG已由别人解散
- **THEN** 普通组不变，AG终态幂等、同事件通知不重复，属性刷新显示终态且不允许继续通信
