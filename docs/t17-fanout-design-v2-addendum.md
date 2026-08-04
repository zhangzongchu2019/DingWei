# T17 fan-out 代理 设计 v2 增补(业主终态架构)

状态:候 delta 审。基线=冻结设计 v1.1@1b67eec(双审 PASS)+实现@6e612bc(F1-F5 绿)。
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

## A. 接入层(多桥接,ws+https)

- `accesses[]`,每项=(bot_channel, mode∈{ws,https}, 凭据)。**ws=复用 internal/feishu LarkGateway**(UnifiedRobot 生产在跑的同一组件,代理进程装配,~几十行胶水);https=v1 已实现回调接收器。
- 约束:同一 bot 同一时刻仅一种模式活跃(飞书侧订阅方式决定互斥);不同 bot 可异构(UnifiedRobot=ws,他 bot=https)。
- **ws 分流语义(迁移安全性的基石)**:同应用多条长连接=事件随机投递到**其中一条**(非广播)。代理与生产 Hub 短暂并存时:直达生产的事件直接处理,落到代理的事件经投递 API 送回生产——**两路都到生产,零丢失、零重复**(去重兜底见 §B)。此语义由阶段 1 影子期实证。
- ws 路径消息已解密已归一(model.Message),白名单直读 SenderOpenID,无需 encrypt_key。

## B. 投递层(多 HUB)

- `sinks[]`,每项=`{hub_url, role: authoritative|besteffort, filter: all|whitelist, suffix: none|测试环境, token}`。
- 降级铁律不变:authoritative(≥1 条)失败=spool 重试+告警;besteffort 故障永不阻塞 authoritative。
- 投递格式=明文归一 payload(Hub webhook 路由 fakeWebhookMessage 分支,webhook.go:42,本就为归一再注入设计;含 msg_id/chat/sender/raw)。
- 投递鉴权:Hub 侧 bot channel 设 verification_token(内部共享秘密,入库 store 已支持 main.go:722),代理投递携带;未配 token 的通道=明文裸收仅告警(main.go:725),生产 sink **必须**配 token。
- **幂等 by construction**:Hub `EnqueueMessage`=`INSERT OR IGNORE`、消息 ID=飞书 msg_id(sqlite.go:1185)→ 代理 at-least-once 重投天然去重。

## C. 出站(托管 ACK+指定来源)

契约(逐跳托管,与 A 泳道 ACK 链同构:每跳 ACK=接管本环节):
1. `POST /outbound` 同步只做:调用方鉴权+schema 校验+bot_channel 已知+**落持久 spool** → `202 {outbound_id}`=Hub 眼中发送成功。
2. 烂件当场拒(401/400/422)不进队列——ACK=开始托管,不托管注定失败件。
3. ACK 后责任全在代理:重试+退避→终败入 DLQ+告警,不静默丢;真实 Lark msg_id 记代理审计日志,以 outbound_id 关联 Hub 账↔代理审计。
4. Hub 失败面收窄为一种:代理不可达/拒收→Hub 现有重试;托管后恒成功。Hub worker 不再被 Lark API 延迟拖住(现 Send 同步等 Lark)。

指定来源两维:
- **走哪个 bot 发**:OutMessage.BotChannelID 现成字段;代理内嵌 `feishu.MultiGateway`(Hub 现跑的多 bot 分发器原样复用)按 bot 选凭据;配置从单 bot 改 `outbound.bots{}` 映射。
- **哪个 Hub 发起**:每 Hub 一把 caller token→后缀策略(测试=[测试环境]幂等追加,生产=无)+审计标签。修复 v1 缺口:/outbound 无条件加后缀(fanout.go:307)改按调用方。

## D. spool(双向持久队列,同一组件)

- 入站(ws 接入→sinks)与出站(→Lark)共用:WAL 落盘于 handler 返回前(ws 事件收到即持久,进程崩=重放不丢)、重试退避、DLQ、水位 metrics+告警。
- ws 接入的必要性根据:回调模式飞书对非 200 有重试;ws 模式消息上 socket 即视为送达,可靠性责任全在代理。

