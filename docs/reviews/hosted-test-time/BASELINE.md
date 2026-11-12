# hosted 修改前测量

基线：`329eb9f9c45d2d785a0429b01eaa0fa5264fe15d`，Go 1.22.2，普通用户，`env -i`、`HUB_TEST_PG_STRICT=1`，不设置外部 DSN；命令及白名单见 raw。先顺序跑完以下两门，之后才修改测试。

## 单包

命令：`go test -json -count=1 -p 2 -timeout 40m ./internal/hosted`。

UTC：2026-11-11T02:36:32.359614+00:00 ～ 2026-11-11T02:47:51.115643+00:00；rc=0；hosted 包 677.002 秒；顶层 185；含子例 346 PASS／0 FAIL／0 SKIP。

|慢用例|秒|
|---|---:|
|TestNodeDownLeavesStateAndCredential|24.22|
|TestSoftHoldReleaseReplayPreservesCurrentHazards|21.99|
|TestRefreshCredResumesSuspendedCredential|18.97|
|TestSoftHoldReleasePendingEventConverges|16.18|
|TestBackupChecksumControlPlane|15.66|
|TestFailedGateStopsTheChain|13.23|
|TestBackupIntegrityRejected|12.54|
|TestOptionsPlanFitsEachGate|11.27|
|TestCreateFailurePathsAbortTheIssue|11|
|TestCheckCreateCodeSplit|11|
|TestLoginTerminalStatesClearLinkAndCode|10.79|
|TestOwnershipEnforced|10.48|
|TestLifecycleOrderOnOneLog|10.45|
|TestRefreshCredNodeOutcomes|10.35|
|TestEnableRestartFailureAfterStarting|10.31|

## 全仓 -p 2

命令：`go test -json -count=1 -p 2 -timeout 40m ./...`。

UTC：2026-11-11T02:47:51.123410+00:00 ～ 2026-11-11T03:04:00.814816+00:00；rc=0；hosted 包 737.067 秒；顶层 185；含子例 346 PASS／0 FAIL／0 SKIP。

|慢用例|秒|
|---|---:|
|TestSoftHoldReleaseReplayPreservesCurrentHazards|25.96|
|TestNodeDownLeavesStateAndCredential|24.53|
|TestRefreshCredResumesSuspendedCredential|19.73|
|TestBackupChecksumControlPlane|19.4|
|TestSoftHoldReleasePendingEventConverges|18.58|
|TestBackupIntegrityRejected|16.31|
|TestFailedGateStopsTheChain|14.03|
|TestOptionsPlanFitsEachGate|12.37|
|TestLoginStartNodeOutcomes|12.18|
|TestLifecycleOrderOnOneLog|11.74|
|TestEnableRestartFailureAfterStarting|11.66|
|TestRefreshCredNodeOutcomes|11.39|
|TestOwnershipEnforced|11.29|
|TestLoginTerminalStatesClearLinkAndCode|11.27|
|TestCheckCreateCodeSplit|10.55|
