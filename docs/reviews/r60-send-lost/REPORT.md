# R60① 发信责任恢复：容量退回复交

组长2026-11-16T19:52:29Z指出旧ba产品的累计栅栏确定耗尽，本轮改为有期限保留并补回归。旧交件6163fbb4/产品ba7acae7撤回，不请求旧锚评审；`evidence/`只属旧ba历史，不能对当前产品运行其manifest后声称覆盖新源。当前权威门为`evidence-retention/`，下面只列本轮固定源；没有自验收、合入或部署。

本件为作者证据，不是独立验收、部署 GO 或目标已读/模型执行证明。冻结产品与测试源 `12f851f239e43a3049cb3c366b335e368295f584`，tree `66a07f90edd5280c7cb0a1a6dd3f9deee7a671c1`，parent `b273dbf33dc63e7a99f594edc7a41641c5859da4`；基 main `d2cc47c7e699eb45efb3a07ee197aa8c20a9f07e`。分支 `coder56solzzc/r60-send-lost`。交件文档另成追加提交，具体当前 tip/tree/parent 以 Git 与交审回信为准，不把 tip 与冻结产品源混写。

授权为组长2026-11-16T16:05:24Z R60-A，业主16:03:17Z“R60 --- OK，可发”，rulings㊿-补123 `2f5560b7`。依据冻结提案 `aaa3f893626004e0140ef7a67316b692a8506b18` 实现。只覆盖 R60①；不含故障接管、上下文共享或多区域实现。未部署、未升级任何在线 helper、未用真实席位发故障探针。

## 实际行为与代码地图

- `tools/sessionhelper/sessionhelper/outbound_queue.py`、`outbound_recovery.py`：在网络写前原子落私有 outbox，request/source/sender/target/正文摘要不变；IPC 与 writer 由 `SessionHelper.run_forever` 管理，独立于单代 WS。断线后只恢复原发送责任，不驱动收件模型盲重做。单 writer flock、逐行租约、崩溃后同 ID 恢复。
- `internal/store/outbound_intent.go` / 迁移 `0077_outbound_intent.sql`：认证 sender/source 唯一键与首条收件 delivery 同一 SQLite 事务；回执在提交后。并发、Hub 重启、回执丢失重交只恢复原 delivery，正文/业务 meta 冲突拒绝，原裁决不覆盖。0077 纳入既有 exact-DDL 丢账补账校验，缺索引或改表拒启。
- `internal/m8/outbound_recovery.go`：恢复标记先于业务分派处理；协商能力、nonce/当前 socket 代际、稳定 sender/target 身份与当前用户/编组均检查。物理发出前再核稳定身份；临时存储/身份读错重试，永久拒绝只生成绑定原 sender 的通用 HUB 失败通知。新同名 sender/target 不继承旧正文/回执。
- `outbound_receipt.py`：只接受当前 socket、关联 nonce、同认证 Hub 地址范围及持久字段完整的控制回执；控制消息不进入收件模型。会话名按平台大小写规范归一，nonce/socket/ID 校验未放宽。
- `send_dingwei.py` / `mcp_shim.py`：caller 在首次 IPC 前持久固定 request ID；`--request-id` 同意图续用，`--status` 与 MCP `dw_send_status` 只查不发。structured accepted/unknown/definitive_failure 分层，CLI 分别退出0/2/1。仅 IPC ok/sent 布尔不得冒充入 Hub。MCP 工具数新增为27，既有权限和错误隔离保留。接收地址不从继承 SH_KEY_ID 猜后缀，只按认证目录公布的别名/全名绑定。

## 明确范围与上线前必须知悉的取舍

1. 首批仅覆盖专属凭据普通根 Agent、单目标可靠会话队列。飞书/外部/子 Agent、未协商持久 ACK 的收方、实际正文 AMR 扇出/语法副作用明确不支持；不删除 AMR 标记把正文当 literal 偷换语义。metadata 控制能力询问不承载业务正文。桌面专用 reply 路径保持独立。
2. 本地已接管仅 `local_queued`；Hub 提交才 `hub_durable_queued`，两者均不等于收件模型完成。前次接纳未知、后次权限拒绝仍保留原 unknown/ID/正文；不能拿当前拒绝证明历史未接受，不能鼓励 caller 换新 ID 重发。
3. 本地待确认上限1024条、正文+meta共32MiB，轻量请求栅栏65536条；最多64次/24小时，30秒租约、1～30秒退避。完全离线也执行期限收口：未曾写出者 rejected，曾尝试者 unknown 并保留正文。未知项不作缓存淘汰。审计8192事件、状态最多最近4条，独立于业务/幂等保留，不代表完整7天统计。
4. Hub 栅栏默认保留7天，`WP_OUTBOUND_INTENT_RETENTION` 可配Go duration、必须严格大于helper24h+2h余量；不合格启动失败。4096/65536仅计窗内栅栏。新受理事务至多删除256条严格过期且已不承担未终结原投递/失败通知身份的栅栏，再原子计数、接纳；窗内真满仍明确拒绝。超期未终结绑定保留、不计窗内容量，其异常历史空间不宣称硬界；原收件delivery/正文/本地unknown不由GC删除。0077尚未上线，按组长要求直接补created_at索引。
   恢复保证针对合规helper的24h自动恢复窗；默认7天覆盖其期限与时钟余量。最小配置额外2h按两端各UTC±15min、累计时钟变化预留1h、重启调度/在途再1h计算。超过假设须停writer核时钟。此修正不提供任意手工/恶意越窗source ID的永久幂等，不能把保留期缩短包装为全时域保证。
