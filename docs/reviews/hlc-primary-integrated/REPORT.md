# HLC 主流整单开发交件（待独立复核）

生成 UTC：2026-11-02T05:24:34.063281+00:00。仓库 `git@git.code.tencent.com:wego-ai/dingwei.git`；分支 `opus50tc2k/hub-graceful-shutdown-impl`。最终交件 commit/tree 以推送回执为准，本报告不自引用其承载提交。

产品修复、完整混合负载和故障/反向控制已执行；S011自动恢复仍红，显式resync可恢复；m8整包race有1项既有时限失败。因此不报整单全绿、不自签收、不构成上线GO。所有实验使用普通dev、白名单环境、隔离HOME/短TMPDIR/IPC、loopback假传输和临时数据库；无部署、systemd修改、apply/archive。

## 源码与合并

| 片段 | commit | tree/说明 |
|---|---|---|
| 六文件独立提交 | 27e2f6ca8aa5a70dc29f8cc6941130a640a3825f | 0f1f9530e29db5646f2a3721f04010dda57c93aa；直接在3f73f80a上，ready/resync/变异/规格；不含lifecycle.go/main.go |
| 普通 merge | 145acdc879ceeef6388cb798615af616437d3ace | 11340b907b837affd935cdfede8d84bfd51877f9；合入修复路2a8be0ab65af5a6593c69c5d98928349f6934b29 |
| A2 测试/主规格 | e1ae78bc50cbb6c1225027c8995c7cc6c34387ca | 5a954ed7b9d4b996cc2cff22e46f35bb75e752de；6件新增测试/最小主规格 |
| B 历史证据 | 3d89412143ac07e296294c704cbc2741c0c267ac | d5edffb8b3e89a9bcdf6d5cbcf09f1f03082c187 |
| mux构造修正及封口回归 | d8e737a4104fd099de2810a36f9a8bdf759afc47 | d49221730b00f50e24e64216610689057f707f834；组长批准的nil非法测试状态修正 |
| S007-full测试 | ba96c8b8f38768a17a5a14ff223f04b2b277f820 | 7c90f92e20c14d9e0aeca90fc80f46237dd9ecad；直接父d8，仅新增测试和说明 |
| 历史原始日志归档/最终执行锚 | d0c294c8ea2863bb9ddeb5838f4ead8c074e0140 | 21a867bf6eb362fe143db6db3cfaa4316f97d4dd；文档证据提交 |

合并冲突逐处处理（5文件）：

- `cmd/workpulse/shutdown_load_test.go`：保留修复路S014三格及正确退码、already_closed计数，保留主流唯一逐席ready和显式resync。回调DB断言先于一般退出断言，反向控制可命中目标。
- `tests/hlc/process_matrix.py`：保留修复路tracked-set、真实写超时及私有TMPDIR覆盖；保留主流反向控制和resync证据；撤分类匹配实际UNEXPECTED_EXIT断言。
- `openspec/changes/hub-graceful-shutdown/design.md`、`context.md`、`tasks.md`：保留双方独立S014语义和主流ready/resync说明，不覆盖历史证据。

main.go/lifecycle.go在merge时与修复路逐字节相同。未合入`coder56solzzc/hlc-cleanup-exit1`/2d3d66d9的夹具改序。客户端先断始终为正向必须通过格。d8之后产品、原S006/S011/S014夹具与process_matrix.py均无diff；仅新增S007-full测试和证据，因此依87b10d9a复用d8未受影响读数，新tip重跑cmd普通/race及canonical。

already_closed依据：钉版`github.com/coder/websocket v1.8.13`（go.mod），`close.go:66`的CloseError、:108/:141的net.ErrClosed及包装错误链；Go标准库EOF/net.ErrClosed/EPIPE/ECONNRESET按errors.Is/As，不按错误文案匹配。取消/超时优先失败，递归遍历errors.Join，逐连接原错保留且计数不依赖实时注册集合。活连接关闭帧写超时为关闭操作失败退1；阶段预算耗尽退2，两者不混写。

## 逐格读数

路径均相对docs/reviews；JSON保留逐阶段耗时、双server结果、剩余项、UTC及逐条原错。最终执行锚d0c；S006/S011/S014及未改包整测执行锚d8。

