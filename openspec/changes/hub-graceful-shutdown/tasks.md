# Tasks

## r48 集成交接（2026-11-02）

- 2026-11-02T06:16:16Z · coder56solzzc.zzc · S-R48-INTEGRATION，依组长 a78e308e 接管令于06:15:12Z接任唯一写者；源码基于81849a388d28bec732848227b548be9a894addc3，普通合并04f7deb0d128a19ff45fb2bceb5f76d9ca40c399，合并提交cabeedab68a19b05d9ca10afcacbe8d6ab7f7664。
  - 完成：无冲突；merge-tree复算为615e28be484d7a63afedacba715d541e5a4394d5，与合并树一致。三处版本同步0.2.3.23、lastReleased为0.2.3.22，固定发布钉版测试同步；页面源码不写版本。View运行件与迁移对比见docs/reviews/r48-integration/PREFLIGHT.json。R5绑定页、R10、R9、R14、R15不在集成范围。
  - 阻塞：本轮新源码门尚未执行，不挪用81849a38门作新tip通过；m8 race既有时限finding、S011自动恢复红继续保留，T-10/11待预发/现网验收，不自验收。
  - 下一步：干净提交后以精确源码串行运行canonical仓库门、版本与Go相关包/整包race（m8 45m）、PG严格整包逐条SKIP、浏览器总runner两渠道、全仓OpenSpec strict；推送核ls-remote后冻结。无部署授权。

初始派单 S-HUB-GRACEFUL-SHUTDOWN-PROPOSAL；唯一执行者 coder56solzzc.zzc。T-01/02及此前T-03事实子项已完成；本次S-R48-PM-WRITEBACK授权从ae9b809b6221eb6e14ab91070357fd22edbf9aef按rulings㉒只回写PM已定值。HLC-PM01～04已定，T-03总项闭合；后续S-HLC-IMPL已授权，当前T-04～09开发与本地执行完成，T-10/11待独立验收和门例外处置。原固定源码基线2af469dd6919d8eb6064aa7da0e646f0851becf8，分支opus50tc2k/hub-graceful-shutdown-spec，仅写本变更目录。

## 1. 提案与校验

- [x] T-01 读取r46固定源、2b4b624c定位/补遗和67f55bbd现场证据，参照r5/r11编写proposal/design、新能力context/增量spec及任务；单列PM点、数值矩阵与所有未确认假设。
- [x] T-02 使用OpenSpec1.16.0/Node≥22.12/OPENSPEC_TELEMETRY=0严格校验、运行仓库要求的构建测试门，检查仅指定目录文档改动；推工蜂并核远端tip，提交冻结交独立审阅。校验仅证明结构，不能证明场景通过。

## 2. 实施前冻结（前置已闭合）

- [x] T-03 负责人审阅、停止事实及HLC-PM01～04定值回写已闭合（rulings㉒）；本勾选只关规格前置，不开放实现/部署。
  - [x] T-03.1 负责人已对37a414ff评审通过，4ad4ba75包含两项小改及HLC-001～009；e43100e3核预发/现网TimeoutStopSec=30s且无drop-in。
  - [x] T-03.2 c90f41c2核KillMode=control-group、SendSIGKILL=yes/FinalKillSignal=SIGKILL及有效W=12秒；ed797ae6核systemd259停止期不执行看门狗，原12秒/15秒冲突解除。HLC-009保留防御性要求，S013隔离监督器格原文不变，只验Hub通知连续性；停止期最终强制兜底为30秒→SIGKILL。
  - [x] T-03.3 c90f41c2核控制器systemctl stop命令期限40秒，返回后轮询上限30秒、间隔1秒，顺序40+30秒，覆盖外层30秒；不移作应用预算。
  - [x] T-03.4 ed797ae6核C五次停用开销0.106086～0.106635秒、TERM约+0.010秒；保守δ≈0.11秒，δ+15+5=20.11≤30。真实连接快照8793为4、443为1、临时端口28；不冒称峰值或负载验收。r44/r45指定切换10.009208/10.011406秒、status1已记context。
  - [x] T-03.5 已独立核0659d208第㉒条：HLC-PM01超时2/正常0/其它1；PM02总15秒、阶段1/8/3/1/2、外层30秒；PM03单实例有界；PM04 stop→新版ready≤20秒、helper与推送流各≤5秒。原选项保留，业主定值与此前组长事实裁定分开追溯。
