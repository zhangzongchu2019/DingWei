# R61 N-3 历史来源保留：实现交件

基main 206d41b26dab646c0e50a111eed5a45ccf7c72f6。原样设计导入c3b3db27c，对6a60a1a6指定目录逐字节相同；首提交只新增该目录7文件。后续实现在本任务分支，SOURCE_COMMIT=a32c877cf6ef21a909d2806f4b08e3e925d05d98。不推main、不部署，不构建/上传发布件；build.sh --test-only只跑规定构建门。

## 实现与迁移

五元组为owner_key/agent_key_id/session_name/history generation/delivery_id，echo顶层delivery_id必须等于origin_delivery_id、turn_id非空、data.lifecycle=admitted、kind=user_input。只采Hub canonical primary可信来源，helper声明仍先剥除；重名更换不继承来源，路由传入入队前捕获的认证目标。无法证明目标稳定身份或sidecar写失败则关闭图标，不影响durable投递；不从正文、地址或邻卡补猜来源。目标离线而当时无法取得认证身份时同样缺证据，不能声称该分支显示图标。

history DB单独新增history_migrations/0078_history_provenance.sql。读取流程为fs.Glob+sort.Strings、按完整文件名查ledger、既存digest必须相等，否则拒启；没有连续编号约束。由当前0001直接到0078合法，已测升级及digest拒绝。应用库internal/store/migrations最新0077保持原字节，无新DDL。

Remember只存最小来源，无正文、open_id或凭据；4KiB记录上限。侧表增删触发器在同一history事务计入Agent64MB/global2GB预算。Append group-commit事务内查证据、增强并重新计费、写事件与消费sidecar；失败全部回滚。增强后超过既有1MB，事件不写、不消费sidecar，记oversize/gap；sidecar保留至TTL，合法重试仍可消费，不追加第二条事件。

TTL、agent/global eviction、queued事件淘汰、generation变化、关闭及删除队列均清理相应sidecar并保持计费一致；仅sidecar被淘汰不伪造正文gap。冲突同键置不可消费标记，不last-write-wins，不因重放恢复可信。旧/无sidecar事件原JSON字节不改、不追溯增强；Page/HTTP envelope保持原样，renderer生产文件未改。

## 设计第6节逐格覆盖

| 判据 | 回归 |
|---|---|
| Agent/timer真实入站→DB/HTTP/SSE→刷新 | TestHistoryProvenanceRealHubAgentTimerReplay；新增history-provenance-browser-regression.js；认证WS和HTTP真实运行，echo为合成协议事件，无模型，无生产数据 |
| queued/turn/五元组/不同origin/缺证据/冲突/客户端伪造/信任 | TestHistoryProvenanceExactAssociationAndFailClosed；ObservationRejectsCopyAndUntrusted；UsesCapturedTargetNotReusedName；RoutingPinsIdentityBeforeEnqueue |
| 同名替换时序 | 真Enqueue wrapper在入队提交后替换在线槽；replacement不得继承，原捕获ID仍可匹配；不借第二PG连接 |
| 迁移升级、编号、ledger | MigrationDigestAndNonconsecutiveNumber；LegacyUpgradeAndUnmatchedBytes；现基无占号冲突，顺延是预检选择规则，不虚构自动重编号行为 |
| 4KiB/配额/TTL/清理 | IdempotencyTrustAndBounds；LifecycleAndEviction；EventEvictionAlsoCleansQueuedEvidence；既有SessionHistoryQuota/Retention/Deletion测试 |
| SQL失败与增强超限 | AppendRollbackAndOversize：SQL插入失败回滚消费；增强超限保留sidecar、0事件、oversize gap，再合法同event ID重试只写1条 |
| 删保护 | 最终40刀业务断言红；临时detached树逐刀复原+最终基线；不合并旧31/34/38或D181的40刀；4KiB边界一刀含Go与SQL CHECK两处冗余防线 |

## 完整门

由gates.json逐门给出命令、SOURCE_COMMIT、开始结束UTC及真实rc。Go原始-v文件保留在本机任务证据目录，SHA列在raw-logs.json；SKIP逐名及条件见SKIPS.json，不把未选中测试或未运行门当通过。最终六门及首轮D181 m8失败的完整原始日志经敏感输出扫描后以gzip原字节入Git，解压SHA与原日志SHA一致；其余历史/中止日志留本机并给SHA。小型变异与浏览器日志一并入件。


当前状态：完整六门全部rc0。PASS数为日志行（含父子层级、count=3重复累计），不是不同业务场景数。

