# Antigravity 回合事件驱动

## Why

dwgemini4 目前以旧 CLI 模式接入 sessionHelper。DingWei 页面可以确认消息已送到 helper，却看不到 Antigravity 的回答和回合结束，因此使用者无法判断任务是否完成。Antigravity 已提供 ACP 标准输入输出协议，可在不读取屏幕文字的情况下获得回答增量、工具事件、完成原因和取消结果。本变更让该席使用现有 driver 事件链路，恢复可见、可核对的回合状态。需求来源：本次用户会话中 dwgemini4 的两条“旧版 helper 已收／状态无法确认”记录。

## Scope

- 新增 Antigravity ACP driver，接入现有 DriverAdapter；不修改 Hub、View 或其它 CLI driver。
- 按信封 ID 持久去重，保留未知投递，不自动重发。
- 将回答增量和终态交给现有 session_event 链路。

## 待确认假设

- 部署席的 Antigravity ACP 与已实测的 1.5.13 事件结构一致；部署后需真实消息复验。

## 待 PM 决策

- 无。此次只接入现有 driver 契约。
