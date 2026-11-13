# S-R59-CLOSEOUT-PREP 准备段

## 范围与授权

依据组长2026-11-13T11:05:53Z派单、docs/runbooks/RELEASE-CLOSEOUT-OPENSPEC.md方案甲及第108条。仓库git@git.code.tencent.com:wego-ai/dingwei.git，分支coder56solzzc/r59-closeout；基线a0e280494b2b086c1fac3f765b0d31eb38ec5c09。main实取153bac4d为祖先，相差50提交；2e1e4c9d→c22bb63f→a0e28049逐段祖先检查通过。只交准备分支，不推进main、不镜像同步、不并下一版、不动现网或镜像；业主目视和组长最终复核仍为后续门。

## 清账与归档

完整19项与两种勾选计数见INVENTORY.md及inventory-before.json。原8项全勾，r58的T-04/05由原版本证据补勾后5/5；另补view-history-object-envelope 3/3。本次共归档10项，保留10项；后者目录逐字与基线一致。HANDOFF.md逐项给出剩余工作与建议版本（建议不构成新派工）。

归档名单：helper-bound-thread、helper-recover-inherit-thread、helper-busy-admission、r58-integration、r59-integration、view-unknown-ux、view-unknown-ux-followup、view-stale-backlog、view-cross-stream-timeline、view-history-object-envelope。归档命令逐项使用OpenSpec1.16.0 archive -y；10项均含增量spec，故无需--skip-specs。非阻断proposal旧格式警告保持原文，未使用--no-validate；归档后全仓strict验证通过。

10个归档变更的26条增量Requirement逐节与对应主spec文本精确一致，核验结果见archive-spec-coverage.json。新主能力view-unknown-ux沿用实际Purpose并补context.md；其它受影响主能力补实现与证据上下文，没有Purpose占位句。归档没有实现Hub expired_unknown草案。

r58不以r59门推算完成：原受测0054bd049与独立证据分支d63bf9d1记录全仓5147/0/71、hosted race三轮555/0/0、canonical1181×2、browser40/40、strict36/0及两刀2/2红，组长rulings补94复核冻结。本次只补勾并归档，不回并整个证据分支。

## 两项补录与复盘

历史信封：补view-history-object-envelope，VHO-001及两个场景；源修复2e1e4c9d及已有真实脱敏首屏22事件、对象/字符串两刀红，逐项Git坐标见TABLE.md。没有新增强制真流抓取门。

跨纪元：选择并入view-cross-stream-timeline的T-05～07，原因是4871e32b配对与bb22e77c定位键保护本来已属于R51-TIMELINE-001及S007，另立同义change会重复需求。a0e28049的REPORT保留四路回放与四刀变异；原主spec已有S007，archive提示already in sync，无需重复增量。

复盘：17834990309f05ffa6c9307a7ba954e5bd133ecc单提交cherry-pick，承载ebd74ffc8e73b9bed09db73d65c0e3e6c3f2bd09。基线没有该文档导致modify/delete，解决方式为保留该tip完整正文；未合它的父提交或其它产品文件。依本席当前规则使用coder56solzzc Git身份，来源精确commit保留以供追溯。该提交仅新增docs/retrospectives/2026-11-13-view-refresh-input-output.md。历史复盘描述02:33Z上线与当时残留，不能代替本次10:49Z换件的最终目视验收。

## 坐标与边界

逐任务TABLE.md由check_coords.py检查10项全勾覆盖，58个坐标缺失0；缺失坐标负控将一个commit置为全零，rc1且明确MISSING。原清账check_coords.py首跑因本机缺7个历史对象报11缺，按精确commit从工蜂fetch后原表45坐标缺失0，未修改历史表。原始成功与负控见raw/。

BOUNDARY.json核基线祖先、复盘来源、保留10项目录零差异。最终git diff --stat a0e28049 HEAD以及目录外路径列表随交付生成；openspec/与docs/以外必须为空。

## 门读数

完整Go命令：

```sh
HUB_TEST_PG_STRICT=1 go test -json -v -count=1 -p=2 -timeout=60m ./cmd/workpulse ./internal/changeflow/... ./internal/coordinatereview/... ./internal/specconfirm/... ./internal/specread/... ./internal/taskdispatch/... ./internal/store/... ./internal/m8/...
```

实际子进程由显式白名单环境启动，普通uid1001；Go1.22.2/Node22.23.1/锁定venv Python3.12.3/OpenSpec1.16.0，详情versions.json与raw/*-command.json；剥离继承SH_/DW_/DINGWEI_状态。Go最终TMPDIR=/tmp/r59g；依赖与编译缓存只读复用本机测试缓存，GOPROXY=off、GOSUMDB=off。-v完整纯文本已从JSON的Output事件逐行重建，见gate-metadata.json的verbose_log。

| 门 | 结果 | UTC |
|---|---|---|
| canonical | rc0；两profile各1203/1203、94文件、27 Python/2 shell，unittest SKIP 0 | 2026-11-13T11:11:38.824979+00:00～2026-11-13T11:21:24.374813+00:00 |
| 指定Go全包最终重跑 | rc0；全部事件2823 PASS/0 FAIL/48 SKIP，顶层1579/0/30 | 2026-11-13T11:15:24.755977+00:00～2026-11-13T11:23:04.486168+00:00 |
| OpenSpec1.16.0全仓strict | 35 passed/0 failed，rc0 | 完整原文raw/final-strict.txt |
| 本次逐任务坐标 | 58 checked/0 missing，10项任务覆盖，rc0 | raw/final-coords.txt |
| 旧清账坐标 | 45 checked/0 missing，rc0 | raw/final-old-coords.txt |
| 归档spec内容核对 | 26条增量Requirement正文与主spec逐节精确一致 | archive-spec-coverage.json |
| 边界与diff-check | rc0；docs/openspec以外0路径、保留10项目录与基线一致 | BOUNDARY.json |

其中internal/m8整包为1545 PASS/0 FAIL/37 SKIP，package rc0。48条SKIP全名与原因见SKIPS.md；均为既有opt-in、独立二进制/浏览器/故障矩阵/性能门或待裁定fixture，不记为通过。其中跨runtime部分在canonical shell另有实际执行，仍不抹掉Go独立整跑中的原始SKIP。

首轮Go整体rc1：2822 PASS/1 FAIL/48 SKIP，仅TestNotifySocketFirstStopPreservesNewListenerAtReboundPath的Unix socket路径过长（bind: invalid argument）；原-v及JSON哈希、命令与失败摘录全部保留。仅缩短TMPDIR后将全部指定包以-count=1完整重跑rc0，该用例PASS；没有改源码、测试或放宽断言。首轮的internal/m8自身包仍PASS，最终以完整重跑读数为交件门。

验证期间产品与OpenSpec归档树保持相同；归档承载提交ee2dacab3只把已核工作树入Git，之后只补docs证据，不改变产品或规格。最终tip/tree由推送核对后的平台回执给出；不以本报告当作独立验收或整版GO。
