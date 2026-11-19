# extseat-lifetime 上下文

发布提交 8213f12937aba18080a384bcabd2ae483d246098（r62，VERSION 0.2.3.42），2026-11-19 现网上线（SWITCH_PASS 19:42:36Z，结项件 review/prodops/r62-prod-closeout-20261119 6bbca0d8d2a951015b88ceed6eff12b8de567d9c）后按方案甲归档建立本主规格。

## 来自 2026-11-19-r62-extseat-ttl（context.md）

# 基线与实施上下文

现有extseat-sender-attribution/context.md/spec.md与agent-http-mcp-gateway/context.md已读。main206d41b2的extshared.go定义GroupTTL=72h、MessageTTL=24h、TokenTTL=336h；extseat/inbox.go受理时将MessageTTL写入inbox/outbox/send_key，后续按存储expires_at判断，tempgroup/service.go创建/合法续期引用GroupTTL。授权策略及扫描器不依赖硬编码3天/24小时。

本片只改常量；真实PG回归复用internal/tempgroup/service_pg_test.go的生产tempgroup+extseat+identity服务fixture，避免以stub组服务代替AG到期。边界固定时钟，不等现实七天；以一微秒前后测试PG精度。还运行extshared/extseat/m8整包三遍PG strict，并额外运行tempgroup三遍，因为新增真实组边界测试在此包。环境白名单，Go1.22.2，锁定Python3.12.3，数据盘独立HOME/TMPDIR，禁止继承在线SH_/DINGWEI_配置。

补充既有测试影响：internal/extseat/core_pg_test.go的MessageExpiryAndRevokedWait改用合法发送先续组，避免AG与消息恰好同时过期；保留原消息过期和token撤权所有断言。9576前驱的canonical rc0、旧夹具rc1、修复夹具单格rc0与中止full-go3原日志均保留；正式门在追加的新冻结源重跑。

最终冻结产品/测试2fd6aeba（完整40位见REPORT），证据b5e6e590102dedfa19c234fd726ea341fb23685e；四包三遍3081顶层/5250含子项PASS、84逐名SKIP、0FAIL/0中断；canonical两profile各1270、Python27/shell2，strict42/0。报告与VERIFY位于docs/reviews/r62-extseat-ttl。独立复核与组长验收待回。

17:33Z追加核项补测源0b9a10f9227a35eaa01117cb767b9c883e6fb9ec，仅新增lifetime_expired_group_pg_test.go；全部TestR62三遍18顶层/45含子项PASS、无跳过。原四包整包仍绑定2fd原锚，新canonical以0b9重跑。已投影旧边界双源红并报组长，不改产品；证据在group-expiry-followup。

2026-11-18T18:05:27Z裁定：已投影件后续组失效的通用边界归r62切片三，本片不加AG发送围栏；90c7以来生产文件零改，0b9仅新增未投影安全格。红探针双源原日志与安全格证据见c3038963的group-expiry-followup目录，文档新锚以回执为准。
