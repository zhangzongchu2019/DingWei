## Purpose

本变更把外部席位消息从仅进入 MCP/Hub 收件队列扩展为同时向外部主体本人飞书私聊发送只读副本，目的是让授权人及时看到双向往来。涉及 extseat、m8 与 Feishu Gateway，不影响原消息、回信权限和身份生命周期。

## ADDED Requirements

### Requirement: SR-006 外部席位双向飞书抄送

系统 MUST 将外部 MCP 席位与其临时组目标之间的双向消息，以只读提醒抄送给该外部席位授权人的飞书私聊。收件人 MUST 由服务端身份绑定及同一飞书应用的 issuer 映射确定。来源抬头 MUST 复用 `extshared.ExternalSenderHeader`。MUST 默认开启抄送；抄送失败 MUST 与主消息投递隔离，并允许独立重试。相同业务消息重试 MUST 不产生重复抄送。

#### Scenario: 外部席位发给内部目标

- WHEN MCP 外部席位的消息受理并投递给内部目标
- THEN 授权人飞书私聊收到同一消息的只读抄送，消息正文仍完整进入原始投递

#### Scenario: 内部目标回复外部席位

- WHEN 内部目标的回复进入外部席位 inbox
- THEN 授权人飞书私聊收到对应只读抄送，外部 Wait 仍收到原始完整回复

#### Scenario: 抄送截断

- WHEN 消息超过飞书抄送长度限制
- THEN 仅抄送内容截断并标记省略，MCP、Hub 与 extseat 持久消息保持原文

#### Scenario: 抄送失败隔离和幂等

- WHEN 飞书发送失败或业务消息被重试
- THEN MCP 主消息成功状态不被更改，抄送独立重试且同一事件只对应一份抄送

#### Scenario: 飞书回复不回流

- WHEN 授权人回复飞书抄送提醒
- THEN 回复不进入 MCP 外部席位 inbox

#### Scenario: 默认开关

- WHEN 未配置抄送开关
- THEN 系统执行抄送
