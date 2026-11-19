# Why

外部IDE/CLI的标准OAuth流程在受保护MCP资源返回401后，需要根据发现信息取得可用client_id。当前平台只有CIMD与固定客户端，缺少动态登记入口，导致依赖DCR的CodeBuddy等客户端无法发起可用授权请求。AUTH-FIRST已经修正协议校验抢在鉴权前的问题，本片在不放宽飞书身份、PKCE、浏览器绑定与通信权限的前提下补受限公共客户端登记，避免以手工签发token替代正确授权流程。

需求来源：[组长08:04:13Z设计输入](dingwei://delivery/dw-dlv_84a2f9a5bb9df7eb6ad435f4ce82d6ea)，以及2026-11-19T08:02:39Z S-MCP-DCR派单；不从Git作者推导授权。

# What Changes

1. 就绪时发现registration_endpoint，增加公共RFC7591登记。
2. HTTP精确回环/白名单HTTPS回调和none/code/S256边界。
3. 独立持久client/配额/GC，不挤占OAuth事务池。
4. 飞书、browser、5分钟、scope/resource等授权合同不变。
5. DCR与CIMD/固定客户端并存，明确关闭与回退。
6. GET/旧SSE取舍、客户端排障和真实HTTP全链/变异验收。

# Impact

拟改internal/mcpgw、cmd/workpulse装配与独立PG加法迁移，相关测试/文档；不改helper/renderer/Agent权限，不改VERSION，不部署。先设计审阅，再实施。

## 待确认假设

- A-DCR-01：组长08:37:52Z已定两段TTL：未绑定24h、成功兑换绑定30d滑动（7～90d）、每主体16绑定；其余额度及可信取源已批准。
- A-DCR-02：HTTPS完整URL名单初始空、私有scheme先拒；实际CodeBuddy redirect未知，不能保证本片覆盖其实际回调。
- A-DCR-03：沿用AUTH-FIRST未认证非法体401，不采纳补件中可能被理解为先400的表述；认证GET仍405，不补旧SSE。

## 待PM决策

- PM-DCR-01：组长08:37:52Z有条件通过，四条已修订；实际IDE回调仍待实测，不扩大通配或私有scheme。
- PM-DCR-02：组长暂定PG0115，动工及交件复核撞号。发布路线仍由组长另定。
