# extseat-lifetime Specification

## Purpose
本变更把外部临时AG与消息存活期从三天和二十四小时改为七天，目的是满足已批准的外部Agent使用周期并保持独立到期边界。涉及extshared常量与既有PG消费服务，不改变令牌十四天、身份授权、历史存储格式或部署权限。

## Requirements

### Requirement: R62TTL-001 临时组七天
临时AG MUST 从创建或既有合法续期事件起使用168小时窗口；now达到存储expires_at时MUST拒绝使用，不依赖扫描及时性。

#### Scenario: R62TTL-S001 七天组边界
- **WHEN** 独立实例的固定时钟分别位于七天前一微秒、恰七天和后一微秒
- **THEN** 前者允许请求且扫描不解散，后两者拒绝且到期扫描解散；令牌仍是336小时。

### Requirement: R62TTL-002 消息独立绝对到期
新外部inbox/outbox消息及send_key MUST 在受理时持久化七天expiry；令牌和组续期MUST NOT延长该消息。已持久expiry MUST NOT批量回改，无新增迁移。

#### Scenario: R62TTL-S002 两方向七天边界
- **WHEN** 消息已受理且令牌/组随后合法续期，再到达消息七天前一微秒、恰七天或后一微秒
- **THEN** 前者可领取/投影，后两者按message_expired终结且不再交付，原expiry不变。

#### Scenario: R62TTL-S003 存量旧expiry
- **WHEN** 服务重建读取已有三天AG和二十四小时消息
- **THEN** 不回填为七天；消息仍按原时刻到期，新受理消息才用七天。

### Requirement: R62TTL-003 续期事件与令牌边界保留
轮询、重复received ACK及相同client_message_id重试 MUST NOT续组；首次有效ACK和新受理发送 MUST按既有事务续组。TokenTTL MUST保持336小时，token续期不修改组或消息expiry。

#### Scenario: R62TTL-S004 轮询和幂等重试
- **WHEN** 连续Check/Wait/WaitReply、重复ACK或相同ID重试
- **THEN** 组到期值不因此改变；首次ACK或新受理发送才正常续至事件时间加168小时。

#### Scenario: R62TTL-S005 十四天令牌边界
- **WHEN** 组由合法发送保持有效且时钟为token十四天前一微秒、恰十四天或后一微秒
- **THEN** 前者可鉴权续令牌，后两者TokenExpired；鉴权不续组。
