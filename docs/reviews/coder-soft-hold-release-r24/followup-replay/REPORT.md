# HOLD-RELEASE R1：旧事件重报幂等与释放收敛

修复交审，尚待 opus50ts 独立复审；不是作者自签 PASS。

仓库 `git@git.code.tencent.com:wego-ai/dingwei.git`，分支 `review/coder/hold-release`。
冻结产品/测试钉 `aca4d9ddec1ea4bbf13e4447b3b1283a872573a1`，tree `548f0b73161ed1b44aa87ea6aec7434b83519247`。
前件 `82743cb651b68a9cbd3a429d6f7c41a2bd55de94`，正式意见 `review/opus50ts/seat-handoff-20261014@7aa8a555efcd1ee69d2bdd15e7c65c1ecf838b1a` 的 R1。
本轮相对前件只改 `internal/hosted/node_quota.go` 并新增 `softquota_replay_test.go`；交件后继仅追加本目录证据。原79成员清单和历史报告保持不变。

## 行为与边界

原来的事件审计/outbox 已幂等，但事件每次重报仍把席位置为 error/soft_quota_held。现在复用同事务、同节点行锁内的审计插入 RowsAffected：只有第一次接受、且同帧没有该席当前快照的 legacy event-only 路径才做事件状态副作用。重复未 ACK 事件仍返回 ACK ID，审计/双目的 outbox 保持原去重，不把历史事件当成当前 hold。

同帧的受信当前席位快照优先。无 hold、合法 last_release_id 和准确既有 owner release_requested 意图同时满足时，仅把 stopped/error 且 detail 精确为 soft_quota_held 的记录收敛为 stopped/soft_quota_released。error 只接纳这一种历史 quota-held 错误，普通 Engine 故障的 detail、destroyed/disabled 均不清除。当前 hold B 非空时不会走解除确认，即使还带旧释放 A 和旧事件 A，也保留 B（有/无 B 事件两格）。没有 owner 意图或代际不匹配仍不能解除。

不增加命令发送、重发、自动启动、凭据恢复或删除数据；仍需之后单独的人类 restart，继续经过既有 soft/B 门。节点实现、权限、通知发送器、监控页面、Schema、helper、VERSION、依赖锁、NodePolicy 和容量计算均不改。这里处理受信当前容量快照及合法持久事件，不声称抵抗恶意节点伪造快照。

## 本轮作者本地验证

白名单环境、私有 HOME/GOCACHE、短 TMPDIR，无外部 DSN；pgtest 自起并清理本地临时 PG，无真实 Docker/宿主/密钥/Agent/通知动作。

* 冻结 aca4d9d + 审查方 fixture：定向 hosted race **16 顶层 / 30 子格，rc0、0fail/skip，57.168s**。其中14顶层为提交内测试，2顶层为字节不变的审查方探针（事务原子性与原 R1）。最终日志 SHA256 `ce0de3d20e9dd0b18f16eb543b0421b7cea421a9fe99a7bc903ba9a9d3e23748`。
* 成功/未知结果各覆盖已接收事件、解除后首次接收事件、旧实现遗留 held error；三次重报后 ACK 清除，再两次干净帧，始终仍停止且解除成功。事件、owner意图、确认审计各一次，owner/ops 通知各投递一次（本地 Store callback，不是实际 Feishu/Agent 送达）。Gateway命令次数不增加。
* 新 B hold（含无 B 事件）、普通故障（含首次迟到旧事件）、销毁/禁用、无 owner 意图、错误代际均保持边界。legacy 无快照事件第一次产生 hold，重报不覆盖之后的普通错误。
* 四组**相同断言的源撤修 0/1/0**：恢复827原文件→原审查探针重现 error/soft_quota_held；仅撤审计收据门→legacy副作用重放红；仅撤同帧快照优先→普通故障被旧事件覆盖红；仅撤 error-held 收敛→成功/未知的历史错误均卡住红。均是断言红，没有编译/环境红；撤修后逐字恢复。
* Go list **64包，无docs包**；夹具为 `.go.fixture`，不成为独立Go测试包。22源钉中20项逐字沿用前件，新增测试与 node_quota.go 为本轮差集；SOURCE.json另核6个不变对象。

原1a9110c2的canonical48+2、五包全race、页面13及其他权限/持久化读数仍仅归原精确源。遵照正式意见，本轮不重复无关长门，不把旧全门改标为 aca4d9d 全门；浏览器/节点/M8源码不变。探索期间有一次dirty源编译绿，完整保留但EXPLORATORY.json明确排除于最终读数，launch-time父提交不是执行源钉。

## 复跑与证据

`SOURCE.json`钉源码和不变对象；`CONTROLS.json`逐项记录源哈希与原/撤/复原读数；`raw/`含JSON流、结果与真实断言。审查方原文件来自7aa8a555的`outbox-rollback-and-replay_test.go.fixture`，SHA256 `9bba003695ba9380fd0cc3ac2e24229446f5f7cc34a44cc016f17fe008e4890c`；复制到隔离checkout的`internal/hosted/zz_review_replay_test.go`运行，未改字节。

可在自己的短私有TMPDIR/HOME/cache白名单环境下，对 aca4d9d 加上述 fixture，执行 `go test -p 1 -race -json -count=1 -timeout=15m -run '^Test(SoftHold|SoftQuota|SoftNode|Quota|Opus)' ./internal/hosted`。不要继承任何外部PG DSN，不使用本席已删除的环境目录。`run_replay.py`是作者本地复现实录，路径需按各自隔离目录设置，不是宿主执行单。

CLEANUP.json记录：cache314266677B→go clean232B→删除0；HOME83050353B、worktree31940253B、短TMP均归零，0自有进程。只保留源码与证据；共享module cache不动。此交件不是合并、运行环境验收、部署/试点GO，dataconn四席冻结及所有业主前置门不变。