| 场景 | 实测/判据 | 证据 |
|---|---|---|
| S001空载 | 实际退0，215ms，双监听关闭差25us | hlc-primary-final-gates/pg-strict.log |
| S002有限慢HTTP | 实际退0，2122ms | hlc-primary-integrated-extra/S002/S002-result.json |
| S003不释放HTTP | 实际退2，8003ms，超时强关 | hlc-primary-integrated-extra/S003/S003-result.json |
| S004/S005 SSE/WS/注册 | 完整负载中长流均结束、注册封口；相关race与撤保护目标红 | S006证据及extra/no-registration-seal |
| S006完整混合race50 | 50/50实际退0，停机3122–3660ms；每轮100 HTTP持久化，100 SSE、5 mux/15 bridge、20 helper WS和viewer/回调收尾，late DB=0 | hlc-primary-integrated/S006-race50/run-*/S006-*.json；SUMMARY.json |
| S007局部诊断（保留） | 实际退2，8004ms；不作为完整混合格替代 | hlc-primary-integrated-extra/S007 |
| S007-full普通10 | 10/10实际退2，8005–8006ms | hlc-primary-s007-final/plain10/run-* |
| S007-full race3 | 3/3实际退2，8015–8017ms | hlc-primary-s007-final/race3/run-* |
| S008队列/事务 | 停机封口排尽23条6批；失败首错不覆写，未提交8；超时剩余19/未尝试15；隔离实际退0/1/2/2 | hlc-s008-queue；final-gates/pg-strict.log；extra/queue-mutations |
| S009撤取消/关闭 | no-sse-cancel、no-ws-close对应目标红，恢复源S00650绿 | hlc-primary-integrated-extra/no-sse-cancel、no-ws-close |
| S010重复信号/启动失败 | 重复信号2131ms退0；bind失败149ms退1 | hlc-primary-integrated-extra/S010、S010-bind |
| S011自动恢复 | 自动格红：stop→ready3544ms；ready→helper398ms/SSE412ms/WS413ms；mux gap1、缺失2、重复0，不自动reset | hlc-primary-integrated/S011/S011-recovery.json |
| S011显式resync | 同feed绑定reset_scope＋新iframe_instance；触发→输出210ms、stop→输出8759ms；缺失2→0、重复0；原次序清理退0 | 同目录S011-explicit-resync.json、S011-new-cleanup-shutdown.json |
| S012内网独立错误 | 实际退1，214ms；公网/内网分别记原错 | hlc-primary-integrated-extra/S012 |
| S013喂狗/慢排空 | 实际退0，13521ms，最长通知间隔3000ms；撤喂狗由隔离监督器真实SIGABRT，12668ms | hlc-primary-integrated-extra/S013、no-watchdog |
| S014客户端先断race3 | 3/3退0、already_closed20=先断20、failed0，1214–1216ms | hlc-primary-integrated/S014 |
| S014 Hub先停race3 | 3/3退0、already_closed0、failed0，1214–1216ms | hlc-primary-integrated/S014-hub-first |
| S014存活关闭失败race3 | 3/3真实关闭帧写超时退1、failed20、closed0，217–223ms | hlc-primary-integrated/S014-live-failure |

S007-full每轮与S006同一完整正常负载，另加内网SSE与公网HTTP各1真实不响应取消的handler。超时强关后仍记internal_sse=1、public_http=1；internal_http=1与同一内网SSE重叠，不多算第三个待处理请求。其它Hub种类均0；正常100HTTP全204且重开SQLite计100，100正常SSE/5mux/15bridge/20helperWS/viewer/回调全部收尾，active_callbacks0/lateDB0/rejected1。逐轮阶段耗时和快照见`hlc-primary-s007-final/SUMMARY.json`及原始shutdown/normal-completion JSON；未固定计数冒充真实阻塞。

S011显式操作为真实Hub/View Service协议级模拟，不声称运行浏览器点击；自动恢复结果不由显式恢复覆盖。补回的两条均在新Hub发布，不能据此推断旧Hub内存历史丢失。独立验收仍需按3f698b77d848/eecfbeb489aa判定。

## 反向控制

| 撤除项 | 结果/命中目标 | 证据 |
|---|---|---|
| already_closed分类 | 3次实际退1，客户端先断EXPECT_EXIT0/UNEXPECTED_EXIT目标红 | hlc-primary-integrated/withdraw-classification/receipt.json |
| S007-full超时退码 | 实际及报告改退1，S007_FULL_EXIT_CODE目标红 | hlc-primary-s007-final/mutant/receipt.json |
| SSE主动取消/WS关闭 | 目标断言红 | hlc-primary-integrated-extra/no-sse-cancel、no-ws-close |
| 回调封口 | after-seal DB调用目标红 | hlc-primary-integrated-extra/no-callback-seal |
| 注册封口 | S005_REGISTRATION_AFTER_SEAL目标红，race | hlc-primary-integrated-extra/no-registration-seal/receipt.json |
| 内网错误采集 | 独立错误结果缺失目标红 | hlc-primary-integrated-extra/no-internal-result/receipt.json |
| 独立停机喂狗 | 监督器真实SIGABRT，目标红 | hlc-primary-integrated-extra/no-watchdog/receipt.json |
| 队列排尽/保留首错 | 两项分别在排尽/first_error断言红 | hlc-primary-integrated-extra/queue-mutations |
| mux定时器后台准入 | MUX_TIMER_AFTER_SEAL目标红；恢复后race3绿 | hlc-primary-precheck |

负向测试的runner rc0表示“成功验证目标红”，底层Go rc1/实际非预期退出不写成产品通过。原始编译失败/长TMPDIR错误/第一次整包nil panic及10m超时均留档，无覆盖删除。

## 409与ready屏障

