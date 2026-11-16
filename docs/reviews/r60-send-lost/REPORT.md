# R60① 发信责任恢复：作者实现与验证交件

本件为作者证据，不是独立验收、部署 GO 或目标已读/模型执行证明。冻结产品与测试源 `ba7acae7798eda8a6d17f3e03dfa07c8c65835f1`，tree `113469c42dca30eae4fa96d95a14d036c8e54a73`，parent `9e1d3017fde8df915b22cd3a690a4d1dbd6fc334`；基 main `d2cc47c7e699eb45efb3a07ee197aa8c20a9f07e`。分支 `coder56solzzc/r60-send-lost`。交件文档另成追加提交，具体当前 tip/tree/parent 以 Git 与交审回信为准，不把 tip 与冻结产品源混写。

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
4. Hub 栅栏每 sender4096条、全局65536条；满时仍恢复旧裁决，新接纳原子拒绝且不留收件/裁决半行。没有自动 GC；长期会达到容量上界并拒绝新接纳。这是安全保留与可用性的取舍，候选定值须组长终审和维护计划，不能声称无限期吞吐或擅自清库。
5. 未知目标、目录截断/临时失败可漏新目标，不猜稳定ID；同 sender 的既有目录可离线缓存，目标/凭据改变仍在网络写前现核。目录最多1024目标，60秒/未知目标触发仅元数据刷新。
6. 新 WS 十秒没有恢复能力，或明确不支持：不写正文并给可见不支持结果。十秒超时不证明对端软件版本老。此路径下先前 unknown 被持久停发，升级后不自动批量补送；本轮无解除停发的管理工具，需授权人工核验。既有成功本地裁决不因当前能力不足变成历史拒绝。
7. 本机 IPC/文件锁实现范围为现有 POSIX helper 环境；不新增原生 Windows IPC 支持。账本0600/WAL/FULL，不共享不同 Agent 私有卷，不上传正文、凭据或本地队列文件。
8. 授权沿用 Identity 当前读校验，不声称 PG/SQLite/网络逐字节写出构成分布式事务或绝对撤权原子性。回滚缺新稳定目标守卫的旧 Hub 前，必须停止新 writer，并由组长核定排空/可见取消新协议待发、保留栅栏；不能单切旧二进制继续发新协议存量。

## 证据与复现

最终命令、UTC、普通uid、固定环境白名单、起止 clean/commit、原始日志 SHA、PASS/SKIP计数在 `evidence/gate-summary.json`；全轮历史在 `evidence/gate-history.json`，原日志确定性 gzip 保存在 `evidence/raw/`。`evidence/product-manifest.json` 从冻结 Git 对象逐文件计算，不含私有状态。`evidence/SKIPS.md` 逐名列跳过原因，SKIP 不混入 PASS。

真实集成覆盖：实际 Hub WS、Hub 重启/同 source 并发、提交后回执丢失恢复、SQLite 原子回滚、配置目录临时失败、身份替换/撤权实发阻断、HUB 失败通知主体隔离、真实 `run_forever` + hosted MCP 本地接管/Hub 持久状态查询；临时 PG strict 夹具不连现网。已有 PG 工具归档缓存复用，测试库仍为每轮独立临时库。Go -p1，env-i 固定 HOME/TMPDIR，继承 SH_/DINGWEI_/DW_/DATAD_ 不传入。

25项删保护基于同一最终源，`mutations.json` 每项含 before/after、compile rc0、非空业务断言与原始日志 SHA；baseline 均绿，跑完变异工作树还原 clean。不把独立故障注入、历史刀或编译失败并计。最后大小写 scope 以独立 capability 业务断言验证；首次直接用例在被删保护下报 domain ERROR 的记录未算最终合格刀。变异驱动首次两个 anchor 名笔误提前退出也未涂为全通过，最终25项仅从实有编译/断言记录合并。

## 必须保留的失败与修正历史

工作态/旧候选原始 rc 见 gate-history，不冒称同一源全绿：本机 overlay 空间耗尽后只迁本席缓存到 data 并保留路径软链接；早期旧工具数26/文件数94夹具更新为27/95；legacy IPC桥只 ok 导致旧成功退出码断言不成立，现收紧为 unknown 而仍验证原 PG 收件正文；hosted桥改真实 run_forever/责任查询；0077补入 exact-DDL 补账。之后候选完整门绿仍通过新反例发现容量拒绝分类、前次未知后拒绝、全离线期限/能力等待/旧unknown禁盲补等真实边界，追加修正重固定源，旧PASS仅历史。集中回归第一次误列不存在测试模块的 rc1 保留，正确模块重跑63项绿，不当产品错误。旧代码反例须读其自身 source/dirty 字段，工作态不伪标冻结源。

## 集成与后续

需 qwen38mvb4 只读独立复核、组长终审后才可合入 R60 集成；本件不自验收。与②分开实施、联合验证：断连减少不替代持久责任，①也不能代替 Pong 读取隔离；集成应覆盖过载/断开后同 ID 原回执恢复。③④①②顺序、基110e2c1f并保留现网 renderer provenance 热修、VERSION0.2.3.40及全量浏览器由集成阶段执行。本源未独立跑浏览器全量、identity/cmd整包或全包race，不借历史复用冒称本轮绿。当前 VERSION仍0.2.3.39，无发布件。


最终机械门收口：9e固定源完整canonical rc1，两profile均仅报环境读取精确白名单与main-guard-last。删除SH_KEY_ID推断使直接环境读取从2减至1，白名单同步收紧而非放宽；新地址测试类移至unittest入口之前。两件仅测试变动形成ba7acae锚，产品逻辑字节不变，但仍在ba新源完整重跑canonical/Go/race/strict及25刀。名字含approved的原始运行标签仅是本席文件名，不代表独立评审或组长批准。

历史保留限度：早期磁盘耗尽后的试验变异有部分同名临时日志在重跑时被覆盖，已留存片段与门记录只按自身SHA/源码状态使用，不补造丢失原件。gate-history只列现存可核原日志，并不保证所有历史试验均有完整原件；这一缺口不计入最终25项合格刀或最终固定源门。

固定锚证据核验补记：ba的go-approved虽然rc0，但启动瞬间工作树dirty，结束clean，采集器保守把source_commit记null（与并发canonical初始临时文件窗口重合，但未抄具体脏路径，不能据此精确定因）。该读数只列历史，不改写元数据冒称固定源；待其它门结束后在完全clean工作树单独重跑go-clean-source，只有这条合格记录计最终Go门。


## 固定源最终门读数

| 门 | rc | 顶层 PASS | 含子项 PASS | 含子项 SKIP | FAIL |
|---|---:|---:|---:|---:|---:|
| canonical-approved | 0 | 6 | 21 | 0 | 0 |
| go-clean-source | 0 | 1361 | 2282 | 42 | 0 |
| race-approved | 0 | 12 | 14 | 0 | 0 |
| build-approved | 0 | 0 | 0 | 0 | 0 |
| strict-approved | 0 | 0 | 0 | 0 | 0 |

Canonical helper 两 profile：claude 1229/1229 rc0；codex 1229/1229 rc0；末尾 BUILD_TESTS_OK helper_profiles=2 python=27 shell=2。表格的 Go PASS 不与 Python/helper 子项混算。focused race 不是全包 race。strict CLI 明细见原日志。
