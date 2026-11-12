# S-VIEW-STALE-BACKLOG 交件报告

## Hub 只读调查

以下均为基线 `329eb9f9`（本分支祖先链上的 Hub 代码）代码事实；“没有持久终态”是由这些分支共同推出的结论。

- `internal/store/session_delivery_ack.go:124-141`：收到 ACK 的 `admission_unknown` 时，若先前状态为 `delivered`，持久行更新为 `state='admission_unknown'`、`last_error='model_admission_unknown'`。代码事实：此处建立未知状态，没有设置过期终态。
- `internal/store/session_delivery_ack.go:90-122`：后到的 `injected` ACK 可把先前 `admission_unknown` 更新为 `injected`；之后只有匹配 turn 的 processed ACK 才把 `injected` 更新为 `processed`。代码事实：迟到正证据的更正路径存在，未来 `expired_unknown` 设计需保留其审计/优先级语义。
- `internal/m8/delivery_ack_v2.go:78-80`：Hub 调用 `rememberAdmissionUnknown` 并发布 `helper_queued` / `model_admission_unknown` 阶段事件。代码事实：该 ACK 通过视图事件暴露为未知状态。
- `internal/store/sqlite.go:2198-2209`：`ExpireSessionDeliveries` 的协议 2 排除条件包括 `delivered`、`injected`、`admission_unknown`。代码事实：该过期器不会将 ACKv2 的 admission_unknown 行转成常规过期状态。
- `internal/store/failure_notice.go:149-165`：离线失败 sweep 查询限定 `COALESCE(ack_protocol,0)<>2`。代码事实：这条 sweep 不会为 ACKv2 unknown 生成失败通知。
- `internal/m8/fresh_target_delivery.go:130-168`：分钟循环触发过期与失败 sweep。结合以上过滤条件可推断：周期调用存在，但它不会终结 ACKv2 unknown。
- `internal/m8/terminal.go:616-643`：`convergeAdmissionUnknownAfterTurn` 依据进程内 map 中不超过 24 小时的记录发布 `unconfirmed_retryable`，删除该 map 项；函数没有调用 store 更新 unknown 行。代码事实：这是回合结束时的视图收敛，不是持久终态转换。
- `internal/m8/terminal.go:645-658`：`rememberAdmissionUnknown` 使用进程内 map 记录时间。代码事实：这段 24 小时窗口依赖进程内记忆，不是跨重启的持久过期时间。
- `internal/m8/view_retry_v2.go:119-125`：当前重发入口只接受 `admission_unknown` 且匹配相应错误和协议的记录。代码事实：新状态若要封住重发必须同步修改校验逻辑。
- `internal/m8/hub.go:4692-4706`：入站 ACK 状态校验的允许集合没有 `expired_unknown`。代码事实：未来新增状态需要决定如何兼容/验证迟到 ACK。

**结论（推断）**：目前没有发现 Hub 将 ACKv2 `admission_unknown` 持久转换为 `expired`、`closed` 或等价终态的路径。View 的本地隐藏只改变当前浏览器显示，不改变 Hub 状态，也不通知其他浏览器。

## `expired_unknown` 设计草案（未实现）

建议从持久化状态进入 `admission_unknown` 的 `updated_at` 起算 24 小时；周期任务仅在仍无 `injected` / `processed` 正证据时把它转换为独立分类 `expired_unknown`。不把它标成 `dead` 或普通失败，也不发失败通知。View 显示“未知状态已超时，仍不能确认是否执行”，默认不显示重发按钮。保留该记录与变更审计；迟到的正 ACK 可把状态更正为实际 `injected` / `processed`，并发布可审计的更正事件。重发校验、ACK 状态验证、数据库迁移、事件 schema、View 映射、统计与清理策略需共同更新。

- 好处：未知积压可跨重启收敛；超时后可明确封住重复重发入口；多浏览器显示一致。
- 代价：涉及 Hub 持久模型、迁移、周期 sweep、迟到 ACK 的状态优先级/更正语义、View 和观测指标；时钟异常或误扫可能暂时隐藏仍可重试项；保留记录意味着不能简单按普通过期删除。

这只是设计草案，本单没有修改 Hub。

## 本单改动与判据

- 修改范围：`view-service/renderer-canary-sse.html`、`view-service/renderer-delivery-status-regression.js`、本 OpenSpec 变更与本目录证据；Hub/helper/版本位/runner 清单未改。
- 新增行为：retryable/retried 统一未知文案并排除队列计时；按页面会话与 delivery ID 本地保存“不再跟踪”标记，TTL 30 天、全局最多 500 项；批量操作需显式确认且默认取消；既有 24 小时收起规则保留。
- 定向浏览器回归：见 `targeted-browser.log`。
- 语义变异原始输出：见 `mutations/`；每刀均以 rc=2 在对应检查名处失败。
- 全量浏览器 runner：40 套均 rc=0、aggregate_rc=0，2026-11-12T09:11:51Z–09:20:13Z；`renderer_delivery_status` 20/20。详见 `browser-receipt.txt`。
- runner 主进程 PATH 的 Node 为 v22.23.1。范围内未改 runner；其中 `mcp_zero_consent` 的子脚本自设 `/usr/bin:/bin:/home/dev/.local/bin`，实际子进程仍用系统 Node v18.19.1，使用 `NODE_PATH=/tmp/vux-playwright18/node_modules` 兼容包；该四链套件仍 rc=0。此边界记录在 `browser-runner-command.log`。
- run-build-tests：rc=0，`BUILD_TESTS_OK helper_profiles=2 python=27 shell=2`，2026-11-12T09:11:51Z–09:20:58Z。
- 定向回归：20/20 PASS，rc=0，2026-11-12T09:12:28Z–09:12:30Z。
- 六项语义变异均 rc=2，并分别在 `RETRYABLE_BACKLOG_NO_QUEUED_TIMER`、`BULK_UNTRACK_DEFAULT_CANCEL`、`BULK_UNTRACK_CONFIRM_IS_LOCAL`、`UNTRACK_PERSISTS_ACROSS_RELOAD`、`UNTRACK_STORAGE_EXPIRES_AFTER_30_DAYS`、`UNTRACK_STORAGE_CAP_500_KEEPS_NEWEST` 变红；原始输出见 `mutations/`。
- OpenSpec strict：37/0；md-render sync：6 段同步；md-render parity：103/103；新增页面行版本号扫描：76 行、0 命中；均 rc=0。
- `git diff --check`：实现初检 rc=0；暂存后再次检查。读数汇总见 `validation-summary.log`。

全量 runner 的 `renderer_c2` 额外打印仓内 known-defect `T-166` 仍有 4 项；该 suite 汇总为 `HARD PASS 25` 且 rc=0，详情在完整 runner 输出记录/收据中。这些提示与本次 View 改动无关，没有作为本次新增判据的通过依据。
