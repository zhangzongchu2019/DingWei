# S-VIEW-REFRESH-MISSING-OUTPUT｜标识与真实事件形状合并补查

本件合并记录前项 session／turn 标识补查与本次形状补查。原提交 `4cc99507f9590187472a4dc6a72e3523c8bb357f` 的文件保持原样；标识补查详见 `SUPPLEMENT-SESSION-IDS.md` 和 `raw/session-id-supplement.txt`。本次在 2026-11-12T15:45Z 左右对同一现网 SQLite 库 `/var/lib/dingwei-dw123/workpulse-history.db` 以 `mode=ro`、`PRAGMA query_only=ON` 查询，同一历史键、UTC 窗口 `[2026-11-12T12:40:00Z, 14:40:00Z)`。未输出任何正文值。

## 前项标识结果要点

原窗口 `user_input` 的 `payload.session_id` 为 `""`，其余三种 kind 均为 `83e9a02a-e451-488e-a699-c62fa95ae27e`。`user_input` 与 `assistant_text` 的非空 `turn_id` 交集为 7 个；各自前三值、此前 24 小时的四种 `session_id` 值及首末时刻见上述标识补查原件。原报告的 `session_id` 去重 2 包含空字符串；只有一个非空值。

## 每种 kind 最新两条的形状

`record_bytes` 来自 `session_history_event` 列；字符串长度按 JSON 解析后的 Unicode 字符数统计，并在 raw 同时给出 UTF-8 字节数。完整八棵键路径树（路径、值类型、字符串长度）见 `raw/shape-supplement.txt`。

| kind | 最新 seq／record_bytes | 次新 seq／record_bytes | 顶层 `payload.text` 长度（最新／次新） |
| --- | --- | --- | --- |
| `user_input` | 3950／720 | 3949／649 | 44／44 字符 |
| `assistant_text` | 3952／671 | 3947／1747 | **53／602 字符**（UTF-8 159／1218 字节） |
| `tool_call` | 3951／1000 | 3946／2156 | 0／0 字符 |
| `state_change` | 3953／695 | 3948／694 | 0／0 字符 |

八条样本的 `turn_id`、`session_id`、`delivery_id` 均位于 `payload.turn_id`、`payload.session_id`、`payload.delivery_id` 顶层，而非 `payload.data`。两条 `assistant_text` 的正文位置是顶层 `payload.text`，`payload.data` 是空对象。两条 `user_input` 的 `payload.session_id` 长度均为 0，`turn_id` 长度分别为 32、0，`delivery_id` 均为 39；另三种 kind 的样本 `session_id` 均为 36、`turn_id` 均为 32、`delivery_id` 均为 39。这里仅报告结构与长度，不推断页面故障成因。

## generation、gap 与体量

`session_history_generation` 对应一行：

| generation | next_seq | oldest_seq | newest_seq | gap_through_seq | retained_bytes |
| --- | ---: | ---: | ---: | ---: | ---: |
| `0fda970aaff3905c61684736ec808452` | 4026 | 1 | 4025 | 0 | 5055064 |

同一历史键在 `session_history_gap` 中 **0 行**。原窗口 47 条的 `record_bytes` 总和 **49,673**，最大值 **3,602**。`next_seq` 等 generation 值为本次查询时读数，不是固定版本钉值。

查询过程未导出正文、写库、改配置或重启现网。`raw/shape-supplement.txt` 为全部八条样本的结构化只读输出；未保存 payload 原值。
