# MCP Gateway Codex Compatibility Baseline

## Purpose

记录现有网关与Codex的协议边界：基线只接受2026-07-28无状态封套、精确回调登记和外部CIMD，Codex0.167.0原生初始化尚不能通过。本文绑定f2a5db5f现状，不把后续兼容实现当作已发布能力，不改变身份与业务授权。

## Requirements

### Requirement: MCB-001 既有封套与身份边界
网关 MUST 独立鉴权2026封套每请求，按现有tenant、scope、audience、AG和Origin处理四通讯工具；基线不接受Codex的2025初始化。

#### Scenario: MCB-001-S01 现有与Codex请求
- **WHEN** 现有2026客户端提供合法头与_meta，或Codex0.167.0发送initialize2025
- **THEN** 前者按权限成功，后者在基线上拒收；不得据OAuth成功声称原生互通

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

### Requirement: ZC-001 先实测默认客户端行为
交件 MUST 用官方 Codex 0.167.0 只配置服务器地址，分别测授权服务器元数据有无 registration_endpoint 的 client_id、redirect_uri、DCR 请求，原文记录结果。桌面不可运行时 MUST 明记未测和原因，不由 CLI 推导桌面。

#### Scenario: ZC-001-S01 两种发现元数据
- **WHEN** 新的隔离配置仅添加 MCP URL，分别发现有／无注册端点的服务器
- **THEN** 记录授权参数、HTTP 请求和二进制 SHA，不使用真实账号、不启动推理

### Requirement: ZC-002 固定原文与有效策略
网关 MUST 对精确默认外部 client_id 使用固定的本地原文副本，附实取时刻及 SHA256，禁止在线获取或坏件后网络兜底；有效策略 MUST 为原文与公开 none、IP 回环回调、authorization_code/code 的交集，不自动接纳 localhost 或 refresh_token。

#### Scenario: ZC-002-S01 正常副本与坏件
- **WHEN** 默认身份请求登录，或测试注入坏件、非回环 HTTPS、非公开身份与缺失支持项
- **THEN** 正常副本无需 resolver 即进入登录；坏件 fail closed，不访问外网

### Requirement: ZC-003 身份和回调精确边界
网关 MUST 保留显式本域 codex.json 及其他外部 CIMD 的既有策略，仅默认身份完全相等时本地解析。回调 MUST 为登记的回环 IP 与 http，端口可变；两个精确本地 Codex 身份按㊿-补20接受 /callback 或 /callback/ 后跟 1～64 位 A-Za-z0-9_- 单段，query 保持登记值；localhost、错 IP、空尾斜杠、编码／多段路径、userinfo、fragment 与无效端口均拒绝。原 URI 保持授权／换票逐字绑定。

#### Scenario: ZC-003-S01 正负边界
- **WHEN** 两条有效本地身份采用合法随机端口，或提交近似身份及错误回调
- **THEN** 仅有效身份和登记回调成功；其他身份仍经原 resolver，错误回调不重定向

### Requirement: ZC-004 不放宽登录和请求鉴权
网关 MUST 保持 PKCE S256、固定 scopes、tenant、audience、token 对 client_id/真实 redirect/resource/verifier 的绑定，以及逐工具请求现核身份；默认身份路径不得获得鉴权例外。

#### Scenario: ZC-004-S01 拒绝越权
- **WHEN** 使用默认身份提交错误授权参数、兑换绑定、组织身份，或握手后身份被撤权
- **THEN** 拒绝签发或调用，且无业务副作用；各守卫单刀变异红在对应断言

### Requirement: ZC-005 原生验证与交审
交件 MUST 附官方 0.167.0 URL-only add 自动登录、四工具和 list_online 的隔离原生 E2E 原文、守卫目标红、m8 全包、mcpgw、canonical、strict 结果；不改版本、不部署、不碰线上席位。

#### Scenario: ZC-005-S01 本地完整证据
- **WHEN** 提交分支坐标供独立复核
- **THEN** 命令、UTC、返回码、SHA 与原文可复跑，模拟飞书与桌面未测限制明确，不自验收

### Requirement: ZC-006 原生回调安全段与专用身份
网关 MUST 仅对默认副本和本域 codex.json 两种精确 client_id 开启补20的回调安全段，不为其他客户端开放路径扩展。实现 MUST 附两身份正负例、换票逐字绑定，以及启用路径、长度上限、字符集、单段四刀目标红；不将 CLI 结果当桌面实测。

#### Scenario: ZC-006-S01 安全段边界与换票
- **WHEN** 两种身份使用 1、12、64 位安全段，或空段、65 位、点、斜杠、编码、另一合法段换票及外部身份提交扩展路径
- **THEN** 仅授权中的合法原 URI 可兑换；其他拒绝，四刀各红在相应判据

### Requirement: ZC-007 免输入授权只出影响方案
交件 MUST 依补19、21说明不经 Agent 执行命令的桌面设置入口及证据等级，提出可信飞书姓名加 UTC 时间的自动短名和默认目标方案，列校验、PG CHECK、地址解析、展示、权限与旧二进制回退影响；未裁定前 MUST 不改表单、名字或目标权限。

#### Scenario: ZC-007-S01 方案与未测边界
- **WHEN** 提交默认授权建议供组长裁定
- **THEN** 说明官方当前文档与本机未测差别，名字来自可信 profile，目标受控且不扩权，长度及默认目标待定，本单不提前实现
