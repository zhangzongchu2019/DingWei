# D2 轻量阻塞卡实现与运行边界

依据第 85 条授权和轻量设计 `7b1e3033`。本件堆叠在 project_context 候选 `9dc27ecbee668dac9706325e0e52b95fe8fba20b` 上；单独审 D2 时从该父锚比较。仅交可审实现，不上线、不升级任何席位、不改真实任务。

PM 三步是「看卡 → 答复或转交 → 看重核结果」。任务看板原地展开阻塞卡，系统带当前任务、spec 修订、指派和版本；PM 不填写内部事件版本或租约。只填卡点、所等人、所等材料，可未分派但明确显示待分派。生产或安全面勾选即完整档，所有者须补明确核对记录；本件不根据不可信文本猜测生产环境。环境或资源状态不能机器证明时必须由重核人记录真实材料，unknown 继续等待。

## API 与身份

Agent 与 Console 分别使用 `/api/agent/console/tasks/{taskID}/blockers`、`/api/console/tasks/{taskID}/blockers`，各有集合及 `/{blockerID}` GET/POST。POST 的 operation 为 create/input/assign_waiter/approve_full/begin_recheck/finish_recheck/cancel/adopt；MCP 暴露 list/set/input/recheck 四工具，管理动作由人类 Console 发起。Agent 只认单一专属 bearer，拒 Cookie 混用；Console 只认人类会话，拒 Authorization。工具不接受身份、凭据、URL 或命令。

当前开发 Agent 或 spec 所有者可登记；所有者登记明确记录 user 身份，不冒充执行者。当前开发/审查与所有者可读；明确所等人只可提交该卡答复，回执隐藏任务上下文及 task_version，不获得整任务/spec 读取权。其他审查人不能冒充所等人回答。答复 source 是服务端已鉴权主体；外部联系人由所有者登记证据引用，不能按外部名字冒充平台身份。

PG 按组、Agent、用户稳定顺序取锁并核实际 active 身份、当前开发/审查组关系；SQL 事务不是只读。SQLite 在同一事务复核 R9 状态、当前修订/SHA、卡版本、事件与请求去重并写审计/outbox；提交前检查 PG 事务仍有效。外部所等人不必有组权限，但当前任务双方仍须有效且该人须被明确指向。真实 PG 测试覆盖答复先提交/停用先提交，不跨库两阶段提交。

## 唯一输入与重核

未采用本功能的任务不自动迁移；显式登记才纳入阻塞门。现存 blocked 文本与新卡不同则保留另一张 needs_owner 卡，不能清掉旧原因。任何未解决卡阻止 R9 start/resume/complete 与旧 task-driver/owner-plan 绕路；即使卡全部解决，所有者的旧 plan 接口也不能替代执行 Agent 发起状态转换。

答复 `(blocker,source,event_id)` 相同重放无新输入，内容不同拒绝；`(actor,request_id)` 绑定完整请求及目标，未知结果用原 ID 查/重试，不能盲换 ID。每一实际新输入有唯一 `(blocker,generation,input_version,assignment_version,recipient)` 唤醒信号；同一事务写输入、卡、任务版本、审计、outbox。仅依赖完成门有有限自动 adapter：指定依赖实际状态 false→true 产生新输入；在线心跳和状态不变不产生事件。没有任意表达式、网络探测、自动资源/授权判断。

当前开发 Agent 以卡 CAS 和 5 分钟租约认领重核。身份/修订/依赖由服务端复核；材料、环境、授权三项各需 pass/fail/unknown 和证据。全过才 resolved，仍不 resume/start/complete；未知或失败回 waiting，无新输入不再循环。重核间新输入使旧核对无法吞掉新版本。改派/修订使未解决卡待所有者重新绑定并清旧租约及完整档批准；resolved/cancelled 保留历史，不重开已结束卡。后续执行仍走当前 R9 身份、依赖和用例门。STOCK 未知提交不能由答复或唤醒自动重派业务写、换 key、清 pending 或签收。

## 通知、driver 恢复与旧端

