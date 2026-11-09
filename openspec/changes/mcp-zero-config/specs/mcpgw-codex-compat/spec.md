## ADDED Requirements

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
