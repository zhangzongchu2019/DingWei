# Tasks

初始派单 S-HUB-GRACEFUL-SHUTDOWN-PROPOSAL；唯一执行者 coder56solzzc.zzc。T-01/02已完成；本次S-HLC-T03-WRITEBACK授权从4ad4ba757cd402fb959700d5294453a60fba7685只回写T-03事实闭合子项。HLC-PM01～04仍待业主定值，T-03总项保持未完成，T-04之后未授权。原固定源码基线2af469dd6919d8eb6064aa7da0e646f0851becf8，分支opus50tc2k/hub-graceful-shutdown-spec，仅写本变更目录。

## 1. 提案与校验

- [x] T-01 读取r46固定源、2b4b624c定位/补遗和67f55bbd现场证据，参照r5/r11编写proposal/design、新能力context/增量spec及任务；单列PM点、数值矩阵与所有未确认假设。
- [x] T-02 使用OpenSpec1.16.0/Node≥22.12/OPENSPEC_TELEMETRY=0严格校验、运行仓库要求的构建测试门，检查仅指定目录文档改动；推工蜂并核远端tip，提交冻结交独立审阅。校验仅证明结构，不能证明场景通过。

## 2. 实施前冻结（待另单）

- [ ] T-03 负责人审阅与事实子项已闭合；HLC-PM01～04仍待业主定值及获批后写回，故总项未完成，不开放实现/部署。
  - [x] T-03.1 负责人已对37a414ff评审通过，4ad4ba75包含两项小改及HLC-001～009；e43100e3核预发/现网TimeoutStopSec=30s且无drop-in。
  - [x] T-03.2 c90f41c2核KillMode=control-group、SendSIGKILL=yes/FinalKillSignal=SIGKILL及有效W=12秒；ed797ae6核systemd259停止期不执行看门狗，原12秒/15秒冲突解除。HLC-009保留防御性要求，S013隔离监督器格原文不变，只验Hub通知连续性；停止期最终强制兜底为30秒→SIGKILL。
  - [x] T-03.3 c90f41c2核控制器systemctl stop命令期限40秒，返回后轮询上限30秒、间隔1秒，顺序40+30秒，覆盖外层30秒；不移作应用预算。
  - [x] T-03.4 ed797ae6核C五次停用开销0.106086～0.106635秒、TERM约+0.010秒；保守δ≈0.11秒，δ+15+5=20.11≤30。真实连接快照8793为4、443为1、临时端口28；不冒称峰值或负载验收。r44/r45指定切换10.009208/10.011406秒、status1已记context。
  - [ ] T-03.5 HLC-PM01～04仍待业主定值；proposal四行原样保留，候选参数未获批，不把组长事实裁定当PM决策。
- [ ] T-04 获实现派单后补主规格最小基线/context并冻结队列/事务确认、WS注册屏障与生命周期挂点；确保与r46及其它在途变更无冲突。

## 3. 实现（未授权）

- [ ] T-05 实施HLC-001/002双端并发停止、单次协调和注册屏障，run显式调用Hub.Close；不以已有无限等待和5秒日志假定结果成功。
- [ ] T-06 实施HLC-003/004长流取消、WS关闭、生产者/队列/存储顺序；保持持久恢复与确认语义，补事务失败证据。
- [ ] T-07 实施HLC-005/006预算、可区分非零退出码、阶段耗时/强关/双server错误及剩余分类记录；接HLC-009独立停机喂狗至最终退出、通知轨迹及有界失败，无SuccessExitStatus掩盖。

## 4. 逐格验证与交审（未执行）

- [ ] T-08 执行HLC-S001空载、S002有限慢HTTP、S003不释放HTTP、S004活跃SSE、S005活跃WS/注册竞争、S006混合、S007混合超时、S008持久化失败、S010重复信号/启动失败、S012内网独立错误、S013超过12秒有界排空/看门狗连续性；按design数值/退出码逐格记录，负载格随机50轮及race，原始日志附固定坐标。
- [ ] T-09 执行HLC-S009主动取消/WS关闭撤除及S005屏障撤除、S012错误采集撤除、S013停喂触发SIGABRT反向控制；每个变异在目标断言红、恢复后绿，无效变异保持未通过。
- [ ] T-10 执行HLC-S011隔离stop→ready和helper/三类流恢复格，记录502/503/EOF/WS观察、消息ID/游标/gap及重复/缺失数，核对HLC-PM04冻结数值；交独立验收，不自验收。
- [ ] T-11 未来实现完成后跑相关Go/race/构建门、全OpenSpec strict，更新代码context/逐格证据；另行申请预发及生产GO，不能以提案归档或tasks勾选代替发布。

## Workflow follow-up

strict规划通过不代表实现/业务验收/发布完成；不archive、不进入apply、不改r46集成或生产配置。所有产品场景当前未执行，不能将T-01/02当作T-08～10完成。

## 交接记录

- 2026-11-01T14:15:22Z · coder56solzzc.zzc · 内容commit 7aba4f1b6e1787e8efd35d8ae06c6b5189bcdc03（本条交接及T-02勾选为后续追加，最终tip/tree见交件消息）。
  - 完成：T-01/02提案编写与结构/仓库检查；OpenSpec1.16.0、Node22.23.1、OPENSPEC_TELEMETRY=0，`openspec validate hub-graceful-shutdown --strict --no-interactive`及`openspec validate --all --strict --no-interactive`均rc=0，全仓13项通过、0失败。8条需求、12个唯一场景，需求正文各不足500字、spec不足30KB。`scripts/run-build-tests.sh` rc=0，helper两profile各1092项、inventory90、Python27组/shell2组；普通dev用户、隔离HOME/临时DB及loopback测试环境，无真实Feishu/生产连接。仅6个本变更目录文档，基线为2af469dd；推送后以ls-remote核同tip并冻结，不自验收。
  - 阻塞：负责人独立审阅；HLC-PM01～04与HLC-A01～04未闭合，尤其systemd TimeoutStopSec待prodops实测。产品生命周期S001～012与变异均未执行；严格结构检查及既有仓库门不代替这些验收。
  - 下一步：工蜂按冻结坐标交审；先另单实测并冻结PM项，再另派实现。不改r46集成分支，不apply/archive，不部署或调整systemd。

