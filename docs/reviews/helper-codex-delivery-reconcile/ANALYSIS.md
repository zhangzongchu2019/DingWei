# S-HELPER-CODEX-DELIVERY-RECONCILE：分析与基线红测

本步仅完成分析、设计及复现测试，不修产品，不执行线上对账。本件不是上线、数据修复或 ACK 批量补写授权。基线 `opus50tc2k/r54-integration` / `6e2c836491f8ba506833d6f849fbe73530e13f92`，基线 tree `2dc7ce8bf75d133c67ad2ab86e4d9390908fc94e`。交审分支 `coder56solzzc/helper-codex-delivery-reconcile`；仓库 `git@git.code.tencent.com:wego-ai/dingwei.git`。唯一写者以 `/tmp/coder56-helper-reconcile.lock` 的原子创建结果为准，本次 owner 为实取 `01a1b9ec-a59e-79c3-97f7-eef3731a32a0`。

依据为 rulings 〔51〕 `review/opus50tc2k/rulings` / `24985813e3a8d3434e7b1872d6df9b6ada9ffa12`，组长派单 request_id `932d3ef7daa0`，及 2026-11-08T10:23:37Z 后发写锁更正（本机认证收件号 `dw-dlv_58b08863d8edd13d39295db188b43afa`）。更正覆盖前一封点名线程的执行权规则，不改变 A/B/C 范围。

## 1. 已核事实与不能外推的部分

