# 基线与实施上下文

现有extseat-sender-attribution/context.md/spec.md与agent-http-mcp-gateway/context.md已读。main206d41b2的extshared.go定义GroupTTL=72h、MessageTTL=24h、TokenTTL=336h；extseat/inbox.go受理时将MessageTTL写入inbox/outbox/send_key，后续按存储expires_at判断，tempgroup/service.go创建/合法续期引用GroupTTL。授权策略及扫描器不依赖硬编码3天/24小时。

本片只改常量；真实PG回归复用internal/tempgroup/service_pg_test.go的生产tempgroup+extseat+identity服务fixture，避免以stub组服务代替AG到期。边界固定时钟，不等现实七天；以一微秒前后测试PG精度。还运行extshared/extseat/m8整包三遍PG strict，并额外运行tempgroup三遍，因为新增真实组边界测试在此包。环境白名单，Go1.22.2，锁定Python3.12.3，数据盘独立HOME/TMPDIR，禁止继承在线SH_/DINGWEI_配置。

补充既有测试影响：internal/extseat/core_pg_test.go的MessageExpiryAndRevokedWait改用合法发送先续组，避免AG与消息恰好同时过期；保留原消息过期和token撤权所有断言。9576前驱的canonical rc0、旧夹具rc1、修复夹具单格rc0与中止full-go3原日志均保留；正式门在追加的新冻结源重跑。
