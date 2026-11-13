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

执行中，最终结果待本次门完成后补齐。Go整跑使用HUB_TEST_PG_STRICT=1、-json -v -count=1，全部SKIP名称与原因单列；canonical使用干净环境与锁定venv。原始长日志只保留本机路径、字节数及SHA256，提交逐行摘录。
