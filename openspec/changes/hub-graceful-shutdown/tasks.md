# Tasks

派单 S-HUB-GRACEFUL-SHUTDOWN-PROPOSAL；唯一执行者 coder56solzzc.zzc。当前只授权T-01/02，r47候选；T-03之后均未授权未开始。固定基线2af469dd6919d8eb6064aa7da0e646f0851becf8，分支opus50tc2k/hub-graceful-shutdown-spec，仅写本变更目录。

## 1. 提案与校验

- [x] T-01 读取r46固定源、2b4b624c定位/补遗和67f55bbd现场证据，参照r5/r11编写proposal/design、新能力context/增量spec及任务；单列PM点、数值矩阵与所有未确认假设。
- [x] T-02 使用OpenSpec1.16.0/Node≥22.12/OPENSPEC_TELEMETRY=0严格校验、运行仓库要求的构建测试门，检查仅指定目录文档改动；推工蜂并核远端tip，提交冻结交独立审阅。校验仅证明结构，不能证明场景通过。

## 2. 实施前冻结（待另单）

- [ ] T-03 负责人独立审阅HLC-001～008；已核e43100e3预发/现网TimeoutStopSec=30s且无drop-in；prodops另单补测信号交付差、其它停止参数、watchdog/重启、控制器期限及真实连接规模；PM01～04定值写回并strict。未满足不开放实现/部署。
- [ ] T-04 获实现派单后补主规格最小基线/context并冻结队列/事务确认、WS注册屏障与生命周期挂点；确保与r46及其它在途变更无冲突。

## 3. 实现（未授权）

- [ ] T-05 实施HLC-001/002双端并发停止、单次协调和注册屏障，run显式调用Hub.Close；不以已有无限等待和5秒日志假定结果成功。
- [ ] T-06 实施HLC-003/004长流取消、WS关闭、生产者/队列/存储顺序；保持持久恢复与确认语义，补事务失败证据。
- [ ] T-07 实施HLC-005/006预算、可区分非零退出码、阶段耗时/强关/双server错误及剩余分类记录；无SuccessExitStatus掩盖。

## 4. 逐格验证与交审（未执行）

- [ ] T-08 执行HLC-S001空载、S002有限慢HTTP、S003不释放HTTP、S004活跃SSE、S005活跃WS/注册竞争、S006混合、S007混合超时、S008持久化失败、S010重复信号/启动失败、S012内网独立错误；按design数值/退出码逐格记录，负载格随机50轮及race，原始日志附固定坐标。
- [ ] T-09 执行HLC-S009主动取消/WS关闭撤除及S005屏障撤除、S012错误采集撤除反向控制；每个变异在目标断言红、恢复后绿，无效变异保持未通过。
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
