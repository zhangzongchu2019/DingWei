# Tasks

- [x] 核对业主 N-1 ruling 与 R60 9818b979 的 helper inbox、busy admission、生命周期及 ACK v2 行为。
- [x] 提交设计供组长审阅；不含产品实现。
- [x] N-1b：组长确认 `queue_full` ACK v2 终态及发件方通知的 Hub 协议落点，并另开分支实现。（结项勾：已拆为独立变更 r61-n1b-queue-full 实现，冻结 ed7259fa，复验 PASS c92efaf2） 凭据见 docs/reviews/r61-closeout/RECEIPTS.md。
- [x] 组长批准 N-1a 范围（2026-11-17T18:26:32Z）。
- [x] 完成 N-1a helper 实现与测试。
- [x] 增加 Codex/Claude、FIFO、重启恢复、unknown 隔离及删保护变异测试。
- [x] N-1b blocked：容量拒收/Hub 通知及旧 Hub 能力协商测试。（结项勾：同上，由 r61-n1b-queue-full 覆盖） 凭据见 docs/reviews/r61-closeout/RECEIPTS.md。
- [x] OpenSpec strict、helper 双 profile、Go roundtrip 与完整 build 门通过；N-1a 已交件。
