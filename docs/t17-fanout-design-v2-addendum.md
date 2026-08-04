# T17 fan-out 代理 设计 v2 增补(业主终态架构)· v2.1

状态:delta 审二轮(首轮 dev56 CHANGES/5=B1-B5,本版全纳)。基线=冻结设计 v1.1@1b67eec(双审 PASS)+实现@6e612bc(F1-F5 绿)。
本增补**不推翻** v1 已审核心(双路复制/白名单 fail-closed/[测试环境]后缀/降级铁律/去重/metrics),只做架构扩展。
业主拍板(08-03):代理=飞书入站+出站唯一关口;两阶段迁移。

## 0. 终态架构(业主图纸)

```
飞书 --->> 接入(ws) ----\
   |                     >-- 飞书代理 --(投递API)--> HUB × N
    --->> 接入(https) --/       |
                                 \--<<--(出站API,托管ACK)-- HUB × N
```

- Hub 彻底不碰飞书:不持凭据、不连 WS、不解密。入站=代理调 Hub 投递 API;出站=Hub 调代理出站 API。
- **飞书开放平台零操作**:ws 接入用现有 AppID/Secret 接管长连接,订阅方式不切;投递鉴权 token=内部共享秘密(飞书不再直连 Hub,verification_token 脱离开放平台语义,自行生成)。外部依赖只剩发布授权。

## A. 接入层(多桥接,ws+https)【B2 修订】

- `accesses[]`,每项=(bot_channel, mode∈{ws,https}, 凭据)。**ws=复用 internal/feishu LarkGateway**(UnifiedRobot 生产在跑同一组件,代理进程装配);https=v1 已实现回调接收器。同 bot 单模式活跃;不同 bot 可异构。
- **阶段 1 性质如实改述(B2)**:影子并联=生产 **Hub 配置零变更**,但**流量路径自代理连上 WS 的第一条事件起即已改变**(分走的那部分事件经代理投递路回生产)——代理从第一刻就在生产入站路径上,按此定值守与门槛,不以"零变更"话术淡化。
- **分流语义=待实证假设,非公理(B2)**:"同应用多连接事件随机单投非广播、断线后事件全回存活连接"以官方语义为设计依据,但**上生产前必须 canary 实证**:先用**隔离 bot**(非 unifiedrobot,零业务流量)起双连接,注入已知 event ID 集,验证①守恒(每 ID 恰达一次)②断开一侧后全量回另一侧③无重复。实证不过=本架构 HELD,回退回调方案(废档件复活)。
- **pre-arm 门(B2,连 WS 前全绿才允许接入)**:①配置校验过+代次确认②authoritative sink 投递 API 连通性+token 401 探测通过③spool 落盘 fsync 自检(写-崩-读回)④磁盘水位>阈值⑤告警通道实测触发一次⑥DLQ 空或已确认。任一不绿=拒连。
- **WAL 先于 SDK 返回(B2)**:接入 handler 契约=事件到达→WAL durable(事务提交+fsync)→才向 SDK 返回成功。**若 larkws SDK 被证实无法保证"handler 返回前不 ack 飞书"(fire-and-forget)→本项 HELD**,替代=接受"崩溃窗内该半事件丢失、由生产直连路侧写补偿核对"并显式披露给业主拍,不默认带病上线。SDK ack 时序在 canary 阶段一并实证。
- ws 路径消息已解密已归一,白名单直读 SenderOpenID,无需 encrypt_key。

## B. 投递层(多 HUB)+WAL 模型【B3 修订】

- `sinks[]`,每项=`{hub_url, role: authoritative|besteffort, filter: all|whitelist, suffix: none|测试环境, token}`。
- **提交语义冻结(B3)**:默认=**ALL authoritative 必须最终成功**(非 quorum;若未来需 quorum 须显式配置项+再审)。入站事件"已接管"判定=**仅看 WAL 提交**,不依赖任何 sink 即时结果。
- **WAL 数据模型(B3)**:每消息一主记录+**每 sink 一状态行** `{sink_id, status: queued|sending|delivered|dlq, attempts, next_retry_at, last_error, terminal_at}`。各 sink 独立游标独立退避=**毒件不 head-of-line**:某消息对某 sink 连败→仅该(消息,sink)对入 sink 级 DLQ,同 sink 后续消息照走,其他 sink 不受扰。
- besteffort 语义:同样落 WAL(崩溃可恢复),但有界=`{max_attempts, ttl}` 到界即弃(丢弃计数入 metrics),永不入告警级 DLQ,永不阻塞 authoritative。
- **存储故障处置(B3)**:磁盘满/写失败=**fail-closed 停收**——ws 接入主动断连(分流语义下事件自动回生产直连路=天然优雅降级,阶段 1);阶段 2(独占)下=拒收出站(503)+入站停 ack+最高级告警。SQLite busy=有界重试+背压;corrupt=启动校验失败即拒起+告警,不带病跑。DLQ 重放=管理 API 显式触发+水位门(重放不得把 spool 推过高水位)。
- 投递格式与鉴权见 §K(接口契约);幂等 by construction:Hub `EnqueueMessage`=`INSERT OR IGNORE`、消息 ID=飞书 msg_id(sqlite.go:1185)→ at-least-once 重投天然去重。

