# 任务

- [x] R60 定锚后基于新分支实现；合入 main `206d41b26dab646c0e50a111eed5a45ccf7c72f6` 无冲突。基点 `638cb324`，设计原文 `6a60a1a625ba36e8240866d641050ed83aaa64ef`。实现锚 `c0ec52db044d92a539a5d7010e597d7338787fc1`。
- [x] Hub 握手响应头、helper 能力字段与兼容矩阵：`evidence/compat/` 四格实际 helper ACK 读数；真实新 Hub 握手与接收端协议格见 `evidence/m8-store-count3-final.log` 中 `TestDeliveryACKV2QueueFullRequiresNegotiationAndCreatesDurableNotice`、`TestDeliveryACKV2QueueFullIgnoredWithoutHelperCapability`、`TestNewHubOldHelperQueueFullKeepsRetryableBusyPath`。四格 probe 通过 helper 方法执行、以握手头有无表示 Hub 代际；没有声称是四套独立 Hub/helper 进程部署。
- [x] ACK v2 精确 `failed/queue_full/retryable=false` 校验与终态事务：m8/store 三轮全包日志及 M02、M03 变异；一次允许的已知 SQLite interrupt 见 REPORT。
- [x] 终态事务与 durable sender notice 同提交；重复 ACK 使用确定 notice ID。session 使用 `failure_notice` delivery 队列；Feishu 使用既有 durable `message` 队列。ACK/notice 事务回滚、固定文案和正文隔离由 store 测试覆盖。
- [x] notice 重启/离线由既有 durable drain 与 retry/dead-letter 路径承担；queue-full session 与 Feishu 行均有对应回归。没有新增 generic notice 表；非 session/Feishu 已支持来源的处理边界见 `docs/reviews/r61-n1b-queue-full/REPORT.md`，这是实现相对 design 的明示偏差。
- [x] 固定发送方文案不带原正文；目标离线沿用既有 durable delivery/消息队列重试。
- [x] 9 把删保护变异均在业务断言转红，见 `docs/reviews/r61-n1b-queue-full/MUTATIONS.md` 与 `evidence/mutations/`。
- [x] 构建门：冻结锚 `c0ec52db044d92a539a5d7010e597d7338787fc1` 上 `build.sh --test-only` 通过，两个 helper profile、Python 27／shell 2。
- [x] m8/store `-count=3` 与 PG strict 已执行；m8 仅一次已知名单内 `SQLITE_INTERRUPT`，store 三轮通过；独立 Z-3 五轮 m8 为 4/5 全绿，另一次仅命中另一已知名单格。每轮原文在 `evidence/`。
- [x] OpenSpec 1.16.0 `validate --all --strict --no-interactive`：43 passed／0 failed，rc=0。

主要证据可直接用 `git cat-file blob <blob-id>` 原样取回：完整门日志 `evidence/m8-store-count3-final.log`＝`1bcc0ffe29fd84c748b9b1b570ad4c0a666d160e`；Z-3 汇总 `evidence/z3/SUMMARY.txt`＝`ea34ba6889c0815d5bd30f58fb6c3b7a6b315228`；OpenSpec strict `evidence/openspec-strict-final.log`＝`0c9b982b6c8c09d71c8bb97840193c42c2eab5f5`；四格 helper ACK `evidence/compat/results.txt`＝`f7455bf5d02cdbeed6d9320a29d7e0a8c0330a89`。全部 37 个 evidence 文件的 blob SHA 与文件 SHA-256 见同目录 `SHA256SUMS`。
