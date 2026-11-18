# R61 第二段 N-1b 集成交件

依据组长2026-11-18T12:24:42Z S-R61-INTEGRATION第二段。冻结合并源 `cd01d67a2c126c850b1229fc486ee30cb1ddd3e8`，tree `9aeff088427e289356f84c022668a15010a12679`；第一父 `0ab43c630c37fdf01ce68d5c66c0db0141a682af`，第二父 `b81737d0897c7edfea171371dba8ead6c1ef8f28`。按 --no-ff 一项一merge，合前ls-remote逐字相符，ed7259fa到来件仅docs/openspec（原文/UTC见premerge.json）。N-1a638cb324已在第一段祖先链，ort自动合并共享test_sessionhelper.py，无冲突、无手工产品编辑。VERSION仍0.2.3.40。N-3／N-1c未合入，不推main、不出发布件、不部署。

## 固定源与边界

第一父链自206d41b2恰有八个merge，前七保持原顺序，末笔cd01。祖先、共享测试自动合并审计见lineage-and-auto-merge-audit.json。cmd、view-service、scripts、deploy、go.mod、go.sum、VERSION与第一段逐字相同；internal/tools仅引入已冻结N1b。原来件raw mutation日志一行尾空格被diff --check检出，保留原SHA/字节，产品与规格范围检查通过，见precommit-whitespace-check.json，不伪称全路径whitespace零问题。

## 门的真实读数

普通用户uid1001、Go1.22.2、锁定Python3.12.3、Node22.23.1/OpenSpec1.16.0、env白名单，独立HOME与数据盘真实TMPDIR。显式HUB_TEST_PG_STRICT=1、DW_DESKTOP_INTEGRATION_PYTHON和DW_FAILURE_NOTICE_PYTHON；无继承SH/DINGWEI环境。真实PGserver_version_num=180003，完整三遍日志版本证据15行，启动和版本位置见postgres-evidence.json。canonical完整通过BUILD_TESTS_OK helper_profiles=2 python=27 shell=2，inventory97；每个profile主套1268＋附加9，合计1277/1277，rc0。codex guide指定格实际ok，独立摘录见codex-guide-verdict.txt；profile摘要与原日志保留。

PASS/SKIP均为日志行数（含重复/子项），非不同场景数；所有跳过逐名及原条件见SKIPS.json。

| 门 | rc | 顶层PASS行 | 含子项PASS行 | SKIP行 | interrupted行 |
|---|---:|---:|---:|---:|---:|
| canonical | 0 | 6 | 21 | 0 | 0 |
| m8-store-full3 | 1 | 4313 | 7244 | 99 | 1 |
| focused-race | 0 | 60 | 82 | 16 | 0 |
| browser-full | 0 | 0 | 0 | 0 | 0 |
| strict | 0 | 0 | 0 | 0 | 0 |
| z3-baseline-target20 | 0 | 20 | 20 | 0 | 0 |
| z3-m8-round1 | 0 | 993 | 1672 | 28 | 0 |
| z3-m8-round2 | 0 | 993 | 1672 | 28 | 0 |
| z3-m8-round3 | 0 | 993 | 1672 | 28 | 0 |
| z3-m8-round4 | 0 | 993 | 1672 | 28 | 0 |
| z3-m8-round5 | 1 | 992 | 1671 | 28 | 1 |

初始m8/store三遍命令整体rc1：m8唯一业务失败为TestSessionNameReconnectKeepsNameAndReleaseReuses的hub_test.go:3505 interrupted(9)；store、migrate、pgtest均rc0。失败原日志与原SHA完整保留，未改FAIL为SKIP，也未记成三遍全绿。按7e622c94d104既定Z-3口径，另行固定五轮m8 count1，4/5轮全包rc0；其余仅允许名单内同机制中断，逐轮结果如表。基线206d41b2同用例count20独立对照如表，不用单跑补绿替代整包。z3-initial-assessment/qualification及gate-acceptance分开记录。未改store、未二分、未扩允许名单，无择优丢弃轮次。

focused race两包rc0，DATA RACE为0；条件性实际CLI崩溃窗等SKIP逐名披露。浏览器全量47/47 rc0，aggregate_rc0，r15 pagination/mutations/follow三格均rc0；继承KNOWN-DEFECT观察照原文保留，不宣称修复。来源strict为44/0、rc0（strict.log），最终文档strict另列准确提交。

## 真实独立进程兼容

三格均启动真实Hub二进制＋独立Python helper进程，真实WS握手、真实_run_once/recv/accept及ACK代码；source发送也走真实WS，Hub SQLite行和failure_notice数量独立查验。旧Hub固定501e5b8a185b20f5c931c8ebb89c16bfd633a01e（本席之前独立构建SHA见binary-shas）；旧helper为真实0.2.3.39源码2e1e4c9db3c42cf7c987a548fa9214aad67a9f0c。合后Hub/helper固定cd01。仅本机loopback、每格私有HOME/状态/DB/IPC与随机一次性fixture身份；无现网或预发访问，无线上探针。

| 组合 | 实际helper ACK | Hub终结/通知 |
|---|---|---|
| 旧Hub501＋合后helper | busy / queue_full / retryable=true | 不dead，failure_notice=0 |
| 合后Hub＋合后helper | failed / queue_full / retryable=false | dead/queue_full，failure_notice=1 |
| 合后Hub＋真实39helper | busy / queue_full / retryable=true | 不dead，failure_notice=0 |

边界明确：为了稳定制造容量1满额，模型消费被fixture暂停，adapter为测试stub、无CLI/模型服务；其余无关后台任务暂停。真实的是独立运输进程、协议协商、接收、容量判定和ACK/Hub落库，不冒称真实CLI完整业务回合。命令/UTC/source/ACK/DB字段与原输出SHA见compat-runs/results和raw；测试exe只为本机联测，未作发布件/未上传。私有数据库、HOME、一次性凭据和exe不入Git。

## 已知证据与交接

来件b81737d0的旧SHA清单有两条obsolete路径，正确相同SHA日志已在r1-r2-followup目录且清单另有正确行。本席复验已披露并通知作者；保留不可变作者证据，不冒称其清单47/47全中，本段新清单仅管本段发布证据。该文档观察不改变ed产品冻结。

完成：第二段N1b顺序合并、本段作者门与三向真实进程协议兼容；首轮非绿和Z-3资格分列。下一步：组长复核后待N3/N1c新冻结锚与第三段令。此作者门不替代独立复核、版本定案或上线GO。

日志发布副本：raw/*.gz中对测试生成的secret/token样式字段脱敏；每份原SHA、发布字节SHA、压缩SHA与替换次数均在raw-logs.json，不把脱敏件冒称与原日志逐字相同。私有原日志仍保留本机。

首份不可变证据提交：`fcfd0ec38a51621fff25ad1f3d7b5d7269f6301b`，tasks和context据此附可git cat-file的坐标。另：go version -m原输出mod行末尾tab同样保留，未为diff whitespace提示改动原证据；不影响产品范围检查。