- [x] T-04 获实现派单后补主规格最小基线/context并冻结队列/事务确认、WS注册屏障与生命周期挂点；确保与r46及其它在途变更无冲突。

## 3. 实现（S-HLC-IMPL已授权，尚未交审）

- [x] T-05 实施HLC-001/002双端并发停止、单次协调和注册屏障，run显式调用Hub.Close；不以已有无限等待和5秒日志假定结果成功。
- [x] T-06 实施HLC-003/004长流取消、WS关闭、生产者/队列/存储顺序；保持持久恢复与确认语义，补事务失败证据。
- [x] T-07 实施HLC-005/006预算、可区分非零退出码、阶段耗时/强关/双server错误及剩余分类记录；接HLC-009独立停机喂狗至最终退出、通知轨迹及有界失败，无SuccessExitStatus掩盖。

## 4. 逐格验证与交审（本地已执行，待独立验收）

- [x] T-08 执行HLC-S001空载、S002有限慢HTTP、S003不释放HTTP、S004活跃SSE、S005活跃WS/注册竞争、S006混合、S007混合超时、S008持久化失败、S010重复信号/启动失败、S012内网独立错误、S013超过12秒有界排空/看门狗连续性；按design数值/退出码逐格记录，负载格随机50轮及race，原始日志附固定坐标。
- [x] T-09 执行HLC-S009主动取消/WS关闭撤除及S005屏障撤除、S012错误采集撤除、S013停喂触发SIGABRT反向控制；每个变异在目标断言红、恢复后绿，无效变异保持未通过。
- [x] T-10 执行HLC-S011隔离stop→ready和helper/三类流恢复格，记录消息ID/游标/gap及重复/缺失数，核对HLC-PM04冻结数值。mux自动恢复格按HLC-008与裁定3f698b77d848照实记gap/缺失；显式resync后5秒内恢复是验收门。独立验收证据：`docs/reviews/hlc-s011-acceptance/RESULT.md`及同目录`raw/`。
- [ ] T-11 未来实现完成后跑相关Go/race/构建门、全OpenSpec strict，更新代码context/逐格证据；另行申请预发及生产GO，不能以提案归档或tasks勾选代替发布。 【乙；交接（组长 2026-11-08 更正）：停机修复已随 r48 上线；本项剩独立 QA 对 S011 的验收与证据回写，不需另行实现。】

## Workflow follow-up

strict规划通过不代表实现/业务验收/发布完成；不archive、不进入apply、不改r46集成或生产配置。产品场景当前执行读数见docs/reviews/hlc-primary-integrated/REPORT.md；T-01/02结构校验不替代这些运行证据。S011自动恢复仍红，m8完整race保留1项既有时限FAIL；不据本地勾选自验收或发布。

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

