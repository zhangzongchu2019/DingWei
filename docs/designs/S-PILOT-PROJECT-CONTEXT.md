# S-PILOT-PROJECT-CONTEXT（一页设计，待审）

基线：r32.1 `36afa6215f00bc39a992441ab0ff8fe81442aa3e`（VERSION 0.2.3.6）。本提交仅设计，不实施、不上线、不重启升级。采用组长方向 B：当前 Hub/Console/R9 为唯一来源，不恢复 datad。诊断前序以 `review/coder/pilot-p1-diag＝a33fcb7c48f8647c7ce91248ba42c1c8a530be1a` 为准。dw_inbox 沿已验收 R9，不另回移；试点 helper 升级提前通知业主，另走发布令。

## 1 接口与四 scope 的新契约

新增只读 `GET /api/agent/console/project-context?scope=tasks|sessions|config|all&limit=50&cursor=…`。保留 MCP 名 `project_context`、必填 `scope` 及四个枚举；可选 `limit`（1–100，默认 50）与不透明 `cursor`，拒未知参数及 agent_id/owner/group/spec_id 选择器，身份和范围只由服务端推导。`all` 由**一次 Hub 请求**聚合，不在 helper 分三次取。

| scope | 新语义与只读数据源 | 明确不包含 |
|---|---|---|
| tasks | 当前请求 **Agent ID** 是开发或审查、当前授权仍有效的 R9 任务；输出 id、角色、状态、版本、阻塞归属、spec/revision 引用与本人任务所需验收项；复用 R9 当前指派/组授权谓词 | 同 owner 的其他 Agent 任务、同组全部任务、旧 datad 台账、已改派/需改派/归档不可读项 |
| sessions | 当前 Agent 的实际编组成员集合与 Hub 在线连接快照求交；含本人及当前同组 active Agent，跨 owner 同组可见，跨组即使同 owner 也不可见；尊重 NoDirectory | 全平台名单、其他编组、离线历史、来源 IP/主机地址/邮箱/凭据/入组码 |
| config | 服务端显式白名单：context_schema、受限接口能力、分页上限、任务状态/确认规则；以及按 tasks 同样授权得到的所属 spec **任务上下文摘要**（spec_id、title、确切 revision、content_sha256、该任务引用的验收项） | env/secret/datad 配置表、任意数据库配置、其他人 spec 全文/评审、分享 capability、未确认修订；指派不自动授全 spec 读取权 |
| all | 上述三项，有界分页；config 的 spec 摘要与本页授权 tasks 对齐 | 不承诺跨 PG/SQLite/在线内存的全局同一时刻快照 |

响应为选中顶层键 `tasks`/`sessions`/`config`（all 三者都有），分页集合统一 `{items:[],next_cursor:…}`；另有 `meta={schema:hub-project-context-v1,source:current_hub,generated_at,observed_at,scope,complete,next_cursor}`。保留名称、调用参数兼容，**数据语义/值结构有版本变更**，不冒称旧 datad JSON 逐字段兼容。无任务或无编组是 200 空集合，且 config 不凭空提供 spec；`complete=false` 与 next_cursor 明示未完，禁止截断后冒充完整。稳定按任务 ID/Agent ID 排序去重，不给未授权数量。候选查询也须仓储层分页（每批至多 101），不复用一次加载全表的便利方法；授权过滤后空页但仍有候选时给继续游标，不能误报结束。

游标为服务端签名的不透明 keyset 状态，绑定 actor、scope、limit、当前授权集合摘要，最长 5 分钟；all 中分别保存 tasks/sessions 位置与结束标记，config 仅引用当前任务页。续页重新鉴权并查询现状；成员/指派集合漂移回 409 `context_changed`，不输出旧缓存。非法游标 400、跨身份 403。线上状态只声明采样时间，不保证多页在线集合不变。响应总上限 256KiB；超过上限在完整条目边界缩页并给游标；单项无法容纳则 413，不能静默裁剪验收标准/正文。

## 2 鉴权、事务与隔离

helper 仅用 enroll 持有的专属 Agent 凭据，借 `_connection()` 的当前受信 Hub origin，从 ws/wss 转 http/https；无专属身份即 `dedicated_agent_credential_required`。凭据只在 helper HTTP Header，不入 MCP 参数、响应、游标或日志；拒重定向，不从调用参数接 URL/headers。Hub 仅认单一 `Authorization: Bearer`；拒人类 cookie（含 bearer/cookie 混用）、旧 service key、伪造身份头、重复 Authorization。每次重核 credential_hash、Agent active 与 owner active；不能靠 owner/admin 角色扩读。

现有 `RequireAgentTaskFlow` 仅作认证模式参考，不将“已认证”当作已核所有权限。新增 identity repository 的有界上下文授权 callback：先取候选组/任务，按既有 `WithTaskAuthorization` 锁序锁 group（排序）→agents（排序）→users（排序），同事务再次核 actor 凭据 hash、各组真实成员与可见 peers；凭据/state 在 np_agent 同行，锁覆盖吊销/轮换。PG 使用普通事务以允许 FOR SHARE，不能在 READ ONLY 事务下求锁失败后无锁回退。无权限的候选丢弃，PG/存储异常则整次 503，无部分正文。

