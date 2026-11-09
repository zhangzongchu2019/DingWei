# 双编解码决策

MCC-001：先有界读回请求、仅按MCP-Protocol-Version=2025-06-18或无头initialize中的2025版本选独立codec，再严格解码相应字段。2026路径原校验/字段/输出保持；不全局关闭DisallowUnknownFields。2025允许该版params._meta、初始化protocolVersion/capabilities/clientInfo、列表cursor与调用name/arguments；四业务工具复用g.call，成功响应按2025结构返回。仅notifications/initialized无id响应202空体，ping有id空result；不新增Mcp-Session-Id/握手授权缓存。

MCC-002：initialize、通知、ping和所有工具均逐HTTP请求Authenticate，继续由相同Seats服务现核tenant/scope/audience/AG及过期/吊销；Origin在公用guard先检查；OAuth/PKCE/token绑定不动。解析/版本错误仍可在认证前拒绝，无授权副作用。

MCC-003：registered只对相同127.0.0.1或::1忽略端口，scheme/IP字面/escaped path/query/其他URI部分仍精确；拒localhost、userinfo/fragment与错路径，不做IP别名归一化。token兑换仍逐字绑定本次实际redirect，非loopback逐字不变。

MCC-004：/mcp/clients/codex.json为配置PublicOrigin生成的公开只读JSON，登记http://127.0.0.1/callback与http://[::1]/callback（无端口）；仅精确同配置URL可直接解析生成值，避免自取私网/回流，同时不增加任意私网例外。该自有文档可用于高端口本地/预发，外部resolver仍只取HTTPS443/公网无跳转。配置来源不从Host/转发头取。

MCC-005：PM最低0.167.0，新add保存issuer绑定的/callback，备用静态ID可选；不配oauth-resource，不兼容0.145。Hub开关关闭时新路径也不登记；现网与预发nginx九类路径清单记录新增文档，不执行配置或开放来源。

MCC-006：真实native add/login/list/call、原始HTTP/RPC和四刀目标红分开计。只读控制或假上游不当真实飞书通过；任何原生失败不记E2E绿。开发夹具失败与有效变异分开保留。没有新增错误码或业务状态。

中间件后补齐MCC-003的HTTPS localhost大小写拒绝（旧HTTPS通用分支漏角）；外部非localhost HTTPS登记仍逐字匹配。所有原生正向/四刀按最终字节重跑，不以中间旧原文替代。