- 2026-11-01T23:19:36.527822+00:00 · coder56solzzc.zzc · S-R48-PM-WRITEBACK，追加起点commit ae9b809b6221eb6e14ab91070357fd22edbf9aef；最终tip/tree见交件消息。
  - 完成：独立fetch核0659d208ce731b73206b06d587c3814649ae11d2第㉒条，业主23:07:47Z答ok；HLC-PM01～04已定并逐项回写proposal/design/spec/context，原4行选项/建议逐字节保留为历史，需求/场景编号不变。T-03及T-03.5闭合，T-04起保持未授权；停止期看门狗防御要求及S013隔离边界不扩大。
  - 验证：本分支本轮独立bash scripts/run-build-tests.sh 2026-11-01T23:10:21.786445+00:00～2026-11-01T23:19:20.011552+00:00 rc0，helper claude 1092/1092、codex 1092/1092、inventory90、Python27组/shell2组；普通dev、锁venv、独立临时HOME/环境白名单，无继承SH/DINGWEI/DW凭据。日志/tmp/r48-pm-hlc-build.log，SHA256 1107a5a187203d464db1edf95e6ee4f88beb7f41831c1cfc60fd28fd8142c8ee。全仓OpenSpec1.16.0/Node22.23.1/telemetry0 strict rc0（提交前最终文档复核），diff-check和目录外0改动检查；旧门通过不代表新产品场景已执行。
  - 阻塞：本单PM编号对应无阻塞；原待PM记录为历史，最新㉒已定表覆盖当前口径。实现/逐格反向验证/真实运行前置仍另派，不将PM批准或T-03勾选当上线验收。
  - 下一步：仅相应变更目录文档追加提交、普通push核ls-remote后冻结；按组长顺序等待Hub停机修复→R10→R9另单实现，均随r48。本单不实现、不apply/archive、不申请新写权限、不做部署。

- 2026-11-01T23:43:51.359072+00:00 · coder56solzzc.zzc · S-HLC-IMPL开发中，当前HEAD ffaa6b27fb4f02660863e0ecc4e8bed41f4be387为r47 c248506c与冻结规格3a73c794的非快进merge；以下产品草稿尚未提交或推送，不作为交审锚。
  - 完成：T-04队列/注册屏障技术裁定已读；运行期best-effort边界、S008限定及queue-ack-durability FINDING已落工作树；未来控制器expected_sigterm_exit1须按0/2区分已记context，不改控制器。生命周期勘察覆盖HTTP、SSE/mux/bridge、三类Hub WS、node WS、Hub后台/定时回调、hosted/cron/Unix notify、队列与SQLite/PG。已起草单次15秒分阶段协调、实例注册门/处理器计数、独立长流取消、专用封口Flush、存储关闭顺序和独立watchdog；尚非完成或验收结论。
  - 局部验证：queue/group/coordinator核心race命令rc0（/tmp/hlc-core-race.log）；现有Hub Close与queue等目标复验rc0（/tmp/hlc-local-step.log）；实际生产装配子进程S001首轮stop 217ms、rc0、监听差16us、五阶段/双server原始结果已记录（/tmp/hlc-evidence/S001-process.log与/tmp/hlc-idle-process-second.log）。首版夹具解析非JSON行停滞而非Hub不退出，已停止该测试父进程并修复逐行解析；首轮/tmp/hlc-idle-process.log保留。上述局部读数不代替50轮/反向变异/PG整包或独立验收。
  - 阻塞：钉版飞书SDK v3.9.7 ws/client.go:206–230的Start成功后永久select{}，ctx取消不使内层Start返回；外层MultiGateway.Start返回不足以证明内部接收器/连接结束。已先报组长，建议停机显式Close连接、封口并等待全部入站回调，纯SDK等待帧注明随进程回收；未改SDK/依赖版本或作此适配，等待裁定。T-04暂不勾选，T-05～11全部保持未完成。
  - 下一步：接SDK边界裁定后继续实现/真实矩阵与反向控制；初步相关包race+PG strict整包正在隔离HOME/环境白名单运行，/tmp/hlc-impl-preliminary-go-race.log及-result.json（完成后生成）。本轮运行中又修过watchdog交接/cron/结果汇总，不能将其结果当最终源码冻结门，完成实现后须重跑最终相关门。全仓strict/run-build-tests、S002～013、50轮、变异、S011恢复尚未完成；不部署、不apply/archive、不自验收、不进入R10/R9。

