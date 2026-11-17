# R60①：调用方账本容量退回复交

组长2026-11-16T23:15:43Z撤销旧12f冻结；旧20:50接受及旧门仅历史。本次交审固定产品与测试源为 `b48a0818aa121a3f68b921fac489926257ad841d`，tree `567f180571e352684e06eb3966c150f8e2b48a19`，parent `1d8b03a6d386f359a23398da34649007d53c1aa2`，基main `d2cc47c7e699eb45efb3a07ee197aa8c20a9f07e`；分支 `coder56solzzc/r60-send-lost`。完整作者门已收齐；交件tip为此后纯docs/openspec追加，准确三值组以Git与交审回信为准。新独立复核与组长终审尚未完成，不自验收、不合入main、不部署。

授权仍为组长2026-11-16T16:05:24Z的R60①与23:15:43Z容量修订令。范围不加入故障接管、共享上下文或多区域实现。只有本机合成夹具与独立临时PG，不用在线席位做故障探针。

## 12f 到新锚的产品增量

12件产品/测试文件，317插入、32删除：`internal/store/outbound_intent.go`、store/cmd两件retention测试；helper的`send_dingwei.py`、`mcp_shim.py`、`config.py`、`app.py`、`outbound_queue.py`、`outbound_recovery.py`与三件现有测试文件。迁移0077、go.mod/go.sum、VERSION、View与其他产品子树本轮不变。相对main的完整manifest另列，不能仅以这12件代替全源清单。

- caller表新增created；旧两列表在BEGIN IMMEDIATE内补列、所有既有行按迁移当前时刻填充，保留ID和摘要，不删除未知历史。创建created/id索引。
- 默认保留7天；helper配置 `SH_OUTBOUND_CALLER_RETENTION_SECONDS` 为整数秒，必须>93600且≤2592000；Hub `WP_OUTBOUND_INTENT_RETENTION` 的Go duration同样>26h且≤720h。非法配置启动拒绝，回执/日志不回显原始输入。helper策略登记到caller库，独立安装CLI先使用已注册策略；无策略时才读自身配置。
- quota仅计窗内caller记录；每次新受理至多清256条严格超期且无待确认责任的caller行。同ID先检查旧摘要并恢复原caller ID；不同摘要仍拒绝，不通过GC冲掉窗内原裁决。
- runtime启动IPC/writer之前登记实际outbox路径，不猜文件名。清理持caller写事务，逐个取得已登记outbox的写保留锁并读local_queued/unknown集合，锁持到caller提交之后；正常outbox写者不取caller锁，无反向锁序。无登记、路径消失、锁忙、损坏或结构不可读均跳过全部caller删除，不把读取失败当作已终结；额外锁获取预算0.25秒，单库等待最多0.05秒，查询本身不宣称硬实时界限。
- `caller_journal_full` 两入口均返回 `outcome=definitive_failure/phase=rejected/reason=caller_journal_full/request_id/retryable=true`；`idempotency_conflict` 同结构但retryable=false。CLI退出1并stderr给reason，MCP为工具结果isError，不能折叠成JSON-RPC -32602。参数本身非法仍可为参数错误。
- 同类outbox终结历史原来也按全表累计65536，本轮一并收敛：只清超期hub_durable_queued/rejected、每次≤256，quota只算窗内。local_queued/unknown不删，1024条/32MiB待确认上限、24小时/64次自动恢复窗、稳定source/sender/target与Hub原子幂等均保持。

## 清理的安全边界

未终结项不删，不代表物理磁盘有固定上限。caller关联库不可读会保守累积；旧路径登记没有自动撤销，迁移需保留可读旧库或经核验维护，不能随便删登记绕过unknown保护。窗外待确认P及未扫净E不计窗内容量，但占空间；需分别观察W/P/E、最老年龄、outbox不可读和清理跳过情况。旧全满两列库迁移时全部填当前时刻，可能在完整安全窗内继续拒新；不以删旧未知ID“立即恢复容量”。

默认7天覆盖helper24h恢复窗与2h以上时钟/重启/在途余量；具体假设及Hub超期未终结delivery/失败通知身份保护见design。超过时钟假设先停writer核时钟。保留窗外人为重放不获永久幂等承诺。oldID在其历史行尚未清理时仍先恢复，GC仅作用其他新请求触发的严格超期记录。