5. 未知目标、目录截断/临时失败可漏新目标，不猜稳定ID；同 sender 的既有目录可离线缓存，目标/凭据改变仍在网络写前现核。目录最多1024目标，60秒/未知目标触发仅元数据刷新。
6. 新 WS 十秒没有恢复能力，或明确不支持：不写正文并给可见不支持结果。十秒超时不证明对端软件版本老。此路径下先前 unknown 被持久停发，升级后不自动批量补送；本轮无解除停发的管理工具，需授权人工核验。既有成功本地裁决不因当前能力不足变成历史拒绝。
7. 本机 IPC/文件锁实现范围为现有 POSIX helper 环境；不新增原生 Windows IPC 支持。账本0600/WAL/FULL，不共享不同 Agent 私有卷，不上传正文、凭据或本地队列文件。
8. 授权沿用 Identity 当前读校验，不声称 PG/SQLite/网络逐字节写出构成分布式事务或绝对撤权原子性。回滚缺新稳定目标守卫的旧 Hub 前，必须停止新 writer，并由组长核定排空/可见取消新协议待发、保留栅栏；不能单切旧二进制继续发新协议存量。

## 证据与复现

最终命令、UTC、普通uid、固定环境白名单、起止 clean/commit、原始日志 SHA、PASS/SKIP计数在 `evidence-retention/gate-summary.json`；全轮历史在 `evidence-retention/gate-history.json`，原日志确定性 gzip 保存在 `evidence-retention/raw/`。`evidence-retention/product-manifest.json` 从冻结 Git 对象逐文件计算，不含私有状态。`evidence-retention/SKIPS.md` 逐名列跳过原因，SKIP 不混入 PASS。

真实集成覆盖：实际 Hub WS、Hub 重启/同 source 并发、提交后回执丢失恢复、SQLite 原子回滚、配置目录临时失败、身份替换/撤权实发阻断、HUB 失败通知主体隔离、真实 `run_forever` + hosted MCP 本地接管/Hub 持久状态查询；临时 PG strict 夹具不连现网。已有 PG 工具归档缓存复用，测试库仍为每轮独立临时库。Go -p1，env-i 固定 HOME/TMPDIR，继承 SH_/DINGWEI_/DW_/DATAD_ 不传入。

36项删保护基于同一最终源，`mutations.json` 每项含 before/after、compile rc0、非空业务断言与原始日志 SHA；baseline 均绿，跑完变异工作树还原 clean。不把独立故障注入、历史刀或编译失败并计。最后大小写 scope 以独立 capability 业务断言验证；首次直接用例在被删保护下报 domain ERROR 的记录未算最终合格刀。变异驱动首次两个 anchor 名笔误提前退出也未涂为全通过，最终36项仅从实有编译/断言记录合并。

## 必须保留的失败与修正历史

工作态/旧候选原始 rc 见 gate-history，不冒称同一源全绿：本机 overlay 空间耗尽后只迁本席缓存到 data 并保留路径软链接；早期旧工具数26/文件数94夹具更新为27/95；legacy IPC桥只 ok 导致旧成功退出码断言不成立，现收紧为 unknown 而仍验证原 PG 收件正文；hosted桥改真实 run_forever/责任查询；0077补入 exact-DDL 补账。之后候选完整门绿仍通过新反例发现容量拒绝分类、前次未知后拒绝、全离线期限/能力等待/旧unknown禁盲补等真实边界，追加修正重固定源，旧PASS仅历史。集中回归第一次误列不存在测试模块的 rc1 保留，正确模块重跑63项绿，不当产品错误。旧代码反例须读其自身 source/dirty 字段，工作态不伪标冻结源。

## 集成与后续

