# T17 fan-out 代理 部署 runbook 终稿 · v2.1(两阶段+canary,可执行版)

前提:设计=v1.1@1b67eec+v2.1 增补过 delta 审;实现=v1@6e612bc+v2 增量(G1-G12)全绿。
**飞书开放平台零操作(生产 bot 与 canary 均无平台侧动作;canary 资产=现成 greyTestRobot,见阶段 0.5)**;外部依赖=发布授权唯一。
执行原则:每步给精确命令与判据;每阶段有门(gate)与自动回滚阈值;prestate 先固化后动手。

## 0. 前置(一次性,全内部)

0.1 prestate 固化(prodops,动手前采集存档):生产 Hub env 快照(`systemctl show workpulse -p Environment` 或 EnvironmentFile 拷贝)、二进制 sha256、当前消息表基线(近 72h 每小时计数)、代理二进制 sha256、fanout.yaml 代次。
0.2 投递 token:`openssl rand -hex 32` 生成→写生产 Hub unifiedrobot bot channel `verification_token`(内部共享秘密,非开放平台件)→同值入代理 sinks[prod].token。测试 Hub 同法独立 token。
0.3 caller token×2(prod/test)、admin token 生成;fanout.yaml 按设计 §E 样例填全,`chmod 600`,owner=服务账号;配置校验须过 sink 地址安全规则(远端必须 https,http 仅 loopback,§E)。
0.4 凭据:FEISHU_APP_ID/SECRET 复制入代理 outbound.bots+accesses。
0.5 部署代理 systemd(`Restart=always`,`LimitNOFILE`,日志入 journald);`curl -s localhost:8795/healthz`=200 且报配置代次;此时 **ws 接入配置为禁用**。
0.6 告警通道实测:手动触发一条测试告警到值守群,确认可达。

## 阶段 0.5:隔离 bot canary(分流语义实证,不碰生产 bot)

**资产前提(C4,已落定 08-03 prodops 确认)**:canary 用现成隔离测试 app=**greyTestRobot**(bot_channel,app_id=cli_aac72db96438dcf9,长连接 WS 模式,零业务流量,owner=prodops,凭据=FEISHU_APP_ID/SECRET env 注入同生产模式独立值)——**现成资产,开放平台零操作成立(生产 bot 与 canary 均无平台侧动作)**。
**目的**:上生产前实证两假设——①同 App 多 WS 连接=事件随机单投非广播、断线全回存活连接②larkws SDK 在 handler 返回前不 ack(WAL 先于 ack 可行)。
1. 用隔离 bot 配 access,起代理连接+一个对照连接(测试工装)。
2. 注入已知 event ID 集(飞书侧发 N=100 条标号消息):核**每 ID 恰达一次**(两连接合计)、断开对照连接→后续 ID 全落代理侧、零重复。
3. SDK ack 时序:handler 内故意 sleep+kill -9,重启后核该事件是否由飞书重推/丢失,得出 ack 语义结论。
**sealed evidence(C4,门的凭据)**:产出两件入仓 commit——`docs/evidence/t17-phase05-conservation.md`(N 条 ID 分布表+守恒结论)+`docs/evidence/t17-phase05-sdk-ack.md`(ack 时序实验记录+结论)。**evidence commit hash 即阶段 1 pre-arm 门第⑦项的引用锚,口头通过无效**。
**门**:①②全过且 evidence 入仓→进阶段 1;②不可钉序→按设计 §A HELD,替代方案报业主显式拍,**不得默认继续**;fallback 回调废档方案若复活=需新发布授权+delta 再审,不得自动执行。

## 阶段 1:影子并联(Hub 配置零变更;流量路径自第一条事件起改变——按生产变更对待,走发布授权)

**pre-arm 门(全绿才允许连 WS,任一红=不连)**:
- [ ] 配置校验过+代次确认(healthz)
- [ ] authoritative sink 连通:`curl -X POST <prod>/webhook/unifiedrobot -d '<带错 token 探测>'`=401(验证 token 生效),带对 token 的合成探测件=200/202
- [ ] spool fsync 自检(内置 self-check:写-kill-读回)通过
- [ ] 磁盘余量>20%
- [ ] 告警通道 0.6 已实测
- [ ] DLQ 空
- [ ] 阶段 0.5 sealed evidence commit hash=`<待填>` 已入仓且两件结论均 PASS(C4:引用 hash 核对,非口头)

