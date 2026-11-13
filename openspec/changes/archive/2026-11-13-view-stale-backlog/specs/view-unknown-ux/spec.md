# 未知投递积压体验

## Purpose

本变更把长时间未知重试状态从误导性排队计时改为清晰的送达不确定文案，并将单项与批量整理从页面内存扩展为按会话隔离的本地持久标记。涉及 View renderer 及其浏览器回归，不改变 Hub、helper、投递状态或 24 小时未知条目收起事实。

## ADDED Requirements

### Requirement: VUX-007 retryable unknown 不显示排队时长

View MUST 对 `delivery_status` 为 `unconfirmed_retryable` 或 `retried`，以及 `reason` 为 `unconfirmed_retryable` 的条目统一显示“Agent 忙时未能确认接收，可能未送达”。等待区与“查看未知条目”均适用；这些条目 MUST NOT 显示“已排队 X 秒，尚未注入会话”计时文案。

#### Scenario: retryable 与 retried 积压

- WHEN 等待区收到状态为 `unconfirmed_retryable` 或 `retried` 的 helper_queued v2 driver 条目，并推进计时超过 30 秒
- THEN 显示统一未知文案且不创建排队计时标签

#### Scenario: 只有 reason 标明 retryable unknown

- WHEN 条目缺少 `delivery_status` 但 `reason` 为 `unconfirmed_retryable`
- THEN 等待区和未知列表仍显示统一未知文案且不出现排队计时

### Requirement: VUX-008 本地不再跟踪按会话持久化并限额

View MUST 将“不再跟踪”记录在 localStorage，键由页面会话名和 delivery ID 共同确定，值为记录时刻。页面重载后 MUST 继续隐藏同会话对应条目；超过 30 天的记录 MUST 自动清除；保留记录总数 MUST 不超过实现声明的上限。该行为仅影响本浏览器，不得发送请求。

#### Scenario: 重载后仍不显示

- WHEN 用户标记一个未知条目并重载页面，同会话再次收到相同 delivery ID
- THEN 等待区与未知列表均不显示该 delivery，且没有控制请求

#### Scenario: 30 天后清理

- WHEN localStorage 中的标记时间早于当前可注入时钟 30 天以上
- THEN 页面初始化时移除该键

#### Scenario: 超过上限保留最近标记

- WHEN 本地存在超过上限的有效标记
- THEN 页面只保留时间最新的上限数量，移除更早标记

### Requirement: VUX-009 批量不再跟踪需取消默认确认

View MUST 在未知列表中提供“全部不再跟踪”，并在应用前显示确认。确认对话框 MUST 让“取消”成为默认焦点与默认动作；取消、Esc 或关闭不得改变条目或发送请求，显式确认只把当前仍为未知的本地明细标记为不再跟踪。

#### Scenario: 默认取消不改变列表

- WHEN 用户打开批量确认并执行默认动作或关闭对话框
- THEN 未知列表和等待区保持不变，且没有请求

#### Scenario: 确认批量本地整理

- WHEN 用户明确确认“全部不再跟踪”
- THEN 当前仍未知的明细从等待区和未知列表隐藏，且没有请求
