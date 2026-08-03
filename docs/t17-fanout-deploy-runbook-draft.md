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

1. 部署代理,`curl /healthz` 确认 `prod_reachable:true`(startup 依赖检查)。
2. 白名单先放**一个内部测试账号**。
3. 用内部账号手动 POST 一条仿真事件到代理(或直接飞书发消息前先做步骤 4 小流量)→ 核 `/metrics` prod_forwarded 增、测试 Hub 收到。
4. **飞书开放平台把事件回调 URL 从生产 Hub 切到代理**(唯一外部变更点;飞书会发 url_verification,由生产路权威应答=切换自验)。
5. 观察窗(建议 2h):`/metrics` 两路计数、生产 Hub 消息无缺漏(对比切换前后消息率)、测试 Hub 只见白名单账号。
6. 测试账号在飞书发消息→测试 Hub CLI 回复→飞书收到带`[测试环境]`后缀=出站链证。

## 4. 回滚(一步)

飞书回调 URL 切回生产 Hub 直连。代理无状态,可直接停服;队列内容丢失=best-effort 语义允许。测试 Hub 出站失败不影响生产(独立链)。

## 5. 真机证据清单(prodops 审环境用,对应设计 §3)

- [ ] /metrics 快照×3(切换后 0h/1h/观察窗末):prod_forwarded 与飞书事件量吻合、test_* 仅白名单量
- [ ] 生产 Hub 消息表切换前后每小时计数对比(无缺漏)
- [ ] 带[测试环境]后缀的真实回复截图+outbound 审计日志行
- [ ] 降级演练:停测试 Hub 5min→生产计数不断流+test_failed 增→恢复
- [ ] healthz 在生产 Hub 短暂重启窗口内转 503 再恢复(startup/周期探测证据)

## 6. 值守

- 告警:prod_failed>0 连续、healthz 503、decrypt_failed 突增(旋转窗口外=配置漂移)
- encrypt_key 旋转:先更 FANOUT_CHANNELS→systemctl restart(或后续热重载接口);旋转窗口 fail-closed=只走生产,无消息丢失风险