1. 白名单=一个内部测试账号;热载开启 unifiedrobot ws 接入(管理 API PUT,代次推进)。
2. 即时核(10min 内):`/metrics` 代理事件计数≈总量一半且 prod_delivered 吻合、spool 深度≈0、生产 Hub 端按 feishu_msg_id 抽 20 条核恰一条入库。
3. **无损验证(核心证据,已知 ID 法优先)**:值守账号发 20 条标号消息(散在 1h 内)→生产 Hub 逐条查得且恰一条;辅证=每小时计数 vs 0.1 基线守恒。
4. 测试链:白名单账号消息→测试 Hub 收到(影子期≈半流量,已知特性)→CLI 回复→飞书实收带[测试环境]后缀。
5. 出站托管链:outbound_id←202→`GET /outbound/{id}` status=sent→代理审计真 msg_id 三点关联。
6. 故障演练(观察窗内做):代理 kill -9→重启,期间已知 ID 消息经生产直连路无损;停测试 Hub 5min→authoritative 两向不受扰。
7. **观察窗≥24h**;**自动回滚阈值(值守执行,不请示)**:authoritative spool 深度>1000 持续 10min、或 DLQ 新增>0、或已知 ID 抽检丢失任一条、或代理 ws 断连>60s 且生产直连路计数未相应回升→**热关 ws 接入**。
**阶段 1 回滚判据**:热关后 `/metrics` 连接数=0+在途排空(spool 清零或手动 drain 确认)+生产计数回全量,三项核毕才算回滚完成。

## 阶段 2:接管翻转(单一 env 变更,低流量窗,发布授权后)

**预检(翻转前)**:阶段 1 观察窗全绿;**回滚路预演**=确认旧 env 快照在手、`FEISHU_TRANSPORT=ws` 所需凭据仍在 env/库(0.1 prestate)、测试环境起过一次 ws receiver 证可回。
1. 生产 Hub EnvironmentFile 追加/修改(原子:写 temp→mv):`FEISHU_TRANSPORT=proxy`、`FEISHU_PROXY_URL=http://127.0.0.1:8795`、`FEISHU_PROXY_TOKEN=<prod caller token>`。
2. `systemctl restart workpulse`(唯一生产侧操作)。其 WS 断开→事件 100% 落代理(canary 已证)→投递 API 全量进生产;出站切托管。
3. 验证(30min):已知 ID 法 10 条全达;测试 Hub 白名单流量升 100%;出站真发一条→202→sent→飞书实收→审计关联;按名发信(/resolve)实测一次;`journalctl -u workpulse | grep "transport.*proxy"` 确认模式。
4. 观察窗 2h:双向 metrics、spool/DLQ、healthz;自动回滚阈值同阶段 1 加:出站 DLQ 新增>0、生产出站 202 失败率>1%。
**阶段 2 回滚(一步+预检兜底)**:env 切回 `FEISHU_TRANSPORT=ws`+restart——代理接入保持 ON=回滚后自动落回阶段 1 影子态(分流语义,不丢消息);**回滚验收=已知 canary ID 10 条逐条达生产,非仅消息率**。要彻底退出再热关接入+核连接退出。

## 真机证据清单(prodops 审环境)

- [ ] 阶段 0.5:sealed evidence 两件入仓(conservation+sdk-ack)+commit hash 与 pre-arm 门引用一致
- [ ] 阶段 1:已知 ID 20 条逐条核对表+每小时守恒辅证+kill -9 无损记录
- [ ] 阶段 2:翻转前后已知 ID 10 条+白名单 100%+出站三点关联样本×3+[测试环境]后缀实收截图
- [ ] DLQ 演练:恒败件→退避→DLQ+告警;DLQ replay 水位门实测
- [ ] 热载防呆:烂配置 PUT 拒(代次不动)/412 并发冲突/无 force 删 authoritative 拒
- [ ] 投递鉴权:401 错 token/重放同 msg_id 库中恰一条
- [ ] 磁盘满演练(容器内):fail-closed=ws 主动断连+503+告警

## 值守与告警

- 告警:authoritative spool>阈值、DLQ 新增、ws 断连>60s、healthz 503、配置代次异动无审计、磁盘水位、出站 sending 停滞>5min。
- 代理=进出双向关口,值守按生产命脉;WAL+Restart=always=崩溃自愈;阶段 2 后生产直连路仍可一步 env 回滚兜底。
- token/凭据旋转:双 token 并收窗≤24h,管理 API 单向设置+热载,fail-closed 语义同 v1。

## 已废弃(留档勿执行)

旧草稿(t17-fanout-deploy-runbook-draft.md@9159734)"订阅方式切换"方案:被终态架构取代(需开放平台操作,现全免)。
