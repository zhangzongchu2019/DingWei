# S-HOSTED-TEST-TIME 交审报告

基线与分支起点 `329eb9f9c45d2d785a0429b01eaa0fa5264fe15d`；分支 `coder56solzzc/hosted-test-time`。原子锁唯一写者 CODEX_THREAD_ID=`01a1c782-7090-7071-8d99-6ea51512ac13`。本报告是实现自测交件，等待组长独立复核；不构建发布件、不部署，不修改已冻结 r57 分支。

hosted 在修改前全仓中用时 737.067 秒，原始单包 677.002 秒。修后默认超时全仓中为 215.617 秒（减少约 70.75%），所有原断言保留，185 个顶层、0 SKIP。主要成因和方案见 [DESIGN.md](DESIGN.md)，修改前两轮各自前 15 个慢用例及命令、UTC 见 [BASELINE.md](BASELINE.md)。

## 判据与读数

|判据|自测读数|
|---|---|
|a：默认 10 分钟、-p 2 全仓 hosted ≤300 秒|全仓 rc0，5145 PASS／0 FAIL／71 SKIP；hosted 215.617 秒，346 个含子例 PASS／0 FAIL／0 SKIP|
|b：hosted -count=3|rc0；1038 个含子例 PASS／0 FAIL／0 SKIP；每轮 185 顶层，共 555|
|c：顶层仍 185、SKIP 不增|前后顶层名称集合完全相同；全仓 71 项 SKIP 身份完全相同；hosted 始终 0 SKIP；185 个原测试主体逐字还原一致|
|d：run-build-tests|rc0；BUILD_TESTS_OK helper_profiles=2 python=27 shell=2；claude/codex 各 1168/1168，93 文件|
|附加冷工具缓存验证|空 XDG_CACHE_HOME、6 个原有 PG 用例并发，6 PASS／0 FAIL／0 SKIP；仅生成 1 个工具二进制|
|git diff --check|rc0|

三轮命令额外使用 `-shuffle=782`，每轮耗时按首个顶层 run 到该轮第 185 个顶层 pass 的 UTC 差计：

|轮次|秒|UTC 起止|
|---|---:|---|
|1|204.554860|2026-11-11T03:16:54.402828381Z ～ 2026-11-11T03:20:18.957688391Z|
|2|197.777257|2026-11-11T03:20:18.957691651Z ～ 2026-11-11T03:23:36.734948448Z|
|3|196.375227|2026-11-11T03:23:36.734950797Z ～ 2026-11-11T03:26:53.110177773Z|

判据 a 命令为 `go test -json -count=1 -p 2 ./...`，没有任何 `-timeout` 参数，也没有 GOFLAGS 环境值；原始基线与重复三轮为测量／完整重复分别显式用 40 分钟上限。三轮包级总耗时 598.713 秒，不能将每个单例的排队时间累加当包耗时。六个夹具所有者的上限在较小的调用方 `-parallel` 下仍受 Go 调度约束；外部共享 DSN 保留串行模式，215.617 秒读数对应本单 env-i 内嵌 PG 环境。

## 命令与 UTC

所有主门使用普通用户、Go 1.22.2、`env -i`、`HUB_TEST_PG_STRICT=1`、lock venv 置于 PATH 前端、独立 HOME、短 TMPDIR；未继承 SH/DINGWEI 凭据或在线状态。白名单原文见 raw/env.json。冷工具验证仅额外设置空 XDG_CACHE_HOME，未复用已有工具缓存。

|门|命令|UTC 起止|rc|
|---|---|---|---:|
|before-hosted|`go test -json -count=1 -p 2 -timeout 40m ./internal/hosted`|2026-11-11T02:36:32.359614+00:00 ～ 2026-11-11T02:47:51.115643+00:00|0|
|before-all|`go test -json -count=1 -p 2 -timeout 40m ./...`|2026-11-11T02:47:51.123410+00:00 ～ 2026-11-11T03:04:00.814816+00:00|0|
|after-all|`go test -json -count=1 -p 2 ./...`|2026-11-11T03:05:06.826634+00:00 ～ 2026-11-11T03:16:50.523475+00:00|0|
|repeat3|`go test -json -count=3 -shuffle=782 -timeout=40m ./internal/hosted`|2026-11-11T03:16:53.120940+00:00 ～ 2026-11-11T03:26:53.122896+00:00|0|
|cold-tools|`go test -json -count=1 -parallel=6 -timeout=3m ./internal/hosted -run ^Test(SchemaApplyRollbackRepeatable|SchemaDropLeavesNoG4Tables|AppliedLedgerRejectsMissingHostedTableWithoutRepair|FrozenR30SchemaModelColumnMigration|NodeSeatPoolQuotaFlow|NodeCredentialLifecycle)$`|2026-11-11T03:26:54.030267+00:00 ～ 2026-11-11T03:27:01.546811+00:00|0|
|build-tests|`sh scripts/run-build-tests.sh`|2026-11-11T03:26:53.148179+00:00 ～ 2026-11-11T03:35:57.348496+00:00|0|
|diff-check|`git diff --check`|2026-11-11T03:35:57.387013+00:00 ～ 2026-11-11T03:35:57.403440+00:00|0|

## 可复核证据与边界

运行 `python3 docs/reviews/hosted-test-time/audit.py` 可核测试主体和源码范围；运行 `python3 docs/reviews/hosted-test-time/verify.py` 可从原始结果核四条判据。raw/ 中保存实际执行器、命令／cwd／UTC／退出码、Go 跳过用例及条件、轮次与输入 SHA，原始日志以 gzip 无损压缩；SHA256SUMS 覆盖报告与证据字节。串行原始树的 737.067 秒大于性能门 300 秒，作为撤掉并行调度的性能反例，不把提高超时算作优化。

源码变更仅 internal/hosted 的 `_test.go` 和本交审目录；不修改产品函数、共享 pgtest 包、tools/pgtest、版本号或依赖。每次 testpool 仍起独立 PG，原收尾／残留断言不变；不新增 t.Skip，不删除用例，不放宽断言，不复用业务数据夹具。所有原测试主体在去掉新增调度行后与基线逐字一致，只有 testpool 的首次工具准备多了 sync.Once。没有增加新的顶层测试。

本封接单 send.py 回执 to 字段原文为 `opus50tc2k.zzc#enrolled`，request_id=`b18f55a88163`，输入地址为 `opus50tc2k.zzc`。最终普通推送与 ls-remote 核验后的精确 tip/tree 由交件回执提供，不把未推送状态当远端交付。