## C. 出站(托管 ACK+指定来源)【B1 修订】

托管契约(逐跳托管,与 A 泳道 ACK 链同构):
1. **幂等键冻结(B1)**:Hub 每次发送携带 `request_id`(调用方生成 UUID);代理以 `(caller_id, request_id)` 唯一约束+`body_sha256`。重复请求:body hash 同→返回**同一 outbound_id**(200,幂等重放);hash 异→**409**。ProxyGateway.Send 实现侧自动生成并在重试间保持 request_id。
2. **202 语义冻结(B1)**:202=**accepted(托管开始),非 delivered**。仅在 WAL 事务提交+fsync 之后返回;崩在 202 前=Hub 重试(同 request_id,幂等吸收);崩在 202 后=WAL 重放,不丢不重。
3. **出站状态机(B1)**:`queued→sending→sent | dlq`;`GET /outbound/{outbound_id}` 返回状态+attempts+真 Lark msg_id(sent 后)+错误(dlq);审计日志以 outbound_id 关联全链。
4. 烂件当场拒(400/401/409/422)不进队列。
5. ACK 后代理责任:重试退避→终败 DLQ+告警。**措辞修正(B1):"Hub 调用面恒成功"仅指接口层;投递终态经 status API/DLQ metrics/告警三面可见,DLQ 非静默。**
6. Hub 失败面:代理不可达/4xx/5xx→Hub 现有重试(带同 request_id);Hub worker 不再被 Lark API 延迟拖住。

指定来源两维(同 v2.0):OutMessage.BotChannelID→内嵌 feishu.MultiGateway 按 bot 选凭据(配置 outbound.bots{});caller token→后缀策略(修 v1 无条件后缀 fanout.go:307)+审计标签。

## D. spool(双向持久队列,同一组件)

- 入站(ws→sinks)与出站(→Lark)共用实现:WAL(SQLite,WAL mode,事务+fsync)、每目标独立状态行(§B 模型)、重试退避、DLQ、水位 metrics+告警。落盘先于对外承诺(入站=先于 SDK 返回,出站=先于 202)。
- ws 接入必要性根据:回调模式飞书对非 200 有重试;ws 模式消息上 socket 即视为送达,可靠性责任全在代理。

## E. 配置(声明式+原子热载)【B4 部分】

- 单文件 fanout.yaml(600):accesses/sinks/outbound(callers+bots)/whitelist。
- **单一真源冻结(B4)**:磁盘配置文件=唯一真源。管理 API 的 PUT 走"校验→**temp 文件+fsync→rename→目录 fsync**→热载"同一持久管道;SIGHUP=从真源重读。**持久化失败=不切换内存态**(报 500,当前配置继续跑);热载失败=保旧配置+告警(烂配置不接管)。
- **并发控制(B4)**:配置代次 generation 单调递增,PUT 必带 `expected_generation`(或 If-Match ETag),不匹配=**412**(lost update 杜绝);文件手改+SIGHUP 同样推进代次;进程内文件锁串行化写。
- 热载语义:sinks/callers/白名单=原子表交换;出站凭据=SetChannels 机制扩展;accesses=生命周期管理(增=pre-arm 门→起连;删=停收→排空在途→关);/healthz 报代次。
- 防呆:authoritative sink 删除/降级需 `force: true` 且仅授权角色(§F),否则拒载。

## F. 管理 API(后台管理程序对接面)【B4 修订】

- **鉴权与监听面冻结(B4)**:admin 端点默认**仅 loopback 监听**(与业务端口分离);跨机管理须 mTLS 或 bearer(管理 token 独立于投递/出站 token)。**管理身份**=每 token 绑定 token_id/角色,审计"谁"=token_id(UI 接入后映射到人)。RBAC:普通角色可改 whitelist/besteffort;authoritative 变更+force+secret 旋转=admin 角色。请求体 size 上限 1MB。
- `GET /admin/config`:敏感字段恒掩码;`PUT`:见 §E CAS+持久管道;secret 只写不读、不落日志(错误信息不回显 secret 内容)。
- 每次写=审计一条(token_id/时间/节/旧代次→新代次/diff 摘要脱敏)。
- 分期:本增补冻结 API 契约(§K),day1 CLI/curl 可管;后台管理 UI(既定后阶段)作客户端,force=显式二次确认。

## G. Hub 侧适配(改动面全清单)

