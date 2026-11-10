## Why

MCP 一期的 AG 双向通信目前经过多个不一致的正文和 JSON 限制，导致同一消息在网关、投影、回信和 helper 上的可接受大小不同。本变更按业主 D-10 改定统一 UTF-8 业务正文上限并验证超限零投递，确保完整传输而不靠截断降级。需求来源：rulings ㊿-补42、组长 S-MCP-P1-LEN 派单 24cd0f824932，2026-11-10T06:00Z 接管裁定。

## What Changes

- 业务正文含服务器抬头上限统一为 1048576 UTF-8 字节。
- JSON 请求与 IPC 行按最坏六倍转义加 65536 元数据预算。
- 三档、三编码、三路径验证与保护撤除变异。

## Capabilities

### New Capabilities
- mcp-message-length：AG 双向消息长度与拒绝语义。

### Modified Capabilities

## Impact

internal/extshared、extseat、mcpgw、m8 和 helper；无迁移、部署或在线配置变动。

### 待确认假设

无；不调整 WebSocket 16MiB 帧上限。

### 待 PM 决策

无。
