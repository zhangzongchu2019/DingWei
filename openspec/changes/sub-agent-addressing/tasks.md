# 第一期设计任务

- [x] T-01 核对派单、规则、基线与全仓解析/使用点，列逐处改法。
- [x] T-02 实测旧组件多段地址、AMR与未知sub_path，保存隔离复现；在基线上给目标红断言。
- [x] T-03 验证钉版0.159.2单进程并列thread和单分道中断，保存原文与schema。
- [x] T-04 完成协议、可信来源、分道driver与r55衔接、生命周期、迁移回退与估算。
- [x] T-05 补AA005/006场景、一期设计增量与首次基线；strict验证。
- [x] T-06 canonical rc0、证据SHA；提交推送后按回执坐标交审。

- [x] T-07 按〔51〕-补5追加随机尾段命名/场景/变异、删外链及现状Purpose；变更和基线strict复跑。
- [x] T-08 追加项canonical rc0与证据SHA，提交推送核远端后交审。
- [x] T-09 第一刀兼容拒收：AMR跳过并提示三段子提及、Hub拒收未协商子路由保留字段、send.py本地预检拒绝；混部说明与逐项变异。证据：`docs/reviews/subagent-p1-compat-guard/REPORT.md`。
- [x] T-10 lifecycle已批默认写回AA-007、future复现与legacy控制、canonical双profile及strict。证据：同目录报告与raw运行记录。

- [x] T-11 M1 核对 79a60a1e 基线、写者锁与设计；Hub parser、当前连接能力协商、全量树与墓碑／别名持久化。
- [x] T-12 M1 加法迁移0075、Hub来源／路由、节点代次delivery ID及旧唯一索引作用域、原子sidecar、出队／ACK拒收。
- [x] T-13 M1 正负与逐项变异、旧数据迁移、m8整包、canonical、strict、future保护及证据归档。
- [x] T-14 M1 提交推送、核远端坐标并交组长独立复核；M2／M3另派。

- [x] T-15 M2 锁定 eae9eca1 基线、唯一写者、r57 版本线；单 broker、独立分道与 scoped 账本。
- [x] T-16 M2 原生控制与可信来源、幂等创建、树 ACK、五参数生命周期和逐条原子退信。
- [x] T-17 M2 正负/逐项变异、r55九格、原生工具、canonical双profile、strict与证据。
- [x] T-18 M2 冻结提交、推送核坐标、交组长独立复核；M3另派。

## 交接记录

2026-11-08 coder56solzzc.zzc；commit：本设计提交（由Git父链定位，避免自指SHA）。完成：T01～05，默认参数未定单列PM01～03；基线行为7控制与2个目标红，native并行证据。阻塞：无设计交付阻塞；生命周期默认/平台上限/墓碑策略等待业主决策，不阻止本次交审。下一步：T06完成后由组长复核，产品实现另派；不启动二期或部署。

2026-11-08 coder56solzzc.zzc；commit：本设计提交。完成：canonical两profile各1097/1097、27Python/2shell rc0，strict rc0；原文归档。同步核对rulings新tip5c142200（仅〔52〕补7/8），基线/提案未漂移。阻塞：无。下一步：Git推送核远端SHA、发送回执，组长复核；不执行产品实现。

2026-11-08 coder56solzzc.zzc；父提交bbfbf95b56b4afdebf48d89730910980f837a0f3（已冻结，不改写）。完成：按ad1a1ee0〔51〕-补5修三项，AA006-S02及变异计划同步；以原生原文复算同分钟UUID尾段，变更与现状spec strict rc0。阻塞：无；生命周期默认/平台上限仍待业主。下一步：T08门禁完成后同分支追加提交并核远端，交审后释放锁，不开始实施。

2026-11-08 coder56solzzc.zzc；commit：本次追加提交（父bbfbf95b）。完成：canonical rc0，claude/codex各1097/1097、27Python/2shell；变更与基线strict均rc0；冻结原raw15件不变。阻塞：无。下一步：推送核ls-remote并交坐标、释放锁，等待复核与业主默认决策，不实施或部署。

2026-11-08 togpt60lunazzc.zzc；基于实现分支父提交 `d6657823`。后续裁定〔51〕-补10（rulings `89bda3bc`）已批准生命周期默认，故本次更新AA-007及设计表；前述“等待业主决策”是历史状态记录。完成：T09兼容拒收实现与三项变异、T10默认值及验证；原始证据见`docs/reviews/subagent-p1-compat-guard/`。不包含创建/注册/子投递能力；不部署。下一步：推送并核ls-remote坐标，提交组长复核。

2026-11-08 coder56solzzc.zzc；CODEX_THREAD_ID=01a1bd1c-c28e-7c60-9594-7894d0078533；执行源见本分支父链。完成：T11～12，专项正负通过，future保护保持绿；产品仅Hub范围，无helper改动。新协议／旧索引兼容／feature-off边界见docs/design/SUBAGENT-P1-M1-HUB-PROTOCOL.md。阻塞：无。下一步：T13正式最终门和每格变异，T14交审；不启动M2／M3，不部署。

2026-11-08 coder56solzzc.zzc；冻结源59c02c90e614af9fb2f14f832caf8807a3b8b6fd（tree760cb23fee405a16dcbcda5c36bef3419229c878，工蜂已核）。完成：T13最终m8整包1524/0、mcpgw125/0、store604/0、extseat与cmd全部过；canonical两profile各1129/1129、27Python/2shell、rc0；strict27/27、build、race、future全过；26刀逐项目标红。跳过条件及失败首跑原文全部保留，报告docs/reviews/subagent-p1-m1-hub/REPORT.md，源码哈希保持不变。本证据提交推进T14 Git交审，独立验收待组长；不自验收，不启动M2/M3，不部署。阻塞：无开发交件阻塞。下一步：核本证据提交远端SHA并投递坐标，等待独立复核。

2026-11-08 coder56solzzc.zzc；父eae9eca1，CODEX_THREAD_ID=01a1bd1c-c28e-7c60-9594-7894d0078533；锁/tmp/coder56-subagent-m2.lock。完成：T15～16，锁版原生工具正负探针及r55九格通过；19刀目标红。阻塞：无；旧主thread不能补装dynamicTools的原生限制已显式记录，未自动替换主绑定。下一步：T17正式冻结门、T18交审；不部署、不启动真实推理，M3另派。

2026-11-09 coder56solzzc.zzc；CODEX_THREAD_ID=01a1bd1c-c28e-7c60-9594-7894d0078533；产品冻结eed6ac54762bb2cf756f78934fbfb2db46f73d64，最终候选826a7c0ecf3d3b5634f69eed479d6c5abdf3b119（只追加测试隔离/turn与task区分断言/精确库存92）。完成：T17最终canonical rc0，双profile各1158/1158、27Python/2shell；24刀M2与21刀r55目标红；r55九格不变；锁版原生create/source工具与future绿；strict29/0；m8 1524/0/37可选SKIP且Go源不变。T18证据追加在本提交，源19件哈希与证据哈希归档，原始失败全部保留，交组长独立复核，不自验收。rulings已核16af735c，派单后仅r56预发结项，无M2规格漂移。阻塞：无交件阻塞；旧主thread无法补装dynamicTools的原生限制已显式报告，不替换主绑定。下一步：核证据提交远端SHA、发送最终回执后等复核，M2锁保留，另一路只读；M3另派，不部署、不动在线席位、不做真实推理。
