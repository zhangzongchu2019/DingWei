# MCP Gateway Codex Compatibility Baseline

## Purpose

记录现有网关与Codex的协议边界：基线只接受2026-07-28无状态封套、精确回调登记和外部CIMD，Codex0.167.0原生初始化尚不能通过。本文绑定f2a5db5f现状，不把后续兼容实现当作已发布能力，不改变身份与业务授权。

## Requirements

### Requirement: MCB-001 既有封套与身份边界
网关 MUST 独立鉴权2026封套每请求，按现有tenant、scope、audience、AG和Origin处理四通讯工具；基线不接受Codex的2025初始化。

#### Scenario: MCB-001-S01 现有与Codex请求
- **WHEN** 现有2026客户端提供合法头与_meta，或Codex0.167.0发送initialize2025
- **THEN** 前者按权限成功，后者在基线上拒收；不得据OAuth成功声称原生互通