- 2026-11-01T14:16:53Z · coder56solzzc.zzc · 本段起点commit 37a414ff050026cc649dcdef13f6633eea252bac，承载本段的追加提交tip见交件消息。
  - 完成：独立fetch并核review/prodops/r5-t08-facts-20261101完整tip e43100e3bdf200caaa67a925e51ca5fd82287bff；同步proposal/design/spec/context/T-03，关闭“TimeoutStopSec未知”这一子项：预发/现网均30秒、无drop-in。以30秒为外层，保留待批应用15秒候选及名义15秒差额，扣除信号交付差δ后要求仍≥5秒SIGKILL前余量；配置值不作停机完成证据。OpenSpec1.16.0/Node22.23.1/telemetry0全仓strict重验rc0，13/13通过；产品文件未变，既有构建门rc0保持为上一内容提交的记录，不冒称重跑或生命周期验收。
  - 阻塞：其余单位停止参数、δ、控制器期限及负载/持久边界仍待补测；PM01～04仍待批。旧交接中TimeoutStopSec待测状态由本条新证据覆盖，历史记录保留。S001～012未执行。
  - 下一步：仅推同一工蜂分支后冻结，按新tip审阅；不实现、不改systemd/部署，不把外层30秒配置写成可在30秒完成的承诺。

- 2026-11-01T14:27:21Z · coder56solzzc.zzc · 本段起点commit eeb104a3cf2df9fb9f888fabfc08cd7237fc3eb7，追加提交tip见交件消息。
  - 完成：按组长对37a414ff的独立评审通过与两项小改，HLC-A01明确S=30满足S≥T+5（T=15），列剩余KillMode/SendSIGKILL/停止期watchdog/控制器轮询上限待测，r45约10秒仅为观测。选择排空期间继续喂狗至最终退出，新增HLC-009及S013（现共9条需求、13个场景），补13～15秒不被SIGABRT打断及撤除继续喂狗的目标红反证；候选预算与四项PM待决保持原样。组长另报原对象独立run-build-tests rc0，此为组长记录，本次未冒称重跑；产品仍未改动。
  - 阻塞：看门狗生命周期交接、通知轨迹、目标systemd停止行为/其余参数及控制器期限仍待实测；PM01～04由组长于r47规划上报业主，当前不改决策。新增S013与原生命周期矩阵均未执行；OpenSpec1.16.0/Node22.23.1/telemetry0全仓strict重验rc0，13/13项通过；不代替生命周期验收。
  - 下一步：strict通过后同分支追加提交并核远端tip后冻结，实现另派；不改r46、systemd或部署。

- 2026-11-01T21:22:16.710636+00:00 · coder56solzzc.zzc · S-HLC-T03-WRITEBACK，追加起点commit 4ad4ba757cd402fb959700d5294453a60fba7685，最终tip/tree见交件消息。
  - 完成：独立fetch核ed797ae6de1f49cc2ad755946f1f3db2147bd8af（父4bfcca6b）及c90f41c2a3e090b34ad17070f5d8beed589cbd7d；仅本变更5份文档回写。HLC-009保留跨版本防御，目标systemd259停止期不检测、原12秒/15秒冲突解除，最终强制兜底仅30秒TimeoutStopSec→SIGKILL；S013 spec原文、design矩阵及反向控制均逐字节保留，context限定为Hub通知连续性。T-03.1～.4事实子项勾选：控制器顺序40+30秒、δ≈0.11秒与0.11+15+5=20.11≤30、连接快照4/1/28及r44/r45约10秒status1。PM01～04四行逐字节保留待业主，T-03总项/T-03.5不勾选。
  - 验证：OpenSpec1.16.0/Node22.23.1/OPENSPEC_TELEMETRY=0，全仓strict13/13 rc0；本轮实际重跑bash scripts/run-build-tests.sh，2026-11-01T21:13:01.410986+00:00～2026-11-01T21:21:50.001345+00:00，rc0，helper两profile各1092/1092、inventory90、Python27组/shell2组。普通dev用户、锁helper-deps-lock-venv、环境白名单/临时HOME，未继承真实SH/DINGWEI/DW凭据。原始日志本席/tmp/hlc-t03-build.log，SHA256 b6e7e26d78e887602bbbcec75d9712be7959942417c52e4e53ff6300ac5e74cb；strict日志/tmp/hlc-t03-strict.log。已核9条需求/13场景及编号不变，正文各不足500字、仅本目录文档、diff-check；交件前对最终文档再次strict核验。既有门通过不代替HLC产品生命周期或S013验收。
  - 阻塞：本单事实回写无阻塞；HLC-PM01～04仍待业主定值，Hub实现/通知轨迹/负载排空与持久边界均未执行。历史交接的停止参数/δ待测状态由本条事实更新覆盖，不改历史提交。
  - 下一步：同分支普通push追加提交、核ls-remote后冻结；等待PM决策和另派实现，不实现、不apply/archive、不部署、不改单位/控制器/运行实例。
