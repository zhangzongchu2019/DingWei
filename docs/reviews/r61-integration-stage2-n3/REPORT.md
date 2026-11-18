# R61 第二段 N-1b＋N-3 合后交件

依据组长2026-11-18T12:57:47Z补充令。固定合后产品/测试源 `4581b34ac1e4b9973d12c559cf6ac7772107bebf`，tree `5c73e93df0255e73137428a9376b65321e91279a`；parents `c430c3c65f0f8d9b669c8bef24f4194871f11283`、`1a2a4160208c839c97e1b35039c7eafc6e7216e1`。N1b冻结产品ed7259fa/作者b81737d0已先按12:24令合入cd01d67a，并在c430c3c6完成仅N1b阶段交件。补充令于14:29Z注入，故保留其历史，在c430上追加N3 no-ff merge；不reset、不重写、不重复N1b。VERSION仍0.2.3.40，N1c未并入，不推main、不出发布件、不部署。

## 合并、来源与边界

合前ls-remote核N1b=b81737d0、N3=1a2a4160、集成=c430c3c6。N3冻结源9313197f8ac3e2a585cd399935d5b313084b3b1e到tip全部docs/openspec。第一父链自main206d41b2恰9个merge，先七项、N1b、N3；原文/UTC见premerge.json、merge.json、invariants.json。首次no-commit merge未附身份被Git拒绝、分支未变；随后每命令用本席身份重试成功。ort自动合并hub.go与runner，无文本冲突、无手工产品解冲突。

N1b R1/R2文件与c430逐字相同，N3八个关键文件与931逐字相同；共享hub保留queue_full协商与legacy重锚显式nil目标。独立history DB新增0078，应用库migration0077及既有迁移均不改。helper全子树、renderer-canary-sse.html（retainCanonicalTrustedProvenance）、cmd、deps、scripts/deploy、VERSION相对c430都不变。N1c25af非祖先。导入原日志有尾空白，保留已冻结原字节，产品/openspec范围check为0，见precommit-audit.json；不虚称全路径whitespace零问题。

## 新合后源完整门

所有门均固定本合并锚重新执行，不复用先前N1b-only或N3单源绿色。普通用户uid1001，Go1.22.2、锁定Python3.12.3、Node22.23.1/OpenSpec1.16.0，env白名单，fresh HOME /data/ai-dev-data/r61i23h与真实数据盘TMPDIR /data/ai-dev-data/r61i23t。PG strict＋DW_DESKTOP_INTEGRATION_PYTHON＋DW_FAILURE_NOTICE_PYTHON显式设置。真实临时PG版本180003，三遍日志15行版本证据，原位置见postgres-evidence.json。

canonical BUILD_TESTS_OK helper_profiles=2 python=27 shell=2，inventory97，两个profile数量/分组与rc见helper-profile-results；codex guide指定格实际ok（独立摘录）。PASS/SKIP是日志行数（含count重复与子项），非不同场景数。跳过逐名和原条件见SKIPS.json。

| 门 | rc | 顶层PASS行 | 含子项PASS行 | SKIP行 | interrupted行 |
|---|---:|---:|---:|---:|---:|
| canonical | 0 | 6 | 21 | 0 | 0 |
| m8-store-full3 | 1 | 4352 | 7427 | 99 | 1 |
| focused-race | 0 | 103 | 175 | 17 | 0 |
| browser-full | 0 | 0 | 0 | 0 | 0 |
| strict | 0 | 0 | 0 | 0 | 0 |
| z3-baseline-target20 | 1 | 18 | 18 | 0 | 2 |
| z3-m8-round1 | 0 | 998 | 1690 | 28 | 0 |
| z3-m8-round2 | 0 | 998 | 1690 | 28 | 0 |
| z3-m8-round3 | 0 | 998 | 1690 | 28 | 0 |
| z3-m8-round4 | 0 | 998 | 1690 | 28 | 0 |
| z3-m8-round5 | 0 | 998 | 1690 | 28 | 0 |
| z3-matching-baseline1-20 | 1 | 19 | 19 | 0 | 1 |

