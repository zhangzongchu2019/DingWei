# view-unknown-ux Specification

## Purpose
本变更把 View 未知投递从含混的排队提示改为明确的未知状态与可撤回决策，目的是降低重复执行风险并支持本页整理。涉及 `view-service` renderer，不影响 Hub、helper 或投递事实。

## Requirements

### Requirement: VUX-001 未知状态使用明确措辞

页面 MUST 对 `model_admission_unknown` 条目显示“已送达，Agent 忙时无法确认是否已接收”，等待区与“查看未知条目”均适用；此状态不得显示“已排队 X 秒”。

#### Scenario: 忙时未知条目不显示排队时长

- WHEN 页面收到 `model_admission_unknown` 条目并经过排队计时刷新
- THEN 等待区和未知条目列表显示指定未知措辞且不出现排队时长

### Requirement: VUX-002 重发须确认且服从已处理事实

页面 MUST 在 `model_admission_unknown` 或 `unconfirmed_retryable` 条目的重发动作前显示确认，说明“可能已被处理，重发可能导致重复执行”，默认选择不得重发。页面一旦收到该 delivery 的 processed 或已回复事件，MUST 移除重发入口并拒绝尚未完成的重发。

#### Scenario: 默认不重发

- WHEN 用户打开重发确认后直接按默认动作或关闭对话框
- THEN 页面不发送重发请求

#### Scenario: 明确确认才重发

- WHEN 用户明确选择重发且该 delivery 尚无已处理或已回复事件
- THEN 页面最多发送一次重发请求

#### Scenario: 确认期间收到已处理事件

- WHEN 重发确认打开后、确认发送前收到该 delivery 的 processed 或已回复事件
- THEN 页面关闭或使确认失效，不发送重发请求且不提供重发入口

### Requirement: VUX-003 不再跟踪仅改变本页展示

页面 MUST 提供“不再跟踪”动作，并仅在本页隐藏对应未知条目；该动作不得发送任何请求或修改 Hub 投递状态。

#### Scenario: 本地隐藏

- WHEN 用户选择某未知条目的“不再跟踪”
- THEN 等待区和未知列表不再显示该项，且网络请求记录不增加

### Requirement: VUX-004 过期未知条目收起

页面 MUST 使用一个 24 小时时限常量；超过该时限的未知条目从等待区收起，在“查看未知条目”列表中保留并标明“已超 24 小时”。时间源 MUST 可由浏览器回归注入。

#### Scenario: 过期项仍可查看

- WHEN 注入时钟使未知条目年龄超过 24 小时并触发刷新
- THEN 等待区不显示该项，未知列表仍显示并标“已超 24 小时”

### Requirement: VUX-005 未变化的未知列表保留 DOM

View MUST 仅在未知明细集合、显示文案、过期标记或辅助计数变化时重建“查看未知条目”列表；纯定时刷新不得替换未变化的列表行或控件。

#### Scenario: 周期刷新保留焦点控件

- WHEN 用户聚焦未知条目的“不再跟踪”按钮，注入时钟前进 3 秒且期间没有事件或条目状态变化
- THEN 按钮保持同一个 DOM 节点并继续获得焦点

#### Scenario: 条目状态发生变化后更新列表

- WHEN 未知条目进入 24 小时过期状态或用户选择“不再跟踪”
- THEN 列表显示更新后的文案/行集合与计数，且下一次相同状态刷新不再替换未变化节点

### Requirement: VUX-006 摘要数字对应本页可见明细

View MUST 将未知列表摘要 N 设为本页当前可见明细行数。helper 聚合数大于本页明细数时，页面 MUST 在列表外说明 helper 上报数与本页明细数，不得把聚合差额作为可见行计入 N。

#### Scenario: helper 聚合数高于明细数

- WHEN helper 上报 5 条未知项而本页只有 2 条明细
- THEN 摘要显示 2，列表有 2 条未知明细，并显示 helper 上报 5 条、本页明细 2 条的辅助说明

#### Scenario: 本地不再跟踪后计数下降

- WHEN 用户将两条可见未知明细中的一条标记为“不再跟踪”
- THEN 摘要和列表行数都从 2 降为 1，helper 聚合辅助说明仍明确展示原上报数与当前可见明细数

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