- 2026-11-01T23:53:09.359773+00:00 · coder56solzzc.zzc · S-HLC-IMPL T-04补充，当前HEAD仍ffaa6b27fb4f02660863e0ecc4e8bed41f4be387；本段产品/测试/证据为未提交草稿，非交审锚。
  - 完成：已从delivery_inbox补读23:23:58Z原裁定及排尽补充；停机专用SealAndFlush封入队、排尽多批及部分batch、保留首错和未提交计数，准入锁/写协程等待受DB阶段约束。实时队列快照在超时竞争时仍记录first_error/failed，运行期Enqueue/Flush保持best-effort。FINDING只记已证两点，业务出站/已发送回执区分，overlay诊断与vet=off原因单列附录。
  - 局部验证：S008确定性23条六批全提交；两批失败时首错保留、未提交8；第二批超时保留19未提交/15未尝试。真实队列接协调器测试0/1/2/2，队列/协调器隔离子进程实际退出0/1/2/2（不冒称完整Hub混合流量验收）；默认vet/race目标格rc0，队列随机顺序50轮rc0。撤排尽/覆盖首错均rc1且红在对应持久性/首错断言；可复跑入口tests/hlc/s008_mutations.py，原始读数docs/reviews/hlc-s008-queue。全仓OpenSpec strict rc0；仓库构建门在跑，结果随后补记。
  - 阻塞：T-04总项仍不勾选，SDK实际运行在Hub还是独立FCS未取得当前实例证据，已向组长请求非敏感Git证据坐标；此前SDK裁定已补读，未修改SDK/独立FCS。完整Hub S006/S011等矩阵、负载随机50轮及独立验收未完成；队列50轮不充当完整矩阵。
  - 下一步：等仓库门结果、证据落盘后继续HLC实现；取得运行拓扑证据再进入获准SDK适配，不进入R10/R9，不部署、不apply/archive、不自验收。

- 2026-11-01T23:57:06.839336+00:00 · coder56solzzc.zzc · T-04排尽补充的最终检查记录，HEAD仍ffaa6b27；本地草稿未提交／推送。
  - 完成：本轮实际bash scripts/run-build-tests.sh rc0，helper两profile各1097、inventory90、Python27组／shell2组；精确UTC与日志SHA见docs/reviews/hlc-s008-queue/build-queue-final.json。运行期边界回归默认vet/race rc0，异步提交失败后Enqueue/Flush仍nil，停机专用SealAndFlush返回首错。全仓strict rc0及diff-check通过；报告/日志/可复跑变异入口均已落盘。
  - 阻塞／下一步：保持T-04总项与后续任务未完成，仍需SDK运行拓扑与完整Hub矩阵、交独立复核；本轮局部自测不构成交件或发布锚。无部署、apply/archive、运行期队列语义或SDK/FCS变更。

- 2026-11-01T23:58:52.335308+00:00 · coder56solzzc.zzc · 队列独立片段已提交并普通push工蜂：opus50tc2k/hub-graceful-shutdown-impl，commit b0f07f377f8f489af64b52c8c2c329244f53e56c，tree 9cf65bf29448770c938f36468eb8307d0c2a40c6；ls-remote一致。仅4件：AsyncDBQueue、队列测试、S008变异runner和FINDING。
  - 完成：停机专用封口排尽及首错/计数片段可独立fetch复核；仓库门/strict/race与上述局部读数保持。
  - 阻塞：Hub协调器／生命周期其它草稿、全矩阵及本地报告尚未提交；协调器子进程读数明确来自本地HLC草稿，不能冒充该队列提交包含完整Hub实现。T-04及后续总项保持未完成。
  - 下一步：等SDK实际拓扑证据，继续既定HLC实现，完成后再交全量Git锚及独立验收；不部署、不apply/archive。

