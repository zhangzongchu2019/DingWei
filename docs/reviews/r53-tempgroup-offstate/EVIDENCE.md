# S-R53-TEMPGROUP-OFFSTATE 自测证据

受测源码：82ef325bb08c09648c8501db7a03bbbb3cf2ba08；父提交de424c5bfa5aa04002dc7fe1011f82ea7c132d8f。证据承载提交在交件回执中报告，其后仅追加文书与原始读数。

执行者coder56solzzc.zzc；实取CODEX_THREAD_ID=01a1b3e7-3c3a-7051-9ae2-dd1c6f58ef44。所有时间为UTC；仓库git@git.code.tencent.com:wego-ai/dingwei.git，分支coder56solzzc/r53-tempgroup-offstate。

Hub按h.TempGroups是否非空注入data-enabled。关闭态不调用临时编组刷新，列表/详情/解散和渲染入口再核同一开关，不显示相关UI/报错；HandleConsoleTempGroups的关闭态503、路由、开态权限与行为保持原样。未写前端版本号；未发现适用于Console页的逐字节golden，现有动态版本与Handler常量测试通过。

| 门 | rc | UTC结束 |
|---|---:|---|
| m8 | 0 | 2026-11-07T03:24:10.467928+00:00 |
| cmd | 0 | 2026-11-07T03:24:08.857049+00:00 |
| browsers | 0 | 2026-11-07T03:28:15.391301+00:00 |
| canonical | 0 | 2026-11-07T03:29:06.905229+00:00 |
| openspec | 0 | 2026-11-07T03:24:09.070263+00:00 |

命令/UTC/环境名及提交见SOURCE.json和raw/*.json；Go整包使用HUB_TEST_PG_STRICT=1、TMPDIR=/tmp。全部门均在冻结源码提交上执行。

## 回归判据

- console_browser原20格：passed=20 failed=0，包括组长报告的3格；D开态套件继续通过（空DOM逐字节不变、双成员详情、取消零写、409可读、成功刷新一次）。
- 新关闭态浏览器套件使用真实Hub Handler输出（TempGroups=nil），覆盖首次加载、多次Agent刷新、打开Agent管理、直接列表/详情/解散入口；零/api/console/temp-groups请求、零console/pageerror/dialog、三个临时编组区块均不可见、无临时编组错误提示。
- 基线统一runner实际已有37套（包含trusted_origins_file_flow）；新增1套后全部38套执行通过，无删减。完整机器回执见raw/browser-receipt.md。
- TestConsoleTempGroupsCapabilityFollowsHub覆盖nil/非nil实际页面注入；TestR53DisabledRouteSetUnchanged在cmd整包通过。

## 单刀变异

- 隔离工作树只把tempGroupsEnabled判断改成return true，不改测试或HTTP夹具；rc=1，准确红在“关态必须零 /api/console/temp-groups 请求”。实际多出列表与详情请求，完整失败原文及唯一diff见raw/mutation.log与raw/mutation.diff。
- 变异前/还原后console_auth.go SHA256相同：2b40e451618736db8042c8285fc886a4f339806f495610fc718d88e2752bc697。正式工作树未变异。

## 整包计数与跳过

- m8：顶层{'pass': 892, 'fail': 0, 'skip': 19}；含子项{'pass': 1499, 'fail': 0, 'skip': 37}。
- cmd：顶层{'pass': 169, 'fail': 0, 'skip': 6}；含子项{'pass': 259, 'fail': 0, 'skip': 6}。

完整跳过清单共43项（含子项）见SKIPS.json，保留每项测试名和原始条件，未计作PASS。默认opt-in的实际CLI/浏览器/旧固定制品与暂缓模型等未假称覆盖；canonical另含跨runtime桌面Hub/真实PG claim门，实际读数见raw/canonical.log。

原runner中renderer_c2的既有KNOWN-DEFECT T-166日志保留（不是本次修复覆盖项）；38套rc0指统一runner的既定判法，不宣称该历史缺陷已修复。

## 边界与复核

只修改Console能力注入/前端保护与夹具/回归登记及本任务OpenSpec文书。源码通过Git托管交换；没有部署、重启、上传源码或二进制，没有修改冻结r53-tempgroup分支或集群工作树。自测结果供组长独立复核，不作为独立验收。完整原始日志若超过250KB以gzip无损保存，解压后的字节数/SHA256和存储哈希均在RAW-MANIFEST.json。
