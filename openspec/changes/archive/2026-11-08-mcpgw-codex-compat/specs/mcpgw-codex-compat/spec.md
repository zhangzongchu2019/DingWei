## ADDED Requirements

### Requirement: MCC-001 双协议通讯编解码
网关 MUST 并存2025-06-18的initialize、notifications/initialized、tools/list、tools/call、ping，与既有2026封套按版本分流，独立严格解码；不全局放宽未知字段。2025通知成功返回202空体，不建立协议授权会话。

#### Scenario: MCC-001-S01 2025与2026同时成功
- **WHEN** 实际Codex0.167.0使用2025初始化/通知/四工具/心跳，既有2026客户端使用合法封套
- **THEN** 两者各按相应结构成功，2026原有负测保持；撤2025入口则initialize目标红

#### Scenario: MCC-001-S02 参数与版本拒绝
- **WHEN** 未知顶层/方法参数、错误版本或不合法通知id到达
- **THEN** 拒绝且无工具副作用，不以宽松JSON解析兜底

### Requirement: MCC-002 每请求最小权限现核
两codec MUST 每次调用共同Authenticate/Seats服务，保持tenant、scope、audience、AG、Origin、PKCE、过期和吊销校验，不以握手缓存授权。

#### Scenario: MCC-002-S01 跨租户与握手后吊销
- **WHEN** 外组织扫码，或初始化后token过期/吊销/丢权限，或非法Origin调用
- **THEN** 无越权签发/执行；放宽tenant时对应组织外登录负测红

### Requirement: MCC-003 IP回环任意端口登记匹配
网关 MUST 对登记127.0.0.1或::1且相同scheme/IP/path/query的回调允许任意有效端口，localhost仍拒，非回环逐字匹配；token仍精确绑定本次实际redirect。

#### Scenario: MCC-003-S01 随机端口登录与拒绝边界
- **WHEN** 注册无端口IP回环文档，实际Codex随机端口回调，或请求localhost/错IP/错path/scheme/query
- **THEN** 仅前者登录成功；退回逐字匹配使登录红，放行localhost使对应负测红

### Requirement: MCC-004 自有静态CIMD与外部取件
网关 MUST 在本域/mcp/clients/codex.json公开生成只读CIMD，client_id绑定配置PublicOrigin，登记两IP回环无端口/callback。仅此精确自有ID本地解析；其他ID仍走原HTTPS/公有IP/无跳转resolver，不新增任意私网例外。

#### Scenario: MCC-004-S01 自有与外部边界
- **WHEN** 读取自有文档/精确ID授权，或提交近似URL/外域/私网CIMD
- **THEN** 自有文档可用且不受请求Host污染，其他ID保持原取件校验，未授权不得读业务

### Requirement: MCC-005 客户端最低版本与路径清单
文档 MUST 要求Codex至少0.167.0，提供显式本域oauth-client-id的add/login主路径说明，禁止配置oauth-resource；记录现网/预发新增静态路由，不部署。关闭态不登记新路径。

#### Scenario: MCC-005-S01 PM接入与关闭态
- **WHEN** PM按文档使用新add/login，或Hub关闭r53
- **THEN** 配置可供0.167.0使用，关闭态没有新文档路径；不承诺0.145 issuer兼容

### Requirement: MCC-006 原生证据与变异
交件 MUST 附真实0.167.0本地add/login/列工具/list_online成功原文、四刀目标红、2026/相关包/canonical/strict结果，禁用真实凭据且不碰节点。

#### Scenario: MCC-006-S01 完整本地交审
- **WHEN** 提交中间/最终坐标
- **THEN** 按该阶段实际完成项附命令/UTC/rc/SHA，原生失败不算成功，未部署且独立复核
