# S-HOSTED-TEST-TIME 返工交审报告

本次追加普通提交的父为 `2eb33cd07872f127bbfea07701466c6b00875dd4`；原分支 `coder56solzzc/hosted-test-time`，初始基线 `329eb9f9c45d2d785a0429b01eaa0fa5264fe15d`。唯一写者 CODEX_THREAD_ID=`01a1c782-7090-7071-8d99-6ea51512ac13`。这是实现自测，等待组长独立复核，不自冻结、不构建发布件、不部署，未动 r57 冻结分支。

## 评审必改与全包审计

初版统一并行遗漏了两个跨文件包级写：TestUpgradeUnknownPlanNotSwallowed 删除／恢复 plans，TestReconcileSeatTimeoutDoesNotStallBatch 改写／恢复 ReconcileSeatTimeout。仅移除这两个原用例的 parallelHostedTest；它们在顶层串行阶段运行并完成 Cleanup 后，183 个并行用例才放行。未改产品函数、未改这两个用例的原测试主体和断言、未加 Skip。185 个原测试主体仍可逐字还原到 329eb9f9。

完整清单见 [GLOBAL-STATE-AUDIT.md](GLOBAL-STATE-AUDIT.md)：**8 项**（2 项不安全写改串行、2 项同步辅助保留、4 项只读／并发安全排除）。go/parser＋go/types 覆盖 33 个测试文件、57 个本包包级变量，158 个直接引用和83个保守候选逐项分类，含 Cleanup、索引赋值、delete、range 赋值、var/短声明别名、函数变量、channel和导入包变量；手工补查全局 setter／环境／目录等。没有第三个待处理的不安全写。

所有 a～d 都在这轮修订后的测试输入上重新执行；raw/test-inputs.json 固定33个测试文件 SHA，最终提交前复核一致。初版原始文件留在 raw/initial-2eb33cd0，仅为历史，不参与本次判据。修改前的329基线单包677.002秒、全仓hosted737.067秒及各自15个慢用例仍见 [BASELINE.md](BASELINE.md)。

## a～g 自测结果

|判据|本轮读数|
|---|---|
|a：不带 -timeout、-p 2 全仓|rc0；全仓 5145 PASS／0 FAIL／71 SKIP；hosted **210.208 秒** ≤300，185顶层／0SKIP|
|b：普通 hosted -count=3|rc0；每轮185顶层，共555；1038含子例PASS／0FAIL／0SKIP|
|c：顶层与SKIP|顶层名称集合前后相同，始终185；hosted始终0SKIP；全仓71项SKIP身份相同；185原测试主体还原审计通过|
|d：run-build-tests|rc0；BUILD_TESTS_OK helper_profiles=2 python=27 shell=2；claude/codex各1168/1168、93文件|
|e：hosted -race -count=3|rc0；每轮185顶层，共555；1038含子例PASS／0FAIL／0SKIP|
|f：新修订输入完整重跑|a～d均本轮重新执行，没有沿用初版读数；输入SHA保持一致|
|g：重新并行 plans 写者|串行控制组rc0；同一读者与写者窗口下重新加并行入口，rc1，读者按 GLOBAL_PLANS_SERIALIZATION_WITNESS 红，写者仍PASS|

普通和 race 的三轮均使用 -shuffle=782，保留40分钟的累计重复上限；这不改变 a 使用的默认10分钟超时。每轮耗时按首个顶层 run 到第185个顶层 pass 的UTC差计，不累加含名额等待的单例Elapsed。

|模式／轮次|秒|UTC起止|
|---|---:|---|
|普通／1|202.131629|2026-11-11T04:06:29.571362445Z ～ 2026-11-11T04:09:51.702991271Z|
|普通／2|204.018261|2026-11-11T04:09:51.70299399Z ～ 2026-11-11T04:13:15.72125439Z|
|普通／3|204.952231|2026-11-11T04:13:15.72125647Z ～ 2026-11-11T04:16:40.673487483Z|
|race／1|204.293389|2026-11-11T04:16:44.292174773Z ～ 2026-11-11T04:20:08.58556368Z|
|race／2|215.721986|2026-11-11T04:20:08.58556635Z ～ 2026-11-11T04:23:44.307552916Z|
|race／3|211.429650|2026-11-11T04:23:44.307556185Z ～ 2026-11-11T04:27:15.737206331Z|

