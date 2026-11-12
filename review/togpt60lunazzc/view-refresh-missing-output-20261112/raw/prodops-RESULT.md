# S-VIEW-REFRESH-MISSING-OUTPUT｜现网历史数据面只读核查

核查时间：2026-11-12T15:04–15:10Z。目标：`opus50tc2k.zzc`；历史库使用短会话名 `opus50tc2k`。窗口为 UTC `[2026-11-12T12:40:00Z, 2026-11-12T14:40:00Z)`，上界不含。全程只读；没有查询或导出正文。

## 库与 schema

- 现网 Hub：`VM-4-13-ubuntu`，`dingwei-dw123-hub.service` active、PID `2278468`，版本 `0.2.3.37 329eb9f9c45d2d785a0429b01eaa0fa5264fe15d`。
- 进程环境 `WP_DATA_DIR=/var/lib/dingwei-dw123`、`DW_SESSION_HISTORY_PERSIST_ENABLED=1`。历史为独立 SQLite 库 `/var/lib/dingwei-dw123/workpulse-history.db`；`mode=ro` 连接并设 `PRAGMA query_only=ON`，库内 runtime `enabled=1, initialized=1`。本次会话历史查询未使用 PG。
- 相关表：`session_history_event`（主键 `owner_key,agent_key_id,session_name,generation,seq`，含 `event_id`、`received_at_ms`、`event_type`、`payload_json`）、`session_history_generation`、`session_history_gap`、`session_history_runtime_state`、`session_history_migrations`。五张表完整现网 schema 见 `raw/history-metadata.txt`。

## 窗口统计

底层 `event_type=session_event` 共 47 条；下表 `kind` 来自 `payload_json.kind`。session/turn 去重按各 kind 的 `payload_json.session_id`、`payload_json.turn_id` 计数，二者均无空值。

| kind | 条数 | session 去重 | turn 去重 |
| --- | ---: | ---: | ---: |
| `user_input` | 14 | 1 | 8 |
| `assistant_text` | 12 | 1 | 7 |
| `tool_call` | 13 | 1 | 7 |
| `state_change` | 8 | 1 | 7 |
| `input_posted` | 0 | 0 | 0 |
| `delivery_update` | 0 | 0 | 0 |

8 条 `state_change` 的 `data.subtype=success`、`data.change=turn_completed`，即本窗口终态类 8 条、7 个不同 turn。14 条 `user_input` 的 `data.lifecycle` 为 queued 7、admitted 7。全部 47 条合计涉及 2 个不同 `session_id` 和 8 个不同 `turn_id`。这些是存储记录计数，不能单独证明某次 View 刷新是否成功渲染。

窗口内只有 47 条，因此“最新 50 条”实际返回全部 47 条；按 `received_at_ms DESC, seq DESC` 排列，`kind | UTC 时刻 | seq` 原文见 `raw/history-metadata.txt`，范围 `seq=3907–3953`，最晚 `2026-11-12T14:35:08.619Z`。

## View 首屏历史上限

现网二进制版本所指向的 Git 提交 `329eb9f9c45d2d785a0429b01eaa0fa5264fe15d` 中，View 刷新使用 `GET /view/{sessionName}/history`，首屏没有 `before` 参数。Hub 的 SQLite Page 查询固定 `ORDER BY seq DESC LIMIT 100`；返回编码后另受 `4<<20`（4 MiB）大小限制，因此实际返回条数可能少于 100。代码原文片段见 `raw/source-limit.txt`。

## 取证边界

只查询 schema、枚举值、计数、时间和 seq；未导出 `payload_json`、`text`、`data.input` 或任何消息正文。没有调用带认证的首屏 HTTP 接口，也未观察浏览器网络响应，故未据此判定页面缺失的原因。一次 SSH 建连超时后只读重连成功；未写库、未重启、未改现网。