Hub 既有 5 秒任务 worker 扫描耐久 outbox，离线或不支持的新类型留待投递；分页不被前 100 个离线项挡住。只发固定安全摘要和对象 ID/版本，不发答复材料或附件。kind 为 task_blocker_ready，只有 Hub 私有控制入口可投递；能力 blocker_notice_v1 + delivery_ack_v1 才发送，driver 额外宣告 blocker_wake_v1。旧 helper 不收到未知控制指令；新非 driver/受限模式仅存通知并 processed，不注入 CLI。

driver 在实际空闲投递边界再次从 Hub 取当前授权卡，旧版本/失权信号取消。固定提示只请求一次恢复条件重核，不把答复当指令，且不附加普通消息的上传/指南动作。helper 会把同 signal_id 原 delivery_id 先存会话本地 journal，再入内存命令队列；pending 重启可恢复，调用协议 deliver 前耐久写 dispatching。dispatching/admitted 重启只查询既有协议 receipt，不再次 deliver；未知窗口标 unknown 并停，不宣称跨未知 RPC 可证明恰一次，也不以回执未知重新发起业务轮次。临时只读失败保留同命令/seq 等待，不生成新输入。

HTTP 已到件、耐久投递、模型认领、重核、恢复任务是不同事实。看板对离线/旧端显示待在线或升级；已可靠投递仍显示待重核，不假装已恢复。

## 迁移与回滚

SQLite schema_migrations 新增 `0058_pilot_task_blockers.sql`，六张附加表；PG 没有 schema 变更。0057 保留设计编号空位，D1 集成时重新核编号冲突。本件不实现完整稿附加的六阶段 typed gate，也不伪称 R9 已有六个状态。helper/MCP 延续本批 0.2.3.7，运行时依赖锁和镜像未改，无新第三方运行时依赖。

坏迁移在真实 SQLite 测试中不留表/账本，原任务保留，可重新应用。兼容脚本用 Git 精确导出旧 r32.1 与本候选代码，验证旧 store 的 Migrate 和任务读取保留新表数据/账本；这不是旧整站写兼容或部署授权。启用后旧二进制不知道新阻塞门，回退须先冻结已纳入任务写入与唤醒 worker，使用兼容版本或另经所有者确认的方案；不得删表/删账本求回退。

验证入口：`tests/pilot-blocker-wake/withdrawals.py`、`sqlite_compat.py`、`local_roundtrip.py`；Go Store/HTTP/真实 PG/可靠 WS、真实 MCP stdio→helper IPC→本地 Hub HTTP、隔离 HOME helper、看板浏览器和全仓门。证据与初败限域见审查报告。

## 第85条附注闭合

- 路由可靠入队后、outbox标记前若进程失败，原先重投会生成另一delivery ID；现只对Hub专属blocker信封，以目标和signal ID的SHA256构造稳定delivery ID。实际SQLite INSERT OR IGNORE幂等，进程重启丢内存claim也不产生第二条可靠投递；仍是至少一次传输，helper沿用delivery/signal持久去重，不宣称物理exactly-once。
- 移除canInput中重复canRead分支，保留独立的仅owner或当前waiting principal答复约束。
- 同一blocker＋已认证actor：累计256条、滚动60秒内10条新input。事务内计数/写入，越限HTTP429、无部分写；幂等重放先判定，不消耗额度。所有者也受限，依赖状态等系统输入不冒充真人答复；需更多真人材料时所有者另建明确卡，禁止自动拆卡规避上限。
- 当前被等方按精确blocker ID可读where/expected_input/kind/ID/version/state，不读spec正文、修订、任务角色、恢复检查或其他人答复历史；列表枚举仍403，task_version隐藏为0。改派/停用仍经PG及SQLite重核。
- 0057仅为此前设计预留，未发布迁移；实际任务阻塞从0058追加，schema_migrations按已提交文件顺序运行，不插空壳0057、不重编号既有迁移。前序0056→0058为有意跳号。
- 回滚冻结写入已列docs/runbooks/PILOT-PREDEPLOY-ROLLBACK.md执行单，须另获部署窗口授权，本候选不执行。
