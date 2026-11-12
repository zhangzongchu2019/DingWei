# 未知投递视图

## Purpose

本变更把 View 未知投递从含混的排队提示改为明确的未知状态与可撤回决策，目的是降低重复执行风险并支持本页整理。涉及 `view-service` renderer，不影响 Hub、helper 或投递事实。

## ADDED Requirements

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
