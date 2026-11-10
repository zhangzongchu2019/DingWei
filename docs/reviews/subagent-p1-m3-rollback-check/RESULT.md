# M3 必补：全库只读回退自检

CODEX_THREAD_ID=01a1bf6a-20da-7533-8984-532fd2ec65a3。需求来源为 review/opus50tc2k/rulings＠dcfd24ef530f7502c593dd88a4e5606bf52a56a8 的〔51〕-补15；本单不做 r57 集成／发布。ZERO-CONSENT 已先交 ea63d294f。M3 三项核验详见 PROVENANCE.json：本机工蜂08:57:40Z远端恰9662deb0、旧树clean且HEAD一致、ps与/proc无旧owner或工作树相关运行进程，才留旧锁 stale-20261109T085804Z，建立本线程锁及独立工作树。

冻结产品／测试 **3e30e81e70c665029912ed306e3b70c17c18ffb3**，tree **afa8abc74009ca6b1d5e42520bb0519fe3db0b15**，父 **9662deb04ecec8ca40690c345446eb22d2e33bc1**。同分支追加，不改写原件。VERSION 未改，无迁移、无新依赖。

## 行为与降级门

cmd/subagent-rollback 新增互斥 --check-all，只读检查明确既有数据库，列出全部有 session_delivery_sub_route 却无 sub_agent_rollback_delivery 审计的投递，JSON 输出 pending 与按根排序的 count／delivery_ids。无未审计投递rc0，有残留rc1，读取失败／缺表／损坏路由也非零，参数错误rc2。不会通过注册根清单或投递终态过滤掉记录；有to按接收根，只有from仍按来源根列出。不会补审计或删除sidecar来制造rc0。来源侧残留若不能由现有单根准备消除，保持feature-off交负责人处理。

OpenSQLiteReadOnly复用既有打开路径，以字面文件路径转义后 mode=ro＋query_only 打开；不建库、不迁移、不修改journal_mode、不checkpoint、不调用Prepare事务。只读连接包含已提交WAL记录。SQLite可创建运行所需WAL/SHM旁文件，业务数据库及已有WAL内容不变；不使用immutable，避免漏读尚未checkpoint的已提交记录。依据 [SQLite WAL只读说明](https://www.sqlite.org/wal.html#read_only_databases) 及本次针对实际驱动的正反测试。

runbook docs/design/SUBAGENT-P1-M3-VIEW.md 第3步每根准备后必须同库 --check-all rc0；第5步在停写一致水位、实际拟降级库上再次检查并记录UTC/JSON/rc，rc0才可继续旧源claimer与获批降级。原封口／逐封退信／unknown／ACK／墓碑／独立发布授权前提保留。

## 正反例与变异

六项定向测试覆盖：空库；两个根三封未审计（含processed原ACK与sent未知）；只完成根A仍列根B；全部准备后空；未登记根的来源sidecar；损坏JSON／缺根；只读不建库、拒写、含URI分隔符路径仍精确；已提交WAL必须检出且DB/WAL摘要不变；CLI退出码／根计数／参数／缺表不自动迁移。

四刀均Go overlay，基线rc0、变异rc1且命中目标断言，非编译红：

| 撤除 | 目标红 |
|---|---|
| 全库查询改为永不返回记录 | AA009_GLOBAL_ROLLBACK_PENDING |
| 撤pending非零的CLI退出门 | AA009_CHECK_CLI_EXIT |
| mode=rw且关闭query_only | AA009_READONLY_WRITE_ACCEPTED |
| immutable忽略未checkpoint WAL | AA009_COMMITTED_WAL_IGNORED |

命令／UTC／rc／原文见 mutation-runs/RESULT.json 与各baseline/mutant.stdout。

实际CLI正反：existing TestSubAgentOfflineRollbackLegacyClaimer 生成完全准备的合成SQLite快照；go run ./cmd/subagent-rollback --hub-db <测试快照> --check-all 输出 pending0／roots空／rc0；另一副本只删 unexecuted-a 的测试审计，输出 pending1／root=root／delivery_ids=[unexecuted-a]／rc1。两次DB SHA前后相同。复现：python3 docs/reviews/subagent-p1-m3-rollback-check/run_cli_smoke.py --output <新的私有临时目录>。数据库未复制入Git，证据只含日志／JSON。

## 门与边界

普通uid1001，官方Go1.22.2，白名单环境、隔离HOME、GOPROXY=off、短TMPDIR=/tmp。store609/0/5，工具包2/0；相关race-count3：store15/0、工具6/0，无DATA RACE；m8全包1533/0/37；strict29/0。canonical完整rc0，claude／codex各1165/1165、92文件、27Python／2shell、BUILD_TESTS_OK，原文见canonical/。完整skip名称见SKIPS.json，原文含条件。原M3的View、helper、迁移、产品版本未改；未重复原已过39格浏览器，因为本补件不改UI或其测试源。

首轮自设“只读不得产生任何旁文件”断言失败，原文保留initial-test-expectation。该假设与SQLite只读WAL定义不符；没有改成immutable掩盖，而是改为业务DB不变／写SQL拒绝，并新增WAL已提交残留不可漏读与DB/WAL内容不变断言。最终全包／race通过；原失败不算PASS。

源码只经Git交付。无部署、预发、在线席位或真实推理动作；未运行生产降级工具，不自验收。

提交前于2026-11-09T09:14:35Z发现rulings更新为a96cbc341f89eea0349ca558af754ba4a5007994，已fetch并读增量：仅新增㊿-补32，修正r56.2／ZERO-CONSENT连接页回环源规则，不改M3〔51〕-补15。本M3范围不变；ZERO-CONSENT已交ea63d294f须随后跟进r56.2新修复并补立即302路径，不以原同意页测试覆盖直接返回。
