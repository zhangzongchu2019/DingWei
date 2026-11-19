# Design

完整设计审阅页：[MCP-DCR](../../../docs/design/MCP-DCR.md)，固定基点4a349ee59a1d8d7b8b9c562c462bded2f8b0f192。本变更只在组长过目后实施；DCR-001～008对应登记、redirect、权限、存储、限流、并存/回退、传输和验证。

## 决策概要

- A-DCR-01：独立PG动态client和计量表，不占短期OAuth池。默认24小时绝对TTL、每resource4096/来源128活跃client；源5/min burst5、全局60/min burst20；IP经可信代理链解析和HMAC，IPv6按/64；参数等待过目。
- A-DCR-02：公共none/code客户端，三种精确HTTP回环及受运营完整URL白名单约束的HTTPS；名单默认空、私有scheme不默认支持；回环仅端口例外，token绑定实际URI。
- A-DCR-03：GET未鉴权401沿用AUTH-FIRST，认证后405；不实现旧SSE、不为未认证JSON诊断先读body。
- DCR-003：每次S256、同浏览器5分钟、固定scope/resource、飞书/tenant/target和seat现核均不变。登记不等于开户，现有OAuth首次完成后自动创建外部Agent。
- DCR-006：精确内置客户端、持久不透明DCR、原CIMD三路分型；关闭仅停新登记与新DCR authorize，原5分钟快照可收尾，既有token不延寿/提权。

## 状态与响应

仅登记记录有效/到期、DCR服务关闭/就绪/不可用状态；不新增业务投递状态。201公共登记，400 invalid_client_metadata/invalid_redirect_uri，413超体，415媒体不符，429限流且Retry-After，503 temporarily_unavailable；关闭404。authorize无效/到期DCR为invalid_client，不网络兜底。规范场景在DCR-001、004～007。

PG迁移编号待组长协调；不在设计稿占号，不改SQLite/history迁移。没有client_secret/注册管理token/refresh授予，不重写CIMD/内置策略，不改Agent TTL、VERSION或部署。
