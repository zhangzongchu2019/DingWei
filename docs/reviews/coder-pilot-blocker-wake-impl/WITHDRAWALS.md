# D2 指定撤修映射

不可变源 `5c368986c0590a361f090a277a955fd8711ab436`；40 格全部命中指定断言后红。基线及全循环恢复 Go/helper 均 rc0。每格拒编译/语法错误、拒由另一门抢先报红。

| 撤修 | 指定断言 | rc |
|---|---|---|
| initial-current-state | `BLOCKER_INITIAL_STATE_NOT_RECHECKED` | 1 |
| card-cas | `BLOCKER_CARD_CAS_BYPASS` | 1 |
| developer-only-lease | `BLOCKER_REVIEWER_OPENED_DEVELOPER_LEASE` | 1 |
| addressed-reply | `BLOCKER_UNADDRESSED_REPLY_ACCEPTED` | 1 |
| private-receipt | `BLOCKER_WAITER_GOT_TASK_CONTEXT` | 1 |
| request-payload-binding | `BLOCKER_REQUEST_HASH_NOT_BOUND` | 1 |
| immutable-event | `BLOCKER_EVENT_REPLACED` | 1 |
| unknown-is-not-pass | `BLOCKER_UNKNOWN_RESOLVED` | 1 |
| full-approval | `BLOCKER_FULL_MODE_APPROVAL_BYPASS` | 1 |
| lease-identity | `BLOCKER_FORGED_LEASE_ACCEPTED` | 1 |
| lease-expiry | `BLOCKER_EXPIRED_LEASE_ACCEPTED` | 1 |
| concurrent-new-input | `BLOCKER_NEW_INPUT_SWALLOWED` | 1 |
| shared-resume-gate | `BLOCKER_LEGACY_RESUME_BYPASS` | 1 |
| final-authority-check | `BLOCKER_FINAL_RECHECK_COMMITTED` | 1 |
| legacy-text-preserved | `BLOCKER_LEGACY_TEXT_DROPPED` | 1 |
| condition-transition | `BLOCKER_VERIFIED_CONDITION_NOT_WAKING` | 1 |
| outbox-repair | `BLOCKER_OUTBOX_NOT_REPAIRED_IDEMPOTENTLY` | 1 |
| owner-cannot-resume | `BLOCKER_OWNER_IMPERSONATED_RESUME` | 1 |
| old-driver-gate | `BLOCKER_LEGACY_DRIVER_START_BYPASS <nil>` | 1 |
| current-group-pair | `BLOCKER_CURRENT_PAIR_GROUP_AUTH_BYPASS` | 1 |
| waiter-no-private-get | `BLOCKER_WAITER_READ_WHOLE_TASK` | 1 |
| pg-human-lock | `BLOCKER_PG_HUMAN_LOCK_NOT_HELD_THROUGH_SQLITE_COMMIT` | 1 |
| control-origin | `BLOCKER_FORGED_CONTROL_ROUTED` | 1 |
| old-helper-capability | `BLOCKER_UNKNOWN_CONTROL_SENT_TO_OLD_HELPER` | 1 |
| fixed-prompt | `BLOCKER_REPLY_INJECTED_AS_COMMAND` | 1 |
| metadata-whitelist | `BLOCKER_UNTRUSTED_METADATA_COPIED` | 1 |
| strict-version-type | `BLOCKER_BAD_SIGNAL_TYPE_ACCEPTED` | 1 |
| current-recipient | `BLOCKER_STALE_WAKE_ENQUEUED` | 1 |
| helper-full-approval | `BLOCKER_FULL_GATE_BYPASS` | 1 |
| pending-restart | `BLOCKER_PENDING_WAKE_LOST_ON_RESTART` | 1 |
| dispatch-journal | `BLOCKER_UNKNOWN_DISPATCH_REPLAYED` | 1 |
| ambiguous-no-rpc-replay | `BLOCKER_UNKNOWN_WINDOW_OPENED_NEW_TURN` | 1 |
| transient-defer-same-command | `BLOCKER_TRANSIENT_FAILURE_DROPPED_WAKE` | 1 |
| owner-old-plan-paired | `BLOCKER_OWNER_PLAN_BYPASS <nil>` | 1 |
| resolved-does-not-resume | `BLOCKER_RECHECK_AUTO_RESUMED` | 1 |
| get-no-business-write | `BLOCKER_GET_WROTE_STATE` | 1 |
| notice-mode-fence | `BLOCKER_NOTICE_INJECTED_TO_LEGACY_OR_RESTRICTED_MODE` | 1 |
| replay-current-privacy | `BLOCKER_CACHED_REPLY_RETAINED_OLD_READING_AUTHORITY` | 1 |
| adopt-revision-durable | `BLOCKER_ADOPT_REVISION_NOT_DURABLE` | 1 |
| adopt-current-fingerprint | `BLOCKER_ADOPT_FINGERPRINT_NOT_REBOUND` | 1 |

owner-old-plan-paired 同时撤共享未解决门和当前执行 Agent 门，须实际越权成功 nil 才计红。get-no-business-write 同时撤只读事务并注入真实 SQLite 更新，以 total_changes 检出。其余格分别限域。

baseline-go/helper 与 restored-go/helper 日志均保留；helper 两端各实际执行 9 项。早期辅助批次标签曾复用最后格 argv，正式完整循环已固定独立 Python 命令。新增修订格首轮误改指纹函数而命中另一断言，检查器拒收该红；SQL 定位修正仅改撤修器，最终整 40 格完整重跑并恢复通过。