## g 的一次性树控制

只在独立 detached 工作树中添加读者和有界窗口：写者 delete 后通知窗口，最多等读者或10秒；读者是并行顶层，收到窗口后读 PlanByID("2c4g")。固定树中写者串行：读者只能在其 Cleanup 恢复套餐后开始，两个用例PASS。变异仅重新加入写者的 parallelHostedTest：读者进入删除窗口并失败，写者原测试依然PASS，环境／编译正常。两轮均带 -race；窗口通道同步避免了内存竞争，本次 g 命中的是判据允许的“读者失败”，不冒称race告警。

临时读者、窗口和重新并行的Go文件只存在于一次性树，不进入交付树或提交；raw/global-plans-mutation.py 是实际执行的生成／运行证据脚本，不是默认测试输入。原始命令、控制／变异SHA、相同读者SHA和单例结果见对应JSON与gzip日志。交付树仍185顶层，证明串行化实际防住套餐丢失。判据e则在没有任何变异／读者注入的真实修订输入上执行全包三轮。

## 实际命令与UTC

普通用户、Go1.22.2、env -i、HUB_TEST_PG_STRICT=1、lock venv PATH、独立测试HOME、短TMPDIR=/tmp/httfix；未继承SH/DINGWEI在线状态。精确白名单见raw/env.json。a明确没有-timeout、也没有GOFLAGS。源码变化只有两个测试调度入口和本评审目录，PG工具／产品／版本号／依赖均未改。

|门|命令|UTC起止|rc|
|---|---|---|---:|
|after-all|`go test -json -count=1 -p 2 ./...`|2026-11-11T03:54:24.901098+00:00 ～ 2026-11-11T04:06:28.317644+00:00|0|
|repeat3|`go test -json -count=3 -shuffle=782 -timeout=40m ./internal/hosted`|2026-11-11T04:06:29.102873+00:00 ～ 2026-11-11T04:16:40.686309+00:00|0|
|race3|`go test -json -race -count=3 -shuffle=782 -timeout=40m ./internal/hosted`|2026-11-11T04:16:40.709589+00:00 ～ 2026-11-11T04:27:16.764900+00:00|0|
|build-tests|`sh scripts/run-build-tests.sh`|2026-11-11T04:27:16.787496+00:00 ～ 2026-11-11T04:36:15.262675+00:00|0|
|diff-check|`git diff --check`|2026-11-11T04:36:15.305333+00:00 ～ 2026-11-11T04:36:15.321895+00:00|0|
|global-plans-control|`go test -json -race -count=1 -timeout=3m ./internal/hosted -run ^(TestUpgradeUnknownPlanNotSwallowed|TestHostedPlansParallelWitness)$`|2026-11-11T03:52:24.185367+00:00 ～ 2026-11-11T03:52:42.175248+00:00|0|
|global-plans-mutant|`go test -json -race -count=1 -timeout=3m ./internal/hosted -run ^(TestUpgradeUnknownPlanNotSwallowed|TestHostedPlansParallelWitness)$`|2026-11-11T03:52:42.175801+00:00 ～ 2026-11-11T03:52:47.624950+00:00|1|

运行 audit.py 核原测试主体／183并行＋2串行；global_audit.py 重做当前源码的类型级候选提取；verify.py 从本轮原始结果核 a～g。所有门命令、cwd、UTC、退出码、Skip名称与条件及实际执行器都在raw，日志gzip无损压缩；SHA256SUMS覆盖最终文档与证据。临时全局写变异不在产品或测试输入里。普通推送后精确tip/tree由交件回执提供，不把本地状态冒报远端交付。

本封返工接单send.py回执to字段原文 `opus50tc2k.zzc#enrolled`，request_id=`58e28c202e7f`，输入地址 `opus50tc2k.zzc`。

判据 c 最终源码／结果联合审计 UTC：2026-11-11T04:37:06.075326+00:00 ～ 2026-11-11T04:37:06.255326+00:00；两项机器核验均rc0，详情见raw/c-audit-execution.json。相对父2eb33cd0的Go测试增量严格只有reconcile_test.go删除两行并行入口。
