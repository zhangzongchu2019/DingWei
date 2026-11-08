# race分片跳过项

统计顶层结果时仅计go test-list中的379项，避免父/子重复计数；子用例若跳过仍在下表另列。每片为独立进程、片内默认串行。

| 片 | 用例 | Skip轮次 | 条件 |
| --- | --- | --- | --- |
| 2 | TestR10RestartChildAuthority | 3 | task_dispatch_restart_test.go:22: subprocess-only probe, exercised by TestR10SeparateProcessRestartPreservesAuthorityAndUnknownNotice |
| 5 | TestFeishuStickyOldHubMigrationRollbackCompatibility | 3 | feishu_sticky_compat_test.go:116: R14_OLD_HUB_BINARY not supplied; exact-r48 compatibility fixture is run by the R14 evidence gate |
| 6 | TestConsoleTaskLegacyDODMigrationPendingProductionCount | 3 | console_task_lineage_test.go:135: K17 covers new writes only; legacy acceptance_json rows containing "DOD" await the production count before any 0049 migration decision |
| 6 | TestR10OldR48HubUpgradeAndRollbackOnIsolatedCopy | 3 | task_dispatch_compat_test.go:196: exact-r48 binary not provided; supplied by the isolated R10 compatibility evidence gate |
| 7 | TestConsoleTaskStartRequiresAcceptanceLinkPendingStartGateRuling | 3 | console_task_lineage_test.go:139: future K7: enforce at the start gate using console_spec_task_acceptance, never acceptance_json; creation remains allowed with zero links until that timing is sealed |