初始m8/store三遍命令非绿，实际FAIL和包结果见parsed-gates/raw原日志；只因既定三格interrupted(9)适用Z-3，首轮rc1不抹除。按7e622c94d104固定五轮，5/5全包rc0，其余仅允许名单内同机制中断，合格；独立main206目标count20对照分列。所有轮次与诊断行保留，不用单跑替代、不择优、不扩名单、不改store。

focused race同时覆盖ACK/queue_full/failure_notice与HistoryProvenance/SessionHistory，包rc/计数/SKIP/竞态按原日志；未跑整包race，不作扩大。浏览器48/48，新增history_provenance与原47套均实际rc0，r15三格及N2回归保留；既有KNOWN-DEFECT观察单列，不宣称修复。来源strict为45/0、rc0，最终文档strict另记准确提交。

## 三向真实进程兼容

三个组合都在合后源重新跑：旧Hub501e5b8a＋合后helper，新合后Hub＋真实0.2.3.39 helper(2e1e4c9d)，另有新新正例。真实shipping Hub独立进程＋Python helper独立进程，真实WS握手/recv/accept/ACK；source也走真实WS，Hub DB状态与通知行数独立查询。

| 组合 | 真实ACK | Hub状态/通知数 |
|---|---|---|
| 旧Hub501＋合后helper | busy/queue_full/retryable=true | queued/helper_busy，通知0 |
| 合后Hub＋合后helper | failed/queue_full/retryable=false | dead/queue_full，通知1 |
| 合后Hub＋真实39helper | busy/queue_full/retryable=true | queued/helper_busy，通知0 |

边界：容量1预填队列、fixture暂停模型消费，adapter为stub且暂停无关后台任务；无真实CLI/模型回合声明。真实的是运输进程、实际协商/accept/ACK代码与DB落库。每格本机loopback、独立HOME/IPC/私有DB、一次性fixture身份，无线上/预发连接、无同名在线席位，无真实凭据。私有状态/DB/凭据及测试exe不进Git，exe非发布件、未上传。命令/UTC/各source/实际ACK/DB字段见compat-runs/results；binary-shas及go version -m只作本机联测来源证明。

## 证据与交接

所有原日志私有保留；Git raw/*.gz为发布副本，测试生成secret/token样式字段脱敏，原SHA/发布SHA/gzipSHA与次数分列raw-logs.json，不把脱敏件称逐字原文。N1b导入旧清单两条obsolete路径按前件已披露，不在本段新清单混计，也不改作者冻结证据。N3既有独立PASS按组长指定621df13e保留，不代替本段作者门，也不冒称本席新做单项独立复核。

完成：依序N1b＋N3合并和合后门、真实501/39协议兼容。下一步：组长/独立复核，N1c待修订冻结后另令并入；VERSION最后一段再定。不推main、不部署。本件不是r61整体验收或发布GO。

补充读数说明：五轮本身全部0 FAIL/0 interrupted，首轮三遍rc1仍保留。main206辅助reconnect目标20次为18 PASS/2次已知中断；与本次实际失败落点同名的TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent对照20次为19 PASS/1次同机制中断，原rc1都保留，不据小样本推断失败率或因果。z3-admissibility-audit.json逐条核8份Go日志的4个实际FAIL均在该测试自身RUN范围内有interrupted(9)诊断（首轮1、辅助2、匹配对照1）；这4个不混进五轮绿色。

PG启动调用链与源码blob SHA见postgres-startup-path.json，运行时版本行见postgres-evidence.json；未记录DSN/密码。TestHistoryProvenance前缀的命名回归摘录见history-specific-verdicts.json，legacy重锚在完整三遍通过3次、race通过1次。单项作者42刀等冻结证据不与本段浏览器内建变异或门计数合并，本段没有冒称全部重放。

go version -m原输出mod行末尾tab原样保留，与导入原日志的尾空白同属原始证据；不为格式检查改写原字节。本段正文/规格与产品范围whitespace检查无错误。

首份不可变证据提交：`5ccfc46bb3f2da227b31c7c0a87cf7024400457f`，tasks/context已附可git cat-file的证据坐标；本段当前候选取代c430作为N1b＋N3合后锚，旧阶段读数与原锚保留。

最终文档strict固定`def17ef7ff6df6f83a34f831aa62e38e315473c1`，45/0、rc0；UTC/原SHA见evidence/final-document-strict.json及同名txt。后续仅封存docs，不再修改openspec。
