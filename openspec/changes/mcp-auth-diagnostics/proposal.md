# 提案

## Why

现网授权失败只返回 access_denied，接入者无法分辨目标填错、飞书账号不符、授权过期或上游暂时故障，运维也缺少安全的关联线索。本变更补充字段旁的操作说明、按原因码呈现的安全失败页和最小结构化日志，使用户能自助修正或重试，并让运维按 attempt 前缀定位，同时不泄露凭据与主体信息。

## What Changes

- 为短名和目标 Agent 字段增加明确的语义与单目标通信说明。
- 为 callback 失败增加固定原因分类、HTML 说明及兼容 OAuth error 的 error_description。
- 为每次 callback 失败记录不含敏感数据的结构化诊断字段。
- 将无法解析的目标分类传递到安全诊断元信息中。

## 需求来源

业主派单 S-MCP-AUTH-DIAG（2026-11-09）；基线源码为 [`0de00bc45d2716cb7ba22b5642dca6f9e73f229f`](https://git.code.tencent.com/wego-ai/dingwei/commit/0de00bc45d2716cb7ba22b5642dca6f9e73f229f)，实现范围为该提交的 `internal/feishulogin`、`internal/mcpgw` 与 `internal/extseat`。

## 范围与影响

仅实现授权页文案、失败分类/说明/日志及相应测试与规格，不部署、不改成功授权协议。回调页面保留既有 OAuth error 值；内部错误只用于固定原因映射。

## 待确认假设

- 对无法证明目标不存在但签发拒绝的情况，按“存在但不可用”分类；目标解析失败归入“不存在”。

## 待 PM 决策

无。