需 qwen38mvb4 只读独立复核、组长终审后才可合入 R60 集成；本件不自验收。与②分开实施、联合验证：断连减少不替代持久责任，①也不能代替 Pong 读取隔离；集成应覆盖过载/断开后同 ID 原回执恢复。③④①②顺序、基110e2c1f并保留现网 renderer provenance 热修、VERSION0.2.3.40及全量浏览器由集成阶段执行。本源未跑浏览器全量、identity整包或全包race；cmd/workpulse整包包含在本轮四树Go门，不借历史复用冒称本轮绿。当前 VERSION仍0.2.3.39，无发布件。


历史ba机械门收口：9e固定源完整canonical rc1，两profile均仅报环境读取精确白名单与main-guard-last。删除SH_KEY_ID推断使直接环境读取从2减至1，白名单同步收紧而非放宽；新地址测试类移至unittest入口之前。两件仅测试变动形成ba7acae锚，产品逻辑字节不变，但仍在ba新源完整重跑canonical/Go/race/strict及25刀。名字含approved的原始运行标签仅是本席文件名，不代表独立评审或组长批准。

历史保留限度：早期磁盘耗尽后的试验变异有部分同名临时日志在重跑时被覆盖，已留存片段与门记录只按自身SHA/源码状态使用，不补造丢失原件。gate-history只列现存可核原日志，并不保证所有历史试验均有完整原件；这一缺口不计入最终36项合格刀或最终固定源门。

固定锚证据核验补记：ba的go-approved虽然rc0，但启动瞬间工作树dirty，结束clean，采集器保守把source_commit记null（与并发canonical初始临时文件窗口重合，但未抄具体脏路径，不能据此精确定因）。该读数只列历史，不改写元数据冒称固定源；待其它门结束后在完全clean工作树单独重跑go-clean-source，只有这条合格记录计最终Go门。



## 容量增量判据与删除保护边界

增量回归覆盖：65536条以上旧全表积累不再拒新、每事务256条有界回收；cutoff-1ns与cutoff/cutoff+1ns分界；窗内同ID原回执；原delivery与失败通知未终结的稳定身份不被删；服务端接受时钟而非caller时间；配置48h实际控制清理；坏配置启动拒绝；队列失败回滚整个GC和接纳。helper WINDOW=24小时合同固定，未改helper恢复时限。

按只计窗内的要求，删GC不应被伪造成必然拒新：删GC的刀红在旧行没清，删仅超期条件红在窗内回执被删；删sender/global时间过滤红在历史积累错误占新配额。第一轮36刀中global过滤刀存活，是过期夹具清一批后剩余刚好低于65536；原始存活记录保留。仅增大测试旧积累至65536+256，产品逻辑字节不变，追加测试源后重新完整跑门和36刀，不抹掉存活结果或改为SKIP。

早期retention增量两次编译错误（误替换与未用import）及一次错误文案的过宽隐私断言失败均原样保留，修正后重新运行；它们不是保护删除的业务红。变异驱动首次扩展列表后case-scope索引错误提前失败也另留原记录，不计最终刀。

第一轮b273完整四树Go rc1仅两条cmd Unix socket测试在listen时报bind invalid argument，其中原始路径111字节超过Unix socket路径界限；保留原日志，后续同目录短路径TMPDIR=/tmp/r60-a重跑完整四树，不改产品修环境。不得把首轮FAIL改成SKIP或用单跑覆盖。

新12f首轮race/build/strict虽rc0，但起跑时工作树dirty，采集器source_commit记null（与canonical起跑窗口重合，未留具体脏路径，不强行归因）。不改原元数据冒称固定源；另在同12f独立clean detached树完整重跑这三门，只有后者列权威门。

当前完整四树Go逐名补记：`TestNotifySocketStopCannotDeleteNewListenerAtSamePath`与`TestNotifySocketFirstStopPreservesNewListenerAtReboundPath`在短TMPDIR下均PASS；`TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent`也在本轮完整运行PASS（0.24s）。此读数仅属①源，不替代②首轮SQLite interrupted(9)披露或将来集成树的完整四树验证。

## 当前固定源最终门读数

| 门 | rc | 顶层 PASS | 含子项 PASS | 含子项 SKIP | FAIL |
|---|---:|---:|---:|---:|---:|
| canonical-retention-final | 0 | 6 | 21 | 0 | 0 |
| go-retention-final | 0 | 1558 | 2583 | 49 | 0 |
| race-retention-pinned | 0 | 20 | 31 | 0 | 0 |
| build-retention-pinned | 0 | 0 | 0 | 0 | 0 |
| strict-retention-pinned | 0 | 0 | 0 | 0 | 0 |

Canonical 两 profile：claude 1230/1230 rc0；codex 1230/1230 rc0；BUILD_TESTS_OK helper_profiles=2 python=27 shell=2。focused race仅覆盖所列筛选，不是全包race；Go包PASS不与Python/helper子项混计。strict明细见原日志。
