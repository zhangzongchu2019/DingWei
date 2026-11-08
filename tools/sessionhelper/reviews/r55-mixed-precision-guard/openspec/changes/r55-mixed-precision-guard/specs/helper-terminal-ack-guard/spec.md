# Mixed Precision Guard

## Purpose

本变更把helper可能提交Hub必拒的混精度终态改为保留原回执与pending，目的是使本地终态和可确认回执保持一致。涉及helper lifecycle及Adapter，不影响Hub校验、schema、冻结预算、原生时刻或ACK原文。

## ADDED Requirements

### Requirement: HTG-001 比较原wire边界
helper MUST 在写processed前，以同事务内原injected payload发生时刻为边界；已确认时用原admitted按既有wire编码恢复。候选processed用同编码比较，早于原边界不提交。outbox保守按可能已发处理，不修改payload，不clamp原始时间、不放松Hub。

#### Scenario: HTG-001-S01 pending及confirmed
- **WHEN** 原injected为微秒观察，native terminal为更早整数秒，无论injected待确认还是已确认
- **THEN** 原回执逐字不变、原入场不变，不写processed或生成其ACK

#### Scenario: HTG-001-S02 wire相等
- **WHEN** 两浮点数有微小差异但既有UTC微秒编码相同
- **THEN** 不仅按未舍入float误拒符合wire边界的完整终态

### Requirement: HTG-002 原因与证据保持
阻止提交时helper SHALL 保留原pending态，持久记录native_terminal_precedes_acked_injected，processed_at/outcome不补造，返回False供Adapter停止terminal观察。原native proof/time保持原文；不改1s/15s/200、ACK协议或代次，不加迁移。

#### Scenario: HTG-002-S01 撤护栏
- **WHEN** 在隔离进程撤去wire比较屏障
- **THEN** mixed pending判据变红；原代码恢复后矩阵转绿且8个控制判定不变

### Requirement: HTG-003 pending收口
相同冲突证据或重启 MUST 不产生必拒processed ACK、额外原生查询或重新执行任务。后续独立一致原证据可按原身份/turn/context校验终结并清原因；若一直不一致 SHALL 交人工对证，不能用等待时刻替代原终态。既有queued/injected可靠重放保留。

#### Scenario: HTG-003-S01 后续一致证据或人工
- **WHEN** pending后同一turn获得完整一致原证据，或只有原冲突缓存
- **THEN** 前者可生成合法processed ACK，后者仍pending且无无效ACK循环，明确人工核对而不自动改时刻
