# 并行测试包级状态写入审计

覆盖 internal/hosted 全部 33 个 `_test.go`，用 go/parser＋go/types 绑定跨文件与导入包符号、检查赋值／索引赋值／delete／自增／channel 操作／全局接收者方法／调用参数，并保守追踪局部别名，包含所有闭包和 Cleanup。人工补查导入包 setter、环境／目录修改、包级初始化及函数变量写入。类型检查通过；本包 57 个包级变量、158 个直接引用、83 个保守候选点、25 个候选变量全部分类。原始结果见 raw/global-state-audit.json；无新增待处置风险。运行 `python3 docs/reviews/hosted-test-time/global_audit.py` 可从当前源码重做类型级候选提取。

清单共 **8 条**：4 项实际共享状态（2 项不安全写改为串行、2 项同步辅助保留），4 项只读／并发安全调用排除。实际无同步的业务状态写入共 4 处，仅涉及前两项；读取 saved 的副本不是写共享变量。

|编号|文件:行（修订树）|变量|性质与处理|
|---|---|---|---|
|1|reconcile_test.go:149、150；副本读取148|plans|delete 与 Cleanup 索引恢复写；TestUpgradeUnknownPlanNotSwallowed 去掉 parallelHostedTest，成为串行顶层；所有并行 PlanByID 读者等其 Cleanup 完成后才运行|
|2|reconcile_test.go:92、93；副本读取91|ReconcileSeatTimeout|300ms 赋值与 Cleanup 恢复写；TestReconcileSeatTimeoutDoesNotStallBatch 去掉并行入口，避免并行 ReconcileNode 读者借用测试超时|
|3|parallel_test.go:15、26、27|hostedTestSlots|初始化在测试前；channel 发送／接收是 Go 同步操作，不重绑定 channel；名额释放仍在最后 Cleanup，保留|
|4|parallel_test.go:16；store_test.go:23|hostedPGWarm|初始化在测试前；仅经 sync.Once.Do 改内部状态，没有直接赋值／重置，保留|
|5|backup_test.go:76|http.DefaultClient|仅调用并发安全的 Client.Do；没有重绑定 DefaultClient 或修改其字段／默认 Transport，排除不安全写|
|6|login_test.go:98|base64.RawURLEncoding|EncodeToString 只读编码表，不赋值／改配置，排除写|
|7|softquota_notice_test.go:12|time.UTC|作为 time.Date 参数读取，不重绑定或修改 Location，排除写|
|8|account_share_test.go:76 等（下表全量列出）|18 个 Err* 哨兵|errors.Is、Error 和原因提取中的只读比较／读取，无赋值或字段写，排除写|

第 8 项变量：`ErrAccountNotAllowed`, `ErrBackupFailed`, `ErrComboNotVerified`, `ErrForbiddenRole`, `ErrInvalidState`, `ErrModelNotAvailable`, `ErrModelNotSupported`, `ErrNoNodeCapacity`, `ErrNodeExists`, `ErrNodeOffline`, `ErrNotFound`, `ErrNotOwner`, `ErrParams`, `ErrPlanExceedsQuota`, `ErrQuotaExceeded`, `ErrRolledBack`, `ErrSoftQuotaHeld`, `ErrStartFailed`。

并行读者路径包括 plans.go:21、lifecycle.go:113、lifecycle_job.go:111、orchestrator.go:220／481；超时读者位于 orchestrator.go:646。

原来所有顶层统一并行的审计只检查了局部夹具与环境，没有正确识别这两处跨文件包级写入；单次 -race 未碰到不证明安全。本次用 Go 的顶层串行阶段隔离全局改写，原测试主体和业务断言仍与 329eb9f9 完全一致。计数保持 185，其中 183 个并行入口、2 个串行全局写用例；不增加 t.Skip。

人工全包补查还覆盖 os.Setenv/Unsetenv/Chdir、rand.Seed、log.SetOutput/SetFlags/SetPrefix、runtime.GOMAXPROCS、http.Handle/HandleFunc、默认客户端／Transport 字段赋值、反射／unsafe 改写和包级函数变量重绑定：未发现。req.Header.Set、req.Trailer.Set、mux.Handle 均是每例局部实例；r.l.now、b.now、s.after* 等注入字段均属于独立实例，不是包级函数变量。普通地图 delete(f,...) 操作也均是局部帧数据。

## 全部候选与排除依据

