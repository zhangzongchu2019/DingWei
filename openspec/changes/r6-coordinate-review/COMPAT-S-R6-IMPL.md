# S-R6-IMPL 评审后四格兼容补测

组长对`0fb9bf577c7a4d49530c37b965397fc9a33474b4`内容评审通过，要求补兼容。此次在同分支追加修补，不改写旧提交。新端源码`f2fb5ec990ac02014d9cde215442c1cfd35e1564`（tree `ea89862bcc5e00b3a0f2bdaa2019e10d4efa4c63`）；旧Hub及旧helper均取固定r46 `2af469dd6919d8eb6064aa7da0e646f0851becf8` / `0.2.3.21`。新端源VERSION仍为`0.2.3.21`，本次不虚构发版号；“新/旧”按上述Git坐标区分，工具/能力实测亦不同。

## 缺口与修补范围

原helper把R6关闭的503及旧Hub无路由404都泛化为`coordinate_review_rejected`，不满足明确反馈判据。产品改动只在两处：

- Hub现有`!coordinateReviewReady()`分支仍HTTP503，但返回封闭错误码`coordinate_review_not_enabled`；所有原前置/授权/业务逻辑不变。
- helper在R6专用入口将HTTP404、或HTTP503且有上述固定错误码，解释为`coordinate_review_not_enabled`，消息为“坐标派审未启用：当前 Hub 未开放此能力、依赖未就绪或版本不支持。”；确定未执行才记`outcome=rejected`。其它5xx写入仍`outcome=unknown`，403等不被误判，原始上游正文不回显，无自动重发。

没有改变MCP工具可见性、能力握手、R5核权、提醒算法、数据库迁移、版本、部署或开关。新helper一升级仍有26个工具且含`dw_coordinate_review`，此事实不由Hub开关控制。

## 实测方式与边界

`tests/r6-compat/run.py`校验旧树HEAD及VERSION，在detached旧树临时放入**测试桥**，调用旧主程序真实`wireNewPlatform`及`registerConsoleRoutes`；不模拟404、不修改旧产品源码，结束删除桥文件。新端使用真实Hub处理器、专属Agent bearer、R5提供方受控夹具、Console创建接口和真实调度器。

每格实际构造对应版本SessionHelper，调用真实MCP stdio子进程→实际Unix IPC→HTTP；同一MCP进程依次调用其它工具、R6工具、其它工具；同时以该helper连接参数和实际能力信封建立同一条本地WS，结束前ping/pong验证仍活。没有启动模型/CLI或连接现网。旧helper的无工具、无能力不是手填结果，而是其真实tools/list与capabilities返回。新端HTTP创建派审也实际执行。

30/90/150分钟为注入业务时钟推进，非现实等待150分钟；实际R6调度/持久槽/个人发送回调被执行，飞书个人绑定及回执为受控测试身份/桩。真实Git授权及飞书发送仍留T-07独立验收；不把本补测称为T-07/T-08。

| 格 | 工具/能力与R6结果 | 其它工具/连接 | 提醒与状态 |
| --- | --- | --- | --- |
| 新helper＋新Hub未开R6 | 工具26、R6能力true；创建503，工具HTTP503明确`coordinate_review_not_enabled`，含中文“坐标派审未启用”；17.65ms，判据<5000ms | 同一MCP前后`dw_task_list`均ok，MCP仍活、同一WS pong | 不创建派审，不记已打开/已审 |
| 新helper＋旧r46 Hub | 工具26、R6能力true；旧主程序真实路由HTTP404，转换同一明确错误，17.92ms，无超时或泛化失败 | 同上；旧Hub接受未知新增能力字段而不踢连接 | 无R6派审/坐标投递 |
| 旧helper0.2.3.21＋新Hub开R6 | 创建HTTP200；工具25且无R6，R6能力false；get与open实调均RPC -32601（unknown tool），get 0.2ms | 同一MCP前后其它工具ok，连接pong | 保留Console未打开待办，30/90/150分钟各一次私聊回调，共3次；210分钟无第四次，相邻≥60分钟；opened=false、completed=false、无R6信封误投旧helper |
| 新helper＋新Hub开R6 | 创建HTTP200；工具26/R6能力true，get及open实调HTTP200，get 18.96ms，收到实际坐标信封 | 同一MCP前后其它工具ok，连接pong | 30分钟首催后打开；90/150/210分钟无后续，计数停1，opened=true但completed=false |

逐格实际耗时、HTTP/RPC码、工具数、提醒时刻及回调数见`COMPAT-S-R6-IMPL.json`，不从截图或代码判断替代读数。

## 反向控制与复跑

- 撤掉helper对旧Hub404的识别：实际旧Hub格红在`R6_COMPAT_CLEAR_DISABLED`。
- 撤掉对新Hub关闭错误码的识别：新Hub未开格红在同一明确关闭断言。
- 把任意503写入都误记未启用：红在`R6_COMPAT_WRITE_UNCERTAINTY_SWALLOWED`，证明没有吞掉不确定写入。

三个故障均非0且命中指定断言，不把编译/安装/配置失败当反证。Python源副本只从Git跟踪文件构造到临时目录，无源码外发，不改工作树；旧树桥文件均清理。

在普通dev用户及清洁环境中运行，固定锁venv、临时HOME、临时DB、loopback；不继承SH_/DINGWEI_/DW_/WP_/FCS_及实际连接凭据。兼容fixture用专门`r6_compat`测试build tag，普通整包不多添skip；显式runner要求四格全部执行，缺配置直接失败。

```sh
git worktree add --detach /tmp/r6-legacy 2af469dd6919d8eb6064aa7da0e646f0851becf8
python3 tests/r6-compat/run.py --legacy-tree /tmp/r6-legacy --output /tmp/r6-compat-grid
python3 tests/r6-compat/mutations.py --legacy-tree /tmp/r6-legacy --output /tmp/r6-compat-mutations
```

原始本地日志：`/tmp/r6-compat-grid/{old-hub,new-hub}.log`、`/tmp/r6-compat-mutations/`、`/tmp/r6-compat-helper-clean.log`、`/tmp/r6-compat-release-clean.log`、`/tmp/r6-compat-gate-clean.log`。小型结构化读数入Git，大型日志保留本地供复核/复跑。最终门读数如下。

## 最终门读数（2026-11-01T16:31:53.971057+00:00）

- 四格全部PASS，专用runner rc0、没有跳过；三个新反向控制全部命中指定断言红、mutation runner rc0；helper定向16项rc0。
- 剥继承变量/锁venv完整`run-build-tests` rc0：helper claude/codex各1097（1088+9，清单90件）、Python27/shell2完成。
- HUB_TEST_PG_STRICT=1 / -count=1 整包m8：1224 PASS / 36既有SKIP，181.121s，rc0。
- HUB_TEST_PG_STRICT=1 / -count=1 整包store：523 PASS / 2既有SKIP，61.253s，rc0。
- 38个既有跳过的用例及条件沿原`SKIPS-S-R6-IMPL.md`；本补测专用tag不在普通整包新增skip，显式四格已执行。
- 单变更/全仓OpenSpec1.16.0 strict rc0，全仓12/12；原R6增量spec、R5读取/核权、迁移0071、提醒逻辑及VERSION均未改变。
- 19998无监听残留；旧detached树测试桥清理完成。T-07/T-08仍未勾选，真实私聊/权限验收与预发另派，无部署/重启/激活。
