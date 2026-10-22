# S-HELPER-STALE-THREAD-FIX 交审

作者 coder56solzzc.zzc。基 r34 c61866cd60ac196cc45f0d3c004e0c5e436a2f9d；冻结源 70358a01e2002ff05e001ce5ff702c238c80f984，tree 108467778b59d5ba6eec23872cdee93b2a1851d1。源码／回归输入 evidence 标签：evidence/coder/helper-stale-thread-fix-10846777。最终候选为本源之上的 docs-only 证据后继，精确坐标随交件信提供。

落实组长批准的诊断第4节1、2、3、5。没有实现历史正文补读，没有修改任何席位或生产配置，没有部署。oaagent 现场根因仍未独立证实。本件不声称其历史 thread 一定可以恢复。

## 行为与设计

入口设计：docs/designs/S-HELPER-STALE-THREAD-FIX.md。

- `codexDriver.py`：initialize 成功增加连接代次，准入缓存同时绑定 RPC 对象、代次、活动 thread。下一次新投递在重连锁内检查原 thread，并持锁至 turn/start，避免验证后换连接。
- read 为 notLoaded 才尝试 resume；同 ID、同 cwd、idle／active 且 canAcceptDirectInput 为真才准入。恢复失败保持旧绑定、账本和上下文，不 thread/start 替代、不重投。原本未绑定的首次会话仍沿用初始 thread/start；不从历史回执猜活动绑定。
- `JsonRpcError` 保存方法、整数错误码和消息供内部分类；对外不输出原始 error.message／data。两个 RPC 客户端都保留协议错误；真实 reader EOF 唤醒在途请求为断连，发送失败清掉 pending，不能混作普通响应超时。
- 历史回执命中不改两个活动 thread 别名或验证缓存。历史 poll、interrupt 确认、状态更新都寻址记录自己的 thread；已经有 turn 的失败投递也不重新提交。
- 派发前先将未知状态落账本，防止受理后响应丢失／进程退出导致再次提交。明确 missing-thread／鉴权拒绝与未知结果分开，未知／内部 RPC 错误也不冒称“未受理”。

| 场景 | 本地命令回执 | 持久账本 | 事件 reason |
|---|---|---|---|
| 原 thread 验证／恢复失败，尚未发 turn/start | failed | failed，保留 thread／envelope | session_unavailable |
| turn/start 明确 missing-thread／鉴权拒绝 | failed | failed | session_unavailable |
| 派发后超时／EOF／未知 RPC 错误／成功响应缺 turn ID | failed（结束本地队列） | unknown，不自动重发 | model_admission_unknown |
| 正常得到 turn ID | accepted／processing，沿用终态机制 | 原有 accepted／processing 路径 | 沿用原事件 |

第三行的 failed 只结束本地排队命令，不声称模型没有执行：它可能仍在运行，须按 unknown 对账。相同 envelope 重送或 helper 重启不能把它变成第二次 turn/start。原消息 delivered／injected／processed ACK 的时点和语义没有改动。

`DriverAdapter` 在未建立 turn 时发带 delivery_id 的结构化失败；两类准入原因可区分。实际 SessionHelper.session_event_loop 的本地假 WS 回归证明它仍经 session_event 转发，不变成 delivery_ack。

`renderer-canary-sse.html` 只在对应输入卡附注“模型未接收：会话不可用”或“模型接收结果未知，请核实后再处理”。保持输入卡已送达状态，不制造 assistant_text、空回合卡或新错误卡，不显示原始私密错误。支持失败先于输入、dw- 前缀归一、重复事件；未匹配缓存最多1000项，reset 清空。截图 raw/final/model-failure.png 为本地夹具，不是线上页面。

## 锁定版本真实协议验证

本组锁文件 deploy/ops/hosted-image/codex-package-lock.json 指定 0.159.2-linux-x64。按该件下载官方平台包，archive SHA256 84a6b35fb45bdcb94cef9fcb329438045a8911f53876cc9a9a7e6f9bb1382eba，162475310字节；提取二进制 SHA256 1748767b230ebfc3d4ab7e4e254920d0c0ad9691fd8c11f190e7d44511a4a92e，版本输出 codex-cli 0.159.2。没有用当前 PATH 的0.163.0替代锁定版证据。

重现入口 `python3 tests/helper-stale-thread/real_protocol.py <上述精确二进制> <新的私有输出目录>`。脚本还硬比二进制 SHA。四个 app-server 均为本次自建 stdio 子进程，私有 HOME／CODEX_HOME、无认证文件／继承凭据，模型提供者仅指127.0.0.1:9，不连接真实模型服务或生产席位；没有 daemon 管理命令。一次本地 turn 用于写出 rollout，不把模型完成作为门。

冻结源实测原始件在 protocol-final/：