## 固定源门与反例

同源完整canonical rc0（claude/codex各1241、BUILD_TESTS_OK helper_profiles=2 python=27 shell=2）；四树Go PG strict rc0，1558顶层/2586含子项PASS、49逐名SKIP、0FAIL。build与OpenSpec strict36/0均rc0。focused race rc0，32顶层/46含子项PASS、0SKIP、0FAIL/0DATA RACE，仅所列筛选。最终元数据与原始日志在 `evidence-caller-retention/`。

`TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent`在本轮完整Go中PASS（0.24s）；`TestNotifySocketStopCannotDeleteNewListenerAtSamePath`与`TestNotifySocketFirstStopPreservesNewListenerAtReboundPath`也均PASS（0.00s）。这不替代②首轮SQLite interrupted(9)原因未定的历史披露，也不是正式集成树的Go门。

聚焦回归覆盖旧schema保留、窗内同ID、容量释放、pending双状态、未知关联/锁忙保守保留、并发writer不能穿过清理快照、真实run_forever先登记/配置传播、CLI与MCP结构化拒绝、helper/Hub上下界、outbox终结历史回收。新版58项集中Python已绿；它不替代完整canonical。

首次08bc完整canonical rc1：两profile仅环境读取精确白名单未同步。单文件CLI确需独立读取保留策略，在白名单精确登记第二处及理由，未扩大任意环境读取。第一轮变异发现过期边界刀存活：有界清理夹具只检查首批旧行，补“积压清完后边界仍保留”断言；产品逻辑字节未因这两次测试追加改变。早期MCP变异anchor缩进笔误、窗内容量刀只抛业务ERROR等不计合格红，全部原记录保留。最终55项均编译rc0且业务断言红（旧36＋新增19），两组baseline及还原后集中回归均绿；不与历史尝试加总。

## 调用方与集成

CLI JSON替代旧sent文本，accepted/unknown/definitive_failure退出0/2/1；local_queued不等于入Hub或模型完成。rc2不要换新ID重复正文，使用 `python3 "$HOME/.dingwei/send.py" --status REQUEST_ID` 查询，必要时仅以原ID/原正文/原目标续用 `python3 "$HOME/.dingwei/send.py" TARGET --request-id REQUEST_ID --file BODY_FILE`。原sender身份也必须保持；超窗不承诺手工永久去重。状态与新发均保留结构化责任字段。

正式集成仍须在110e2c1f按③④①②重做，VERSION0.2.3.40仅该阶段；保留现网renderer provenance热修及回归。①②必须在正式提交补真实helper unknown→dw_send_status→同source/delivery恢复联合故障格并删保护红，不能用普通CLI同ID两次或Hub单写槽用例代替。此源不含②，未宣称该联合格已完成；正式新树还须重跑同 --request-id 两个真实CLI进程仅一条Hub投递的回归，旧12f本地预合读数不当本源证据。③独立变更和caller输出契约/板件更新归集成说明。本轮未跑View浏览器全量、identity整包、全包race，不复用旧门当本源新实跑。

## 历史证据

旧 `evidence/`、`evidence-retention/` 与 `HISTORY-before-caller-retention.md` 按其自身源保留；其中旧接受、冻结、数字和环境失败不能外推。最新证据目录为 `evidence-caller-retention/`，需要独立复核后才能谈冻结。

## 最终固定源门明细

| 门 | rc | 顶层PASS | 含子项PASS | 含子项SKIP | FAIL |
|---|---:|---:|---:|---:|---:|
| canonical-caller-final | 0 | 6 | 21 | 0 | 0 |
| go-caller-final | 0 | 1558 | 2586 | 49 | 0 |
| race-caller-final | 0 | 32 | 46 | 0 | 0 |
| build-caller-final | 0 | 0 | 0 | 0 | 0 |
| strict-caller-final | 0 | 0 | 0 | 0 | 0 |
| python-caller-final | 0 | 0 | 0 | 0 | 0 |

Python unittest数以原日志Ran行计，不与Go的--- PASS计数混加；集中58项、还原后54项不是两个额外产品门总数。OpenSpec全仓36/0，helper inventory95；canonical两个profile均1241/1241。新源author门不替代独立复核。
