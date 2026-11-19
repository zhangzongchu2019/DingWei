# mcp-dynamic-client-registration 上下文

发布提交 8213f12937aba18080a384bcabd2ae483d246098（r62，VERSION 0.2.3.42），2026-11-19 现网上线（SWITCH_PASS 19:42:36Z，结项件 review/prodops/r62-prod-closeout-20261119 6bbca0d8d2a951015b88ceed6eff12b8de567d9c）后按方案甲归档建立本主规格。

## 来自 2026-11-19-mcp-dcr（specs/mcp-dynamic-client-registration/context.md）

# 现状与坐标

基点4a349ee59a1d8d7b8b9c562c462bded2f8b0f192（r61版本0.2.3.41）。本能力首次变更，现有AS无registration_endpoint，只有CIMD与两个固定Codex身份；POST /oauth/register不存在。已有AUTH-FIRST使/mcp未鉴权请求先401，认证后只支持单端点POST，GET405。

mcpgw/oauth.go负责metadata/authorize/connect/callback/token；clients.go负责外部CIMD/redirect；store.go和PG0107负责加密短期事务；cmd/workpulse/mcpgw.go与r53_extseat.go装配；extwire/issuer.go→extseat.Service.IssueToken在可信飞书回调后自动建立外部Agent，不依赖Console先创建托管Agent。当前scope/resource、5分钟浏览器绑定和S256均已实现。

设计页docs/design/MCP-DCR.md。新PG迁移未占号，无产品实现；不读现网/别席历史或凭据。普通用户Go1.22.2、锁定Python3.12.3、OpenSpec1.16.0、白名单/数据盘HOME与TMP；只在授权后的实现阶段跑真实隔离PG/HTTP。变更SOURCE/证据坐标在tasks交接中追加。

08:37:52Z组长有条件通过后已同步两段TTL、主体16上限、回调诊断与开户去重边界。实现落点新增mcpgw/dcr{,_policy,_store}.go、cmd/workpulse/mcpgw_dcr.go及PG0115；生命周期用现有lifecycle.Group，不新增依赖；实际IDE接入尚未实测，关闭/回退及实现门以最终REPORT锚为准。