- 生产 Hub 采用=纯配置:`FEISHU_TRANSPORT=proxy`+PROXY_URL/TOKEN(proxy 档现码=出站 ProxyGateway+nil receiver,入站走常驻 webhook 路由 main.go:150)。
- Hub 二进制两处加性小改(仅 transport=proxy 路径生效):①ProxyGateway 名称解析透传(stub proxygateway.go:67→调代理 /resolve);②ProxyGateway.Send:生成/保持 request_id,返回 outbound_id 记账(schedule_worker.go:406 簿记,零逻辑变更)。
- collector:代理路=nil,阶段 2 明示接受,后续按需透传(不阻本件)。

## H. 迁移两阶段(详见 runbook 终稿;门与阈值见彼)

- 阶段 0.5(新增,B2):隔离 bot canary 实证分流语义+SDK ack 时序,不过=HELD。
- 阶段 1 影子并联:Hub 配置零变更(流量路径有变,如实);pre-arm 门全绿才连;回滚=热关接入+确认连接退出与在途排空。
- 阶段 2 接管翻转:单一 env 变更+重启;回滚=env 切回(预检旧路可起)。

## K. 接口契约 v1(versioned schema+错误码)【B5 新增】

所有接口带 `"v":1`;内部流量一律 TLS 或同机 loopback;token 双活轮换窗(新旧并收≤24h)。

**K1 入站投递(代理→Hub)** `POST /webhook/{channel}`,明文归一 payload(Hub fake 分支,webhook.go:42),**token 承载钉死=payload 顶层 `"token"` 字段**(Hub verifyWebhookToken 读顶层 token 或 header.token,main.go:829-833;fake 分支对多余字段容忍=Hub 零改):
```json
{"v":1,"token":"<sink token>","msg_id":"om_x","chat_type":"group|personal","chat_id":"oc_x","open_id":"ou_x","sender_open_id":"ou_x","text":"...","raw":{...},"ts":1690000000}
```
错误码:200/202=收;401=token 错(代理按 authoritative 语义重试并告警,不丢);4xx 其他=毒件入 DLQ。**重放防护(B5)**:重复注入被 Hub 幂等吸收(ID=msg_id,INSERT OR IGNORE);伪造新 ID 需持 token;token 仅内网/TLS 流转+可轮换;残余风险=持 token 者可注入,等同现状飞书回调模式凭 token 注入,不新增面。负例:401(错 token)/重放(同 msg_id 二投=库中恰一条)。

**K2 出站(Hub→代理)** `POST /outbound`
请求:`{"v":1,"request_id":"uuid","bot_channel":"unifiedrobot","to_id":"...","to_type":"group|personal","text":"...","raw":{...}}`+`Authorization: Bearer <caller token>`。
响应:202 `{"outbound_id":"ob_x"}`(WAL 落盘后);200 同体(幂等重放);错误码:400 schema/401 鉴权/404 bot_channel 未知/409 request_id 冲突(body hash 异)/413 超限/422 语义烂件/503 spool 故障(fail-closed)。
**K3 状态** `GET /outbound/{outbound_id}`→`{"status":"queued|sending|sent|dlq","attempts":n,"lark_msg_id":"om_x|null","error":"..|null"}`(401 需 caller token,仅本 caller 可查)。
**K4 名称解析** `POST /resolve` `{"v":1,"bot_channel":"..","name":"..","chat_type":".."}`→`{"id":".."}`;404=无此名;缓存 TTL 代理侧。
**K5 管理** `GET/PUT /admin/config`(§E/§F:If-Match/412/掩码/审计);`POST /admin/reload`;`POST /admin/dlq/replay`(水位门)。

## I. 测试计划(G 组,v1 F 组保留)【B1-B5 扩】

G1 ws 装配归一/G2 分流守恒(**已知 event ID 集**逐条核:双连守恒+断连重分配全回存活侧+零重复;receive→WAL 间 kill、WAL→sink 间 kill 各一例,不以小时总量代替)/G3 托管 ACK(202 前 fsync 实证 kill -9 重放;**响应丢失重试同 request_id→同 outbound_id 零双发;409 负例;状态机全迁移+GET status**)/G4 DLQ+告警/G5 热载防呆(烂配置/force/**412 代次冲突/写盘失败不切内存/坏 secret 旋转回退**)/G6 管理 API(掩码/审计/**未授权 401/size 413**)/G7 多 bot 出站选路/G8 /resolve 透传/G9 阶段 2 翻转+回滚演练/G10 sink 语义(**一 authoritative 败一成=败者独立重试不影响成者;besteffort 恒败不阻塞;毒件不 HOL**)/G11 存储故障(磁盘满 fail-closed=ws 断连;SQLite busy 背压)/G12 投递鉴权负例(**401 错 token/重放幂等**)。

## J. 披露与不做

- 后台管理 UI 不在本件;collector 透传延后;开放平台全程零操作(外部依赖=发布授权唯一)。
- **HELD 项预告(B2)**:larkws SDK ack 时序若实证不可钉序→§A 替代方案交业主显式拍,不默认上。
- 废档:回调切换方案(runbook-draft@9159734);代理独占 WS 而 Hub 不适配变体。