非源码输入：[ts3dev55 自查报告](https://dw123.cmjrt.com/f/u_zhangzongchu/opus50tc2k.zzc/26/1108/54a2bff09b87987a4fd08832e87a5198.md)、[稍后采样附表](https://dw123.cmjrt.com/f/u_zhangzongchu/opus50tc2k.zzc/26/1108/a191243f7dd8d76a5c41f79cc88c9c36.csv)。本次直接读取两件并复算 SHA，见 `raw/source-report-metadata.json`；未进入对方机器、未读取其源码补丁。

- 报告第一快照为 admitted 56、unknown 160；后采样 CSV 实数 218＝57 admitted＋161 unknown，161 unknown 均无 turn ID。报告的 52 条原生已完成是对第一快照 56 条的核验，不能写成“57 条逐条独立核验已有52条完成”。本席只独立复算 CSV 数量，52 条与日志统计是报告方证据。
- 启动段 admission_timeout 101、RECOVERED 101、STALLED 51、INCOMPLETE_NATIVE_EVIDENCE 18、正常 done 18，均为报告方统计。计数相等不证明逐条一一因果，也不证明52条的全部丢失原因。
- 本机第二样本：10:20 派单正文点名 `01a19c8c`，实际本会话环境为 `01a1b9ec`，随后组长重定执行者再改为写锁。正文线程号没有改变 helper 的实际投递目标。这仅证明“文本寻址≠结构化 thread 路由”的操作现象；本步不实现子 Agent/多 thread 路由，不改 helper 绑定。
- 开工前以 SQLite `mode=ro`、`query_only=ON` 查询本机 delivery_inbox。截止新裁定前有64条组长非processed记录（仅标题/投递号/状态元数据见 `raw/earlier-leader-nonterminal.json`）。其中当日旧单 `dw-dlv_3247eb74f3a73264c036b886fae413f9`《S-GATES-GREEN派单》与 `dw-dlv_556097863f5a5962f932cba7faeecc70`《最小拒绝审计裁定》已被07:55冻结结项覆盖；其余多为历史已交件/裁定/提示，不能仅凭本地非终态宣称“业务未处理”。已向组长报告，全部不自动执行、不改库、不补 ACK。

## 2. A：终态为何不能可靠进入账本

以下为基线源码可核机制与本地受控复现，非对 ts3dev55 每条历史的最终归因。

| 挂点 | 基线行为 | 后果 |
| --- | --- | --- |
| `driver_adapter.py:420` `_bounded_deliver` | 从 dispatch 起计入场期限；超时调用 `_transport_unknown(...recover=True)` | 等忙回合与已发RPC未确认被同一超时路径处理 |
| `driver_adapter.py:468` `_recover_transport` | abort旧传输，等待自己的worker，stop旧driver，重新构造对象 | 原driver的通知队列、原生观察时间、turn映射、事件缓冲不能由新对象自动继承 |
| `driver_adapter.py:809` `_event_bridge` | 每次调用 `self.driver.events()`，未传since；去重使用对象同一性 | 只覆盖同一对象内缓冲重订阅，没有跨对象/进程的可持久续点；不是原生服务端通知游标 |
| `codexDriver.py:1291` `_pump_notifications` | 泵持本次rpc局部引用；通知映射依赖内存turn→envelope | 替换/断开与通知到达竞争可留下未观测终态；完成通知先于映射注册时也可能缺delivery关联 |
| `codexDriver.py:1340`、`:928` | admitted_at来自本机收到turn/started或turn/start响应的 `time.time()`；completed事件同样用观察时刻 | 这些是旧对象的原始观察时间，不是history中的原生发生时间；丢失后不能以当前时间补造 |
| `codexDriver.py:947` `poll_receipt`、`:1604` `_read_turn_state` | 已知turn可查completed/failed/interrupted，但只读desc前20；done排空超时可返回done而未发布完整原生事件 | “回合完成/receipt已done”与“inbox可processed”实际分离；既有failed/done短路也会跳过进一步对账 |
| `codexDriver.py:1482`、`:1510` history回放 | since回放只读asc前200；忽略nextCursor；即使turn含startedAt/completedAt，生成的terminal不携admitted_at/processed_at；还对非终态生成change=turn_completed形状 | 主动调用history也不能补齐ACK证据；不能把视图回放接口直接当终态修复器 |
| `driver_adapter.py:721` `_record_event_lifecycle` | 缺原始时间即打印INCOMPLETE_NATIVE_EVIDENCE，不推进ACK | 此保护正确，应保留；不能为了消除积压放宽 |

同driver健康重订阅已有“先登记consumer再读缓冲”及同对象去重保护，不是所有重订阅都必然丢事件。直接stdio模式的abort会终止其自有app-server，可能连带结束正在跑的回合；私有daemon模式关闭连接并不等于daemon/turn结束。重连只初始化RPC，绑定thread的read/resume通常在下一次deliver才验证；因此不能假设新连接立刻恢复旧thread的实时订阅。

## 3. B：unknown缺少只读对账；长回合放大风险

`codexDriver.py:950`：持久记录state=unknown且无turn ID时，poll直接给内部failed receipt，连history查询都不发；持久driver账本仍unknown。`get_by_envelope`虽能找到旧thread记录，也不会按clientId搜索原生turn。adapter将其留为admission_unknown并禁止自动重投，这个防重边界应保留。

`timeout_feedback.py`使用真实CodexProtocolDriver/DriverAdapter/DeliveryInbox，加原生形状RPC屏障夹具：已有长回合inProgress，新消息在`_wait_until_idle_for_delivery`等待；将入场期限缩为0.06秒后，测到`turn/start=0`、transport healthy、inbox unknown、请求替换次数1。用例红在 `BUSY_WAIT_TIMEOUT_REQUESTS_TRANSPORT_REPLACEMENT_BEFORE_ANY_TURN_START`。屏障finally释放，自己的worker已join；没有实际启动替换进程。这证明“未提交的新消息因等待旧回合→unknown→请求替换健康传输”的前半链，无需真实等待60秒。

结合A的内存证据丢失机制，存在“长回合→新投递入场超时→替换→旧终态观测中断→STALLED/更多unknown”的放大路径。尚未以真daemon+长回合串联完整101次，也未证明所有STALLED源于此；报告数据只支持风险相容。后续应分别计量：等忙耗时、真实turn/start发出时刻、响应/通知收到时刻、替换代次与原因、受影响在途turn数。健康忙回合不应仅因新消息的入场期限就替换，传输确实失活/写者卡死才走受控恢复；未提交的本地排队与可能提交的unknown须有明确内部阶段，不能因为“等待忙”凭空赋予原生入场。

## 4. 原生证据对账设计（后续实现提议，本步未实现）

### 身份与扫描

对账输入固定为helper身份/席位、delivery ID、原request/clientUserMessageId、绑定thread及可信历史绑定集合、原dispatch intent、ACK协议/代次；不使用正文前缀或正文近似匹配。历史集合从本席driver持久记录、受保护的绑定历史/私有endpoint状态取得；不能遍历其他席位所有thread，也不能仅凭一份`get_by_envelope LIMIT 1`声称覆盖全部历史。

A先按已知 `(thread_id, turn_id)` 精确找；B在当前及所有可信历史绑定thread中，以userMessage.clientId精确等于clientUserMessageId匹配。调用`thread/turns/list`的`itemsView=full`，遍历opaque nextCursor；禁止拿View的`turn:...:event:...`当原生API分页游标。支持可核的只读等价接口`thread/read(includeTurns=true)`时可适配，但必须单独验证该版本/加载状态/完整性；未materialized、notLoaded、归档不可读、超时、分页未尽、重复匹配、字段缺失都不能写成“无turn”的确定结论。

锁定版原生协议支持startedAt/completedAt（秒，nullable）、userMessage.clientId、nextCursor/backwardsCursor，见 `raw/native-schema-extract.json`。本机实际测试发现：空thread尚未materialized时turns/list回-32600，thread/read含turns可能回-32601；首次userMessage落盘后turns/list可用，不能把前述错误变成安全重投依据。官方[App Server说明](https://developers.openai.com/codex/app-server)仅作接口背景；本件字段及运行行为以锁定二进制生成schema和实际响应为主，不拿别的Agents API端点替代Codex协议。

### 证据与状态推进

每次候选记录保存原生来源/版本、thread/turn/clientId、status、startedAt/completedAt、查询时刻、分页范围、原状态/ACK代次及原生响应摘要哈希；默认不记消息正文、工具输出、凭据。查询时刻与原生发生时刻分开。只接受有限数值、合法单位、started≤completed、精确身份及turn一致性；旧缺时刻记录继续unknown，不用UUID时间、文件mtime、usage、结束文本或当前时钟推算。

- 找到唯一inProgress且原生startedAt完整：以审计化、幂等的证据合并将unknown→admitted，保留原生开始时间。既有`record_admitted_before_deadline`故意拒绝超时后accepted-only，不能复用它偷偷重开窗口；需单独的原生只读对账路径，复用/收紧现有`record_admitted`的证据入口。
- 找到唯一终态且两个原生时刻完整：以现有`record_native_terminal`事务语义合并injected/processed outbox，outcome按completed/interrupted/failed分别落；processed不等于业务成功。原ACK代次不重新发明，重复对账幂等，不替换已绑定turn/outcome。先落持久状态及outbox再发布；回收原生证据独立于某个已释放的内存command。
- 已确认admitted但无结束时间：仍admitted。缺字段、有歧义、查不到、查询失败：保留原状态/unknown，记录有限原因，无ACK推进、无自动重投。即使负扫描完整也只是“本次范围未查到”，不能证明历史上从未执行。
- 要检查存储层现有时间验证：`record_native_terminal`当前没有完整的finite/未来时间/精度校验；秒级原生startedAt可能比浮点dispatch intent小不足1秒，不能机械比较后误拒。处理精度容差必须可审，不能借容差任造时间。此设计不把现有宽松函数说成已经满足全部新约束。

### 事件恢复与游标

先建立实时订阅，再查询所有待对账turn；用稳定 `(seat,thread,turn,terminal-kind)` 幂等合并终态。实时delta仍按既有item/对象规则处理，同cursor的不同delta不能丢。持久记录pending集合及证据来源/恢复进度，跨新driver和helper重启恢复；不靠对象id或内存live序号跨连接连续。绑定重新验证与必要的thread恢复属于明确连接生命周期，不在“只读对账”函数偷偷执行turn/start/resume。首次缺订阅的情况不能靠轮询以外的虚构游标解决。

对账轮询有总预算、分页上限、每thread速率与退避；上限触发输出incomplete并留pending，不给阴性结论。不每个轮询复制完整线程历史到日志。范围限定两处helper修复及必要证据持久性；不借机重设计Hub/View、多地址或多thread路由。

## 5. C：正式版本如何替代对方本地补丁

只依据自查报告描述，未读也不声称审过那份本机补丁的逐字diff。报告称其在回合结束后history补输出并合成turn_completed却缺原生时间，adapter正确拒绝。

正式代码已有capability-gated `_take_completed_item_backfill`：只读完成agentMessage，以thread/turn/item稳定键供View替换，不写driver receipt、inbox终态或ACK；现有 `test_codex_item_backfill`覆盖此分离。沿用/补强它供用户可见输出补取，保留视图能力协商、完整item筛选和去重。终态证据通过上节独立路径取得并持久验证；展示完成不能冒充投递已确认，不合成缺证据的成功ACK。

后续上线时由获授权owner/ops在对方机器先备份本地补丁与运行版本指纹、部署正式Git构建、核文件hash/实际loaded版本、以唯一测试投递验证输出与ACK两条链，确认后再撤本地补丁。该步骤由后续发布单授权，不在本步执行；ts3dev55仍负责161条未知的实际对账，本席不代为批量修状态。

## 6. 复现入口、结果与边界

`tests/helper-codex-delivery-reconcile/real_protocol.py`：每格私有HOME/CODEX_HOME、清空继承环境、独立app-server stdio，硬核官方锁定二进制SHA `1748767b230ebfc3d4ab7e4e254920d0c0ad9691fd8c11f190e7d44511a4a92e`（0.159.2）。模型端点仅127.0.0.1:9，无认证文件/真实推理。等本次userMessage原生落盘后仅中断自己创建的turn，取得原生interrupted终态，不把模型成功作为测试前提。测试结束只terminate/wait自己的子进程，不管理在线daemon。A2故意丢掉原生通知、不起泵，模拟替换后仅剩持久driver账本；新对象共享本测试RPC以隔离“服务器死亡”变量，测试的不是daemon进程重启全链。

```sh
python3 -B tests/helper-codex-delivery-reconcile/real_protocol.py \
  --binary /tmp/coder-stale-protocol-01592/codex \
  --output /tmp/helper-reconcile-native-UNIQUE
python3 -B tests/helper-codex-delivery-reconcile/timeout_feedback.py
```

| 用例 | 基线读数 | 红点/控制含义 |
| --- | --- | --- |
| `test_A_history_replay_preserves_native_times_for_ack` | FAIL | 原生history完整但回放terminal缺时间，inbox仍admitted |
| `test_A_replacement_poll_and_resubscribe_recover_lost_terminal` | FAIL | 新对象无游标订阅没有终态恢复 |
| `test_B_timeout_finds_client_id_on_bound_thread_without_resubmit` | FAIL | 原生clientId存在，poll却没有恢复turn映射 |
| `test_B_timeout_finds_client_id_on_historical_thread_without_rebind` | FAIL | 旧thread持久记录可用仍不对账；不重投 |
| `test_control_no_matching_turn_stays_unknown_without_resubmit` | PASS | 空/未materialized范围不推进账本；receipt内部failed不冒充持久failed |
| `test_control_output_only_terminal_cannot_advance_ack` | PASS | 缺原生时刻的合成terminal被正确拒绝 |
| `test_control_complete_native_evidence_can_settle_terminal` | PASS | 同一adapter/inbox有完整原生证据即可processed/interrupted，排除存储夹具无效 |
| `test_busy_wait_without_turn_start_must_not_replace_healthy_transport` | FAIL | turn/start=0的健康忙等待也请求替换 |

最终原生7格：rc1、FAIL4/PASS3/ERROR0/SKIP0；超时夹具1格：rc1、FAIL1/ERROR0/SKIP0。见 `raw/native-final.log`、`raw/native-final/RESULT.json`、逐格原生响应和 `raw/timeout-feedback.log`。五个目标红正是本步要求；不加expectedFailure、不改产品让它们转绿、不混入规范发布门冒称整体已修好。

前五次开发夹具运行原文保留为native-run1～5.log：早期查询在rollout未materialized时错误、过早中断缺clientId、错误把内部failed receipt当持久failed等均已修正；不能用这些fixture红冒充产品缺陷。最终等待原生userMessage/起止时间后才断言，四个红均落目标断言而非协议异常。

规范 `scripts/run-build-tests.sh` 另列最终读数于 `VALIDATION.md`：系统Python首轮discovery缺四模块非产品回归，换锁依赖venv重跑。意向红测是独立显式入口，未登记到默认helper库存；后续实现获批后再转成常规回归并更新库存。初始产品hash与基线相同见 `raw/product-source-pins.json`。本步无产品变异，因为尚未引入待撤的修复保护；原生完整/缺失两控制格已验证证据门有效。

## 7. 改动面、风险与工期区间

后续最小产品改动预计在codexDriver（原生查询/分页/字段验证/历史绑定/恢复事件）、driver_adapter（只读对账调度与忙等/实际提交阶段分离）、inbox_lifecycle及driver账本（幂等证据合并与必要的最小持久索引/绑定历史）。是否必须schema迁移须先核旧数据可恢复性后单独冻结；本步未加迁移、配置键或生产接口。既有原生证据门不放宽。View输出补取只复用当前能力路径，Hub多thread路由不进入范围。

主要风险是误关联导致错误ACK、漏历史绑定或分页导致假阴性、秒级时间精度误判、晚事件与旧ACK代次竞争、恢复RPC影响正在运行的turn，以及把completed/failed/interrupted混为业务成功。需要后续加入重复clientId歧义、分页>200、nullable时间、错误thread/旧代次、冲突终态、进程重启及通知/响应乱序反测；本步原生终态是interrupted，不声称覆盖自然completed/failed全部版本行为。

工作量估计（不含业主排队/发布窗口）：两处实现及持久证据最小路径约2～4人日；若历史绑定需新迁移、旧版本协议兼容或daemon完整故障注入，约4～7人日；独立复核及回归另1～2人日。第一步本报告＋红测已完成，不把这个估计当实施授权。下一步由组长冻结接口/存储/预算后另派实现，之后才讨论正式版本替换补丁和上线验证。

## 8. 提交前裁定同步

2026-11-08T10:47Z已fetch并读远端rulings `fe847fc94a9903bbd64c9cac6df424556c43ec7e`，相对24985813仅rulings文档追加。〔51〕-补2记业主已定“不存在拒收并提醒、上限3层、第一期Codex并列会话”；组长明确在本第一步交件之后另派详细设计，再复核后派实现。故本步仍不做子Agent寻址设计/实现，不把已确认方向误报成尚未作答，也不自行接下一单。基线r54仍6e2c8364。