| 操作 | 实际结果 |
|---|---|
| 私有 app-server 重启后 read 已持久 thread | 同 ID，status=notLoaded，canAcceptDirectInput=null |
| resume，excludeTurns=true | 同 ID、同 cwd、idle、canAcceptDirectInput=true |
| 对临时／不存在 thread 调 read | code=-32600，消息精确为 thread not loaded 加该 ID |
| 对上述 thread 调 resume | code=-32600，消息精确为 no rollout found for thread id 加该 ID |
| 对上述 thread 调 turn/start | code=-32600，消息精确为 thread not found 加该 ID |
| 真实 helper driver＋真实自建 stdio transport，持久 ID | read→resume→turns/list→turn/start，回 processing，无 thread/start |
| 同样接线但缺失 ID | read→resume 后 session_unavailable；无 turn/start／thread/start |

分类同时比方法、错误码类型／值、准确 ID，不按 -32600 或宽泛子串猜测。未知协议形态失败关闭。两个客户端的错误解析与 EOF 有假输入回归；真实探测只覆盖自建 stdio，不声称真实 daemon_ws／proxy 生命周期或现场账号历史已联测。

官方说明仅作辅助链接：[Codex app-server](https://learn.chatgpt.com/docs/app-server)；本件实际方法、参数和错误形态以上述锁定版实测为准，不拿当前文档替代版本证明。

## 验证

以下均为作者本机读数，非独立交叉核；prodops 另审。正式门均来自不可变 detached 检出70358a0；raw/final/ 是正式读数，raw/ 根目录与 preliminary/ 是前序记录。

| 门 | 结果 |
|---|---|
| 冻结源23项定向回归 | rc0 |
| 22项撤修／恢复 | 全部指定断言红，全部恢复绿 |
| helper 全套，隔离 HOME、env -i、锁定 venv | 810项，rc0，无跳过 |
| canonical，隔离 HOME、离线缓存 | BUILD_TESTS_OK python=57 shell=2，rc0 |
| go vet ./... | rc0 |
| 全仓 Go（-json -count=1 -timeout=30m） | rc0；53测试包通过，13包无测试；38条条件跳过事件，逐项见 GO-SUMMARY.json（不宣称0 SKIP） |
| 真实协议与 driver 两路径 | LOCAL_REAL_PROTOCOL_PASS |
| View真实浏览器4套＋静态2项 | 均rc0，见下 |

22项撤修覆盖验证／恢复缺失、错误绑定放行、缺失时新建、历史绑定污染／读写错位、未知准入落库与重投、派发前账本、已受理失败重投、未知 RPC 错判未受理、错误形态丢失、missing分类、EOF误作超时、私密错误泄漏、结构化失败、ACK改动、View不可见／乱序／ACK覆盖／缓存无界。检查器从异常对象取实际 AssertionError 文本；subTest 单独取断言，不能因为 traceback 含测试源码标记就报“指定红”。原始撤修、恢复日志与 rc 在 withdrawals-final/，CHECKS.json逐项记录。运行：用锁定venv执行 tests/helper-stale-thread/check.py，NODE_PATH 指已装的本地 Playwright，PW_CHANNEL=chrome；不下载依赖。

真实浏览器：model-admission（15项）、failcard、t074 delivery ID、replay-seq；静态：t086 deferred-clear、waiting-stage。原 t086 从仓库根目录运行因相对路径 ENOENT rc1，原样保留；改在 frozen/view-service 目录执行，rc0。未改该脚本。浏览器回归只接本地夹具，非生产冒烟。

前序失败如实保留：首次非冻结 helper 806项中2败（旧错误文本断言、guide 假RPC缺 thread/read）；补去敏错误字段断言及真实形状的只读响应后定向通过。首版撤修器漏收 subTest，指定红标志为false，修检查器后完整重跑；未把原轮记PASS。初次真实协议脚本错误地等本地失败模型turn完成，未过该门；改为等生成rollout再重启，最终两类方法及真实driver验证通过；原轮只有stderr，完整transcript未留存，明确标注。

## 兼容、回滚与发布边界

- 账本 schema、Hub协议、接口授权、runtime requirements锁和 VERSION 均未改；VERSION仍0.2.3.8，发布号由组长集成时另定。
- 结构化出站仍需要现有 SH_EMIT_SESSION_EVENT=1。本件不擅改席位开关；只升级helper而不更新renderer时不能承诺出现新增模型状态附注。实际发布需包含配套renderer，批次仍待授权。
- 新的 unknown／无turn账本项用于防重。旧helper可读同一表，但不具备完整新语义；回退前须冻结投递并核清 unknown，不能删账本或把未知项自动回放。代码不提供自动切线程／恢复上下文操作。
- 历史正文补读、跨helper进程持久化活动thread选择、oaagent现场复现均不在本件；不从历史envelope擅自接管活动线程。没有扩大权限、修改席位或部署。

SOURCE-MANIFEST.json 记录冻结源非docs文件的Git blob、字节与SHA256；EVIDENCE-MANIFEST.json记录本目录证据。VERIFY.py 从Git对象独取复核，可对任意候选提交运行，不依赖工作区文件。二进制包、私有HOME、账本、凭据均不入Git。