原S006 50轮42绿/8次409，8次都在hlcOpenSSE建负载阶段、没有执行SIGTERM。原父日志无逐次UTC；409绝对时刻、SIGTERM时刻和相减差均不可得，不虚造数值。r47 c248506c同一起步段100轮22次409；bdac3593同段30轮9次；对照均未发SIGTERM。组长eecfbeb489aa裁定既有起步竞争，唯一逐20席ready屏障可留。完整原件、fixture同源SHA及限制见`hlc-readiness-comparison/REPORT.md`；新版50绿不能消除原8红。

## 整包/仓库门与唯一失败

| 检查 | 结果 | 精确证据 |
|---|---|---|
| 合入后vet＋相关包普通 | 最初nil夹具红保留；授权修构造后相关普通整包rc0 | hlc-primary-precheck；hlc-primary-integrated |
| 相关包race | cmd/feishu/fcs/bus/lifecycle通过；m8首次10m超时，后完整45m自然结束为下行结果 | hlc-primary-integrated/related-race.log |
| m8完整race45 | 2648.265s，rc1，仅TestCompletedTotalCapAt100kIsBatchedAndCheapAfterwards时限FAIL；无DATA RACE | hlc-primary-integrated-tail/m8-race45.log:4783 |
| PG严格相关整包 | HUB_TEST_PG_STRICT=1，cmd/m8/feishu/fcs/bus/lifecycle/store/repo/pgtest rc0，04:26:00–04:33:41Z | hlc-primary-final-gates/pg-strict.log、RUN.json |
| 最终cmd vet/普通/race | d0c上全包rc0；普通190.317s，race399.057s | hlc-primary-s007-final/cmd-{vet,plain,race}.log |
| 全仓OpenSpec strict | d0c上14/14 rc0，文档提交前再次核验 | hlc-primary-s007-final/openspec-strict.log |
| 最终canonical | d0c上05:06:06.116705–05:14:49.445426Z，rc0；helper两profile各1097/inventory90，Python27/shell2 | hlc-primary-s007-final/build-tests.log、RUN.json |
| 钉版SDK独立进程 | 真实loopback连接关闭，外层接收器返回；SDK Start静态等待帧1由进程回收 | hlc-primary-integrated-extra/pinned-sdk-process.log |

最终canonical SHA256：`89127e9ab48a0b2f001552603480f35f3ad04682a64b9bb150c3d46ae1f42e9f`。d8旧canonical rc0原样留`hlc-primary-final-gates`，不当最终新测试源码的仓库门。

m8阈值原值50ms（replay_guard_test.go:555–556），整包60.688101ms。四组同机同白名单/锁venv/短TMPDIR各10次：r47普通10/10（8.701180–9.728580ms）；r47 race0/10（60.053852–65.525438ms）；d8普通10/10（8.588711–10.282195ms）；d8 race0/10（56.201929–63.444921ms）。amr.go/replay_guard_test.go/amr_test.go两锚逐blob相同，停机修复无该路径diff，符合a7d6e937判据：既有race构建下时限过紧，非本单回归。保留整包rc1，不改阈值、不跳过、不用窄测替代整包；详`docs/findings/replay-cap-race-timing-c248506c.md`、`hlc-replay-cap-control/RESULTS.json`。原log SHA256：`9f8423d547d5f4c6a1846999ed6df8808d12533c2cb46f1b46f9fba8018dadf3`。

运行期队列best-effort与事务确认边界保持原裁定；停机专用SealAndFlush不扩大为跨action原子性承诺，旧queue-ack-durability FINDING仍在。SDK内层Start残余不冒称全部goroutine join。

## SKIP与T-04～T-11

逐项清单见[SKIPS.md](SKIPS.md)（含每个CLI子格及原因，默认SKIP与canonical/显式覆盖分别标注），原始集合见FINAL-SKIPS.json。PG严格无因缺PG而静默SKIP；浏览器/独立旧二进制/真实CLI和已延期模型格按清单保留，不合并为通过。

| 任务 | 状态与依据 |
|---|---|
| T-04 | 已完成开发前置：e1ae78bc最小主规格/context；队列/注册/SDK拓扑裁定与边界明确 |
| T-05 | 开发完成：双HTTP并发关监听、单次协调、注册/后台准入封口；S001/S005/S010及相关race/撤保护证据 |
| T-06 | 开发完成：流取消/WS关闭、回调封口等待、队列专用排尽及DB关闭顺序；S00650/S008/S014及撤保护证据 |
| T-07 | 开发完成：阶段预算/0-1-2分类、双server原错/剩余快照、独立喂狗；S003/S007-full/S012/S013证据 |
| T-08 | 逐格执行完成：完整混合race50；S007-full额外普通10/race3及真实剩余项；不以局部S007替代 |
| T-09 | 有效反向控制执行完成：上表每个撤除项均目标红，源恢复绿证据保留 |
| T-10 | 保持未勾：本地自动/显式两格执行并交证据，但自动恢复仍红，独立验收尚未完成；不把显式格写成自动恢复通过 |
| T-11 | 保持未勾：相关/PG/strict/canonical已执行；m8整包race仍rc1既有时限FAIL；独立验收及预发/生产GO未完成 |

本次仅提交开发与验证证据，等待组长独立核证；未进入R10/R9或新R15项目。
