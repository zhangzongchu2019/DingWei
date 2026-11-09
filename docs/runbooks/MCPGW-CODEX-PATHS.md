# r56 Codex MCP 网关路径清单

依据 rulings ㊿-补10（0877df37）。本件是现网/预发待集成清单，不执行nginx修改、换版或开放来源；P2b GO由组长另发。

## 两环境的九类路径

| 精确路径或既有前缀 | 方法 | Hub登记/作用 | 正式入口 | 预发入口 |
|---|---|---|---|---|
| /.well-known/oauth-protected-resource/mcp | GET | 资源发现 | https://dw123.cmjrt.com | 管理员配置的预发PublicOrigin（当前方案https://staging-dw123.cmjrt.com:8443） |
| /.well-known/oauth-authorization-server | GET | OAuth发现 | 同上 | 同上 |
| /mcp | POST | 2025/2026双codec；无主动GET流，GET返回405 | 同上 | 同上 |
| /api/external/（既有前缀） | POST | 原四REST操作 | 同上 | 同上 |
| /oauth/authorize | GET | PKCE授权 | 同上 | 同上 |
| /oauth/token | POST | 兑换 | 同上 | 同上 |
| /feishu/connect | GET/POST | 组织登录表单 | 同上 | 同上 |
| /mcp/oauth/feishu/callback | GET | 飞书回调 | 同上 | 同上 |
| **/mcp/clients/codex.json（新增）** | **GET** | **本域静态CIMD主路径；不读业务身份** | **同上** | **同上** |

如现网另有已批:50443入口，也须对照同一九类清单；issuer/resource/client_id始终由Hub配置PublicOrigin决定，不由访问Host/转发头决定，不得混用端口构造另一身份。

Hub由Gateway.Register新增精确路由；默认关闭/密钥缺失等关闭态不登记该路由。业务CIMD文档生成client_id=PublicOrigin+/mcp/clients/codex.json，redirect_uris精确为 `http://127.0.0.1/callback` 与 `http://[::1]/callback`，均不写端口；公开客户端none、authorization_code/code，不含密钥。

## nginx待办（两环境分别审）

- 在既有MCP网关路径规则旁新增 **location = /mcp/clients/codex.json**，沿用该server现有Hub上游、TLS与转发头设置，不新增宽泛location /mcp。
- 新路径与既有发现路径一样不挂Basic/OAuth登录页，不302重定向；由Hub返回JSON，非nginx临时手工拼接域名文档。
- 保留各环境已批准的来源策略。预发继续只放行已批来源；正式首批PM的来源策略由组长/业主另定。清单不授权放开公网或取消其余路径的Basic。
- 检查新精确location不会被旧的/mcp前缀、Basic继承或静态站点抢走；现网与预发各自GET结果的client_id须与相应PublicOrigin一致。
- 本次不改变飞书后台回调URI，不上传证书、凭据或配置包。外部CIMD仍可取，chatgpt.com可达性归prodops另测。

自有URL在服务端只按**与配置完全相等**的一个client_id直接解析同份生成文档，避免同机私网DNS/回流阻断；不对任意用户URL做私网例外。外部CIMD的HTTPS443、公有IP、无重定向与回绑防护保持。

## 集成验证与回退

本地原生0.167.0 add/login/列四工具/list_online通过并有四刀目标红后，再独立复核与预发换版。请求逐次鉴权，不能以一次initialize缓存授权。检查2026原封套和REST不变、非法Origin/tenant/AG/PKCE拒绝、localhost拒绝；本域文档主路径开启/关闭态与上述清单一致。

回退兼容版本时移除新增静态location或恢复旧MCP开关/路由集，保留数据库与业务身份数据；无需schema回退。旧网关不保证Codex可用，因此回退须同步通知PM暂停此入口。具体上线/回退命令、制品与时间由发布单提供，本件不执行。

## S-MCP-ZERO-CONFIG 加法说明（待复核、未部署）

基于发布提交 68505cd3，无新增 HTTP 路由，无 DCR endpoint，无数据库或版本变更。仅精确 client_id `https://chatgpt.com/oauth/codex/client.json` 本地读取内嵌原文，避免现网 403；不是任意域名缓存或私网取件例外。原文 SHA、取得时刻、安全交集与测试见 `docs/reviews/mcp-zero-config/RESULT.md`。

既有本域文档路由及显式 client_id 继续使用；其他外部 ID 仍用原 resolver。默认副本保留原文中的 IPv4 `/callback`；按后续㊿-补20，两个精确本地 Codex 身份的匹配层额外接受 `/callback/<1～64 位 A-Za-z0-9_- 单段>`，仍 http、同登记 IP、合法端口、精确 query，localhost／空尾斜杠／多段／编码拒绝。授权和换票 URI 逐字相同。未扩大外部 ID 前缀、未合成 IPv6。桌面版未测，不能依据 CLI 验收目标桌面。

回退该增量恢复显式本域身份接入，不 DROP、不删身份记录。产品关闭态、逐次鉴权及 tenant/audience/PKCE 均不变。此附录不执行部署或调整线上来源策略。
