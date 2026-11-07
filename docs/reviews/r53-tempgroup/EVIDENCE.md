# S-R53-TEMPGROUP-IMPL D 路后端交件

本件补齐 r53 最后一条后端链：A 开户可创建 PG 权威双成员 AG 与可达性镜像；B/C 通过真实 Groups 装配；Console 只列登录者相关内部对端的 AG，并允许两侧成员主人或管理员现核后解散。到期/解散立即拒绝普通通信，后台补扫、在途结算与通知可重试。

- 工蜂仓库：wego-ai/dingwei；分支 coder56solzzc/r53-tempgroup。
- 产品源码：9e19d816df8b70225a046a71398e979f22db6399；tree 438db012b3884fbcdcb7f584c8ca20cd9a42955d；父基线 c5152a213783e63626c161afefb34a7f45cfaea3。
- 实取执行线程：01a1b33c-11e7-7ec0-8de2-ce60abd9d9f5；单写锁 r53-tempgroup-b.writer.lock 持到推送及交件回报完成。
- 前端459105324f79629a3aac92cfb1de6e25547b1e31原样合入。A 仅增加公开 SettleDissolvedGroup 供 D 提交通知前调用，A 自己结算 A 的表；不改 B/C 协议、不改 helper。
- 迁移0106全部表列、序号备份、错误、三接口、main生命周期与回退语义见 internal/tempgroup/INTERFACE.md。迁移/claim/cmd 期望串及 RequiredTables 按本次明确授权同步0106。

## 验证

| 包/门 | 顶层通过 | 子项通过 | SKIP | 最终结果 |
| --- | ---: | ---: | ---: | --- |
| tempgroup | 4 | 4 | 0 | pass |
| extseat | 8 | 12 | 0 | pass |
| mcpgw | 25 | 95 | 0 | pass |
| extwire | 5 | 0 | 0 | pass |
| migrate | 27 | 15 | 0 | pass |
| workpulse | 168 | 90 | 6 | pass |

- race -count=3：tempgroup 顶层12/子项12、extseat 顶层24/子项36，均包级PASS、零SKIP。
- D浏览器、m8定向、OpenSpec strict、四变异均通过。
- 正式短TMPDIR build.sh rc0：go1.22.2，SOURCE_COMMIT=9e19d816df8b70225a046a71398e979f22db6399；helper两个profile各1097/1097，27 Python/2 shell，PG claim五子项原样实跑PASS；四个二进制只保留本机bin/，哈希见RESULT.json。


最终命令、UTC、源码锚与退出码见 RESULT.json/raw/*.json；Go 的包级状态、顶层/子项通过数和每个 SKIP 原因见 GO-SUMMARY.json。测试使用普通用户 dev、官方 go1.22.2、本地真实临时 PostgreSQL；剥离继承 SH_/DINGWEI_/DW_/DATAD_，不用现网库或凭据。

四项变异在最终9e19锚各由语义断言杀死：

| 变异 | 必须出现的断言 |
| --- | --- |
| 不写 np_group_member 镜像 | mirror members missing |
| Recheck 改按传入 Group 查找 | group dissolved or expired（仅AgentID的真实反查格） |
| 原生 SEQUENCE 换事务计数表 | sequence rollback reused number |
| 删除两层登录者限定 | login-owner filter |

过滤有列表 SQL 与详情授权两层防护；早期只删列表谓词的变异被详情层挡住，仍绿，原始读数已保留。最终过滤变异同时移除两层，红在跨主人隔离断言，不以编译失败充数。

## 失败、复跑与范围

- 系统 Python 首次 helper 发现失败（缺 websockets.sync）；改用已有仓外 helper-deps-lock-venv 复跑，不改测试门/依赖清单。该探索日志不是最终构建证据。
- 首次最终锚六包命令的 cmd 仅两项 Unix socket 路径过长失败；测试运行器 TMPDIR 前缀过长。保留原始 rc1；同源码改 TMPDIR=/tmp 后两项定向通过，并严格重跑 cmd 整包。其他五包原命令已通过，不冒认首次组合命令 rc0。
- 第一次最终锚 build.sh 的 helper 两 profile 各1097通过，但在末尾跨运行时入口的 PG claim.cancel_then_new 与桌面两子项失败；同一长 TMPDIR。完整跨运行时入口在 TMPDIR=/tmp 原样复跑rc0，随后同源码以短TMPDIR重跑完整build.sh；两轮原始记录均保留。
- cmd 整包另有6个既有 opt-in SKIP：独立R50二进制兼容、独立R30二进制回退兼容，以及 HLC fault/watchdog、bind failure、load、S007 full 四个独立进程矩阵。各用例名与原跳过条件在 GO-SUMMARY.json；未把它们计PASS。
- 额外仓储检查 rc1：父 c515 与当前均为既有 sql.Open 10>基线5；SystemCtx均16，逃逸均1，输出按行集合完全相同（只有遍历输出次序不同）。BOUNDARY-COMPARISON.json 和双原始日志证明零新增违例；未放宽该门，不把它列绿。
- 两次已替代源码的构建子树因源码后续修订主动终止，不作为最终9e19构建读数；产品提交只追加，未改写远端。
- 真实飞书后台登记/生产登录和第三方 MCP 客户端现场互操作未在本地执行，不计通过。真实ABCD开户装配用的是本地假飞书上游＋真PG，测试确认 REST 解散前200、解散后403 group_dissolved。
- 通知是持久终态后的完整目录快照与 HTTP 请求现核拒绝；可在崩溃后重放相同快照，不声称网络 exactly-once 或对端已处理。received 不回退，queued/leased/inflight 的结算用真实 A 方法验证。
- 未部署、未操作生产迁移/重启；源码与证据仅经 Git 交付，二进制只在本机保留并记录哈希。集群暂停与 cluster 锁释放由原持锁线程01a1b1a2负责，本线程未改其树/锁。

实现自测交件，独立复核/预发放行由组长组织，不自验收。
