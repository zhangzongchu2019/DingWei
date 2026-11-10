# 第一期 M2：helper 分道实现

基线 M1 `eae9eca1ac67bf580f4b0f8dccca5f92d7e62a88`。本次为 `S-SUBAGENT-P1-M2`，使用 r57 版本线；M1 已获〔51〕-补12（43c312e8）认可，不并入 r56。只改 helper，不部署、不修改在线配置、不启动真实推理。规格映射 AA004、006、007、008、009，冻结默认和 r55 不变量保持。

## 连接与 broker

`SH_SUB_AGENTS_V1=1` 显式开启，默认关闭；仅 Codex DriverAdapter 支持。专用席位鉴权后才宣告能力，连接能力、树 ACK 和发信回执均绑定当前 WS。重连先失去 ready，完整快照获得本连接 ACK 后才能接子信，失败不回退主席。Hub 的 conn_seq 由鉴权连接给出；本地不猜、不继承。

`app_server_broker.py` 独占一个底层 transport；底层负责唯一请求 ID 与 response map，初始化只一次。broker 公平调度有界分道队列，控制帧保留短优先通道。物理写和响应等待分离，等待 A 响应不挡 B 写；入场计时在物理提交回调才开始。Facade 校验自身 immutable thread 绑定；server request 按 method+id 区别响应，即使双方 id 一样也不混用。外层 params.threadId 是唯一事件归属；缺失或未知 thread 的事件只保留有界隔离元数据，不猜父。底层及每道事件队列也有界，溢出不伪造终态。

每节点独立 DriverAdapter、FIFO、CodexProtocolDriver。Facade.stop 只解除自身订阅，关闭 A 不杀共享进程；只有席级 broker 在实际失活/写阻塞时恢复。恢复保留绑定、增加物理 epoch，过期工具响应不能写入新连接。重启/恢复不重投 unknown。r55 的进程内及文件级对账互斥继续复用相同 driver 账本路径；共享 200 条/15s 席级预算，原来的每 thread 15s、1s时间容差、分页与混精度保护继续运行。只读对账不创建/恢复 native thread。

## 持久化与生命周期

`sub_agent_store.py` 为 helper inbox SQLite 添加 `helper_sub_agent_tree`、`helper_sub_agent`、`helper_sub_agent_alias`、`helper_sub_agent_return`、`helper_sub_agent_native_tools`、`helper_sub_agent_tool_send`、`helper_sub_agent_delivery_scope`。既有 inbox、driver ledger 只加列及索引，旧数据默认主席作用域；不 DROP 表。所有旧 inbox 查询仍用 session_name，其内部存储值对子节点编码 root+lane_id+generation；这不是新 Agent 身份或对外地址。行中也保存显式 lane_id/generation。driver 的 get、get_by_envelope、枚举、更新、reconcile CAS 都显式限制节点与代次；回调再匹配 lane/generation 和原 dispatch/ACK generation。M1现有确认帧不回显节点字段，故收信同事务保存不可变投递作用域sidecar；确认按Hub delivery_id唯一匹配该sidecar后进入scoped inbox，歧义拒绝，禁止从账本LIMIT 1猜兄弟。

创建先持久 intent，再 CAS 到 submitted，才发一次 thread/start。重复 request_id 必须策略和别名一致；submitted 无结果在崩溃后保留 creation_unknown，绝不再建第二条 native thread、也不从 thread/list 猜归属。创建 RPC 的等待不持有管理锁，不能挡住父关闭或生命周期封口；响应回来时节点若已 draining/ended，只登记原生绑定和墓碑，不复活。绑定后预备 active 快照，但公开 registered=false，接信 guard 仍等对应 revision ACK；只有已 ACK 且 active 的节点可接信。默认名字用原生 UUID 无连字符的随机末8位，冲突依次加至10/12…32位；别名与墓碑永久保留，完整 UUID 冲突拒绝。

五参数显式解析并持久：完成即退开、idle3600s、max172800s、每节点 cap432000s、grace600s。idle/max 允许显式 null，平台 cap 和 grace 保持上界；不把0解释成禁用。显式 task_completed 才触发完成策略，turn/completed 只结算投递。排队、执行、待工具和不确定工作不算 idle；重启时直接查持久账本，不能等 adapter 恢复后才发现旧工作。绝对寿命与原 created_at 累计，离线和重启不清零。父结束级联，运输断线不等于父结束。

封口在同一个 SQLite 写事务先置 draining，再按每封信判定。物理提交前先持久 native_dispatch_at；封口竞争后未提交的写者再检查节点，禁止写出。无提交标记且无入场证据的信生成逐条退信 outbox 和真实 failed ACK；可能已写的保留 admission_unknown，不生成 processed。退信只带原 delivery/node/generation 和来源，不复制业务正文；每条记录独立追踪返回、已知失败或不确定结果。发信前持久 uncertain，崩溃后不自动重发。超过 grace 只中断/退订该节点自己的 thread，然后 ended；不 kill 共享 app-server。

## 原生工具来源与告知

只为新 thread 安装 dynamicTools：dingwei_self_context、sub_agent_control、dingwei_send_message。控制创建限主席，孩子只能关闭/完成自己；严格拒绝未知参数、兄弟目标和后代创建。发信来源从原生 server request 的外层 threadId，经 facade 的持久 node/generation 与当前 turn 双重绑定生成。arguments 只有 to/body/reply_policy；完整目标保留子路径并补既有#key传输封装；不接受参数、环境变量或 IPC 自报 from。工具发信在网络发送前持久占用 thread/turn/callId 和节点代次；重复调用返回已记回执或明确 unknown，崩溃不重发。共享 send.py/IPC 继续主席入口，未增加任何子来源选择开关。

钉版0.159.2 的真实动态工具调用已用独立 app-server 和 loopback 合成 SSE 验证，不涉及真实模型请求或凭据。该版 thread/start 支持 dynamicTools，thread/resume 没有补装字段：历史主 thread 若创建时未安装工具，不能声称重连后获得工具。helper 保存工具安装记录并标记 `sub_agent_existing_root_tools_unavailable`；不更换现有主 thread，不伪造来源。新建孩子始终在 thread/start 安装工具。历史主 thread 的控制入口需要运营方显式建立带工具的新主 thread；本单不执行迁移。此限制随交件明确报告，不把 schema 支持误报成已验证所有旧主 thread。

创建时先给独立 developerInstructions 说明受控身份与上下文查询；UUID 返回后的完整名、父、直接孩子、已解析策略和 revision 在首个正常业务 turn 的独立平台输入项提供。树变化只在下一正常 turn 更新，原业务正文独立且字节与 body_hash 不变；无业务时不为上下文额外启动推理。

## 回退与验证边界

M3 负责 View/Console、混部与真实回退演练，本单不扩大至这几项。计划回退时先由仍支持该协议的 helper 显式关闭父/子，完成逐条退信与 unknown 审计，并上报 ended 快照获得 ACK；然后关闭 helper 及开关。不能把 unknown 改 processed 以清空队列。保留加法表、墓碑、原账本和哈希，不 DROP、不把子记录导入主席。旧 Hub/旧 helper 或能力未就绪继续走 M1 的拒收保护。

复核入口：`tools/sessionhelper/tests/test_sub_agent_helper.py`；`tests/subagent-p1-m2/mutations.py` 私有副本逐刀；`tests/subagent-p1-m2/native_tools.py` 锁版原生工具探针。所有验证隔离 HOME、短 TMPDIR，剥离 SH_/DINGWEI_/DW_/DATAD_；Go uid1001。失败首跑、最终读数与源码哈希在交审报告保留。