- 2026-11-02T00:14:17.496148+00:00 · coder56solzzc.zzc · SDK裁定落实，起点b0f07f377f8f489af64b52c8c2c329244f53e56c；实现与证据仍待本轮追加Git锚，不自验收。
  - 完成：独立fetch核c15d6d7884f8452bf7717818f329ec192bca5094/tree2ca8a513ee5e7d1255069a235aa419c0eed783d8，现网SDK属Hub、预发fake、无独立FCS；位置前置解除。SDK适配Close→Seal→Wait、迟到拒绝计数/零DB与在途真实返回、可选接线到8秒drain及安全快照已起草；保留SDKStart残余并明确不冒称全部SDK goroutine已join，不改依赖/协议/FCS进程。根回调已准入工作保留context值，改受workCtx/强制截止而非t0接收器cancel约束。
  - 局部读数：真实dispatcher+临时SQLite回调扩展active0/rejected1/totalDB1/afterSealDB0/Close1/SDKStartNotJoined1；撤封口变异afterSealDB1且S006_POST_SEAL_DB_ZERO目标红；回调扩展race随机顺序50轮rc0，不充作完整混合负载50轮。feishu/lifecycle/fcs默认vet/race rc0；SDK实际loopback连接独立进程Close后真实对端断开，Start等待帧1单列，普通go test rc0。默认套件TestReceiverPinnedSDKProcess因必须独立进程回收残余而skip，已显式运行，不计skip为通过。Hub接线race格8秒超时/退出2/不关DB通过；默认fake空载等最终目标复核与仓库构建门仍运行，完成后补记。
  - 夹具修正：钉版SDK首轮把pingLoop的created-by归因误计为第二个Start等待帧，改为匹配实际栈帧后通过；main抽接线函数首轮多一层指针导致编译失败，修正后race通过。原失败日志保留，不当产品故障或有效变异。
  - 阻塞：SDK位置已解除；完整S006混合HTTP/SSE/mux/WS与其它S002～013/恢复/全量反证仍未完成，T-04及T-05～11总项不据局部格勾选，当前不是完整HLC交件。
  - 下一步：冻结本轮源码和逐格证据，待门结束后追加Git并报告；继续完整矩阵，独立验收与部署另走原门，不apply/archive/改独立FCS或R10/R9。

- 2026-11-02T00:16:24.111519+00:00 · coder56solzzc.zzc · SDK本轮门完成，中途源锚拟追加；不是整单交件。
  - 完成：本轮run-build-tests rc0，00:06:09.412389Z～00:14:57.970021Z，helper各1097、Python27／shell2；最终main目标race rc0，含SDK8秒超时分类2／DB不关闭、当前实际Hub fake S001退出0／1227ms／listener差20us及S008。全仓strict rc0、9需求13场景；SDK回调扩展50轮及明确默认skip/独立SDK进程读数均见docs/reviews/hlc-sdk-ingress。
  - 阻塞／下一步：主规格基线和完整HLC矩阵/全量50轮/恢复/独立复核仍未完成，不依据本轮局部格勾选T-04～11；保存实施中途Git锚后继续原单，不把中途锚请作验收或上线。

- 2026-11-02T01:48Z · coder56solzzc.zzc · S006／S011首轮，起点ee7be94d；本段为待追加中途锚，不是交件。
  - 完成：隔离真实Hub装配/临时PG及3个Python VS负载夹具已运行；S006 100HTTP/100SSE/5mux+15bridge/20会话WS/飞书回调完整首轮rc0，停2131ms退出0、阶段0/2119/0/7/0ms、持久HTTP100、迟到DB0/拒绝1、所有业务owned计数0。S011首轮rc1，补齐时间点后仍红：stop→READY2202ms、20helper36ms、SSE/WS46ms；mux明确epoch_changed gap后5秒缺失2、重复0，未自动reset凑绿。证据docs/reviews/hlc-process-first/REPORT.md。
  - 阻塞：新增S011与现有mux显式resync协议的口径疑点已报组长dw-dlv_30213f93175447ed7db62ae303ac584e；其它验证可继续，不阻塞S006随机50轮。
  - 下一步：S006混合随机50/race已起，随后其余反向控制→PG严格相关整包→仓库门，暂估04:30Z须据S011裁定/实测校准；不据局部读数勾选总项、不提前执行预告任务。