PG 授权锁保持期间，在**一个 SQLite 只读事务快照**重取 R9 当前角色/assignment_version/needs_reassignment 与 spec 确切修订；不得从首次候选查询直接返回，也不得事务外逐项补私有投影。延用 R9 对组、spec owner 与开发/审查对的有效性检查；不能仅检查请求 Agent 是否尚在某组。再取带时间的在线快照、完成 PG 存活/授权重核，提交读事务后才输出；任一步失败丢弃响应缓冲。若候选版本已变，409 要求重读。该查询不调用 R9 `invalidateTaskIfDenied` 或修改任务/通知，失权清理由既有 reconciliation 处理。

并发口径：撤权/移组/改派先完成时，不返回旧任务或原组名册；读授权与 SQLite 读快照先取得时，可返回该读取时点获准的完整快照，随后请求必须重核拒绝或过滤。测试用数据库屏障控制两种顺序。跨库没有两阶段提交，不声称全局原子快照；连接丢失/PG 提交失败整次 503。`all` 任一组件失败都失败关闭，不把其余组件作为成功返回。GET 不写业务/已读/通知状态；HTTP 日志只记 actor、scope、结果码/条数，不记正文、游标、URL 参数原文或凭据；Cache-Control:no-store。

## 3 helper/MCP 与旧端处理

新增本机 `project_context_v1` IPC，包含 scope/limit/cursor，固定路由至上述接口；传输受限 socket 策略仍生效，upload_only/send_body_only 不开放此能力。新 helper 的新 project_context 路径**忽略 SH_DATAD_URL、DATAD_URL、SH_DATAD_TOKEN、DATAD_TOKEN**，如存在只按键名一次告警；无旧服务回退。旧 shim 若向新 helper 的 vsapi_v1 发这三个历史 context GET 路径，明确 `upgrade_required`，不把它偷偷转成其他身份/语义；其他历史工具（如 heartbeat_report）不在本件重设计范围。

| 组合 | 结果 |
|---|---|
| 新 shim＋新 helper＋新 Hub | 新语义；原调用只带 scope 仍可用 |
| 旧 helper（含现有 r32.1 0.2.3.6） | 其 project_context 仍是旧 datad 管道，不承诺自行恢复；升级新 shim 却留旧 helper 时拒绝/提示成套升级，禁止回落旧 datad |
| 新 helper＋旧 Hub | 404/426 映射 `project_context_unsupported`/升级提示，不返回假空、不回落旧数据 |
| 无专属凭据、停用 Agent/owner | 明拒，不能借旧账号 key 或 datad token 绕过 |

HTTP 读超时 10 秒、IPC helper 截止 15 秒、shim 20 秒，单次请求不自动重试；超限/坏 JSON/非预期响应 schema 拒绝，响应体有界。拟下一兼容功能版本 0.2.3.7（实现前核版本占用；VERSION、helper、shim/Hub 版本门同步），A/B 不变；用 `project_context_v1` capability 表明新语义，不能仅从 helper 版本猜 Hub 已上线。本件无 schema/迁移账本变更；只读现有 PG 身份/组、SQLite R9/spec 表。旧二进制仍可读原库；回滚后新 context 能力消失，应明示 unsupported，不把旧工具有名等同已恢复。

## 4 实现后测试与撤修计划（本提交未运行）

- C1 鉴权矩阵：专属 Agent 正例；cookie/混凭据/重复头/旧 key/伪造 actor/停用 owner 与 Agent/轮换旧凭据均拒；撤鉴权指定红。
- C2 两组＋跨 owner 同组＋同 owner 异组；名册与任务不串读、NoDirectory 生效；移除组谓词与 NoDirectory 各指定红。
- C3 当前指派、改派/移组/吊销与分页并发两提交顺序（真 PG＋SQLite 屏障）；状态读取异常、needs_reassignment、已归档拒；撤当前角色重核/PG 锁/版本检查各指定红。
- C4 spec 只投影任务引用的确定修订/验收项，其他 spec/未确认修订/全文不泄露；撤关联和字段白名单各指定红。
- C5 PG/SQLite/在线源错误、PG 中途断连、投影失败：all 无部分数据，业务表/通知/processed/已读不变；撤失败关闭与 no-write 各指定红。
- C6 四 scope 原参数、空集合、默认/上限/无效 limit、重复与缺项、过期/跨 actor/变更游标、分页末页及大响应；撤游标绑定/上限/完整性标志各指定红。
- C7 HTTP→新 IPC→真实 shim MCP 生产接线；旧键存在仍零 datad 出站、旧 Hub/旧 helper/混装明确拒；撤无回退/凭据不重定向/隔离模式门各指定红。
- C8 相邻行为：R9 `dw_task_list` 与本 API 同权限谓词；dw_inbox driver 本地已读及自动投递/processed ACK、desktop 路径、spec 创建/修改兼容不变。检查两条现有任务列表路径无新旁路。
- 交件门：全仓 go test ./... 默认并行、vet（既有 copylocks 按登记限域，不伪造全绿）、canonical、定向 race/真 PG 并发、helper 全套隔离 HOME＋锁定 venv、MCP 兼容、输入 manifest 与撤修指定红。浏览器如仅接口变更给可复核输入等价证明；若改页面则跑受影响门。所有失败首轮原样保留。

r32.1 driver 收件箱代码/用例逐项确认见 [附件](S-PILOT-PROJECT-CONTEXT-INBOX-PROOF.md)。设计 PASS 只允许后续另令实现；不构成升级试点席位或部署许可。
