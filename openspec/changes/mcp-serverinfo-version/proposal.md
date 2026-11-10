# 提案：统一 MCP serverInfo 版本来源

## Why
当前 2026 serverInfo 的版本仍是历史分支标签，与当前发行版本不一致；2025 initialize 也保留独立的名称与版本字面量，可能随发行号更新而漂移。本变更让两种协议的初始化/发现结果共用网关身份常量，并将版本常量直接绑定 internal/version.Version，使发布时只需维护版本包的一处来源。工具列表与调用响应结构保持既有边界，2025 的列表和调用仍无 2026 metadata。

## 来源
派单：S-MCP-SERVERINFO-VERSION（r57 可选候选）；版本来源为 `internal/version.Version`。

## 范围
仅修改 mcpgw 的 serverInfo 版本来源与相应测试，不修改协议结果结构、工具行为或外部依赖。

## 待确认假设
无。

## 待 PM 决策
无。
