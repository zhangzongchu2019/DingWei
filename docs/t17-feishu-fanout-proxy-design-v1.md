# T17 飞书消息 fan-out 代理 设计 v1

派工:coordinator 08-03。业主需求四条:①飞书回调→代理→复制到生产 Hub+测试 Hub ②测试 Hub CLI 回复回飞书带`[测试环境]`后缀 ③仅白名单用户消息进测试 Hub ④独立新服务(现桥接=单 bot 通道,不升级)。

## 1. 架构总览

```
                          ┌──────────────┐   raw bytes(同步,权威)   ┌──────────┐
飞书事件回调 ──POST──────▶│  fanout 代理  │ ────────────────────────▶│ 生产 Hub  │──LarkGateway──▶ 飞书
(一个回调URL指向代理)      │  (新独立服务) │                          └──────────┘
                          │              │   raw bytes(异步,白名单)  ┌──────────┐
                          │              │ ────────────────────────▶│ 测试 Hub  │
                          │              │◀────────────────────────│(transport │
                          └──────┬───────┘   POST /outbound         │ =proxy)  │
                                 │  加[测试环境]后缀                 └──────────┘
                                 └────────Lark API(代理持测试路凭据)──▶ 飞书
```

- **入站**:飞书平台的事件回调 URL 从"直指生产 Hub"改为"指向代理"。代理收到原始 POST 后:生产路=**原样字节转发**(含 headers)到生产 Hub `POST /webhook/{channel}`,**同步**、以生产 Hub 响应为对飞书的响应(权威路);测试路=白名单命中才转发,**异步 best-effort**。两 Hub 各自照旧解密/验 token/challenge/归一——代理不改写入站语义,生产 Hub **零改动、零感知**(回滚=回调 URL 切回生产 Hub 即完成)。
- **出站(需求②)**:测试 Hub 的 Feishu transport 配置为 `proxy` 模式(见 §4,唯一一处 hub 小改),把 OutMessage POST 给代理 `/outbound`;代理**统一加 `[测试环境]` 后缀**后经自持的 LarkGateway 凭据发飞书。生产 Hub 出站不经代理(现状直连不变)。

## 2. 入站细节

- 端点:`POST /webhook/{channel}`(与 Hub 同形,飞书侧只换域名/路径前缀即可)。
- **challenge(url_verification)**:同步转发生产 Hub,把生产 Hub 的 challenge 响应原样回给飞书(权威=生产)。测试 Hub 的 challenge 由其自己首次配置时单独走(或代理复制一份 fire-and-forget,失败不影响)。
- **白名单(需求③)**:代理需读 sender open_id ⇒ 代理持有与 bot 通道相同的 encrypt_key/verification_token 配置(加密事件先解密再判;明文直判)。判据:`event.sender.sender_id.open_id ∈ 白名单`;白名单=配置文件(JSON,open_id 列表+可选 chat_id 列表),SIGHUP/定时重载。**解密仅用于读 sender 判白名单;转发仍是原始字节**(两 Hub 语义自洽,代理零改写)。非消息类事件(challenge 外的系统事件)默认只走生产路。
- **去重**:飞书按 event_id 重推;代理不去重(两 Hub 各自处理重复,现状已如此),仅在测试路维护 event_id LRU 防代理自身重试造成的重复注入。

## 3. 降级语义(需求④,铁律=生产投递永不被测试侧阻塞)

| 故障 | 行为 |
|---|---|
| 测试 Hub down/慢 | 测试路异步队列(有界,默认 1024),满即丢最旧+计数;转发超时 3s 丢弃;**生产路完全不受影响** |
| 生产 Hub down | 代理对飞书回 5xx ⇒ 飞书原生重推;测试路本次不投(避免测试比生产先看到) |
| 代理 down | 等价于现状回调不可达,飞书重推;运维 runbook:回调 URL 切回生产 Hub 直连=一步回滚 |
| 出站 Lark API 失败 | 对测试 Hub 回 5xx,由测试 Hub 出站队列按现有 outbound 重试逻辑处理 |
| 白名单文件坏/缺 | fail-closed:测试路全停(只走生产),日志告警;绝不 fail-open 把全量消息灌测试 |

可观测:`/metrics`(转发计数/丢弃计数/两路延迟)+`/healthz`。

## 4. 唯一 hub 侧小改(明示,不擅动)

测试 Hub 需 `FEISHU_TRANSPORT=proxy` 新模式(现仅 fake/lark):实现一个 `feishu.Transport`,`Send()`=POST OutMessage JSON 到 `WP_FEISHU_PROXY_URL` + bearer(代理发 token)。约 60-80 行,只在 transport 装配处一个 case,fake/lark 路径零触碰;生产 Hub 不启用即零影响。**[测试环境]后缀在代理侧加**(非测试 Hub 内):凭据与标记集中在代理,测试 Hub 不持真飞书凭据(测试环境失守也发不了未标记消息=安全边界)。

替代方案(已比较,不采):a)测试 Hub 配独立测试 bot 应用直发——会话里显示为另一个 bot,打断"同会话回复"体验,且测试 Hub 持真凭据;b)代理拦生产 Hub 出站——生产路径引入新单点,违背"生产零感知"。

## 5. 服务形态与配置

- 同仓新 `cmd/feishu-fanout`(Go,复用 internal/feishu 的 payload 解析/LarkGateway),独立进程/端口,systemd 单元独立。
- 配置(env):`FANOUT_ADDR`/`FANOUT_PROD_URL`/`FANOUT_TEST_URL`/`FANOUT_WHITELIST_FILE`/`FANOUT_CHANNELS`(JSON:每 channel 的 encrypt_key/verification_token)/`FANOUT_LARK_APP_ID/SECRET`(出站测试路凭据)/`FANOUT_OUTBOUND_TOKEN`。
- 不落库:白名单与通道配置皆文件/env,代理无状态(队列内存态,丢失可接受=best-effort 语义)。

## 6. 测试计划(交付物第三项,四类全覆盖)

| # | 类 | 断言 |
|---|---|---|
| F1 | 复制 | 白名单内消息:生产+测试两 Hub 各收到一份原始字节等价的 webhook;生产响应=对飞书响应 |
| F2 | 白名单 | 名单外:仅生产收到;名单内:双收;名单文件热重载生效;文件坏=fail-closed 仅生产 |
| F3 | 测试标记 | 测试 Hub 出站经 `/outbound`:文本尾部恰一个`[测试环境]`;幂等(已带后缀不重复加);生产出站不经代理无标记 |
| F4 | 降级 | 测试 Hub 不可达:生产照常、丢弃计数增;生产不可达:对飞书 5xx 且测试路不投;队列满丢最旧;challenge 经生产权威应答 |
| F5 | e2e smoke | 真代理进程+两个真(或 httptest)Hub:F1-F4 全链走一遍(复用 reply-origin smoke 的 harness 形制) |

## 7. 里程碑

D1=本设计(整版提审,≤3 轮);D2=实现(代理主体+hub proxy transport 小改);D3=F1-F5 测试。D2+D3=设计冻结后 2 工作段。