## E. 配置(声明式+原子热载)

- 单文件 fanout.yaml(600):accesses/sinks/outbound(callers+bots)/whitelist。
- 热载语义:sinks/callers/白名单=原子表交换即刻生效;出站凭据=SetChannels 现机制扩展;accesses=带生命周期(新增 ws=起连,删除=排空在途优雅关)。
- 触发:SIGHUP(现有)+`POST /admin/reload`;/healthz 报配置代次。
- 防呆铁律:①烂配置不接管(校验失败=保旧配置跑+告警,热载最坏结果是"没变"非"停摆")②authoritative sink 删除/降级需显式 `force: true`,否则拒载。

## F. 管理 API(后台管理程序对接面)

- **真源在代理本地**(方向性:代理是全部 Hub 上游,配置真源入下游库=反向依赖,违降级铁律)。
- `GET/PUT /admin/config`(整体/分节),PUT=校验→持久→热载,与文件+SIGHUP 同一校验管道(防呆一视同仁)。
- 敏感字段单向:secret/key/token 只可设置旋转,GET 恒掩码;每次写=审计一条(谁/何时/哪节/代次)。
- 分期:本增补冻结 **API 契约**,day1 可 CLI/curl 全量管理;后台管理程序 UI(既定排后阶段)到位时作客户端直接对接,代理侧零再改。UI 上 force=显式二次确认。

## G. Hub 侧适配(改动面全清单)

- 生产 Hub 采用=**纯配置**:`FEISHU_TRANSPORT=proxy`+PROXY_URL/TOKEN(proxy 档现码返回出站 ProxyGateway+nil receiver=WS 自然停,入站走常驻 webhook 路由 main.go:150)。
- Hub 二进制两处**加性**小改(仅 transport=proxy 路径生效,ws/fake 零触碰):
  1. ProxyGateway 名称解析透传(现 stub ResolveNameToID=ErrNotImplemented proxygateway.go:67→调代理新增 /resolve 端点;代理有凭据可调 Lark 通讯录);
  2. ProxyGateway.Send 返回 outbound_id(托管凭证)——Hub 记账字段照塞(schedule_worker.go:406 簿记用途,零逻辑变更)。
- collector:代理路=nil(管理面采集降级),阶段 2 明示接受,后续按需给代理加采集透传(不阻本件)。

## H. 迁移两阶段(详见 runbook 终稿)

- 阶段 1 影子并联:生产**零变更零重启**,代理 ws 接入与生产 WS 并存(分流语义,§A),真流量实证无损+测试链全验;回滚=配置热关接入,秒级生产无感。
- 阶段 2 接管翻转:单一 env 变更+重启,代理独占进出;回滚=env 切回+重启(代理接入可保持 ON,分流下安全)。

## I. 测试计划 delta(G 组,v1 F 组保留)

G1 ws 接入装配归一(=LarkGateway 既有行为回归)/G2 分流影子无损(双连并存,事件计数守恒+零重复)/G3 托管 ACK(202+spool 落盘先于 ACK,kill -9 重放不丢)/G4 DLQ+告警(注入恒败件)/G5 热载+两防呆(烂配置/force)/G6 管理 API(掩码/审计/代次)/G7 多 bot 出站选路/G8 名称解析透传/G9 阶段 2 翻转+回滚演练(容器内全链)。

## J. 披露与不做

- 后台管理 UI 不在本件(API 先行);collector 透传延后;飞书开放平台全程零操作(原"三外部件"中开放平台两项作废,外部依赖=发布授权唯一)。
- 已废弃备选留档:纯回调切换方案(v1 runbook 步 4,订阅方式切换)——被终态架构取代;代理独占 WS 而 Hub 不适配的变体(分流分裂)——不成立。