- 2026-11-02T02:12Z · coder56solzzc.zzc · 按组长7ae62a07b8bf单写裁定整合，bdac3593推送执行流为唯一写者。
  - 完成：删除重复早期resync夹具，仅保留一套实际客户端同feed reset_scope＋新iframe_instance流程；S011自动红与显式格分列的3f698b77d848已同步design/context/spec，T04主能力最小历史基线/context已补。新增真实故障格S002/S003/S007/S010/S012/S013及20真实WS注册竞争格；局部读数不勾整单。
  - 阻塞：peer-first清理停机退1经21959f655019判为本单产品缺陷，由已批准独立工作树修，主流待核证后合入；不能以改变客户端关闭顺序洗白。原混合50轮42过/8次起步409保留；逐席ready屏障版50轮rc0须待r47同起步对照及SIGTERM前证据一起解释，不提前删除原红。
  - 下一步：补409基线对照与原时刻缺失限制、其余有效变异；然后PG严格相关整包、仓库门，合入退1修复后在干净源锚重跑S011及相应门。当前不是交件，不部署、不自验收。

- 2026-11-02T02:17:17.095880+00:00 · coder56solzzc.zzc · HLC-010/S014 独立缺陷片段，基线bdac3593，分支opus50tc2k/hlc-drain-exit1；提交坐标待验证后追加。
  - 完成：对端已关闭类型分类、逐连接原错快照、客户端先断及Hub先停／活连接关闭失败／撤分类测试已实现。
  - 阻塞：等待单路测试窗口，当前不把未运行验证记为通过；S006原8轮无SIGTERM及409绝对时间，保持缺项，不加ready屏障。
  - 下一步：独立资源逐格验证、变异、仓库门及严格规格核验后只交坐标／读数；唯一写者复核合入，不部署、不apply/archive。

- 2026-11-02T02:47:01.064768+00:00 · coder56solzzc.zzc · HLC-010/S014片段验证完成；独立分支opus50tc2k/hlc-drain-exit1，直接基线bdac3593，坐标以本提交及交接回执为准。
  - 完成：真实完整Hub对端先断退0/already_closed20、Hub先停退0、存活连接写超时退1/failed20，各race3轮；原错逐条保留。撤分类3轮真实退1且断言目标红；相关Go race与OpenSpec strict rc0。最终完整run-build-tests rc0（helper各1097/Python27/shell2），首轮长TMPDIR致Unix socket超长的rc1与短根shell复核rc0一并保留。
  - 阻塞：本片段无待测项，等待唯一写者独立核证与合入；整单HLC及S006完整回归不据此关闭。S006原8轮未发SIGTERM/409绝对UTC缺项，r47对照22/100、bdac9/30，新对照不冒充旧时差。
  - 下一步：只交工蜂仓库/ref/commit/tree与读数，释放单路窗口给主流；不自合入、部署、apply/archive，不改原树或S006 ready屏障。

- 2026-11-02T02:56:55.524177+00:00 · coder56solzzc.zzc · 合并后整包前置检查，源码起点3d8941214。
  - 完成：四个独立提交已推送并核远端；vet通过。普通整包触发旧mux过期夹具nil Hub panic，定位ee7be94d引入后台生命周期登记后，旧测试直接构造registry未绑定Hub；仅将夹具改用newTestHub绑定所有者，原过期时限/metadata清除断言全部保留，无产品修改。
  - 阻塞：普通整包rc1原日志保留，尚未启动最终50轮；该修正后的目标格及完整门仍须重跑，不记通过。
  - 下一步：先复核目标格，再提交测试修正，在干净精确源锚重跑vet/相关整包和既定单路矩阵及仓库门。

  - 补充：c248506c隔离目标格race3次通过，属于本单生命周期挂点暴露的测试构造回归。产品registry唯一构造console_mux.go:129绑定Hub、:133使用Hub；补封口后到期回调拒绝记录expiryRejected及对应断言，不新增nil放行路径，不启动未登记工作；完整重验待执行。

  - 2026-11-02T03:00:41.570166+00:00：两目标格race各3轮rc0；撤后台准入保护后新增格在MUX_TIMER_AFTER_SEAL拒绝记录断言红（rc1），未改产品准入语义。原整包panic、r47目标格绿、修正后目标格及变异原始日志见docs/reviews/hlc-primary-precheck；整包与最终门仍待执行。

