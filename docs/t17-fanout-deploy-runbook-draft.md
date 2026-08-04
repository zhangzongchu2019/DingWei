# T17 fan-out 代理 部署 runbook(草稿,D4 段候审毕定稿)

前提:实现 tip=6e612bc(tag t17-fanout-6e612bc)通过整版审;真机证据要求见设计 §3。

## 1. 部署形态

- 二进制:`go build -o /usr/local/bin/feishu-fanout ./cmd/feishu-fanout`
- systemd 单元(独立于 workpulse):`feishu-fanout.service`,`Restart=always`,`EnvironmentFile=/etc/dingwei/fanout.env`
- 端口:默认 127.0.0.1:8795,公网经 nginx 反代(与 hub webhook 同域名不同路径,或独立子域)

## 2. 配置清单(/etc/dingwei/fanout.env,600 权限)

| 变量 | 值来源 | 敏感 |
|---|---|---|
| FANOUT_ADDR | 127.0.0.1:8795 | 否 |
| FANOUT_PROD_URL | 生产 Hub base(内网) | 否 |
| FANOUT_TEST_URL | 测试 Hub base(内网) | 否 |
| FANOUT_CHANNELS | 与生产 bot_channel 同源的 encrypt_key/verification_token JSON | **是** |
| FANOUT_WHITELIST_FILE | /etc/dingwei/fanout-whitelist.json | 否 |
| FANOUT_OUTBOUND_TOKEN | 新生成随机 token(测试 Hub FEISHU_PROXY_TOKEN 同值) | **是** |
| FANOUT_LARK_APP_ID/SECRET | 与生产同 bot 应用凭据(设计 §4:代理代持) | **是** |
| FANOUT_LARK_BOT_CHANNEL | 出站 bot channel id | 否 |

测试 Hub 侧:`FEISHU_TRANSPORT=proxy` + `FEISHU_PROXY_URL=http://<代理>/outbound` + `FEISHU_PROXY_TOKEN`。生产 Hub:**零配置变更**。

## 3. 切换步骤(顺序敏感)

> **★现场事实修正(08-03 prodops 确认)**:生产 Hub 当前 `FEISHU_TRANSPORT=ws`(larkws 长连接,bot_channel=unifiedrobot,app_id=cli_aace34514e781cd2,凭据=FEISHU_APP_ID/FEISHU_APP_SECRET env 注入),**现无回调 URL**。原步骤 4"改回调 URL"不适用,修正为【订阅方式切换】方案(下)。设计核心(代理=唯一入口复制点/白名单/后缀/降级)不变;生产 Hub 仍零代码变更——webhook 路由 `POST /webhook/{channel}` 常驻注册(main.go:150,与 transport 无关),归一产物与长连接同形(webhook.go:13),WS 网关保持在跑=即时回滚垫。

0. **前置配置(新增,prodops 执行)**:
   a. 在飞书开放平台为该应用生成/查取 Verification Token 与 Encrypt Key,写入生产 Hub unifiedrobot bot channel 记录(webhook 验证用,main.go:718/722;不设=明文裸收,拒)。同值进代理 FANOUT_CHANNELS。
   b. 确认代理公网路径(nginx 反代)可达,url_verification 明文/加密两分支代理→生产转发链自测过(F 组用例已盖,真机 curl 复测一次)。
1. 部署代理,`curl /healthz` 确认 `prod_reachable:true`(startup 依赖检查)。
2. 白名单先放**一个内部测试账号**。
3. 用内部账号手动 POST 一条仿真事件到代理→ 核 `/metrics` prod_forwarded 增、测试 Hub 收到、生产 Hub webhook 路径入库正常(webhook 归一链真机首证)。
4. **飞书开放平台把该应用事件订阅方式从"长连接"切换为"回调 URL",URL 填代理地址**(唯一外部变更点;飞书发 url_verification→代理转生产权威应答=切换自验)。切换瞬间可能有秒级事件间隙,选低流量窗执行。生产 Hub 的 WS 连接会转闲置——**不要停**,它是回滚垫。
5. 观察窗(建议 2h):`/metrics` 两路计数、生产 Hub 消息无缺漏(对比切换前后消息率)、测试 Hub 只见白名单账号。
6. 测试账号在飞书发消息→测试 Hub CLI 回复→飞书收到带`[测试环境]`后缀=出站链证。

## 4. 回滚(一步)

**飞书开放平台把订阅方式切回"长连接"**——生产 Hub 的 WS 网关一直在跑,切回即恢复原态,零生产侧操作。代理无状态,可直接停服;队列内容丢失=best-effort 语义允许。测试 Hub 出站失败不影响生产(独立链)。

> 备选方案 b(未采,留档):代理自持 WS 长连接收事件再复制推两 Hub。不采理由:①飞书长连接事件对多连接为分流语义,代理与生产 Hub 双连=事件分裂,故生产 Hub 必须停 WS→失去"WS 保持在跑"的即时回滚垫;②代理需合成 webhook 报文注入生产,信任面扩大;③偏离已冻结设计的字节复制模型。仅当业主明确拒绝订阅方式切换时再议。

## 5. 真机证据清单(prodops 审环境用,对应设计 §3)

- [ ] /metrics 快照×3(切换后 0h/1h/观察窗末):prod_forwarded 与飞书事件量吻合、test_* 仅白名单量
- [ ] 生产 Hub 消息表切换前后每小时计数对比(无缺漏)
- [ ] 带[测试环境]后缀的真实回复截图+outbound 审计日志行
- [ ] 降级演练:停测试 Hub 5min→生产计数不断流+test_failed 增→恢复
- [ ] healthz 在生产 Hub 短暂重启窗口内转 503 再恢复(startup/周期探测证据)

## 6. 值守

- 告警:prod_failed>0 连续、healthz 503、decrypt_failed 突增(旋转窗口外=配置漂移)
- encrypt_key 旋转:先更 FANOUT_CHANNELS→systemctl restart(或后续热重载接口);旋转窗口 fail-closed=只走生产,无消息丢失风险
