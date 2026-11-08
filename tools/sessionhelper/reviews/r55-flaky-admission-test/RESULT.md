# S-R55-FLAKY-ADMISSION-CLOCK-TEST

CODEX_THREAD_ID=01a1bacf-9317-7b91-bf86-0a0c88c834f6；to=opus50tc2k.zzc。基线 opus50tc2k/r55-integration 41b3e9ad；分支 coder56solzzc/r55-flaky-admission-test；依据 rulings〔53〕-补7 d55ad52d。只修测试，不改产品与入场截止语义；交件不自验收、不部署。

## 改动

唯一既有文件改动是 tools/sessionhelper/tests/test_dispatch_lifecycle.py 的 AdmissionDeadlineTest.test_clock_includes_time_since_persisted_dispatch_intent。其余新增件仅为本目录测试复现和证据。没有产品文件、规格需求、配置、版本、迁移或库存数变更。

旧测试在 begin_dispatch 真正提交SQLite后 sleep(.15)，要求整个调用耗时 <.23秒，只有80ms调度/IO余量。现在仍调用真实 begin_dispatch 及实际 Adapter，控制 time.time 与 time.monotonic：落盘时墙钟1000、单调200，提交后把两者同时推进1秒，admission_timeout保持.1秒；不sleep、不比较真实运行耗时。断言未调用deliver、落盘intent仍1000、入场已耗时精确1秒、状态为admission_unknown。两种时钟都推进，核对Adapter从持久intent重建单调时钟起点，不是只测ledger到期。

将夹具deliver/poll的回执从processing改为unknown，是为错误实现提供确定的返回路径：若变异错误地调用deliver，仍能结束而不在冻结时钟下循环；随后的零调用断言会失败，不能靠unknown回执让错误实现通过。正向过期路径本来就不得调用deliver/poll，正向语义不受此夹具改动影响。

保留既有恢复完成的Event.wait(1)，在离开时钟patch前等待本地空操作恢复线程；这只是线程收尾的有界等待，不是入场截止的时间余量或运行耗时断言。恢复行为本身由文件内其他用例验证。

## 复现与变异

```sh
PATH=/tmp/coder-0232-locked-venv/bin:$PATH PYTHONPATH=tools/sessionhelper python3 -m unittest -v tools.sessionhelper.tests.test_dispatch_lifecycle.AdmissionDeadlineTest.test_clock_includes_time_since_persisted_dispatch_intent
PATH=/tmp/coder-0232-locked-venv/bin:$PATH python3 tools/sessionhelper/reviews/r55-flaky-admission-test/reproduce.py --output /tmp/new-flaky-repeat --count 50
PATH=/tmp/coder-0232-locked-venv/bin:$PATH python3 tools/sessionhelper/reviews/r55-flaky-admission-test/reproduce.py --mutation
PATH=/tmp/coder-0232-locked-venv/bin:$PATH sh scripts/run-build-tests.sh
OPENSPEC_TELEMETRY=0 /tmp/coder56solzzc-openspec-1.16.0/node_modules/.bin/openspec validate --all --strict
```

输出目录必须是新路径。50次每次独立Python进程重新加载整个文件，非只重复目标方法。实际首轮执行脚本原文保存在raw/actual-repeat-runner.py；reproduce.py提供等价的可参数化入口。每轮UTC、loadavg三元组（1/5/15min）、rc、用时及逐轮原文在raw，统计不把低load说成已实测15以上负载。

一刀变异在隔离Python进程中替换 _bounded_deliver：先deliver，返回后令_dispatch_started=time.monotonic()，重起入场截止。夹具立即返回unknown，无真实CLI/运输；预期rc1，红在 `PERSISTED_INTENT_DEADLINE_RESET: expired intent must not reach deliver`，因为旧intent被再次派发。未修改工作树或其他时限；不是导入/依赖/超时错误。原有持久化、异常隔离等场景仍由整文件覆盖。

## 验证与交接

整文件50次全部rc0，每次38项，共1900项；1分钟load范围0.976074～2.567383。canonical rc0，两profile各1127/1127，另27 Python与2 shell全过；strict 25/0。最终数字见raw/RUNS.json及原文，两份SHA清单覆盖证据和测试输入；规格没有语义变更，仅只读strict整体验证，不新增需求或修改仓根OpenSpec。

T-01 已领取唯一写者锁，核基线与裁定；T-02 已替换墙钟余量断言并核目标/变异；T-03 整文件50次、canonical与strict已完成，RUNS与SHA齐备，待独立复核。执行者coder56solzzc.zzc；本次提交的完整commit/tree经push和ls-remote一致后在回执给出；下一步独立复核，不自验收。无节点/现网/部署操作。

交接记录：2026-11-08 coder56solzzc.zzc；commit为本次提交。完成T01～03及所有本地测试/目标变异，rulings截至d55ad52d无漂移；阻塞：无。下一步普通push、核远端后交审，写者锁保留待复核。