此表保留保守别名误报和只读参数，避免把机器候选误称真正的写。源码均位于 internal/hosted/。

|文件:行|所属函数|变量|候选类型|分类|
|---|---|---|---|---|
|account_share_test.go:76|TestAccountSecretValidation|ErrParams|call-argument-review|8（只读哨兵）|
|account_share_test.go:83|TestAccountSecretValidation|ErrParams|call-argument-review|8（只读哨兵）|
|account_share_test.go:89|TestAccountSecretValidation|ErrParams|call-argument-review|8（只读哨兵）|
|account_share_test.go:93|TestAccountSecretValidation|ErrParams|call-argument-review|8（只读哨兵）|
|account_share_test.go:250|TestCreateSeatPKConflictIsNotAccountError|ErrAccountNotAllowed|call-argument-review|8（只读哨兵）|
|backup_test.go:76|uploadBackup|DefaultClient|method-call:Do|5（并发安全API）|
|backup_test.go:323|TestSweepExpiredBackupsReclaimsOnlyStalePending|ErrNotFound|call-argument-review|8（只读哨兵）|
|claude_model_test.go:78|TestClaudeModelCreateRejectsUnapprovedAndMalformedBeforeIssue|ErrModelNotAvailable|call-argument-review|8（只读哨兵）|
|claude_model_test.go:89|TestClaudeModelCreateRejectsUnapprovedAndMalformedBeforeIssue|ErrModelNotAvailable|call-argument-review|8（只读哨兵）|
|claude_model_test.go:100|TestClaudeEmptyModelPreservesDefaultAndUnverifiedComboClosed|ErrComboNotVerified|call-argument-review|8（只读哨兵）|
|lifecycle_race_test.go:76|TestEnableRestartFailureAfterStarting|ErrStartFailed|call-argument-review|8（只读哨兵）|
|lifecycle_race_test.go:86|TestEnableRestartFailureAfterStarting|ErrNodeOffline|call-argument-review|8（只读哨兵）|
|lifecycle_race_test.go:132|TestFailureAfterAgentConnectedKeepsOnline|ErrNodeOffline|call-argument-review|8（只读哨兵）|
|lifecycle_test.go:248|TestLifecycleJobDuplicatePolicy|ErrInvalidState|call-argument-review|8（只读哨兵）|
|lifecycle_test.go:484|TestFailedGateStopsTheChain|ErrInvalidState|call-argument-review|8（只读哨兵）|
|lifecycle_test.go:498|TestRestartAfterFailedStopResumesCredential|ErrNodeOffline|call-argument-review|8（只读哨兵）|
|lifecycle_test.go:529|TestOwnershipEnforced|ErrNotOwner|call-argument-review|8（只读哨兵）|
|lifecycle_test.go:620|TestUpgradeRollbackOnNodeFailure|ErrRolledBack|call-argument-review|8（只读哨兵）|
|lifecycle_test.go:665|TestUpgradeWhileDisabledRejected|ErrInvalidState|call-argument-review|8（只读哨兵）|
|lifecycle_test.go:778|TestNodeDownLeavesStateAndCredential|ErrNodeOffline|call-argument-review|8（只读哨兵）|
|lifecycle_test.go:790|TestNodeDownLeavesStateAndCredential|ErrNodeOffline|call-argument-review|8（只读哨兵）|
|lifecycle_test.go:815|TestNodeDownLeavesStateAndCredential|ErrNodeOffline|call-argument-review|8（只读哨兵）|
|lifecycle_test.go:826|TestNodeDownLeavesStateAndCredential|ErrNodeOffline|call-argument-review|8（只读哨兵）|
|lifecycle_test.go:839|TestNodeDownLeavesStateAndCredential|ErrNodeOffline|call-argument-review|8（只读哨兵）|
|lifecycle_test.go:849|TestNodeDownLeavesStateAndCredential|ErrStartFailed|call-argument-review|8（只读哨兵）|
|lifecycle_test.go:863|TestDestroyBackupFailedTyped|ErrBackupFailed|call-argument-review|8（只读哨兵）|
|lifecycle_test.go:939|TestDestroyStopsBeforeBackup|ErrNodeOffline|call-argument-review|8（只读哨兵）|
|login_test.go:98|authJSON|RawURLEncoding|method-call:EncodeToString|6（只读）|
|minimum_plan_test.go:121|TestMinimumPlanQuotaDebitsAndEveryDimension|ErrPlanExceedsQuota|call-argument-review|8（只读哨兵）|
|minimum_plan_test.go:196|TestMinimumPlanOptionsAndRealCreateResources|ErrPlanExceedsQuota|call-argument-review|8（只读哨兵）|
|minimum_plan_test.go:196|TestMinimumPlanOptionsAndRealCreateResources|ErrParams|call-argument-review|8（只读哨兵）|
|node_quota_test.go:74|TestSoftNodeSchedulingConsentPriorityCapacityAndAudit|ErrNoNodeCapacity|call-argument-review|8（只读哨兵）|
|node_quota_test.go:85|TestSoftNodeSchedulingConsentPriorityCapacityAndAudit|ErrNoNodeCapacity|call-argument-review|8（只读哨兵）|
|node_quota_test.go:132|TestSoftNodeSchedulingConsentPriorityCapacityAndAudit|ErrNoNodeCapacity|call-argument-review|8（只读哨兵）|
|node_quota_test.go:215|TestSoftHeldLifecycleRefusesBeforeSideEffects|ErrSoftQuotaHeld|call-argument-review|8（只读哨兵）|
|node_quota_test.go:218|TestSoftHeldLifecycleRefusesBeforeSideEffects|ErrSoftQuotaHeld|call-argument-review|8（只读哨兵）|
|node_quota_test.go:321|TestQuotaUsageWarningNotifiesWithoutPrematureHold|ErrNoNodeCapacity|call-argument-review|8（只读哨兵）|
|orchestrator_test.go:250|TestClaudeMinimumPlanRemainsUnverified|ErrComboNotVerified|call-argument-review|8（只读哨兵）|
|orchestrator_test.go:327|TestCreateRejections|ErrQuotaExceeded|call-argument-review|8（只读哨兵）|
|orchestrator_test.go:808|TestCreateModelRejections|ErrModelNotAvailable|call-argument-review|8（只读哨兵）|
|orchestrator_test.go:813|TestCreateModelRejections|ErrModelNotSupported|call-argument-review|8（只读哨兵）|
|orchestrator_test.go:831|TestPickNodeStaleFrameTreatedOffline|ErrModelNotAvailable|call-argument-review|8（只读哨兵）|
|parallel_test.go:26|parallelHostedTest|hostedTestSlots|channel-send|3（channel同步）|
|parallel_test.go:27|parallelHostedTest|hostedTestSlots|channel-receive|3（channel同步）|
|plans_test.go:36|TestCheckCreateDiskGate|ErrPlanExceedsQuota|call-argument-review|8（只读哨兵）|
|quotasvc_test.go:31|TestQuotaCheckBoundaries|ErrQuotaExceeded|call-argument-review|8（只读哨兵）|
|quotasvc_test.go:69|TestQuotaCheckBoundaries|ErrQuotaExceeded|call-argument-review|8（只读哨兵）|
|quotasvc_test.go:77|TestQuotaCheckBoundaries|ErrQuotaExceeded|call-argument-review|8（只读哨兵）|
|quotasvc_test.go:147|TestCheckCreateCodeSplit|ErrPlanExceedsQuota|call-argument-review|8（只读哨兵）|
|quotasvc_test.go:147|TestCheckCreateCodeSplit|ErrQuotaExceeded|call-argument-review|8（只读哨兵）|
|r31_hosted_test.go:89|TestR31NodeRegistrationCredentialAndAtomicAudit|ErrNodeExists|call-argument-review|8（只读哨兵）|
|r31_hosted_test.go:110|TestR31NodeRegistrationCredentialAndAtomicAudit|ErrNotFound|call-argument-review|8（只读哨兵）|
|r31_hosted_test.go:185|TestR31HardCapacityAllDimensionsAndLegacyUnknown|ErrNoNodeCapacity|call-argument-review|8（只读哨兵）|
|r31_hosted_test.go:192|TestR31HardCapacityAllDimensionsAndLegacyUnknown|ErrNoNodeCapacity|call-argument-review|8（只读哨兵）|
|r31_hosted_test.go:340|TestR31AdmissionAndNodeRevocationCommitOrder|ErrForbiddenRole|call-argument-review|8（只读哨兵）|
|r31_hosted_test.go:365|TestR31HardConcurrentReservationsDoNotOversell|ErrNoNodeCapacity|call-argument-review|8（只读哨兵）|
|r31_hosted_test.go:381|TestR31AdmissionUsesOwnerNotAdministratorAuditActor|ErrForbiddenRole|call-argument-review|8（只读哨兵）|
|readapi_test.go:252|TestJobProgressForOwnerBySeat|ErrNotOwner|call-argument-review|8（只读哨兵）|
|reconcile_test.go:91|TestReconcileSeatTimeoutDoesNotStallBatch|ReconcileSeatTimeout|assignment-or-alias-write|2（副本读取／不安全写，串行）|
|reconcile_test.go:92|TestReconcileSeatTimeoutDoesNotStallBatch|ReconcileSeatTimeout|assignment-or-alias-write|2（副本读取／不安全写，串行）|
|reconcile_test.go:93|TestReconcileSeatTimeoutDoesNotStallBatch|ReconcileSeatTimeout|assignment-or-alias-write|2（副本读取／不安全写，串行）|
|reconcile_test.go:133|TestEmptyCreatedByRefused|ErrNotOwner|call-argument-review|8（只读哨兵）|
|reconcile_test.go:148|TestUpgradeUnknownPlanNotSwallowed|plans|assignment-or-alias-write|1（副本读取／不安全写，串行）|
|reconcile_test.go:149|TestUpgradeUnknownPlanNotSwallowed|plans|builtin-mutation:delete|1（副本读取／不安全写，串行）|
|reconcile_test.go:150|TestUpgradeUnknownPlanNotSwallowed|plans|assignment-or-alias-write|1（副本读取／不安全写，串行）|
|reconcile_test.go:152|TestUpgradeUnknownPlanNotSwallowed|ErrParams|call-argument-review|8（只读哨兵）|
|reconcile_test.go:152|TestUpgradeUnknownPlanNotSwallowed|ErrRolledBack|call-argument-review|8（只读哨兵）|
|seat_capacity_reason_test.go:33|requireCapacityReasons|ErrNoNodeCapacity|call-argument-review|8（只读哨兵）|
|seat_capacity_reason_test.go:42|TestCapacityReasonSanitizationAndCompatibility|ErrNoNodeCapacity|method-call:Error|8（只读哨兵）|
|seat_capacity_reason_test.go:48|TestCapacityReasonSanitizationAndCompatibility|ErrNoNodeCapacity|call-argument-review|8（只读哨兵）|
|seat_capacity_reason_test.go:86|TestCapacityRequestReasonAndModelSelection|ErrModelNotAvailable|call-argument-review|8（只读哨兵）|
|seat_capacity_reason_test.go:90|TestCapacityRequestReasonAndModelSelection|ErrModelNotAvailable|call-argument-review|8（只读哨兵）|
|seat_capacity_reply_test.go:125|TestCapacityFinalGateLifecycleJobs|ErrNotOwner|call-argument-review|8（只读哨兵）|
|seat_capacity_reply_test.go:213|TestCapacityFinalGateCreateJobsAndCleanup|ErrNotOwner|call-argument-review|8（只读哨兵）|
|seat_capacity_test.go:87|TestCapacityRealCreatePendingEightNinthAndExistingStart|ErrNoNodeCapacity|call-argument-review|8（只读哨兵）|
|seat_capacity_test.go:133|TestCapacityRealCreatePendingEightNinthAndExistingStart|ErrNoNodeCapacity|call-argument-review|8（只读哨兵）|
|seat_capacity_test.go:172|TestCapacityStoreSerializesConcurrentReservations|ErrNoNodeCapacity|call-argument-review|8（只读哨兵）|
|seat_capacity_test.go:209|TestCapacityUnknownReservesForeignLoadAndLegacyHard|ErrNoNodeCapacity|call-argument-review|8（只读哨兵）|
|seat_capacity_test.go:229|TestCapacityUnknownReservesForeignLoadAndLegacyHard|ErrNoNodeCapacity|call-argument-review|8（只读哨兵）|
|seat_capacity_test.go:237|TestCapacityUnknownReservesForeignLoadAndLegacyHard|ErrNoNodeCapacity|call-argument-review|8（只读哨兵）|
|seat_capacity_test.go:244|TestCapacityUnknownReservesForeignLoadAndLegacyHard|ErrNoNodeCapacity|call-argument-review|8（只读哨兵）|
|softquota_notice_test.go:12|TestQuotaNoticeDistinguishesMeasurementFailureFromUsageThreshold|UTC|call-argument-review|7（只读）|
|store_test.go:23|testpool|hostedPGWarm|method-call:Do|4（Once同步）|