| 门 | rc | 顶层PASS日志行 | 含子项PASS日志行 | SKIP日志行 | interrupted日志行 |
|---|---:|---:|---:|---:|---:|
| browser-full | 0 | 0 | 0 | 0 | 0 |
| strict | 0 | 0 | 0 | 0 | 0 |
| build-canonical | 0 | 6 | 21 | 0 | 0 |
| store-full3 | 0 | 1344 | 2343 | 15 | 0 |
| m8-full3 | 0 | 2970 | 5004 | 87 | 4 |
| focused-race | 0 | 40 | 84 | 1 | 0 |

浏览器receipt为DW_BROWSER_RECEIPT_V1文本，46套必须各自rc0；strict实际42/0。canonical以BUILD_TESTS_OK为判据，表内其Go子测试日志行不是完整Go包统计。


## 历史失败与范围

初期夹具有编译导入/组权限/未发能力帧/短名SSE定位错误，均修正后全链路重跑，日志原样留存。本机首轮canonical PATH误选系统Python导致4个helper模块无收集用例，rc1；锁定venv后完整重跑，首轮不抹除。早期因测试加强和目标身份绑定而中止的构建轮次不计PASS。

最终40个有效cut均业务断言红，0幸存、0编译错、0panic。开刀前三项与还原后两项基线均rc0；此前31/34/38刀分别属于旧提交及中间测试锚，分开保留、不外推、不叠加。

完成：实现及六门作者验证；无实现阻塞。待完成：独立复核与组长终审，作者结果不替代这两项。下一步：togpt60独立复核，组长决定集成；不自行并main/部署/发布。

设计同步：开工时首提交仍严格等同指定6a60a1a6；复查设计分支最新8107bc5a时发现仅追加已派单明确的oversize定稿，交件文档同步该措辞与第6节第5格，不改变冻结产品/测试源。

首轮D181完整m8三遍rc1：2969顶层/5003含子项PASS，87次逐名SKIP，唯一FAIL TestSessionNameReconnectKeepsNameAndReleaseReuses hub_test.go:3505 interrupted(9)，与旧N-4落点相同；不擅自沿用r60豁免。浏览器首轮rc3尚未执行用例，原因是新增Go夹具的4个环境名未登记；a32c877cf仅补runner登记，Hub/store逻辑和Go测试相对D181零差异。旧日志、首轮rc及SHA均保留，最终所有门在A32冻结源重新执行，不用单跑覆盖首轮。

## 复核入口

仓库 `git@git.code.tencent.com:wego-ai/dingwei.git`；实现分支 `coder56solzzc/r61-n3-history-provenance`。只读获取本分支及设计来源分支 `togpt60lunazzc/r61-n1-busy-wait`，以确保批准锚6a60a1a6对象存在，再从仓根运行 `python3 docs/reviews/r61-n3-history-provenance/VERIFY.py` 与 `sha256sum -c docs/reviews/r61-n3-history-provenance/SHA256SUMS`。核验脚本只读Git对象/本地文件，不联网、不启动服务；核验成功仅说明作者证据自洽，不替代独立复跑。

### 实际读取范围说明

产品与测试固定SOURCE_COMMIT；完整门执行期间工作树仅追加docs/openspec交件文档，未改冻结产品/测试。OpenSpec strict检查实际交件文档工作树；最终文档提交后另核strict并记录具体提交坐标，不把旧设计模板的strict冒称为最终文档strict。浏览器先完整执行，再启动独立临时树的变异，避免洪泛速率门受并发测量影响。

### 回放夹具的真实性边界

两例使用本地认证WS入站、真实SQLite与真实HTTP GET `/view/Kimi.zzc/history`；helper admitted echo为明确构造的协议帧，不运行模型CLI。SSE ID来自真实SSE handler的ring快照。浏览器另起本地renderer，用从同一DB/API导出的history响应加载页面，实时events流为空，确保图标仅由历史提供；这不是浏览器与原测试Hub同时在线的全链路生产重放。夹具身份、正文、delivery/message/turn等均为本地合成数据，无在线席位或他席历史。

诊断计数与测试失败分开报告：最终m8即使rc0，也不得据此写“0 interrupted”。逐轮interrupted行号保留在parsed-gates.json；关停阶段lease release日志里的同名字串不自动证明与断言失败同一机制。首轮D181的hub_test.go:3505断言失败仍如实保留，不能以最终完整重跑或单测通过抹除。

本单没有修改modernc驱动或声明修复N-4；最终完整重跑通过也不是N-4已消失的证据。

作者原始证据提交：`9a145fd138c7d15bf1ec027b773b960919e69056`（源码与六门/40刀/首轮失败均可git cat-file）；tasks.md已附此不可变坐标。完成实现与作者验证，无实现阻塞；独立复核及组长终审仍待办。

最终文档strict：`b3d84844bcff0e8bea4ba4573a2d8018dd02fd06` 上完整 `validate --all --strict` 为42/0、rc0；命令/UTC/原日志SHA见evidence/final-strict.json，后续证据提交不再修改openspec目录。
