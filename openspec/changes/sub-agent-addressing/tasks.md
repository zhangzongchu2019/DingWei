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

## 交接记录

2026-11-08 coder56solzzc.zzc；commit：本设计提交（由Git父链定位，避免自指SHA）。完成：T01～05，默认参数未定单列PM01～03；基线行为7控制与2个目标红，native并行证据。阻塞：无设计交付阻塞；生命周期默认/平台上限/墓碑策略等待业主决策，不阻止本次交审。下一步：T06完成后由组长复核，产品实现另派；不启动二期或部署。

2026-11-08 coder56solzzc.zzc；commit：本设计提交。完成：canonical两profile各1097/1097、27Python/2shell rc0，strict rc0；原文归档。同步核对rulings新tip5c142200（仅〔52〕补7/8），基线/提案未漂移。阻塞：无。下一步：Git推送核远端SHA、发送回执，组长复核；不执行产品实现。

2026-11-08 coder56solzzc.zzc；父提交bbfbf95b56b4afdebf48d89730910980f837a0f3（已冻结，不改写）。完成：按ad1a1ee0〔51〕-补5修三项，AA006-S02及变异计划同步；以原生原文复算同分钟UUID尾段，变更与现状spec strict rc0。阻塞：无；生命周期默认/平台上限仍待业主。下一步：T08门禁完成后同分支追加提交并核远端，交审后释放锁，不开始实施。

2026-11-08 coder56solzzc.zzc；commit：本次追加提交（父bbfbf95b）。完成：canonical rc0，claude/codex各1097/1097、27Python/2shell；变更与基线strict均rc0；冻结原raw15件不变。阻塞：无。下一步：推送核ls-remote并交坐标、释放锁，等待复核与业主默认决策，不实施或部署。

2026-11-08 togpt60lunazzc.zzc；基于实现分支父提交 `d6657823`。后续裁定〔51〕-补10（rulings `89bda3bc`）已批准生命周期默认，故本次更新AA-007及设计表；前述“等待业主决策”是历史状态记录。完成：T09兼容拒收实现与三项变异、T10默认值及验证；原始证据见`docs/reviews/subagent-p1-compat-guard/`。不包含创建/注册/子投递能力；不部署。下一步：推送并核ls-remote坐标，提交组长复核。
