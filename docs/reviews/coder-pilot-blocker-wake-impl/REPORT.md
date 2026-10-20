# D2 阻塞自动提醒／一次重核候选

状态：作者候选已完成本地验证，交组长审＋prodops 交叉核。依据第 85 条授权；轻量设计 7b1e3033。评审是组长＋prodops，不送 togpt60。没有部署、席位升级、真实任务或外部消息动作。

分支 `review/coder/pilot-blocker-wake-impl`；D2 比较基线是第 1 件 project_context 候选 `9dc27ecbee668dac9706325e0e52b95fe8fba20b`。运行源 `43af5b9f891f336c4776657f40d2359901843083`；完整可复跑源码/撤修锚 `5c368986c0590a361f090a277a955fd8711ab436`，tree `a9f7d047dd8586beaaab0d9a822fc3de2a9ecd12`。两者只差独立撤修器的 SQL 定位，1767 项其他非 docs 输入 blob 一致。其后交件只加本件文档和证据，不改程序、测试或依赖。

实现入口 [说明](../../designs/S-PILOT-BLOCKER-WAKE-IMPLEMENTED.md)。PM 从原任务看卡、答复/转派、看重核，不操作事件/租约字段。明确登记才纳入，未配置所等人显示待分派，不全算 PM。真实生产/安全标记升完整档，需所有者补批准记录；程序不根据自由文本猜风险。

关键结果：答复、新材料或已验证依赖状态变化只产生 D2 唯一输入与 outbox；无新输入不重复。当前执行 Agent 取得版本租约后重核材料、环境、授权，身份/修订/依赖由服务端重核；全部通过也不自动 resume/start/complete。旧 task-driver 和 owner-plan 不可绕门。真实业务写、配置、发布与最终签收均未授权。

身份隔离：PG 当前组、Agent、用户锁持续到 SQLite 提交；明确所等人只有该卡输入权，无整任务/spec 读权，回执含当前权限裁剪，改派后旧缓存回执也不能保留原读权。答复 source 由鉴权传输设置。改派/修订使未解决卡需重新绑定，不把历史 resolved/cancelled 卡批量重开。

可靠性：SQLite 同事务提交卡/输入/任务版本/审计/outbox；HTTP 稳定 request_id 和 source/event 去重。helper 先存会话本地 journal，再入命令队列；pending 可恢复，dispatching/admitted 只查原协议 receipt，不重发 deliver。未知 RPC 窗口停 unknown，不承诺可证明的物理恰一次，也不靠新 ID 重开执行。固定内置提示和受限 metadata 不含答复正文、附件或普通消息附加动作。旧 helper 保留待升级；非 driver 只收通知。

## 验证读数

| 门 | 正式结果与限域 |
|---|---|
| Go 全仓 | gates-release/go.json：43af5b9 不可变导出，rc0；65 包＝52 测试包通过＋13 无测试文件包，30 项可选环境条件跳过逐项保留 GO-PACKAGES.json；UTC 2026-10-20T17:10:21.227254+00:00 至 2026-10-20T17:22:47.645096+00:00 |
| vet／canonical／race | gates-release：同一 43af5b9 不可变导出三门均 rc0；canonical python=53 shell=2，含实际 MCP→IPC→HTTP 本地闭环与真实 PG |
| helper | HELPER-FINAL.json：332d77d 源，隔离 HOME、env 清空、锁定 venv，765 项 rc0；205 个 helper 输入 blob 与最终源逐字相同，不重跑相同输入冒充新独立证据 |
| 撤修 | withdrawals-complete：5c36898，40 格全部指定断言红；基线及恢复 Go、helper 均 rc0，详 WITHDRAWALS.md |
| 浏览器 | 新阻塞看板 11/0；原任务看板 48 断言；R9 11/0、hosted 32/0、console 20/0、enroll 20/0、vega 12/0；R8 最终 13/0。输入等价见 INPUT-EQUIVALENCE.json |
| 旧库兼容 | sqlite-compat-release：实际旧 r32.1 store Migrate＋任务读取保留新库阻塞/输入/outbox/0058 账本；只限启动/读，不是旧整站写兼容 |
| 迁移回滚 | 真 SQLite 坏 0058 整事务回滚，无新表/账本残留，原任务保留；正确迁移可重施，单测在最终全仓内 |

环境为本机 VM-4-12-ubuntu，UID1001，16 核，Go1.22.2，Python 锁定 venv `/data/ai-dev-data/helper-deps-lock-venv`。依赖离线；PG 为严格临时 embedded PG，未设置外部 DSN，HOME 只接既有 PG 二进制缓存，不复制在线状态或凭据。真实协议测试均 loopback 本地假服务，不是生产、43、dw123 或 dataConnector 验收。

## 首败与证据边界

1. 首轮 helper 765 项 rc1 中 19 个子失败均是共享 MCP fixture 仍断言 13 个工具，而新清单为 17；canonical 同处 rc1。只修测试清单，第二轮隔离 HOME 765 项和 canonical 通过。原始日志不改写。
2. R8 浏览器首轮 Agent 选项未就绪即计数失败。基线 r32.1 与候选相同页面/旧脚本的后续 A/B 均 13/0，不能据此宣称基线复现红。静态页面先显面板再异步加载选项，推断为等待条件缺口；测试追加等待 options 非空，保留精确数量断言，生产 R8 页面未改，最终 13/0。
3. 工作区门过程中自查发现旧缓存回执未按改派后的读权限裁剪，已以 4737bfa 修复并补独立撤修。早期工作区全仓读数均保留，正式门改用 43af5b9 不可变 Git 导出，避免后续文档工作影响输入。
4. 另发现 adopt 只更新响应、未将修订三元组持久化，已修复并增加提交后重读与同卡去重回归；修订/所等人变化同时更新卡 fingerprint。运行源 43af5b9 包含这些修复，前序门均保留。新增修订撤修初次误改指纹函数，红在另一断言，被检查器拒收；仅修独立撤修器定位后完整重跑，未把该初红冒称通过。
5. 只有依赖完成状态支持有限自动条件 adapter；资源、环境、授权等需真实证据重核。不实现完整稿附加六阶段 typed gate 或无授权外部回调。运行时依赖锁与镜像未改，helper/MCP 延续本批 0.2.3.7。
6. 旧二进制可启动/读新库，却不懂 D2 写门；启用后回退须冻结已纳入任务的写入/唤醒 worker，使用兼容回滚方案，不删阻塞历史或迁移账本。
7. HTTP 接收、可靠投递、模型认领、重核、恢复与人类签收分别记录，不互相替代；本报告作者读数待组长审和 prodops 独立交叉核。

源码清单 SOURCE-MANIFEST.json 共 1768 项。证据清单 EVIDENCE-MANIFEST.json 与 Git blob 全量核对见 FINAL-VERIFICATION.json（该验证回执自身不作递归清单成员）。原始失败与日志保持原样；源码、排除原始日志的说明差分均 rc0；全差分 rc2，原始日志空白不改写，见 QUALITY.json。仓库默认忽略 .log，所有本件日志均显式 force-add 入 Git，非仅本地留存。