- 2026-11-02T04:51:52.486056+00:00 · coder56solzzc.zzc · 按组长87b10d9ad08a4d41a15c236370c00da0补S007-full，基于d8e737a4，仅测试与测试说明。
  - 完成：保留原局部S007不改，新增完整混合负载另加内网SSE/公网HTTP各1的真实卡住处理器；首轮实际8005ms退2，正常HTTP100条落库，SSE100/mux5/bridge15/helperWS20/回调全部收尾，超时剩余internal_sse1、公网HTTP1（internal_http1与同一SSE重叠）。产品字节未改；首次漏strings导入编译红另存，修正后才实际执行产品路径。
  - 阻塞：T08保持未勾，尚需新tip上普通10轮/race3轮/撤超时退出码分类目标红，以及cmd/workpulse普通/race与仓库门。
  - 下一步：单独测试提交；同一源码串行完成上述检查。d8的完整门、m8 race既有时限失败与40次基线对照均保留为历史证据，不修改阈值或冒报全绿。

- 2026-11-02T05:24:34.063281+00:00 · coder56solzzc.zzc · 主流合并验证完成，最终执行锚d0c294c8ea2863bb9ddeb5838f4ead8c074e0140（tree21a867bf6eb362fe143db6db3cfaa4316f97d4dd），最终文档/证据提交坐标见推送回执。
  - 完成：27e2f6ca六文件独立提交→145acdc8普通merge修复路2a8be0ab，五处冲突逐项说明；A2/B/d8及ba96独立完整S007测试已普通推送。S006 race50全部退0；S014三格各race3及撤分类红；S007-full普通10/race3全部退2、约8秒，正常完整负载收尾/HTTP100持久化，真实pending内网SSE1/公网HTTP1，撤退出码目标红。其余故障格/有效反向控制与独立SDK进程完成；PG严格相关整包、最终cmd普通/race、14项strict及canonical rc0（helper各1097/Python27/shell2）。T-04～09依据逐项见最终报告；原始日志/SKIP逐项保留。
  - 保留：S011自动格红（mux缺2），同feed显式resync后缺失0/重复0，210ms及stop→output8759ms分列；m8完整race2648.265s rc1，仅既有50ms时限失败，r47/d8四组各10次对照符合组长a7d6e937判据，FINDING另存，不改阈值。原409八轮无逐次UTC/SIGTERM，禁止补造时差；22/100与9/30对照支持保留单套ready屏障。
  - 待办：T-10/11保持未勾，交组长独立验收/例外处置及后续GO；无部署、apply/archive或新项目授权。当前无测试进程。

### S-HLC-S011-ACCEPTANCE 交接（2026-11-08）
- T-10：已完成并勾选。基于 `opus50tc2k/r55-integration` `7b123ca4a51fdde77a417d3ce7f03d01235d23ca` 独立执行本地隔离矩阵；停机至ready、20个假helper、SSE、普通WS及显式mux resync均满足冻结上限。mux自动格仍如实记录gap及2条缺失，按HLC-008和裁定 `3f698b77d848` 属既定设计，不作为此项的失败条件。证据：`docs/reviews/hlc-s011-acceptance/RESULT.md` 与同目录 `raw/`。
- T-11：未完成、保持未勾。S011独立验收已完成，但本任务不包括全量交付门、预发/生产GO及业主批准；由组长安排后续结项及GO。
